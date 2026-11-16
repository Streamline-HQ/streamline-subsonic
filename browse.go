package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic-ext/envelope"
	"github.com/streamline-hq/streamline-subsonic-ext/ids"
)

const (
	browsePageSize = 20
	browseListSize = 10
	browseMaxList  = 500
	browseMaxPage  = 100
	// browseSimilar is getArtistInfo's count default and the similar and top
	// songs' one.
	browseSimilar   = 20
	browseSongCount = 50
	browseMaxOffset = 1 << 30
)

func init() {
	register("getArtists", getArtists)
	register("getIndexes", getIndexes)
	register("getArtist", getArtist)
	register("getAlbum", getAlbum)
	register("getSong", getSong)
	register("getMusicDirectory", getMusicDirectory)
	register("getGenres", getGenres)
	register("getArtistInfo", getArtistInfo)
	register("getArtistInfo2", getArtistInfo2)
	register("getAlbumInfo", getAlbumInfo)
	register("getAlbumInfo2", getAlbumInfo)
	register("getSimilarSongs", getSimilarSongs)
	register("getSimilarSongs2", getSimilarSongs2)
	register("getTopSongs", getTopSongs)
}

// What a client can play: a track with a file that is still on disk. These
// are fragments of constant text, joined with an alias the caller owns.
func browsePlayable(t string) string {
	return "EXISTS (SELECT 1 FROM streamline_music_files pf WHERE pf.track_id = " +
		t + ".id AND pf.missing = 0)"
}

// The library holds every release group of a followed artist, wanted or not,
// and a client shows each one it has no file for as an empty album, so an
// album is listed only when one of its tracks is playable.
func browseListedAlbum(a string) string {
	return "EXISTS (SELECT 1 FROM streamline_tracks lt WHERE lt.album_id = " +
		a + ".id AND " + browsePlayable("lt") + ")"
}

// browseListedArtist keeps an artist with at least one listed album, owned or
// co-credited.
func browseListedArtist(ar string) string {
	return "EXISTS (SELECT 1 FROM streamline_album_artists la " +
		"JOIN streamline_albums lal ON lal.id = la.album_id " +
		"WHERE la.artist_id = " + ar + ".id AND " + browseListedAlbum("lal") + ")"
}

// browseOfArtist is true for an album the artist owns or is credited on.
func browseOfArtist(a string) string {
	return "EXISTS (SELECT 1 FROM streamline_album_artists oa WHERE oa.album_id = " +
		a + ".id AND oa.artist_id = ?)"
}

// browseGenreList is album a's genres as a JSON array: the release group's
// top genres the core stores, an empty array when it holds none or anything
// else than an array.
func browseGenreList(a string) string {
	return "(CASE WHEN json_valid(" + a + ".genres) AND json_type(" + a +
		".genres) = 'array' THEN " + a + ".genres ELSE '[]' END)"
}

// browseAlbumGenre is the one genre an album or its songs show: the album's
// first own genre, else the genre of artist ar it is filed under.
func browseAlbumGenre(a, ar string) string {
	return "COALESCE((SELECT gl.value FROM json_each(" + browseGenreList(a) +
		") gl WHERE gl.type = 'text' AND gl.value <> '' ORDER BY gl.key LIMIT 1), " +
		ar + ".genre, '')"
}

// browseHasGenre is true for an album al in the genre its two placeholders
// name (pass the genre twice), whatever its case: one of the album's own
// genres, or, for an album with none, the genre of an artist it is listed
// under, owner or co-credited.
var browseHasGenre = "(EXISTS (SELECT 1 FROM json_each(" + browseGenreList(
	"al",
) + ") gh " +
	"WHERE gh.type = 'text' AND lower(gh.value) = lower(?)) OR (" +
	"json_array_length(" + browseGenreList(
	"al",
) + ") = 0 AND EXISTS (" +
	"SELECT 1 FROM streamline_album_artists ga " +
	"JOIN streamline_artists gx ON gx.id = ga.artist_id " +
	"WHERE ga.album_id = al.id AND lower(gx.genre) = lower(?))))"

