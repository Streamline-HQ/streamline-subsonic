package main

import (
	"io"
	"os"
	"path"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
	"github.com/streamline-hq/streamline-subsonic/lrc"
)

func init() {
	register("getLyrics", lyricsByName)
	register("getLyricsBySongId", lyricsBySongID)
}

// creditedArtist is the artist a song is named by: its own credit, else the
// album owner.
func creditedArtist(credit, owner string) string {
	if credit == "" {
		return owner
	}
	return credit
}

// lyricsMaxSongs bounds how many songs of one name getLyrics reads files for,
// and lyricsMaxCandidates how many of that name it weighs the artist of.
const (
	lyricsMaxSongs      = 100
	lyricsMaxCandidates = 1000
)

// lyricsSource is one version of a song's lyrics and the language it is in,
// an ISO 639 code or lrc.UnknownLang.
type lyricsSource struct {
	lang   string
	lyrics lrc.Lyrics
}

// lyricsOf reads a song's lyrics: a sidecar .lrc of the same name first, then
// every lyrics tag embedded in the file. Any of them may be LRC-timed, and a
// version met twice is listed once. With all false it stops at the first one
// found, which is what getLyrics answers.
func lyricsOf(c *reqCtx, hostPath string, all bool) []lyricsSource {
	var out []lyricsSource
	add := func(text, lang string) {
		p := lrc.Parse(text)
		if len(p.Lines) == 0 {
			return
		}
		for _, have := range out {
			if have.lyrics.Synced == p.Synced && have.lyrics.Plain() == p.Plain() {
				return
			}
		}
		// An LRC [la:] tag in the text names it best, the tag's key next.
		if p.Lang != "" {
			lang = p.Lang
		}
		out = append(out, lyricsSource{lang: lrc.NormalizeLang(lang), lyrics: p})
	}
	if text, ok := lyricsSidecar(c, hostPath); ok {
		add(text, "")
		if len(out) > 0 && !all {
			return out
		}
	}
	res, err := sdk.FFmpeg("ffprobe", lrc.ProbeArgs(hostPath)...)
	if err != nil || res.Error != "" || res.ExitCode != 0 {
		return out
	}
	for _, tag := range lrc.TagsFromProbe(res.Stdout) {
		add(tag.Text, tag.Lang)
		if len(out) > 0 && !all {
			break
		}
	}
	return out
}

// lyricsSidecar reads the .lrc next to the song at hostPath: the same name
// with the extension .lrc in any case (Song.lrc, Song.LRC). The folder is
// listed only when the exact name is not there.
func lyricsSidecar(c *reqCtx, hostPath string) (string, bool) {
	song, ok := c.req.GuestPath(hostPath)
	if !ok {
		return "", false
	}
	dir := path.Dir(song)
	want := strings.TrimSuffix(path.Base(song), path.Ext(song)) + ".lrc"
	if text, ok := lyricsRead(path.Join(dir, want)); ok {
		return text, true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.Name() != want && strings.EqualFold(e.Name(), want) &&
			e.Type().IsRegular() {
			return lyricsRead(path.Join(dir, e.Name()))
		}
	}
	return "", false
}

func lyricsRead(guest string) (string, bool) {
	f, err := os.Open(guest) //nolint:gosec // a mapped library path
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, lrc.MaxFile))
	if err != nil || len(buf) == 0 {
		return "", false
	}
	return string(buf), true
}

// lyricsFile is the path of a track's first file on disk, if it has one.
func lyricsFile(trackID uint32) (string, bool, error) {
	r, found, err := queryRow(
		`SELECT path FROM streamline_music_files
		WHERE track_id = ? AND missing = 0 ORDER BY id LIMIT 1`,
		trackID,
	)
	if err != nil || !found {
		return "", false, err
	}
	return r.str(0), true, nil
}

// lyricsByName finds a song by artist and title and answers its lyrics as
// plain text; an empty element when there are none. The artist is the one
// the song is credited to or its album's, matched without regard to case in
// any script, which SQLite's lower() only does for ASCII.
func lyricsByName(c *reqCtx) sdk.Response {
	artistName := strings.TrimSpace(c.param("artist"))
	title := strings.TrimSpace(c.param("title"))
	out := &lyrics{}
	if title == "" {
		return c.ok(&body{Lyrics: out})
	}
	rows, err := query(`SELECT t.id, t.title, ar.name, t.artist_credit
		FROM streamline_tracks t
		JOIN streamline_albums al ON al.id = t.album_id
		JOIN streamline_artists ar ON ar.id = al.artist_id
		WHERE lower(t.title) = lower(?)
		AND EXISTS (SELECT 1 FROM streamline_music_files f
			WHERE f.track_id = t.id AND f.missing = 0)
		ORDER BY t.id LIMIT ?`, title, lyricsMaxCandidates)
	if err != nil {
		return c.failErr(err)
	}
	read := 0
	for _, r := range rows {
		owner, credit := r.str(2), r.str(3)
		if artistName != "" && !strings.EqualFold(artistName, owner) &&
			!strings.EqualFold(artistName, credit) {
			continue
		}
		if read == lyricsMaxSongs {
			break
		}
		read++
		hostPath, ok, err := lyricsFile(r.uint(0))
		if err != nil {
			return c.failErr(err)
		}
		if !ok {
			continue
		}
		if found := lyricsOf(c, hostPath, false); len(found) > 0 {
			out = &lyrics{
				Artist: creditedArtist(credit, owner),
				Title:  r.str(1),
				Value:  found[0].lyrics.Plain(),
			}
			break
		}
	}
	return c.ok(&body{Lyrics: out})
}

// lyricsBySongID is the OpenSubsonic songLyrics endpoint: every version of
// the song's lyrics it has, as lines, timed when the source is LRC.
func lyricsBySongID(c *reqCtx) sdk.Response {
	id, err := c.idParam("id", ids.KindTrack)
	if err != nil {
		return c.failErr(err)
	}
	r, found, err := queryRow(
		`SELECT t.title, ar.name, t.artist_credit
		FROM streamline_tracks t
		JOIN streamline_albums al ON al.id = t.album_id
		JOIN streamline_artists ar ON ar.id = al.artist_id
		WHERE t.id = ?`,
		id,
	)
	if err != nil {
		return c.failErr(err)
	}
	if !found {
		return c.failErr(envelope.NotFound())
	}
	out := &lyricsList{StructuredLyrics: []structuredLyrics{}}
	hostPath, ok, err := lyricsFile(id)
	if err != nil {
		return c.failErr(err)
	}
	if !ok {
		return c.ok(&body{LyricsList: out})
	}
	for _, src := range lyricsOf(c, hostPath, true) {
		lines := make([]lyricLine, 0, len(src.lyrics.Lines))
		for _, l := range src.lyrics.Lines {
			lines = append(lines, lyricLine{Start: l.Start, Value: l.Value})
		}
		out.StructuredLyrics = append(out.StructuredLyrics, structuredLyrics{
			DisplayArtist: creditedArtist(r.str(2), r.str(1)),
			DisplayTitle:  r.str(0),
			Lang:          src.lang,
			Synced:        src.lyrics.Synced,
			Line:          lines,
		})
	}
	return c.ok(&body{LyricsList: out})
}
