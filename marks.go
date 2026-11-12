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

func init() {
	register("star", star)
	register("unstar", unstar)
	register("setRating", setRating)
	register("scrobble", scrobble)
	registerDecorator(decorateMarks)
}

// mark is one listener's annotation on one target.
type mark struct {
	starredAt time.Time
	rating    uint8
	playCount uint32
	playedAt  time.Time
}

// markTargets maps a Subsonic id kind to the core view holding its rows.
var markTargets = map[string]string{
	ids.KindTrack:  "streamline_tracks",
	ids.KindAlbum:  "streamline_albums",
	ids.KindArtist: "streamline_artists",
}

// decorateMarks fills the caller's own star, rating and play count into every
// song, album and artist of b after one read per kind.
func decorateMarks(c *reqCtx, b *body) error {
	col := collectMarkTargets(b)
	byKind := map[string][]uint32{}
	add := func(raw string) {
		if kind, id, err := ids.Parse(raw); err == nil {
			byKind[kind] = append(byKind[kind], id)
		}
	}
	for _, ch := range col.children {
		add(ch.ID)
	}
	for _, al := range col.albums {
		add(al.ID)
	}
	for _, ar := range col.artists {
		add(ar.ID)
	}
	if col.dir != nil {
		add(col.dir.ID)
	}
	if len(byKind) == 0 {
		return nil
	}
	m, err := loadMarks(c.user.ID, byKind)
	if err != nil {
		return err
	}
	if len(m) == 0 {
		return nil
	}
	for _, ch := range col.children {
		if a, ok := m[ch.ID]; ok {
			ch.Starred = optTimestamp(a.starredAt)
			ch.UserRating = a.rating
			ch.PlayCount = a.playCount
			ch.Played = optTimestamp(a.playedAt)
		}
	}
	for _, al := range col.albums {
		if a, ok := m[al.ID]; ok {
			al.Starred = optTimestamp(a.starredAt)
			al.UserRating = a.rating
			al.PlayCount = a.playCount
			al.Played = optTimestamp(a.playedAt)
		}
	}
	for _, ar := range col.artists {
		if a, ok := m[ar.ID]; ok {
			ar.Starred = optTimestamp(a.starredAt)
			ar.UserRating = a.rating
		}
	}
	if col.dir != nil {
		if a, ok := m[col.dir.ID]; ok {
			col.dir.Starred = optTimestamp(a.starredAt)
			col.dir.UserRating = a.rating
			col.dir.PlayCount = a.playCount
			col.dir.Played = optTimestamp(a.playedAt)
		}
	}
	return nil
}

// loadMarks reads the user's annotations on the given ids, keyed by Subsonic
// id.
func loadMarks(userID uint32, byKind map[string][]uint32) (map[string]mark, error) {
	out := map[string]mark{}
	for kind, list := range byKind {
		if _, known := markTargets[kind]; !known {
			continue
		}
		slices.Sort(list)
		list = slices.Compact(list)
		rows, err := queryIn(
			`SELECT target_id, starred_at, rating, play_count, played_at
			FROM annotations
			WHERE user_id = ? AND target_kind = ? AND target_id IN ({in})`,
			list, userID, kind,
		)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[ids.Format(kind, r.uint(0))] = readMark(r, 1)
		}
	}
	return out, nil
}

// readMark reads starred_at, rating, play_count, played_at from column i.
func readMark(r row, i int) mark {
	return mark{
		starredAt: r.time(i),
		rating:    uint8(min(max(r.int(i+1), 0), 5)),
		playCount: r.uint(i + 2),
		playedAt:  r.time(i + 3),
	}
}

// setMark reads the caller's annotation on one target, lets fn change it and
// writes it back, creating the row on first use. It runs inside the caller's
// transaction. A target that is no row of the core library is error 70.
func setMark(
	userID uint32,
	kind string,
	id uint32,
	fn func(*mark),
) error {
	table, known := markTargets[kind]
	if !known {
		return envelope.NotFound()
	}
	if _, found, err := queryRow(
		"SELECT 1 FROM "+table+" WHERE id = ?",
		id,
	); err != nil {
		return err
	} else if !found {
		return envelope.NotFound()
	}
	var cur mark
	if r, found, err := queryRow(
		`SELECT starred_at, rating, play_count, played_at FROM annotations
		WHERE user_id = ? AND target_kind = ? AND target_id = ?`,
		userID, kind, id,
	); err != nil {
		return err
	} else if found {
		cur = readMark(r, 0)
	}
	fn(&cur)
	_, err := exec(
		`INSERT INTO annotations
			(user_id, target_kind, target_id, starred_at, rating, play_count, played_at,
			updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, target_kind, target_id) DO UPDATE SET
			starred_at = excluded.starred_at, rating = excluded.rating,
			play_count = excluded.play_count, played_at = excluded.played_at,
			updated_at = excluded.updated_at`,
		userID, kind, id, storedOrNil(cur.starredAt), cur.rating, cur.playCount,
		storedOrNil(cur.playedAt), nowStored(),
	)
	return err
}