// browseOtherFolder reports a musicFolderId naming a folder this server does
// not have: the library is the one folder, id 1.
func browseOtherFolder(c *reqCtx) bool {
	folder := c.param("musicFolderId")
	return folder != "" && folder != "1"
}

// browseLike is the LIKE pattern matching text that contains s, whatever its
// case; the statements using it declare ESCAPE '\'.
func browseLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.ToLower(s)) + "%"
}

type browseArtist struct {
	id         uint32
	mbid       string
	name       string
	sortName   string
	genre      string
	overview   string
	overviewFR string
	updated    string
	albumCount int
}

var browseArtistColumns = `ar.id, ar.mbid, ar.name, ar.sort_name, ar.genre,
	ar.overview, ar.updated_at,
	(SELECT count(DISTINCT ca.album_id) FROM streamline_album_artists ca
		JOIN streamline_albums cal ON cal.id = ca.album_id
		WHERE ca.artist_id = ar.id AND ` + browseListedAlbum("cal") + `),
	ar.overview_fr`

// biography is the English overview, else the French one.
func (a browseArtist) biography() string {
	if a.overview != "" {
		return a.overview
	}
	return a.overviewFR
}

func scanBrowseArtist(r row) browseArtist {
	return browseArtist{
		id: r.uint(0), mbid: r.str(1), name: r.str(2), sortName: r.str(3),
		genre: r.str(4), overview: r.str(5), updated: r.str(6),
		albumCount: int(r.int(7)), overviewFR: r.str(8),
	}
}

// browseArtists reads artists matching where (SQL over alias ar, constant
// text), in order, with a limit when it is positive.
func browseArtists(
	where string,
	args []any,
	order string,
	limit, offset int,
) ([]browseArtist, error) {
	sql := "SELECT " + browseArtistColumns + " FROM streamline_artists ar WHERE " +
		where + " ORDER BY " + order
	if limit > 0 {
		sql += " LIMIT ? OFFSET ?"
		args = append(slices.Clone(args), limit, offset)
	}
	rows, err := query(sql, args...)
	if err != nil {
		return nil, err
	}
	out := make([]browseArtist, len(rows))
	for i, r := range rows {
		out[i] = scanBrowseArtist(r)
	}
	return out, nil
}

func (a browseArtist) toID3() artistID3 {
	return artistID3{
		ID:            ids.Artist(a.id),
		Name:          a.name,
		AlbumCount:    a.albumCount,
		CoverArt:      ids.Artist(a.id),
		MusicBrainzID: a.mbid,
		SortName:      a.sortName,
		MediaType:     "artist",
	}
}

func browseArtistsOf(ars []browseArtist) []artistID3 {
	out := make([]artistID3, 0, len(ars))
	for _, a := range ars {
		out = append(out, a.toID3())
	}
	return out
}

type browseAlbum struct {
	id         uint32
	mbid       string
	title      string
	released   string
	added      string
	artistID   uint32
	artistName string
	genre      string
	songs      int
	duration   uint32
}

type browseAlbumQuery struct {
	join  string
	where string
	args  []any
	order string
	limit int
	// offset applies with limit.
	offset int
}

var browseAlbumColumns = `al.id, al.mbid, al.title, al.release_date, al.added_at,
	ar.id, ar.name, ` + browseAlbumGenre("al", "ar") + `,
	(SELECT count(t.id) FROM streamline_tracks t
		WHERE t.album_id = al.id AND ` + browsePlayable("t") + `),
	(SELECT COALESCE(sum(t.duration), 0) FROM streamline_tracks t
		WHERE t.album_id = al.id AND ` + browsePlayable("t") + `)`

