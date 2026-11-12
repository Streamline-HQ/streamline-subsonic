package main

import (
	"crypto/rand"
	"encoding/base64"
	"math"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
)

func init() {
	register("getShares", getShares)
	register("createShare", createShare)
	register("updateShare", updateShare)
	register("deleteShare", deleteShare)
}

// shareTokenBytes is the entropy of a share link, the only thing standing
// between the library and whoever holds the URL.
const shareTokenBytes = 16

func newShareToken() (string, error) {
	b := make([]byte, shareTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// share is a share row with its items, in order.
type share struct {
	ID          uint32
	Token       string
	OwnerID     uint32
	Description string
	ExpiresAt   time.Time
	LastVisited time.Time
	VisitCount  uint32
	Created     time.Time
	Items       []string
}

func (s *share) expired(now time.Time) bool {
	return !s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt)
}

const shareColumns = `id, token, owner_id, description, expires_at,
	last_visited_at, visit_count, created_at`

func scanShare(r row) *share {
	return &share{
		ID:          r.uint(0),
		Token:       r.str(1),
		OwnerID:     r.uint(2),
		Description: r.str(3),
		ExpiresAt:   r.time(4),
		LastVisited: r.time(5),
		VisitCount:  r.uint(6),
		Created:     r.time(7),
	}
}

// loadShareItems fills Items of each share in one read.
func loadShareItems(ss []*share) error {
	byID := make(map[uint32]*share, len(ss))
	keys := make([]uint32, 0, len(ss))
	for _, s := range ss {
		byID[s.ID] = s
		keys = append(keys, s.ID)
	}
	rows, err := queryIn(
		`SELECT share_id, item FROM share_entries
		WHERE share_id IN ({in}) ORDER BY share_id, position`,
		keys,
	)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if s, ok := byID[r.uint(0)]; ok {
			s.Items = append(s.Items, r.str(1))
		}
	}
	return nil
}

// sharePlaylist reports a playlist's owner and whether it is public.
func sharePlaylist(id uint32) (ownerID uint32, public, found bool, err error) {
	r, found, err := queryRow(
		"SELECT owner_id, public FROM playlists WHERE id = ?", id,
	)
	if err != nil || !found {
		return 0, false, false, err
	}
	return r.uint(0), r.bool(1), true, nil
}

// shareExists is whether a core row of the view exists.
func shareExists(view string, id uint32) (bool, error) {
	_, found, err := queryRow("SELECT 1 FROM streamline_"+view+" WHERE id = ?", id)
	return found, err
}

// shareItems reads createShare's ids: songs, albums, artists, or playlists
// the caller can see.
func shareItems(c *reqCtx) ([]string, error) {
	raw := c.multi("id")
	if len(raw) == 0 {
		return nil, envelope.MissingParam("id")
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		kind, id, err := ids.Parse(s)
		if err != nil {
			return nil, envelope.NotFound()
		}
		var found bool
		switch kind {
		case ids.KindTrack:
			found, err = shareExists("tracks", id)
		case ids.KindAlbum:
			found, err = shareExists("albums", id)
		case ids.KindArtist:
			found, err = shareExists("artists", id)
		case ids.KindPlaylist:
			found, err = sharePlaylistVisible(c, id)
		default:
			return nil, envelope.NotFound()
		}
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, envelope.NotFound()
		}
		out = append(out, ids.Format(kind, id))
	}
	return out, nil
}

// sharePlaylistVisible checks a playlist can be published by the caller.
// Someone else's private playlist does not exist for a member; an admin can
// read it, but may still publish only their own or a public one.
func sharePlaylistVisible(c *reqCtx, id uint32) (bool, error) {
	owner, public, found, err := sharePlaylist(id)
	if err != nil || !found {
		return false, err
	}
	if public || owner == c.user.ID {
		return true, nil
	}
	if c.user.isAdmin() {
		return false, envelope.NotAuthorized()
	}
	return false, nil
}

