package main

import (
	"bytes"
	"html/template"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/extensions/sdk"
)

func init() { registerShare(serveShare) }

// serveShare serves share links to anyone holding one, with no account: the
// page at /<token>, and the audio and covers it embeds (/<token>/stream/<id>,
// /<token>/cover/<album>). The host hands the path relative to the /share
// mount. Each request re-reads the share, so a deleted or expired link stops
// working at once, and a visitor reaches only the tracks the share expands to.
func serveShare(r sdk.Request) sdk.Response {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return shareText(http.StatusMethodNotAllowed, "method not allowed")
	}
	parts := strings.Split(strings.TrimPrefix(r.Path, "/"), "/")
	switch {
	case len(parts) == 1:
		return sharePage(r, parts[0])
	case len(parts) == 3 && parts[1] == "stream":
		return shareStream(r, parts[0], parts[2])
	case len(parts) == 3 && parts[1] == "cover":
		return shareCover(r, parts[0], parts[2])
	}
	return shareNotFound()
}

// shareText answers a plain error; every share answer, a 404 included, keeps
// crawlers away.
func shareText(status int, msg string) sdk.Response {
	resp := textResponse(status, msg)
	resp.Headers["X-Robots-Tag"] = []string{"noindex, nofollow"}
	return resp
}

func shareNotFound() sdk.Response {
	return shareText(http.StatusNotFound, "404 page not found")
}

func shareInternal(what string, err error) sdk.Response {
	sdk.Log("error", what, "error", err.Error())
	return shareText(http.StatusInternalServerError, "internal error")
}

// openedShare is a live share, its owner and the track ids it holds.
type openedShare struct {
	share
	owner    *account
	trackIDs []uint32
}

// openShare loads a live share. A missing or expired share is a plain 404, the
// same for both, so a link reveals nothing. A share lives only while its owner
// is not locked and may still share: an admin revoking the permission ends every link the
// account handed out, not just new ones. The ids are cheap to read; each route
// loads only the tracks it serves.
func openShare(token string) (*openedShare, *sdk.Response) {
	notFound := shareNotFound()
	if !shareTokenShape.MatchString(token) {
		return nil, &notFound
	}
	r, found, err := queryRow(
		"SELECT "+shareColumns+" FROM shares WHERE token = ?", token,
	)
	if err != nil {
		resp := shareInternal("loading a share failed", err)
		return nil, &resp
	}
	if !found {
		return nil, &notFound
	}
	s := scanShare(r)
	if s.expired(time.Now()) {
		return nil, &notFound
	}
	owner, found, err := loadAccount(s.OwnerID)
	if err != nil {
		resp := shareInternal("loading a share's owner failed", err)
		return nil, &resp
	}
	if !found || owner.Locked || !owner.CanShare {
		return nil, &notFound
	}
	if err := loadShareItems([]*share{s}); err != nil {
		resp := shareInternal("loading a share's items failed", err)
		return nil, &resp
	}
	trackIDs, err := shareTrackIDs(s.Items, s.OwnerID)
	if err != nil {
		resp := shareInternal("expanding a share failed", err)
		return nil, &resp
	}
	return &openedShare{share: *s, owner: owner, trackIDs: trackIDs}, nil
}

func sharePathID(raw string) (uint32, bool) {
	n, err := strconv.ParseUint(raw, 10, 32)
	return uint32(n), err == nil
}

// shareMinKbps and shareMaxKbps bound a capped transcode.
const (
	shareMinKbps = 32
	shareMaxKbps = 320
)