// browseAlbums reads albums with the artist each is filed under. The query's
// join, where and order are constant text, its values are arguments, in the
// order the placeholders appear: join, where, then limit and offset.
func browseAlbums(q browseAlbumQuery) ([]browseAlbum, error) {
	sql := "SELECT " + browseAlbumColumns + " FROM streamline_albums al " +
		"JOIN streamline_artists ar ON ar.id = al.artist_id " + q.join +
		" WHERE " + q.where + " ORDER BY " + q.order
	args := slices.Clone(q.args)
	if q.limit > 0 {
		sql += " LIMIT ? OFFSET ?"
		args = append(args, q.limit, q.offset)
	}
	rows, err := query(sql, args...)
	if err != nil {
		return nil, err
	}
	out := make([]browseAlbum, len(rows))
	for i, r := range rows {
		out[i] = browseAlbum{
			id: r.uint(0), mbid: r.str(1), title: r.str(2), released: r.str(3),
			added: r.str(4), artistID: r.uint(5), artistName: r.str(6),
			genre: r.str(7), songs: int(r.int(8)), duration: r.uint(9),
		}
	}
	return out, nil
}

// browseYear is the year a stored release date starts with, 0 when there is
// none.
func browseYear(released string) uint16 {
	if len(released) < 4 {
		return 0
	}
	y, err := strconv.Atoi(released[:4])
	if err != nil {
		return 0
	}
	return uint16(min(max(y, 0), math.MaxUint16))
}

func (a browseAlbum) toID3() albumID3 {
	return albumID3{
		ID:            ids.Album(a.id),
		Name:          a.title,
		Artist:        a.artistName,
		ArtistID:      ids.Artist(a.artistID),
		CoverArt:      ids.Album(a.id),
		SongCount:     a.songs,
		Duration:      a.duration,
		Year:          browseYear(a.released),
		Genre:         a.genre,
		Created:       timestamp(parseTime(a.added)),
		MusicBrainzID: a.mbid,
		MediaType:     "album",
	}
}

// toChild is an album in the folder-style responses: a directory under its
// artist.
func (a browseAlbum) toChild() child {
	id3 := a.toID3()
	return child{
		ID:            id3.ID,
		Parent:        id3.ArtistID,
		IsDir:         true,
		Title:         id3.Name,
		Album:         id3.Name,
		Artist:        id3.Artist,
		Year:          id3.Year,
		Genre:         id3.Genre,
		CoverArt:      id3.CoverArt,
		Duration:      id3.Duration,
		Created:       id3.Created,
		AlbumID:       id3.ID,
		ArtistID:      id3.ArtistID,
		MediaType:     "album",
		MusicBrainzID: id3.MusicBrainzID,
	}
}

func browseAlbumsOf(als []browseAlbum) []albumID3 {
	out := make([]albumID3, 0, len(als))
	for _, a := range als {
		out = append(out, a.toID3())
	}
	return out
}

func browseAlbumChildren(als []browseAlbum) []child {
	out := make([]child, 0, len(als))
	for _, a := range als {
		out = append(out, a.toChild())
	}
	return out
}

// browseIgnoredArticles are the leading words the index files an artist
// past, "The Beatles" under B, as getIndexes and getArtists advertise them.
const browseIgnoredArticles = "The El La Los Las Le Les"

// browseSortKey is what the index sorts and files an artist by: its sort
// name, else its name, either without a leading article. A MusicBrainz sort
// name has already moved it ("Beatles, The"), so this only matters for a
// name or a sort name that still starts with one.
func browseSortKey(a browseArtist) string {
	key := a.sortName
	if key == "" {
		key = a.name
	}
	return browseStripArticle(key)
}

func browseStripArticle(name string) string {
	for article := range strings.FieldsSeq(browseIgnoredArticles) {
		n := len(article)
		if len(name) > n+1 && name[n] == ' ' &&
			strings.EqualFold(name[:n], article) {
			if rest := strings.TrimSpace(name[n+1:]); rest != "" {
				return rest
			}
		}
	}
	return name
}

func browseIndexLetter(key string) string {
	if r, _ := utf8.DecodeRuneInString(key); unicode.IsLetter(r) {
		return string(unicode.ToUpper(r))
	}
	return "#"
}

func browseSortedArtists() ([]browseArtist, error) {
	artists, err := browseArtists(browseListedArtist("ar"), nil, "ar.id", 0, 0)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(artists, func(i, j int) bool {
		return strings.ToLower(browseSortKey(artists[i])) <
			strings.ToLower(browseSortKey(artists[j]))
	})
	return artists, nil
}