// shareTrackIDs resolves a share's items to the track ids it holds now, in
// item order: an album by disc and position, an artist album by album. It
// reads ids only; shareSongs loads what is playable among them.
func shareTrackIDs(items []string, ownerID uint32) ([]uint32, error) {
	var out []uint32
	for _, item := range items {
		kind, id, err := ids.Parse(item)
		if err != nil {
			continue
		}
		var got []row
		switch kind {
		case ids.KindTrack:
			out = append(out, id)
			continue
		case ids.KindAlbum:
			got, err = query(
				`SELECT id FROM streamline_tracks WHERE album_id = ?
				ORDER BY disc, position, id`, id,
			)
		case ids.KindArtist:
			got, err = query(
				`SELECT t.id FROM streamline_tracks t
				JOIN streamline_albums a ON a.id = t.album_id
				WHERE t.album_id IN (
					SELECT album_id FROM streamline_album_artists WHERE artist_id = ?
				)
				ORDER BY a.release_date, a.id, t.disc, t.position, t.id`, id,
			)
		case ids.KindPlaylist:
			got, err = sharePlaylistTracks(id, ownerID)
		}
		if err != nil {
			return nil, err
		}
		for _, r := range got {
			out = append(out, r.uint(0))
		}
	}
	return out, nil
}

// sharePlaylistTracks is a playlist's track ids. A playlist made private
// after it was shared leaves the share, and so does one that is gone.
func sharePlaylistTracks(id, ownerID uint32) ([]row, error) {
	owner, public, found, err := sharePlaylist(id)
	if err != nil || !found || (!public && owner != ownerID) {
		return nil, err
	}
	return query(
		"SELECT track_id FROM playlist_tracks WHERE playlist_id = ? ORDER BY position",
		id,
	)
}

// shareTrack is a playable track as a share shows it: the song entry and the
// album facts the page needs.
type shareTrack struct {
	song       child
	trackID    uint32
	albumID    uint32
	albumTitle string
	artist     string
}

var shareContentTypes = map[string]string{
	"flac": "audio/flac",
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/opus",
	"aac":  "audio/aac",
}

func shareSuffix(filePath, format string) string {
	if s := strings.TrimPrefix(strings.ToLower(path.Ext(filePath)), "."); s != "" {
		return s
	}
	if format == "other" {
		return ""
	}
	return format
}

func shareContentType(suffix string) string {
	if ct, ok := shareContentTypes[suffix]; ok {
		return ct
	}
	return "application/octet-stream"
}

// shareKbps is the file's probed bitrate, else one estimated from its size
// over the duration; 0 when neither is known.
func shareKbps(bitrate, size int64, duration uint32) uint32 {
	if kbps := bitrate / 1000; kbps > 0 || duration == 0 || size <= 0 {
		return uint32(min(max(kbps, 0), math.MaxUint32))
	}
	return uint32(min(max(size*8/int64(duration)/1000, 0), math.MaxUint32))
}

// shareSongs loads the playable tracks among ids, in the order of ids: those
// with a file on disk, the lowest-id file being the one played.
func shareSongs(want []uint32) ([]shareTrack, error) {
	if len(want) == 0 {
		return nil, nil
	}
	uniq := slices.Clone(want)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	rows, err := queryIn(
		`SELECT t.id, t.mbid, t.album_id, t.title, t.disc, t.position,
			t.duration, t.artist_credit,
			al.title, al.release_date, ar.id, ar.name, ar.genre,
			f.path, f.format, f.size, f.duration, f.bitrate, f.added_at
		FROM streamline_tracks t
		JOIN streamline_albums al ON al.id = t.album_id
		JOIN streamline_artists ar ON ar.id = al.artist_id
		JOIN streamline_music_files f ON f.id = (
			SELECT min(id) FROM streamline_music_files
			WHERE track_id = t.id AND missing = 0
		)
		WHERE t.id IN ({in})`,
		uniq,
	)
	if err != nil {
		return nil, err
	}
	albumIDs := make([]uint32, 0, len(rows))
	for _, r := range rows {
		albumIDs = append(albumIDs, r.uint(2))
	}
	co, err := shareCoArtists(albumIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint32]shareTrack, len(rows))
	for _, r := range rows {
		byID[r.uint(0)] = shareTrackOf(r, co)
	}
	out := make([]shareTrack, 0, len(want))
	for _, id := range want {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// shareCoArtist is an artist an album is listed under without owning it.
type shareCoArtist struct {
	id   uint32
	name string
}

func shareCoArtists(albumIDs []uint32) (map[uint32][]shareCoArtist, error) {
	rows, err := queryIn(
		`SELECT aa.album_id, ar.id, ar.name
		FROM streamline_album_artists aa
		JOIN streamline_artists ar ON ar.id = aa.artist_id
		WHERE aa.owner = 0 AND aa.album_id IN ({in})`,
		albumIDs,
	)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32][]shareCoArtist)
	for _, r := range rows {
		out[r.uint(0)] = append(
			out[r.uint(0)],
			shareCoArtist{id: r.uint(1), name: r.str(2)},
		)
	}
	return out, nil
}

