package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/creds"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
)

func init() {
	register("getBookmarks", getBookmarks)
	register("createBookmark", createBookmark)
	register("deleteBookmark", deleteBookmark)
	register("getPlayQueue", getPlayQueue)
	register("getPlayQueueByIndex", getPlayQueueByIndex)
	register("savePlayQueue", savePlayQueue)
	register("savePlayQueueByIndex", savePlayQueueByIndex)
}

// queueMillis reads a non-negative millisecond count.
func queueMillis(raw string) (int64, error) {
	ms, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || ms < 0 {
		return 0, &envelope.Error{
			Code:    envelope.CodeGeneric,
			Message: "position must be a non-negative number",
		}
	}
	return ms, nil
}

func getBookmarks(c *reqCtx) sdk.Response {
	rows, err := query(`SELECT track_id, position_ms, comment, created_at, changed_at
		FROM bookmarks WHERE user_id = ? ORDER BY changed_at, track_id`, c.user.ID)
	if err != nil {
		return c.failErr(err)
	}
	trackIDs := make([]uint32, len(rows))
	for i, r := range rows {
		trackIDs[i] = r.uint(0)
	}
	songs, err := songsByID(trackIDs)
	if err != nil {
		return c.failErr(err)
	}
	out := &bookmarks{Bookmark: make([]bookmarkInfo, 0, len(rows))}
	for _, r := range rows {
		song, ok := songs[r.uint(0)]
		if !ok {
			continue
		}
		out.Bookmark = append(out.Bookmark, bookmarkInfo{
			Position: r.int(1),
			Username: c.user.Email,
			Comment:  r.str(2),
			Created:  timestamp(r.time(3)),
			Changed:  timestamp(r.time(4)),
			Entry:    song,
		})
	}
	return c.respond(&body{Bookmarks: out})
}

func createBookmark(c *reqCtx) sdk.Response {
	id, err := c.idParam("id", ids.KindTrack)
	if err != nil {
		return c.failErr(err)
	}
	raw := c.param("position")
	if raw == "" {
		return c.failErr(envelope.MissingParam("position"))
	}
	pos, err := queueMillis(raw)
	if err != nil {
		return c.failErr(err)
	}
	if _, found, err := queryRow(
		"SELECT 1 FROM streamline_tracks WHERE id = ?", id,
	); err != nil {
		return c.failErr(err)
	} else if !found {
		return c.failErr(envelope.NotFound())
	}
	now := nowStored()
	if _, err := exec(`INSERT INTO bookmarks(user_id, track_id, position_ms, comment,
			created_at, changed_at) VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, track_id) DO UPDATE SET
			position_ms = excluded.position_ms,
			comment = excluded.comment,
			changed_at = excluded.changed_at`,
		c.user.ID, id, pos, strings.TrimSpace(c.param("comment")), now, now,
	); err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}

func deleteBookmark(c *reqCtx) sdk.Response {
	id, err := c.idParam("id", ids.KindTrack)
	if err != nil {
		return c.failErr(err)
	}
	if _, err := exec(
		"DELETE FROM bookmarks WHERE user_id = ? AND track_id = ?", c.user.ID, id,
	); err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}

// queueSaved is the caller's stored queue reduced to playable entries, with
// the current index moved to match.
type queueSaved struct {
	positionMS int64
	changedBy  string
	changed    string
	entries    []child
	current    int // -1 when the current entry is gone
}

// queueLoad reads the caller's queue; found is false when nothing is saved.
func queueLoad(userID uint32) (saved queueSaved, found bool, err error) {
	head, found, err := queryRow(`SELECT current_index, position_ms, changed_by,
		changed_at FROM play_queues WHERE user_id = ?`, userID)
	if err != nil || !found {
		return queueSaved{}, false, err
	}
	rows, err := query(
		"SELECT track_id FROM play_queue_entries WHERE user_id = ? ORDER BY position",
		userID,
	)
	if err != nil {
		return queueSaved{}, false, err
	}
	trackIDs := make([]uint32, len(rows))
	for i, r := range rows {
		trackIDs[i] = r.uint(0)
	}
	songs, err := songsByID(trackIDs)
	if err != nil {
		return queueSaved{}, false, err
	}
	saved = queueSaved{
		positionMS: head.int(1),
		changedBy:  head.str(2),
		changed:    timestamp(head.time(3)),
		current:    -1,
	}
	for i, id := range trackIDs {
		song, ok := songs[id]
		if !ok {
			continue
		}
		if i == int(head.int(0)) {
			saved.current = len(saved.entries)
		}
		saved.entries = append(saved.entries, song)
	}
	return saved, true, nil
}