func browseGroupArtists(artists []browseArtist) []index {
	out := []index{}
	for _, ar := range artists {
		letter := browseIndexLetter(browseSortKey(ar))
		if n := len(out); n == 0 || out[n-1].Name != letter {
			out = append(out, index{Name: letter, Artist: []artistID3{}})
		}
		last := &out[len(out)-1]
		last.Artist = append(last.Artist, ar.toID3())
	}
	return out
}

func getArtists(c *reqCtx) sdk.Response {
	artists, err := browseSortedArtists()
	if err != nil {
		return c.failErr(err)
	}
	if browseOtherFolder(c) {
		artists = nil
	}
	return c.respond(&body{Artists: &indexes{
		IgnoredArticles: browseIgnoredArticles,
		Index:           browseGroupArtists(artists),
	}})
}

// getIndexes is the folder-based twin of getArtists: the library has the one
// music folder, so the same artist grouping answers it. With
// ifModifiedSince at or after the index's lastModified it answers the
// element with no index, as nothing changed since the client's copy.
func getIndexes(c *reqCtx) sdk.Response {
	artists, err := browseSortedArtists()
	if err != nil {
		return c.failErr(err)
	}
	modified, err := browseIndexModified(artists)
	if err != nil {
		return c.failErr(err)
	}
	out := &folderIndexes{
		IgnoredArticles: browseIgnoredArticles,
		LastModified:    modified,
		Index:           []index{},
	}
	since, sent := c.millisParam("ifModifiedSince")
	unchanged := sent && modified <= since.UnixMilli()
	if !browseOtherFolder(c) && !unchanged {
		out.Index = browseGroupArtists(artists)
	}
	return c.respond(&body{Indexes: out})
}

// browseIndexModified is when the index last changed, epoch ms. A core time
// moves when an artist, album or track row changes, but nothing records an
// artist leaving the library (its row deleted, or its last file gone from
// disk), so the index is fingerprinted by what it lists and the time kept in
// library_index moves to now whenever the fingerprint does. The first time
// it is seen counts as now too: a client may hold a copy from before. A
// listener's own marks (stars, ratings) are not part of it.
func browseIndexModified(artists []browseArtist) (int64, error) {
	h := sha256.New()
	for _, a := range artists {
		_, _ = fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00%d\n",
			a.id, a.name, a.sortName, a.mbid, a.albumCount)
	}
	fingerprint := hex.EncodeToString(h.Sum(nil))
	r, found, err := queryRow(
		"SELECT fingerprint, changed_ms FROM library_index WHERE id = 1",
	)
	if err != nil {
		return 0, err
	}
	if found && r.str(0) == fingerprint {
		return r.int(1), nil
	}
	newest, err := browseLibraryModified(artists)
	if err != nil {
		return 0, err
	}
	changed := max(time.Now().UnixMilli(), newest)
	if found {
		changed = max(changed, r.int(1)+1)
	}
	if _, err := exec(
		`INSERT INTO library_index(id, fingerprint, changed_ms) VALUES(1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET fingerprint = excluded.fingerprint,
			changed_ms = excluded.changed_ms`,
		fingerprint, changed,
	); err != nil {
		return 0, err
	}
	return changed, nil
}

// browseLibraryModified is the newest change to anything an index entry
// reflects: an artist row, or an album or track under it (album_count, a new
// release).
func browseLibraryModified(artists []browseArtist) (int64, error) {
	var newest int64
	for _, ar := range artists {
		newest = max(newest, parseTime(ar.updated).UnixMilli())
	}
	for _, table := range []string{"streamline_albums", "streamline_tracks"} {
		r, found, err := queryRow("SELECT max(updated_at) FROM " + table)
		if err != nil {
			return 0, err
		}
		if found {
			newest = max(newest, r.time(0).UnixMilli())
		}
	}
	return newest, nil
}

func browseLoadArtist(id uint32) (browseArtist, error) {
	artists, err := browseArtists("ar.id = ?", []any{id}, "ar.id", 0, 0)
	if err != nil {
		return browseArtist{}, err
	}
	if len(artists) == 0 {
		return browseArtist{}, envelope.NotFound()
	}
	return artists[0], nil
}