func shareTrackOf(r row, co map[uint32][]shareCoArtist) shareTrack {
	var (
		trackID    = r.uint(0)
		albumID    = r.uint(2)
		credit     = r.str(7)
		albumTitle = r.str(8)
		ownerName  = r.str(11)
		filePath   = r.str(13)
		suffix     = shareSuffix(filePath, r.str(14))
		duration   = r.uint(16)
	)
	if duration == 0 {
		duration = r.uint(6)
	}
	song := child{
		ID:            ids.Track(trackID),
		Parent:        ids.Album(albumID),
		Title:         r.str(3),
		Album:         albumTitle,
		Artist:        ownerName,
		Track:         uint16(min(max(r.int(5), 0), math.MaxUint16)),
		DiscNumber:    uint8(min(max(r.int(4), 0), math.MaxUint8)),
		Year:          uint16(min(max(r.time(9).Year(), 0), math.MaxUint16)),
		Genre:         r.str(12),
		CoverArt:      ids.Album(albumID),
		Size:          r.int(15),
		ContentType:   shareContentType(suffix),
		Suffix:        suffix,
		Duration:      duration,
		BitRate:       shareKbps(r.int(17), r.int(15), duration),
		Created:       optTimestamp(r.time(18)),
		AlbumID:       ids.Album(albumID),
		ArtistID:      ids.Artist(r.uint(10)),
		Type:          "music",
		MediaType:     "song",
		MusicBrainzID: r.str(1),
	}
	// A song of a split release is named by its own credited artist, with
	// that artist's id when the album is listed under them.
	if credit != "" && !strings.EqualFold(credit, ownerName) {
		song.Artist, song.ArtistID = credit, ""
		for _, a := range co[albumID] {
			if strings.EqualFold(a.name, credit) {
				song.ArtistID = ids.Artist(a.id)
			}
		}
	}
	return shareTrack{
		song: song, trackID: trackID, albumID: albumID,
		albumTitle: albumTitle, artist: ownerName,
	}
}

// toShareInfo describes s for its owner, whose tracks have been expanded.
func toShareInfo(c *reqCtx, s *share, ts []shareTrack) shareInfo {
	entries := make([]child, 0, len(ts))
	for _, t := range ts {
		entries = append(entries, t.song)
	}
	return shareInfo{
		ID:          ids.Share(s.ID),
		URL:         c.req.Origin + "/share/" + s.Token,
		Description: s.Description,
		Username:    c.user.Email,
		Created:     optTimestamp(s.Created),
		Expires:     optTimestamp(s.ExpiresAt),
		LastVisited: optTimestamp(s.LastVisited),
		VisitCount:  s.VisitCount,
		Entry:       entries,
	}
}

func getShares(c *reqCtx) sdk.Response {
	rows, err := query(
		"SELECT "+shareColumns+" FROM shares WHERE owner_id = ? ORDER BY created_at, id",
		c.user.ID,
	)
	if err != nil {
		return c.failErr(err)
	}
	ss := make([]*share, len(rows))
	for i, r := range rows {
		ss[i] = scanShare(r)
	}
	if err := loadShareItems(ss); err != nil {
		return c.failErr(err)
	}
	// One read for every share's tracks, not one per share.
	byShare := make([][]uint32, len(ss))
	var all []uint32
	for i, s := range ss {
		byShare[i], err = shareTrackIDs(s.Items, s.OwnerID)
		if err != nil {
			return c.failErr(err)
		}
		all = append(all, byShare[i]...)
	}
	loaded, err := shareSongs(all)
	if err != nil {
		return c.failErr(err)
	}
	byTrack := make(map[string]shareTrack, len(loaded))
	for _, t := range loaded {
		byTrack[t.song.ID] = t
	}
	out := &shares{Share: make([]shareInfo, 0, len(ss))}
	for i, s := range ss {
		ts := make([]shareTrack, 0, len(byShare[i]))
		for _, id := range byShare[i] {
			if t, ok := byTrack[ids.Track(id)]; ok {
				ts = append(ts, t)
			}
		}
		out.Share = append(out.Share, toShareInfo(c, s, ts))
	}
	return c.ok(&body{Shares: out})
}

