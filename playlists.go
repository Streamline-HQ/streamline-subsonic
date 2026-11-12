package main

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
)

// playlistEntryBatch is how many entries one INSERT writes.
const playlistEntryBatch = 200

func init() {
	register("getPlaylists", getPlaylists)
	register("getPlaylist", getPlaylist)
	register("createPlaylist", createPlaylist)
	register("updatePlaylist", updatePlaylist)
	register("deletePlaylist", deletePlaylist)
}

type playlistRecord struct {
	id      uint32
	ownerID uint32
	owner   string
	name    string
	comment string
	public  bool
	created time.Time
	changed time.Time
}

const playlistSelect = `SELECT p.id, p.owner_id, COALESCE(l.email, ''), p.name,
		p.comment, p.public, p.created_at, p.changed_at
	FROM playlists p
	LEFT JOIN streamline_user_logins l ON l.user_id = p.owner_id`

func playlistRecordOf(r row) playlistRecord {
	return playlistRecord{
		id: r.uint(0), ownerID: r.uint(1), owner: r.str(2), name: r.str(3),
		comment: r.str(4), public: r.bool(5), created: r.time(6), changed: r.time(7),
	}
}

// playlistTrackIDs reads the stored entries of each playlist, in order.
func playlistTrackIDs(playlistIDs []uint32) (map[uint32][]uint32, error) {
	rows, err := queryIn(`SELECT playlist_id, track_id FROM playlist_tracks
		WHERE playlist_id IN ({in}) ORDER BY playlist_id, position`, playlistIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32][]uint32, len(playlistIDs))
	for _, r := range rows {
		out[r.uint(0)] = append(out[r.uint(0)], r.uint(1))
	}
	return out, nil
}

// playlistVisible is whether the caller may read p: its owner, anyone when it
// is public, an admin.
func (c *reqCtx) playlistVisible(p playlistRecord) bool {
	return p.public || p.ownerID == c.user.ID || c.user.isAdmin()
}

// loadPlaylist reads a playlist the caller may see. Someone else's private
// playlist does not exist for them.
func (c *reqCtx) loadPlaylist(id uint32) (playlistRecord, error) {
	r, found, err := queryRow(playlistSelect+" WHERE p.id = ?", id)
	if err != nil {
		return playlistRecord{}, err
	}
	if !found {
		return playlistRecord{}, envelope.NotFound()
	}
	p := playlistRecordOf(r)
	if !c.playlistVisible(p) {
		return playlistRecord{}, envelope.NotFound()
	}
	return p, nil
}

// ownPlaylist loads a playlist the caller may change: their own, or any when
// they are an admin.
func (c *reqCtx) ownPlaylist(id uint32) (playlistRecord, error) {
	p, err := c.loadPlaylist(id)
	if err != nil {
		return playlistRecord{}, err
	}
	if p.ownerID != c.user.ID && !c.user.isAdmin() {
		return playlistRecord{}, envelope.NotAuthorized()
	}
	return p, nil
}

func playlistInfoOf(p playlistRecord, entries []child) playlistInfo {
	var duration uint32
	for _, e := range entries {
		duration += e.Duration
	}
	out := playlistInfo{
		ID:        ids.Playlist(p.id),
		Name:      p.name,
		Comment:   p.comment,
		Owner:     p.owner,
		Public:    p.public,
		SongCount: len(entries),
		Duration:  duration,
		Created:   timestamp(p.created),
		Changed:   timestamp(p.changed),
	}
	if len(entries) > 0 {
		out.CoverArt = out.ID
	}
	return out
}