// browseArtistAlbums is an artist's listed albums, owned and co-credited,
// oldest first with the undated before them.
func browseArtistAlbums(artistID uint32) ([]browseAlbum, error) {
	return browseAlbums(browseAlbumQuery{
		where: browseOfArtist("al") + " AND " + browseListedAlbum("al"),
		args:  []any{artistID},
		order: "al.release_date, al.title, al.id",
	})
}

func browseLoadAlbum(id uint32) (browseAlbum, error) {
	albums, err := browseAlbums(browseAlbumQuery{
		where: "al.id = ?", args: []any{id}, order: "al.id",
	})
	if err != nil {
		return browseAlbum{}, err
	}
	if len(albums) == 0 {
		return browseAlbum{}, envelope.NotFound()
	}
	return albums[0], nil
}

func browseAlbumSongs(albumID uint32) ([]child, error) {
	return songChildren(songQuery{
		where: "t.album_id = ?",
		args:  []any{albumID},
		order: "t.disc, t.position, t.id",
	})
}

func getArtist(c *reqCtx) sdk.Response {
	id, err := c.idParam("id", ids.KindArtist)
	if err != nil {
		return c.failErr(err)
	}
	ar, err := browseLoadArtist(id)
	if err != nil {
		return c.failErr(err)
	}
	albums, err := browseArtistAlbums(id)
	if err != nil {
		return c.failErr(err)
	}
	ar.albumCount = len(albums)
	return c.respond(&body{Artist: &artistWith{
		artistID3: ar.toID3(),
		Album:     browseAlbumsOf(albums),
	}})
}

func getAlbum(c *reqCtx) sdk.Response {
	id, err := c.idParam("id", ids.KindAlbum)
	if err != nil {
		return c.failErr(err)
	}
	al, err := browseLoadAlbum(id)
	if err != nil {
		return c.failErr(err)
	}
	songs, err := browseAlbumSongs(id)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{Album: &albumWith{albumID3: al.toID3(), Song: songs}})
}

func getSong(c *reqCtx) sdk.Response {
	id, err := c.idParam("id", ids.KindTrack)
	if err != nil {
		return c.failErr(err)
	}
	songs, err := songChildren(songQuery{where: "t.id = ?", args: []any{id}})
	if err != nil {
		return c.failErr(err)
	}
	if len(songs) == 0 {
		return c.failErr(envelope.NotFound())
	}
	return c.respond(&body{Song: &songs[0]})
}

// getMusicDirectory walks the folder view: an artist holds its albums, an
// album its songs.
func getMusicDirectory(c *reqCtx) sdk.Response {
	kind, id, err := c.anyIDParam("id")
	if err != nil {
		return c.failErr(err)
	}
	switch kind {
	case ids.KindArtist:
		ar, err := browseLoadArtist(id)
		if err != nil {
			return c.failErr(err)
		}
		albums, err := browseArtistAlbums(id)
		if err != nil {
			return c.failErr(err)
		}
		dir := &directory{ID: ids.Artist(ar.id), Name: ar.name, Child: []child{}}
		for _, al := range albums {
			ch := al.toChild()
			ch.Parent = dir.ID
			dir.Child = append(dir.Child, ch)
		}
		return c.respond(&body{Directory: dir})
	case ids.KindAlbum:
		al, err := browseLoadAlbum(id)
		if err != nil {
			return c.failErr(err)
		}
		songs, err := browseAlbumSongs(id)
		if err != nil {
			return c.failErr(err)
		}
		return c.respond(&body{Directory: &directory{
			ID:     ids.Album(al.id),
			Parent: ids.Artist(al.artistID),
			Name:   al.title,
			Child:  songs,
		}})
	default:
		return c.failErr(envelope.NotFound())
	}
}