func shareStream(req sdk.Request, token, rawID string) sdk.Response {
	s, failed := openShare(token)
	if failed != nil {
		return *failed
	}
	id, ok := sharePathID(rawID)
	// The link plays with its owner's rights: an account that may not stream
	// must not reach the files through a share either.
	if !ok || !s.owner.CanStream || !slices.Contains(s.trackIDs, id) {
		return shareNotFound()
	}
	ts, err := shareSongs([]uint32{id})
	if err != nil {
		return shareInternal("loading a shared track failed", err)
	}
	if len(ts) == 0 {
		return shareNotFound()
	}
	song := ts[0].song
	r, found, err := queryRow(
		`SELECT path FROM streamline_music_files
		WHERE track_id = ? AND missing = 0 ORDER BY id LIMIT 1`, id,
	)
	if err != nil {
		return shareInternal("loading a shared file failed", err)
	}
	if !found {
		return shareNotFound()
	}
	hostPath := r.str(0)
	// A file outside the granted folder cannot be sent.
	if _, mapped := req.GuestPath(hostPath); !mapped {
		return shareNotFound()
	}
	original := &sdk.SendFile{Path: hostPath, ContentType: song.ContentType}
	resp := sdk.Response{
		Status:  http.StatusOK,
		Headers: map[string][]string{"X-Robots-Tag": {"noindex, nofollow"}},
	}
	// The owner's bitrate cap holds here as on their own stream; without
	// ffmpeg the host serves the original.
	limit := int(s.owner.MaxBitRate)
	if limit > 0 && (song.BitRate == 0 || int(song.BitRate) > limit) {
		resp.StreamFFmpeg = &sdk.StreamFFmpeg{
			Args: shareMP3Args(
				hostPath,
				min(max(limit, shareMinKbps), shareMaxKbps),
			),
			ContentType: "audio/mpeg",
			Headers:     resp.Headers,
			Fallback:    original,
		}
		return resp
	}
	resp.SendFile = original
	return resp
}

// shareMP3Args is the ffmpeg command line that converts src to an MP3 stream
// on stdout: first audio stream only, no tags, no video.
func shareMP3Args(src string, kbps int) []string {
	return []string{
		"-hide_banner", "-nostdin", "-loglevel", "error",
		"-i", src,
		"-map", "0:a:0",
		"-vn", "-sn", "-dn",
		"-map_metadata", "-1",
		"-c:a", "libmp3lame",
		"-b:a", strconv.Itoa(kbps) + "k",
		"-f", "mp3",
		"pipe:1",
	}
}

func shareCover(req sdk.Request, token, rawID string) sdk.Response {
	s, failed := openShare(token)
	if failed != nil {
		return *failed
	}
	id, ok := sharePathID(rawID)
	if !ok {
		return shareNotFound()
	}
	found, err := shareHasAlbum(s.trackIDs, id)
	if err != nil {
		return shareInternal("checking a shared cover failed", err)
	}
	artwork, mapped := req.Folder("artwork")
	if !found || !mapped {
		return shareNotFound()
	}
	rel := path.Join("albums", strconv.FormatUint(uint64(id), 10), "poster.jpg")
	contentType, ok := posterContentType(path.Join(artwork.GuestPath, rel))
	if !ok {
		return shareNotFound()
	}
	return sdk.Response{
		Status:  http.StatusOK,
		Headers: map[string][]string{"X-Robots-Tag": {"noindex, nofollow"}},
		SendFile: &sdk.SendFile{
			Path:        path.Join(artwork.HostPath, rel),
			ContentType: contentType,
		},
	}
}

// shareHasAlbum is whether a playable track among ids is on the album.
func shareHasAlbum(trackIDs []uint32, albumID uint32) (bool, error) {
	uniq := slices.Clone(trackIDs)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	rows, err := queryIn(
		`SELECT 1 FROM streamline_tracks t
		WHERE t.album_id = ? AND t.id IN ({in})
		AND EXISTS (
			SELECT 1 FROM streamline_music_files f
			WHERE f.track_id = t.id AND f.missing = 0
		) LIMIT 1`,
		uniq, albumID,
	)
	return len(rows) > 0, err
}

type sharePageTrack struct {
	ID       uint32
	Title    string
	Artist   string
	Album    string
	Duration string
}

type sharePageData struct {
	Title       string
	Description string
	Owner       string
	Expires     string
	CoverAlbum  uint32
	Token       string
	Tracks      []sharePageTrack
}

func shareClock(sec uint32) string {
	return strconv.Itoa(int(sec/60)) + ":" + shareTwoDigits(int(sec%60))
}

func shareTwoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// shareTitle names the page after what was shared: the single album when the
// share is one, else the description.
func shareTitle(description string, ts []shareTrack) string {
	if description != "" {
		return description
	}
	if len(ts) == 0 {
		return "Shared music"
	}
	first := ts[0]
	for _, t := range ts {
		if t.albumID != first.albumID {
			return "Shared music"
		}
	}
	return first.albumTitle + " — " + first.artist
}

