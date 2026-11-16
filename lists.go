package main

import (
	"strconv"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic-ext/envelope"
)

func init() {
	register("getAlbumList", getAlbumList)
	register("getAlbumList2", getAlbumList2)
	register("getRandomSongs", getRandomSongs)
	register("getSongsByGenre", getSongsByGenre)
}

// listsYear is the year of a release date, as SQL over alias al.
const listsYear = "CAST(substr(al.release_date, 1, 4) AS INTEGER)"

// listsYearRange is fromYear..toYear, both inclusive. reversed reports
// toYear < fromYear, which the API defines as newest first.
func listsYearRange(from, to int) (lo, hi int, reversed bool) {
	if to < from {
		return to, from, true
	}
	return from, to, false
}

// listsYearParam reads a year parameter: error 10 when required and absent,
// error 0 when it is not a number. present is false for an absent optional
// one.
func listsYearParam(
	c *reqCtx,
	name string,
	required bool,
) (year int, present bool, err error) {
	raw := strings.TrimSpace(c.param(name))
	if raw == "" {
		if required {
			return 0, false, envelope.MissingParam(name)
		}
		return 0, false, nil
	}
	n, convErr := strconv.Atoi(raw)
	if convErr != nil {
		return 0, false, &envelope.Error{
			Code:    envelope.CodeGeneric,
			Message: "Not a year: " + name,
		}
	}
	return min(max(n, 0), 9999), true, nil
}

// listsMarked orders the caller's albums by what they did to them.
func listsMarked(c *reqCtx, cond, order string, size, offset int) browseAlbumQuery {
	return browseAlbumQuery{
		join: `JOIN annotations an ON an.target_kind = 'al' AND an.target_id = al.id
			AND an.user_id = ?`,
		where:  cond + " AND " + browseListedAlbum("al"),
		args:   []any{c.user.ID},
		order:  order + ", al.id",
		limit:  size,
		offset: offset,
	}
}

// listsAlbums serves both getAlbumList and getAlbumList2. type is required
// and one of the list types the API names: error 10 when absent, error 0 for
// any other value.
func listsAlbums(c *reqCtx) ([]browseAlbum, error) {
	size := c.intParam("size", browseListSize, 1, browseMaxList)
	offset := c.intParam("offset", 0, 0, browseMaxOffset)
	q := browseAlbumQuery{
		where: browseListedAlbum("al"), limit: size, offset: offset,
	}
	switch c.param("type") {
	case "":
		return nil, envelope.MissingParam("type")
	case "random":
		q.order = "RANDOM()"
	case "newest":
		// An album row is created with its artist's whole discography, so its
		// own creation time says nothing about when its music arrived.
		q.order = `(SELECT max(` + coreMillis("nf.added_at") + `)
			FROM streamline_music_files nf
			JOIN streamline_tracks nt ON nt.id = nf.track_id
			WHERE nt.album_id = al.id AND nf.missing = 0) DESC, al.id DESC`
	case "alphabeticalByArtist":
		q.order = "ar.name, al.release_date, al.title, al.id"
	case "byYear":
		from, _, err := listsYearParam(c, "fromYear", true)
		if err != nil {
			return nil, err
		}
		to, _, err := listsYearParam(c, "toYear", true)
		if err != nil {
			return nil, err
		}
		lo, hi, reversed := listsYearRange(from, to)
		q.where += " AND al.release_date IS NOT NULL AND " + listsYear +
			" BETWEEN ? AND ?"
		q.args = []any{lo, hi}
		q.order = "al.release_date, al.id"
		if reversed {
			q.order = "al.release_date DESC, al.id"
		}
	case "byGenre":
		genre := c.param("genre")
		if genre == "" {
			return nil, envelope.MissingParam("genre")
		}
		q.where += " AND " + browseHasGenre
		q.args = []any{genre, genre}
		q.order = "al.title, al.id"
	case "starred":
		q = listsMarked(
			c,
			"an.starred_at IS NOT NULL",
			"an.starred_at DESC",
			size,
			offset,
		)
	case "highest":
		q = listsMarked(
			c,
			"an.rating > 0",
			"an.rating DESC, an.updated_at DESC",
			size,
			offset,
		)
	case "frequent":
		q = listsMarked(
			c,
			"an.play_count > 0",
			"an.play_count DESC, an.played_at DESC",
			size,
			offset,
		)
	case "recent":
		q = listsMarked(
			c,
			"an.played_at IS NOT NULL",
			"an.played_at DESC",
			size,
			offset,
		)
	case "alphabeticalByName":
		q.order = "al.title, al.id"
	default:
		return nil, &envelope.Error{
			Code:    envelope.CodeGeneric,
			Message: "Unknown album list type",
		}
	}
	if browseOtherFolder(c) {
		return nil, nil
	}
	return browseAlbums(q)
}

func getAlbumList(c *reqCtx) sdk.Response {
	als, err := listsAlbums(c)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{AlbumList: &albumList{Album: browseAlbumChildren(als)}})
}

func getAlbumList2(c *reqCtx) sdk.Response {
	als, err := listsAlbums(c)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{AlbumList2: &albumList2{Album: browseAlbumsOf(als)}})
}

func getRandomSongs(c *reqCtx) sdk.Response {
	from, hasFrom, err := listsYearParam(c, "fromYear", false)
	if err != nil {
		return c.failErr(err)
	}
	to, hasTo, err := listsYearParam(c, "toYear", false)
	if err != nil {
		return c.failErr(err)
	}
	out := &songs{Song: []child{}}
	if browseOtherFolder(c) {
		return c.respond(&body{RandomSongs: out})
	}
	q := songQuery{
		where: "1 = 1",
		order: "RANDOM()",
		limit: c.intParam("size", browseListSize, 1, browseMaxList),
	}
	if g := c.param("genre"); g != "" {
		q.where += " AND " + browseHasGenre
		q.args = append(q.args, g, g)
	}
	if hasFrom {
		q.where += " AND al.release_date IS NOT NULL AND " + listsYear + " >= ?"
		q.args = append(q.args, from)
	}
	if hasTo {
		q.where += " AND al.release_date IS NOT NULL AND " + listsYear + " <= ?"
		q.args = append(q.args, to)
	}
	songs, err := songChildren(q)
	if err != nil {
		return c.failErr(err)
	}
	out.Song = songs
	return c.respond(&body{RandomSongs: out})
}

func getSongsByGenre(c *reqCtx) sdk.Response {
	genre := c.param("genre")
	if genre == "" {
		return c.failErr(envelope.MissingParam("genre"))
	}
	out := &songs{Song: []child{}}
	if browseOtherFolder(c) {
		return c.respond(&body{SongsByGenre: out})
	}
	found, err := songChildren(songQuery{
		where:  browseHasGenre,
		args:   []any{genre, genre},
		order:  "t.id",
		limit:  c.intParam("count", browseListSize, 1, browseMaxList),
		offset: c.intParam("offset", 0, 0, browseMaxOffset),
	})
	if err != nil {
		return c.failErr(err)
	}
	out.Song = found
	return c.respond(&body{SongsByGenre: out})
}
