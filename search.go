package main

import (
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
)

func init() {
	register("search", searchLegacy)
	register("search2", searchFolder)
	register("search3", searchID3)
}

// maxSearchSize bounds search2/search3 counts. Clients sync the whole library
// by paging an empty query at their own count and stop at the first short
// page, so a ceiling below the counts they send ends the sync early.
const maxSearchSize = browseMaxList

// searchQuery is the query parameter with the double quotes some clients wrap
// it in removed: Symfonium and DSub send `""` to mean "everything" when they
// sync the whole library through search3.
func searchQuery(c *reqCtx) string {
	q := strings.TrimSpace(c.param("query"))
	if len(q) >= 2 && strings.HasPrefix(q, `"`) && strings.HasSuffix(q, `"`) {
		q = q[1 : len(q)-1]
	}
	return strings.TrimSpace(q)
}

const searchLike = ` LIKE ? ESCAPE '\'`

// searchAlbumMatch matches an album by its title or the name of any artist it
// is listed under, so a query naming an artist lists their music, as the
// reference server and Navidrome do. It takes two arguments, both the pattern.
const searchAlbumMatch = `(lower(al.title)` + searchLike + ` OR EXISTS (
	SELECT 1 FROM streamline_album_artists ma
	JOIN streamline_artists mx ON mx.id = ma.artist_id
	WHERE ma.album_id = al.id AND lower(mx.name)` + searchLike + `))`

type searchHits struct {
	artists []browseArtist
	albums  []browseAlbum
	songs   []child
}

// searchAll is the shared body of search2 and search3.
func searchAll(c *reqCtx) (searchHits, error) {
	var out searchHits
	if browseOtherFolder(c) {
		return out, nil
	}
	pattern := browseLike(searchQuery(c))

	if n := c.intParam("artistCount", browsePageSize, 0, maxSearchSize); n > 0 {
		ars, err := browseArtists(
			"lower(ar.name)"+searchLike+" AND "+browseListedArtist("ar"),
			[]any{pattern}, "ar.name, ar.id",
			n, c.intParam("artistOffset", 0, 0, browseMaxOffset),
		)
		if err != nil {
			return out, err
		}
		out.artists = ars
	}

	if n := c.intParam("albumCount", browsePageSize, 0, maxSearchSize); n > 0 {
		als, err := browseAlbums(browseAlbumQuery{
			where:  searchAlbumMatch + " AND " + browseListedAlbum("al"),
			args:   []any{pattern, pattern},
			order:  "al.title, al.id",
			limit:  n,
			offset: c.intParam("albumOffset", 0, 0, browseMaxOffset),
		})
		if err != nil {
			return out, err
		}
		out.albums = als
	}

	if n := c.intParam("songCount", browsePageSize, 0, maxSearchSize); n > 0 {
		songs, err := browseSongs(browseSongQuery{
			where:  "(lower(t.title)" + searchLike + " OR " + searchAlbumMatch + ")",
			args:   []any{pattern, pattern, pattern},
			order:  "t.title, t.id",
			limit:  n,
			offset: c.intParam("songOffset", 0, 0, browseMaxOffset),
		})
		if err != nil {
			return out, err
		}
		out.songs = songs
	}
	return out, nil
}

func searchFolder(c *reqCtx) sdk.Response {
	hits, err := searchAll(c)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{SearchResult2: &searchResult2{
		Artist: browseArtistsOf(hits.artists),
		Album:  browseAlbumChildren(hits.albums),
		Song:   orEmpty(hits.songs),
	}})
}

func searchID3(c *reqCtx) sdk.Response {
	hits, err := searchAll(c)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{SearchResult3: &search3{
		Artist: browseArtistsOf(hits.artists),
		Album:  browseAlbumsOf(hits.albums),
		Song:   orEmpty(hits.songs),
	}})
}

func orEmpty(s []child) []child {
	if s == nil {
		return []child{}
	}
	return s
}

// searchLegacy is the deprecated first search: songs only, matched on the artist,
// album and title parameters together, or on any of the three by "any".
func searchLegacy(c *reqCtx) sdk.Response {
	where, args := "1 = 1", []any(nil)
	like := func(param string) (string, bool) {
		v := strings.TrimSpace(c.param(param))
		return browseLike(v), v != ""
	}
	if p, ok := like("artist"); ok {
		where += " AND lower(ar.name)" + searchLike
		args = append(args, p)
	}
	if p, ok := like("album"); ok {
		where += " AND lower(al.title)" + searchLike
		args = append(args, p)
	}
	if p, ok := like("title"); ok {
		where += " AND lower(t.title)" + searchLike
		args = append(args, p)
	}
	if p, ok := like("any"); ok {
		where += " AND (lower(t.title)" + searchLike + " OR lower(al.title)" +
			searchLike + " OR lower(ar.name)" + searchLike + ")"
		args = append(args, p, p, p)
	}
	if since, ok := c.millisParam("newerThan"); ok {
		where += " AND EXISTS (SELECT 1 FROM streamline_music_files nf" +
			" WHERE nf.track_id = t.id AND nf.missing = 0 AND " +
			coreMillis("nf.added_at") + " > ?)"
		args = append(args, since.UnixMilli())
	}
	total, err := browseSongCountOf(where, args)
	if err != nil {
		return c.failErr(err)
	}
	offset := c.intParam("offset", 0, 0, browseMaxOffset)
	songs, err := browseSongs(browseSongQuery{
		where:  where,
		args:   args,
		order:  "t.title, t.id",
		limit:  c.intParam("count", browsePageSize, 1, browseMaxPage),
		offset: offset,
	})
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{SearchResult: &searchResult{
		Offset: offset, TotalHits: total, Match: orEmpty(songs),
	}})
}

// browseSongCountOf counts the playable tracks matching where.
func browseSongCountOf(where string, args []any) (int, error) {
	r, _, err := queryRow(
		`SELECT count(t.id) FROM streamline_tracks t
		JOIN streamline_albums al ON al.id = t.album_id
		JOIN streamline_artists ar ON ar.id = al.artist_id
		WHERE `+browsePlayable("t")+" AND "+where,
		args...,
	)
	if err != nil || r == nil {
		return 0, err
	}
	return int(r.int(0)), nil
}