func sharePage(req sdk.Request, token string) sdk.Response {
	s, failed := openShare(token)
	if failed != nil {
		return *failed
	}
	ts, err := shareSongs(s.trackIDs)
	if err != nil {
		return shareInternal("expanding a share failed", err)
	}
	if req.Method == http.MethodGet {
		if _, err := exec(
			`UPDATE shares SET visit_count = visit_count + 1, last_visited_at = ?
			WHERE id = ?`,
			nowStored(), s.ID,
		); err != nil {
			sdk.Log("warn", "counting a share visit failed", "error", err.Error())
		}
	}
	data := sharePageData{
		Title:       shareTitle(s.Description, ts),
		Description: s.Description,
		Owner:       s.owner.DisplayName,
		Token:       s.Token,
		Tracks:      make([]sharePageTrack, 0, len(ts)),
	}
	if !s.ExpiresAt.IsZero() {
		data.Expires = s.ExpiresAt.UTC().Format("2 January 2006")
	}
	for _, t := range ts {
		data.Tracks = append(data.Tracks, sharePageTrack{
			ID:       t.trackID,
			Title:    t.song.Title,
			Artist:   t.song.Artist,
			Album:    t.song.Album,
			Duration: shareClock(t.song.Duration),
		})
	}
	if len(ts) > 0 {
		data.CoverAlbum = ts[0].albumID
	}
	var buf bytes.Buffer
	if err := sharePageTmpl.Execute(&buf, data); err != nil {
		return shareInternal("rendering a share page failed", err)
	}
	return sdk.Response{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type":    {"text/html; charset=utf-8"},
			"Cache-Control":   {"no-store"},
			"Referrer-Policy": {"no-referrer"},
			"X-Robots-Tag":    {"noindex, nofollow"},
		},
		Body: buf.Bytes(),
	}
}

// The page carries no script: the host's CSP allows none, and native audio
// controls are all a listener needs.
var sharePageTmpl = template.Must(template.New("share").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>{{.Title}}</title>
<style>
:root { color-scheme: light dark; --bg: #f6f6f4; --fg: #1c1c1a; --muted: #6b6b66; --card: #ffffff; --line: #e3e3de; }
@media (prefers-color-scheme: dark) { :root { --bg: #141413; --fg: #ecece8; --muted: #9b9b95; --card: #1f1f1d; --line: #2e2e2b; } }
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 15px/1.45 system-ui, -apple-system, "Segoe UI", sans-serif; }
main { max-width: 720px; margin: 0 auto; padding: 32px 16px 48px; }
header { display: flex; gap: 20px; align-items: center; margin-bottom: 28px; }
header img { width: 132px; height: 132px; border-radius: 8px; object-fit: cover; background: var(--line); flex: none; }
h1 { font-size: 1.5rem; margin: 0 0 6px; overflow-wrap: anywhere; }
.meta { color: var(--muted); font-size: .9rem; }
ol { list-style: none; margin: 0; padding: 0; }
li { background: var(--card); border: 1px solid var(--line); border-radius: 8px; padding: 12px 14px; margin-bottom: 10px; }
.row { display: flex; justify-content: space-between; gap: 12px; }
.title { font-weight: 600; overflow-wrap: anywhere; }
.sub { color: var(--muted); font-size: .85rem; }
.dur { color: var(--muted); font-variant-numeric: tabular-nums; font-size: .85rem; }
audio { width: 100%; margin-top: 8px; height: 36px; }
footer { margin-top: 32px; color: var(--muted); font-size: .8rem; text-align: center; }
@media (max-width: 480px) { header { flex-direction: column; align-items: flex-start; } }
</style>
</head>
<body>
<main>
<header>
{{if .CoverAlbum}}<img src="/share/{{.Token}}/cover/{{.CoverAlbum}}" alt="">{{end}}
<div>
<h1>{{.Title}}</h1>
<div class="meta">{{len .Tracks}} track{{if ne (len .Tracks) 1}}s{{end}}{{if .Owner}} · shared by {{.Owner}}{{end}}{{if .Expires}} · until {{.Expires}}{{end}}</div>
</div>
</header>
{{if .Tracks}}<ol>
{{range .Tracks}}<li>
<div class="row"><div><div class="title">{{.Title}}</div><div class="sub">{{.Artist}} — {{.Album}}</div></div><div class="dur">{{.Duration}}</div></div>
<audio controls preload="none" src="/share/{{$.Token}}/stream/{{.ID}}"></audio>
</li>
{{end}}</ol>{{else}}<p class="meta">Nothing in this share can be played right now.</p>{{end}}
<footer>Shared from Streamline</footer>
</main>
</body>
</html>
`))