// storedOrNil is a time as the extension's tables keep it, NULL for the zero
// time.
func storedOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return formatStored(t)
}

type markTarget struct {
	kind string
	id   uint32
}

// starTargets reads star and unstar's three id parameters. id may name any
// kind; albumId and artistId are the ID3 forms and must be of their own.
func starTargets(c *reqCtx) ([]markTarget, error) {
	var out []markTarget
	for _, p := range []struct{ name, kind string }{
		{"id", ""},
		{"albumId", ids.KindAlbum},
		{"artistId", ids.KindArtist},
	} {
		for _, raw := range c.multi(p.name) {
			kind, id, err := ids.Parse(raw)
			if err != nil || (p.kind != "" && kind != p.kind) {
				return nil, envelope.NotFound()
			}
			if _, known := markTargets[kind]; !known {
				return nil, envelope.NotFound()
			}
			out = append(out, markTarget{kind, id})
		}
	}
	if len(out) == 0 {
		return nil, envelope.MissingParam("id")
	}
	return out, nil
}

func star(c *reqCtx) sdk.Response   { return applyStar(c, true) }
func unstar(c *reqCtx) sdk.Response { return applyStar(c, false) }

func applyStar(c *reqCtx, on bool) sdk.Response {
	targets, err := starTargets(c)
	if err != nil {
		return c.failErr(err)
	}
	now := time.Now().UTC()
	err = tx(func() error {
		for _, t := range targets {
			if err := setMark(c.user.ID, t.kind, t.id, func(m *mark) {
				switch {
				case !on:
					m.starredAt = time.Time{}
				case m.starredAt.IsZero():
					m.starredAt = now
				}
			}); err != nil {
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

func setRating(c *reqCtx) sdk.Response {
	if err := c.require(permRate); err != nil {
		return c.failErr(err)
	}
	kind, id, err := c.anyIDParam("id")
	if err != nil {
		return c.failErr(err)
	}
	if c.param("rating") == "" {
		return c.failErr(envelope.MissingParam("rating"))
	}
	rating := c.intParam("rating", -1, -1, 6)
	if rating < 0 || rating > 5 {
		return c.fail(envelope.CodeGeneric, "rating must be between 0 and 5")
	}
	err = tx(func() error {
		return setMark(c.user.ID, kind, id, func(m *mark) {
			m.rating = uint8(rating) //nolint:gosec // bounded above
		})
	})
	if err != nil {
		return c.failErr(err)
	}
	return c.ok(nil)
}

// play is one submitted scrobble: a track and when it was heard.
type play struct {
	trackID  uint32
	albumID  uint32
	artistID uint32
	duration uint32
	at       time.Time
}

// scrobble records plays (submission=true, the default) or reports what is
// playing now (submission=false). A play counts on the track, its album and
// its artist, so the album lists' frequent and recent read their own rows.
func scrobble(c *reqCtx) sdk.Response {
	raw := c.multi("id")
	if len(raw) == 0 {
		return c.failErr(envelope.MissingParam("id"))
	}
	times := c.multi("time")
	plays := make([]play, 0, len(raw))
	for i, s := range raw {
		kind, id, err := ids.Parse(s)
		if err != nil || kind != ids.KindTrack {
			return c.failErr(envelope.NotFound())
		}
		r, found, err := queryRow(
			`SELECT t.album_id, a.artist_id, t.duration
			FROM streamline_tracks t JOIN streamline_albums a ON a.id = t.album_id
			WHERE t.id = ?`, id)
		if err != nil {
			return c.failErr(err)
		}
		if !found {
			return c.failErr(envelope.NotFound())
		}
		at := time.Now().UTC()
		if i < len(times) {
			if ms, err := strconv.ParseInt(
				strings.TrimSpace(times[i]),
				10,
				64,
			); err == nil &&
				ms > 0 {
				at = time.UnixMilli(ms).UTC()
			}
		}
		plays = append(plays, play{id, r.uint(0), r.uint(1), r.uint(2), at})
	}
	player := c.param("c")

	if !c.boolParam("submission", true) {
		last := plays[len(plays)-1]
		if err := setNowPlaying(
			c.user.ID,
			player,
			last.trackID,
			last.duration,
		); err != nil {
			return c.failErr(err)
		}
		return c.ok(nil)
	}

	err := tx(func() error {
		for _, p := range plays {
			bump := func(m *mark) {
				m.playCount++
				if m.playedAt.IsZero() || p.at.After(m.playedAt) {
					m.playedAt = p.at
				}
			}
			for _, t := range []markTarget{
				{ids.KindTrack, p.trackID},
				{ids.KindAlbum, p.albumID},
				{ids.KindArtist, p.artistID},
			} {
				if err := setMark(c.user.ID, t.kind, t.id, bump); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return c.failErr(err)
	}
	for _, p := range plays {
		if err := finishNowPlaying(c.user.ID, player, p.trackID); err != nil {
			return c.failErr(err)
		}
	}
	return c.ok(nil)
}