// playlistEntries are the stored entries that still have a file, in order and
// keeping duplicates: what a client sees and indexes.
func playlistEntries(stored []uint32, songs map[uint32]child) []child {
	out := make([]child, 0, len(stored))
	for _, id := range stored {
		if s, ok := songs[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

func playlistWithEntries(p playlistRecord) (*playlistWith, error) {
	stored, err := playlistTrackIDs([]uint32{p.id})
	if err != nil {
		return nil, err
	}
	songs, err := playlistSongs(stored[p.id])
	if err != nil {
		return nil, err
	}
	entries := playlistEntries(stored[p.id], songs)
	return &playlistWith{
		playlistInfo: playlistInfoOf(p, entries),
		Entry:        entries,
	}, nil
}

// playlistTrackParam reads a repeated track-id parameter, refusing an id that
// names no track.
func (c *reqCtx) playlistTrackParam(name string) ([]uint32, error) {
	raw := c.multi(name)
	out := make([]uint32, 0, len(raw))
	for _, s := range raw {
		kind, id, err := ids.Parse(s)
		if err != nil || kind != ids.KindTrack {
			return nil, envelope.NotFound()
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return out, nil
	}
	uniq := slices.Clone(out)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	rows, err := queryIn("SELECT id FROM streamline_tracks WHERE id IN ({in})", uniq)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(uniq) {
		return nil, envelope.NotFound()
	}
	return out, nil
}

// getPlaylists lists the caller's playlists and everyone's public ones. An
// admin may name another user and gets all of theirs.
func getPlaylists(c *reqCtx) sdk.Response {
	var (
		rows []row
		err  error
	)
	if name := c.param(
		"username",
	); name != "" &&
		!strings.EqualFold(name, c.user.Email) {
		if !c.user.isAdmin() {
			return c.failErr(envelope.NotAuthorized())
		}
		rows, err = query(
			playlistSelect+" WHERE l.email = ? ORDER BY p.name, p.id",
			strings.ToLower(name),
		)
	} else {
		rows, err = query(
			playlistSelect+" WHERE p.owner_id = ? OR p.public = 1 ORDER BY p.name, p.id",
			c.user.ID,
		)
	}
	if err != nil {
		return c.failErr(err)
	}
	records := make([]playlistRecord, len(rows))
	listed := make([]uint32, len(rows))
	for i, r := range rows {
		records[i] = playlistRecordOf(r)
		listed[i] = records[i].id
	}
	stored, err := playlistTrackIDs(listed)
	if err != nil {
		return c.failErr(err)
	}
	var every []uint32
	for _, id := range listed {
		every = append(every, stored[id]...)
	}
	songs, err := playlistSongs(every)
	if err != nil {
		return c.failErr(err)
	}
	out := &playlists{Playlist: make([]playlistInfo, 0, len(records))}
	for _, p := range records {
		out.Playlist = append(
			out.Playlist,
			playlistInfoOf(p, playlistEntries(stored[p.id], songs)),
		)
	}
	return c.ok(&body{Playlists: out})
}

func getPlaylist(c *reqCtx) sdk.Response {
	id, err := c.idParam("id", ids.KindPlaylist)
	if err != nil {
		return c.failErr(err)
	}
	p, err := c.loadPlaylist(id)
	if err != nil {
		return c.failErr(err)
	}
	full, err := playlistWithEntries(p)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{Playlist: full})
}

// playlistReplaceEntries rewrites a playlist's entries. It runs inside the
// caller's transaction.
func playlistReplaceEntries(playlistID uint32, trackIDs []uint32) error {
	if _, err := exec(
		"DELETE FROM playlist_tracks WHERE playlist_id = ?",
		playlistID,
	); err != nil {
		return err
	}
	for start := 0; start < len(trackIDs); start += playlistEntryBatch {
		batch := trackIDs[start:min(start+playlistEntryBatch, len(trackIDs))]
		args := make([]any, 0, 3*len(batch))
		for i, id := range batch {
			args = append(args, playlistID, start+i, id)
		}
		marks := strings.TrimSuffix(strings.Repeat("(?, ?, ?),", len(batch)), ",")
		if _, err := exec(
			"INSERT INTO playlist_tracks(playlist_id, position, track_id) VALUES "+marks,
			args...,
		); err != nil {
			return err
		}
	}
	return nil
}

// createPlaylist creates a playlist from name, or with playlistId replaces an
// existing one's songs.
func createPlaylist(c *reqCtx) sdk.Response {
	if err := c.require(permPlaylists); err != nil {
		return c.failErr(err)
	}
	trackIDs, err := c.playlistTrackParam("songId")
	if err != nil {
		return c.failErr(err)
	}
	var id uint32
	if c.param("playlistId") != "" {
		if id, err = playlistReplace(c, trackIDs); err != nil {
			return c.failErr(err)
		}
	} else {
		name := strings.TrimSpace(c.param("name"))
		if name == "" {
			return c.failErr(envelope.MissingParam("name"))
		}
		if id, err = playlistCreate(c, name, trackIDs); err != nil {
			return c.failErr(err)
		}
	}
	p, err := c.loadPlaylist(id)
	if err != nil {
		return c.failErr(err)
	}
	full, err := playlistWithEntries(p)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{Playlist: full})
}

func playlistCreate(c *reqCtx, name string, trackIDs []uint32) (uint32, error) {
	var id uint32
	err := tx(func() error {
		now := nowStored()
		res, err := exec(
			`INSERT INTO playlists(owner_id, name, comment, public, created_at, changed_at)
			VALUES(?, ?, '', 0, ?, ?)`,
			c.user.ID,
			name,
			now,
			now,
		)
		if err != nil {
			return err
		}
		id = uint32(res.LastInsertID) //nolint:gosec // an autoincrement rowid
		return playlistReplaceEntries(id, trackIDs)
	})
	return id, err
}

func playlistReplace(c *reqCtx, trackIDs []uint32) (uint32, error) {
	id, err := c.idParam("playlistId", ids.KindPlaylist)
	if err != nil {
		return 0, err
	}
	p, err := c.ownPlaylist(id)
	if err != nil {
		return 0, err
	}
	name := p.name
	if given := strings.TrimSpace(c.param("name")); given != "" {
		name = given
	}
	err = tx(func() error {
		if _, err := exec(
			"UPDATE playlists SET name = ?, changed_at = ? WHERE id = ?",
			name, nowStored(), id,
		); err != nil {
			return err
		}
		return playlistReplaceEntries(id, trackIDs)
	})
	return id, err
}

// updatePlaylist edits the header and the songs. songIndexToRemove counts
// entries as getPlaylist listed them, so the stored list is first reduced to
// those: an entry whose file is gone would otherwise shift every index.
func updatePlaylist(c *reqCtx) sdk.Response {
	if err := c.require(permPlaylists); err != nil {
		return c.failErr(err)
	}
	id, err := c.idParam("playlistId", ids.KindPlaylist)
	if err != nil {
		return c.failErr(err)
	}
	p, err := c.ownPlaylist(id)
	if err != nil {
		return c.failErr(err)
	}
	add, err := c.playlistTrackParam("songIdToAdd")
	if err != nil {
		return c.failErr(err)
	}
	if c.params.Has("name") {
		p.name = strings.TrimSpace(c.param("name"))
		if p.name == "" {
			return c.fail(envelope.CodeGeneric, "name must not be empty")
		}
	}
	if c.params.Has("comment") {
		p.comment = c.param("comment")
	}
	if c.params.Has("public") {
		p.public = c.boolParam("public", p.public)
	}
	remove := c.multi("songIndexToRemove")
	var entries []uint32
	changeEntries := len(remove) > 0 || len(add) > 0
	if changeEntries {
		stored, err := playlistTrackIDs([]uint32{p.id})
		if err != nil {
			return c.failErr(err)
		}
		kept, err := playlistRemoveEntries(stored[p.id], remove)
		if err != nil {
			return c.failErr(err)
		}
		entries = append(kept, add...)
	}
	err = tx(func() error {
		if _, err := exec(
			`UPDATE playlists SET name = ?, comment = ?, public = ?, changed_at = ?
			WHERE id = ?`,
			p.name, p.comment, playlistFlag(p.public), nowStored(), p.id,
		); err != nil {
			return err
		}
		if !changeEntries {
			return nil
		}
		return playlistReplaceEntries(p.id, entries)
	})
	if err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}

// playlistRemoveEntries drops the entries at remove, indexes into the list
// getPlaylist shows, from the stored ids. Entries whose file is gone are
// invisible to those indexes but stay stored, so a song that comes back is
// still in the playlist after an unrelated edit.
func playlistRemoveEntries(stored []uint32, remove []string) ([]uint32, error) {
	if len(remove) == 0 {
		return slices.Clone(stored), nil
	}
	songs, err := playlistSongs(stored)
	if err != nil {
		return nil, err
	}
	visible := len(playlistEntries(stored, songs))
	drop := map[int]bool{}
	for _, s := range remove {
		i, err := strconv.Atoi(s)
		if err != nil || i < 0 || i >= visible {
			return nil, &envelope.Error{
				Code:    envelope.CodeGeneric,
				Message: "songIndexToRemove out of range",
			}
		}
		drop[i] = true
	}
	kept := make([]uint32, 0, len(stored))
	seen := 0
	for _, id := range stored {
		if _, live := songs[id]; live {
			i := seen
			seen++
			if drop[i] {
				continue
			}
		}
		kept = append(kept, id)
	}
	return kept, nil
}

func deletePlaylist(c *reqCtx) sdk.Response {
	if err := c.require(permPlaylists); err != nil {
		return c.failErr(err)
	}
	id, err := c.idParam("id", ids.KindPlaylist)
	if err != nil {
		return c.failErr(err)
	}
	if _, err := c.ownPlaylist(id); err != nil {
		return c.failErr(err)
	}
	err = tx(func() error {
		if _, err := exec(
			"DELETE FROM playlist_tracks WHERE playlist_id = ?",
			id,
		); err != nil {
			return err
		}
		_, err := exec("DELETE FROM playlists WHERE id = ?", id)
		return err
	})
	if err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}

func playlistFlag(b bool) int {
	if b {
		return 1
	}
	return 0
}