// browseAlbumGenres is every listed album with each genre it is in, one row
// per album and genre (column gkey the genre folded to lower case, name its
// spelling): the album's own genres, else those of the artists it is listed
// under, as browseHasGenre matches them.
var browseAlbumGenres = `WITH ag AS (
	SELECT al.id AS album_id, lower(g.value) AS gkey, g.value AS name
	FROM streamline_albums al, json_each(` + browseGenreList("al") + `) g
	WHERE g.type = 'text' AND g.value <> '' AND ` + browseListedAlbum("al") + `
	UNION
	SELECT al.id, lower(gx.genre), gx.genre
	FROM streamline_albums al
	JOIN streamline_album_artists ga ON ga.album_id = al.id
	JOIN streamline_artists gx ON gx.id = ga.artist_id
	WHERE json_array_length(` + browseGenreList("al") + `) = 0
		AND gx.genre <> '' AND ` + browseListedAlbum("al") + `
), bg AS (
	SELECT album_id, gkey, min(name) AS name FROM ag GROUP BY album_id, gkey
)`

// getGenres lists the genres of the listed albums: an album's own (the
// release group's top genres on MusicBrainz), else its artists'. An album
// counts once per genre, under every artist it is listed under, and a genre
// no listed album is in is left out.
func getGenres(c *reqCtx) sdk.Response {
	rows, err := query(browseAlbumGenres + `
		SELECT min(bg.name), count(*),
			(SELECT count(t.id) FROM streamline_tracks t
				JOIN bg st ON st.album_id = t.album_id
				WHERE st.gkey = bg.gkey AND ` + browsePlayable("t") + `)
		FROM bg GROUP BY bg.gkey`)
	if err != nil {
		return c.failErr(err)
	}
	out := &genres{Genre: make([]genre, 0, len(rows))}
	for _, r := range rows {
		out.Genre = append(out.Genre, genre{
			Value: r.str(0), AlbumCount: int(r.int(1)), SongCount: int(r.int(2)),
		})
	}
	sort.Slice(out.Genre, func(i, j int) bool {
		return strings.ToLower(out.Genre[i].Value) <
			strings.ToLower(out.Genre[j].Value)
	})
	return c.ok(&body{Genres: out})
}

// browseArtistOf resolves an artist, album or song id to its artist, as
// getArtistInfo and the similar-songs calls accept any of the three.
func browseArtistOf(c *reqCtx) (browseArtist, error) {
	kind, id, err := c.anyIDParam("id")
	if err != nil {
		return browseArtist{}, err
	}
	var where string
	switch kind {
	case ids.KindArtist:
		where = "ar.id = ?"
	case ids.KindAlbum:
		where = "ar.id = (SELECT artist_id FROM streamline_albums WHERE id = ?)"
	case ids.KindTrack:
		where = `ar.id = (SELECT x.artist_id FROM streamline_albums x
			JOIN streamline_tracks xt ON xt.album_id = x.id WHERE xt.id = ?)`
	default:
		return browseArtist{}, envelope.NotFound()
	}
	artists, err := browseArtists(where, []any{id}, "ar.id", 0, 0)
	if err != nil {
		return browseArtist{}, err
	}
	if len(artists) == 0 {
		return browseArtist{}, envelope.NotFound()
	}
	return artists[0], nil
}

// browseRelated is the ids of the artists the core relates to the seed
// artist, constant SQL taking the seed's id five times then its MusicBrainz
// id twice (browseRelatedArgs): those it shares a release with (a split or a
// collaboration), those sharing a band member, a member who is an artist of
// the library or a band the seed belongs to, and the artists credited on each
// other's releases (track artist, featuring, performer).
const browseRelated = `SELECT s2.artist_id FROM streamline_album_artists s1
	JOIN streamline_album_artists s2 ON s2.album_id = s1.album_id
	WHERE s1.artist_id = ?
UNION SELECT m2.artist_id FROM streamline_artist_members m1
	JOIN streamline_artist_members m2 ON m2.mbid = m1.mbid
	WHERE m1.artist_id = ? AND m1.mbid <> ''
UNION SELECT x.id FROM streamline_artist_members m
	JOIN streamline_artists x ON x.mbid = m.mbid
	WHERE m.artist_id = ? AND m.mbid <> ''
UNION SELECT x.id FROM streamline_music_credits mc
	JOIN streamline_artists x ON x.mbid = mc.mbid
	WHERE mc.mbid <> '' AND mc.kind IN ('artist', 'featuring', 'performer')
		AND (mc.album_id IN (SELECT album_id FROM streamline_album_artists
				WHERE artist_id = ?)
			OR mc.track_id IN (SELECT rt.id FROM streamline_tracks rt
				JOIN streamline_album_artists ra ON ra.album_id = rt.album_id
				WHERE ra.artist_id = ?))
UNION SELECT m.artist_id FROM streamline_artist_members m
	WHERE m.mbid = ? AND m.mbid <> ''
UNION SELECT ca.artist_id FROM streamline_music_credits mc
	LEFT JOIN streamline_tracks ct ON ct.id = mc.track_id
	JOIN streamline_album_artists ca
		ON ca.album_id = COALESCE(mc.album_id, ct.album_id)
	WHERE mc.mbid = ? AND mc.mbid <> ''
		AND mc.kind IN ('artist', 'featuring', 'performer')`