func createShare(c *reqCtx) sdk.Response {
	if err := c.require(permShare); err != nil {
		return c.failErr(err)
	}
	// A share link streams with the owner's rights, so one who may not stream
	// may not hand out a link that does.
	if err := c.require(permStream); err != nil {
		return c.failErr(err)
	}
	items, err := shareItems(c)
	if err != nil {
		return c.failErr(err)
	}
	token, err := newShareToken()
	if err != nil {
		return c.failErr(err)
	}
	s := &share{
		Token:       token,
		OwnerID:     c.user.ID,
		Description: strings.TrimSpace(c.param("description")),
		Created:     time.Now().UTC(),
		Items:       items,
	}
	var expires any
	if at, ok := c.millisParam("expires"); ok && at.UnixMilli() > 0 {
		s.ExpiresAt = at
		expires = formatStored(at)
	}
	err = tx(func() error {
		res, err := exec(
			`INSERT INTO shares(token, owner_id, description, expires_at, created_at)
			VALUES(?, ?, ?, ?, ?)`,
			s.Token, s.OwnerID, s.Description, expires, formatStored(s.Created),
		)
		if err != nil {
			return err
		}
		s.ID = uint32(min(max(res.LastInsertID, 0), math.MaxUint32))
		for pos, item := range items {
			if _, err := exec(
				"INSERT INTO share_entries(share_id, position, item) VALUES(?, ?, ?)",
				s.ID,
				pos,
				item,
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return c.failErr(err)
	}
	trackIDs, err := shareTrackIDs(s.Items, s.OwnerID)
	if err != nil {
		return c.failErr(err)
	}
	ts, err := shareSongs(trackIDs)
	if err != nil {
		return c.failErr(err)
	}
	return c.ok(&body{Shares: &shares{Share: []shareInfo{toShareInfo(c, s, ts)}}})
}

// ownShare checks the share is the caller's; another account's share does not
// exist for them.
func ownShare(c *reqCtx, id uint32) error {
	_, found, err := queryRow(
		"SELECT 1 FROM shares WHERE id = ? AND owner_id = ?", id, c.user.ID,
	)
	if err != nil {
		return err
	}
	if !found {
		return envelope.NotFound()
	}
	return nil
}

// updateShare changes the description and expiry; expires 0 removes it.
func updateShare(c *reqCtx) sdk.Response {
	if err := c.require(permShare); err != nil {
		return c.failErr(err)
	}
	id, err := c.idParam("id", ids.KindShare)
	if err != nil {
		return c.failErr(err)
	}
	if err := ownShare(c, id); err != nil {
		return c.failErr(err)
	}
	err = tx(func() error {
		if _, ok := c.params["description"]; ok {
			if _, err := exec(
				"UPDATE shares SET description = ? WHERE id = ?",
				strings.TrimSpace(c.param("description")), id,
			); err != nil {
				return err
			}
		}
		at, ok := c.millisParam("expires")
		if !ok {
			return nil
		}
		var expires any
		if at.UnixMilli() > 0 {
			expires = formatStored(at)
		}
		_, err := exec("UPDATE shares SET expires_at = ? WHERE id = ?", expires, id)
		return err
	})
	if err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}

func deleteShare(c *reqCtx) sdk.Response {
	if err := c.require(permShare); err != nil {
		return c.failErr(err)
	}
	id, err := c.idParam("id", ids.KindShare)
	if err != nil {
		return c.failErr(err)
	}
	if err := ownShare(c, id); err != nil {
		return c.failErr(err)
	}
	err = tx(func() error {
		if _, err := exec(
			"DELETE FROM share_entries WHERE share_id = ?",
			id,
		); err != nil {
			return err
		}
		_, err := exec("DELETE FROM shares WHERE id = ?", id)
		return err
	})
	if err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}