func getPlayQueue(c *reqCtx) sdk.Response {
	q, found, err := queueLoad(c.user.ID)
	if err != nil {
		return c.failErr(err)
	}
	if !found {
		return c.ok(nil)
	}
	out := &playQueue{
		Position:  q.positionMS,
		Username:  c.user.Email,
		Changed:   q.changed,
		ChangedBy: q.changedBy,
		Entry:     q.entries,
	}
	if q.current >= 0 {
		out.Current = q.entries[q.current].ID
	} else {
		out.Position = 0
	}
	return c.respond(&body{PlayQueue: out})
}

func getPlayQueueByIndex(c *reqCtx) sdk.Response {
	q, found, err := queueLoad(c.user.ID)
	if err != nil {
		return c.failErr(err)
	}
	if !found {
		return c.ok(nil)
	}
	out := &playQueueByIndex{
		Position:  q.positionMS,
		Username:  c.user.Email,
		Changed:   q.changed,
		ChangedBy: q.changedBy,
		Entry:     q.entries,
	}
	if q.current >= 0 {
		cur := q.current
		out.CurrentIndex = &cur
	} else {
		out.Position = 0
	}
	return c.respond(&body{PlayQueueByIndex: out})
}

// queueTrackIDs reads a repeated track-id parameter, refusing an id that
// names no track.
func queueTrackIDs(c *reqCtx, name string) ([]uint32, error) {
	raw := c.multi(name)
	trackIDs := make([]uint32, 0, len(raw))
	for _, s := range raw {
		kind, id, err := ids.Parse(s)
		if err != nil || kind != ids.KindTrack {
			return nil, envelope.NotFound()
		}
		trackIDs = append(trackIDs, id)
	}
	uniq := slices.Clone(trackIDs)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	rows, err := queryIn("SELECT id FROM streamline_tracks WHERE id IN ({in})", uniq)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(uniq) {
		return nil, envelope.NotFound()
	}
	return trackIDs, nil
}

// savePlayQueue stores the queue with current naming a track id; the first
// entry holding it becomes the current one.
func savePlayQueue(c *reqCtx) sdk.Response {
	trackIDs, err := queueTrackIDs(c, "id")
	if err != nil {
		return c.failErr(err)
	}
	current := 0
	if raw := c.param("current"); raw != "" {
		kind, id, err := ids.Parse(raw)
		if err != nil || kind != ids.KindTrack {
			return c.failErr(envelope.NotFound())
		}
		current = slices.Index(trackIDs, id)
		if current < 0 {
			return c.fail(envelope.CodeGeneric, "current is not in the queue")
		}
	}
	return queueStore(c, trackIDs, current)
}

func savePlayQueueByIndex(c *reqCtx) sdk.Response {
	trackIDs, err := queueTrackIDs(c, "id")
	if err != nil {
		return c.failErr(err)
	}
	current := 0
	if c.param("currentIndex") != "" {
		current = c.intParam("currentIndex", -1, -1, len(trackIDs))
		if current < 0 || current >= len(trackIDs) {
			return c.fail(envelope.CodeGeneric, "currentIndex is out of range")
		}
	}
	return queueStore(c, trackIDs, current)
}

// queueEntryChunk keeps one INSERT well under the bound-parameter limit.
const queueEntryChunk = 200

// queueStore overwrites the caller's queue; an empty one clears it.
func queueStore(c *reqCtx, trackIDs []uint32, current int) sdk.Response {
	var pos int64
	if raw := c.param("position"); raw != "" {
		p, err := queueMillis(raw)
		if err != nil {
			return c.failErr(err)
		}
		pos = p
	}
	client := creds.SanitizeClient(c.param("c"))
	err := tx(func() error {
		if _, err := exec(
			"DELETE FROM play_queues WHERE user_id = ?", c.user.ID,
		); err != nil {
			return err
		}
		if _, err := exec(
			"DELETE FROM play_queue_entries WHERE user_id = ?", c.user.ID,
		); err != nil {
			return err
		}
		if len(trackIDs) == 0 {
			return nil
		}
		if _, err := exec(`INSERT INTO play_queues(user_id, current_index,
				position_ms, changed_by, changed_at) VALUES(?, ?, ?, ?, ?)`,
			c.user.ID, current, pos, client, nowStored(),
		); err != nil {
			return err
		}
		for start := 0; start < len(trackIDs); start += queueEntryChunk {
			chunk := trackIDs[start:min(start+queueEntryChunk, len(trackIDs))]
			args := make([]any, 0, len(chunk)*3)
			for i, id := range chunk {
				args = append(args, c.user.ID, start+i, id)
			}
			values := strings.TrimSuffix(
				strings.Repeat("(?, ?, ?),", len(chunk)),
				",",
			)
			if _, err := exec(
				"INSERT INTO play_queue_entries(user_id, position, track_id) VALUES "+values,
				args...,
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}