func browseRelatedArgs(ar browseArtist) []any {
	return []any{ar.id, ar.id, ar.id, ar.id, ar.id, ar.mbid, ar.mbid}
}

// browseSimilarArtists are the artists the core relates to ar
// (browseRelated), then, to fill n, others sharing its genre; each part in a
// random order, so a client's "similar" row does not always show the same
// few. The core holds no listening-based similarity such as last.fm's.
func browseSimilarArtists(ar browseArtist, n int) ([]browseArtist, error) {
	if n == 0 {
		return nil, nil
	}
	related, err := browseArtists(
		"ar.id IN ("+browseRelated+") AND ar.id <> ? AND "+browseListedArtist("ar"),
		append(browseRelatedArgs(ar), ar.id), "RANDOM()", n, 0,
	)
	if err != nil || len(related) >= n || ar.genre == "" {
		return related, err
	}
	same, err := browseArtists(
		"ar.genre = ? AND ar.id <> ? AND ar.id NOT IN ("+browseRelated+") AND "+
			browseListedArtist("ar"),
		append([]any{ar.genre, ar.id}, browseRelatedArgs(ar)...),
		"RANDOM()", n-len(related), 0,
	)
	if err != nil {
		return nil, err
	}
	return append(related, same...), nil
}

func browseArtistInfoParts(c *reqCtx) (artistInfoBase, []artistID3, error) {
	ar, err := browseArtistOf(c)
	if err != nil {
		return artistInfoBase{}, nil, err
	}
	similar, err := browseSimilarArtists(
		ar, c.intParam("count", browseSimilar, 0, browseMaxPage),
	)
	if err != nil {
		return artistInfoBase{}, nil, err
	}
	return artistInfoBase{Biography: ar.biography(), MusicBrainzID: ar.mbid},
		browseArtistsOf(similar), nil
}

func getArtistInfo(c *reqCtx) sdk.Response {
	base, similar, err := browseArtistInfoParts(c)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{
		ArtistInfo: &artistInfo{artistInfoBase: base, SimilarArtist: similar},
	})
}

func getArtistInfo2(c *reqCtx) sdk.Response {
	base, similar, err := browseArtistInfoParts(c)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{
		ArtistInfo2: &artistInfo2{artistInfoBase: base, SimilarArtist: similar},
	})
}

// getAlbumInfo answers both getAlbumInfo and getAlbumInfo2: the two share a
// response. An album has no stored notes, only its MusicBrainz id.
func getAlbumInfo(c *reqCtx) sdk.Response {
	kind, id, err := c.anyIDParam("id")
	if err != nil {
		return c.failErr(err)
	}
	var where string
	switch kind {
	case ids.KindAlbum:
		where = "al.id = ?"
	case ids.KindTrack:
		where = "al.id = (SELECT album_id FROM streamline_tracks WHERE id = ?)"
	default:
		return c.failErr(envelope.NotFound())
	}
	albums, err := browseAlbums(browseAlbumQuery{
		where: where, args: []any{id}, order: "al.id",
	})
	if err != nil {
		return c.failErr(err)
	}
	if len(albums) == 0 {
		return c.failErr(envelope.NotFound())
	}
	return c.ok(&body{AlbumInfo: &albumInfo{MusicBrainzID: albums[0].mbid}})
}

// browseSimilarSongs is a random mix of the seed artist's songs, those of
// the artists the core relates to it (browseRelated) and those of the albums
// in its genre: what a client's "instant mix" plays.
func browseSimilarSongs(c *reqCtx) ([]child, error) {
	ar, err := browseArtistOf(c)
	if err != nil {
		return nil, err
	}
	where := "(" + browseOfArtist("al") + ` OR EXISTS (
		SELECT 1 FROM streamline_album_artists rs
		WHERE rs.album_id = al.id AND rs.artist_id IN (` + browseRelated + `))`
	args := append([]any{ar.id}, browseRelatedArgs(ar)...)
	if ar.genre != "" {
		where += " OR " + browseHasGenre
		args = append(args, ar.genre, ar.genre)
	}
	return songChildren(songQuery{
		where: where + ")",
		args:  args,
		order: "RANDOM()",
		limit: c.intParam("count", browseSongCount, 1, browseMaxList),
	})
}

func getSimilarSongs(c *reqCtx) sdk.Response {
	s, err := browseSimilarSongs(c)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{SimilarSongs: &songs{Song: s}})
}

func getSimilarSongs2(c *reqCtx) sdk.Response {
	s, err := browseSimilarSongs(c)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{SimilarSongs2: &songs{Song: s}})
}

// topSongsPlays is the plays of every track, summed over every listener.
const topSongsPlays = `JOIN (SELECT target_id AS tid, sum(play_count) AS plays
	FROM annotations WHERE target_kind = 'tr' AND play_count > 0
	GROUP BY target_id) pl ON pl.tid = t.id`

// getTopSongs ranks an artist's songs by plays summed over every listener.
// A song nobody played is not a top song, so a fresh library answers none.
// The name matches whatever its case, in any script (SQLite's lower() folds
// ASCII only, so the comparison is made here); of two artists of that name,
// the one whose songs were played most, then the one with songs to play,
// answers.
func getTopSongs(c *reqCtx) sdk.Response {
	name := strings.TrimSpace(c.param("artist"))
	if name == "" {
		return c.failErr(envelope.MissingParam("artist"))
	}
	limit := c.intParam("count", browseSongCount, 1, browseMaxList)
	out := &songs{Song: []child{}}
	artistID, found, err := topSongsArtist(name)
	if err != nil {
		return c.failErr(err)
	}
	if !found {
		return c.respond(&body{TopSongs: out})
	}
	ranked, err := songChildren(songQuery{
		join:  topSongsPlays,
		where: browseOfArtist("al"),
		args:  []any{artistID},
		order: "pl.plays DESC, t.id",
		limit: limit,
	})
	if err != nil {
		return c.failErr(err)
	}
	out.Song = ranked
	return c.respond(&body{TopSongs: out})
}

// topSongsArtist is the artist getTopSongs answers for name.
func topSongsArtist(name string) (id uint32, found bool, err error) {
	rows, err := query("SELECT id, name FROM streamline_artists ORDER BY id")
	if err != nil {
		return 0, false, err
	}
	var candidates []uint32
	for _, r := range rows {
		if strings.EqualFold(strings.TrimSpace(r.str(1)), name) {
			candidates = append(candidates, r.uint(0))
		}
	}
	switch len(candidates) {
	case 0:
		return 0, false, nil
	case 1:
		return candidates[0], true, nil
	}
	ranked, err := queryIn(
		`SELECT cand.id FROM streamline_artists cand WHERE cand.id IN ({in})
		ORDER BY (SELECT COALESCE(sum(pl.plays), 0) FROM streamline_tracks t
				JOIN streamline_albums al ON al.id = t.album_id `+topSongsPlays+`
				WHERE EXISTS (SELECT 1 FROM streamline_album_artists oa
					WHERE oa.album_id = al.id AND oa.artist_id = cand.id)) DESC,
			`+browseListedArtist("cand")+` DESC, cand.id`,
		candidates,
	)
	if err != nil {
		return 0, false, err
	}
	if len(ranked) == 0 {
		return candidates[0], true, nil
	}
	return ranked[0].uint(0), true, nil
}
