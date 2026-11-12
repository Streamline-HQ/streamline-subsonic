package main

import (
	"math"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
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

var browseContentTypes = map[string]string{
	"flac": "audio/flac",
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/opus",
	"aac":  "audio/aac",
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
	updated    string
	albumCount int
}

var browseArtistColumns = `ar.id, ar.mbid, ar.name, ar.sort_name, ar.genre,
	ar.overview, ar.updated_at,
	(SELECT count(DISTINCT ca.album_id) FROM streamline_album_artists ca
		JOIN streamline_albums cal ON cal.id = ca.album_id
		WHERE ca.artist_id = ar.id AND ` + browseListedAlbum("cal") + `)`

func scanBrowseArtist(r row) browseArtist {
	return browseArtist{
		id: r.uint(0), mbid: r.str(1), name: r.str(2), sortName: r.str(3),
		genre: r.str(4), overview: r.str(5), updated: r.str(6),
		albumCount: int(r.int(7)),
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
	ar.id, ar.name, ar.genre,
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

type browseSongQuery struct {
	join  string
	where string
	args  []any
	order string
	limit int
	// offset applies with limit.
	offset int
}

// The first file a track still has on disk is the one a client is offered.
const browseSongSQL = `SELECT t.id, t.mbid, t.title, t.disc, t.position, t.duration,
	t.artist_credit, t.added_at,
	al.id, al.title, al.release_date,
	ar.id, ar.name, ar.genre,
	f.path, f.size, f.format, f.duration, f.bitrate
FROM streamline_tracks t
JOIN streamline_albums al ON al.id = t.album_id
JOIN streamline_artists ar ON ar.id = al.artist_id
JOIN streamline_music_files f ON f.id = (
	SELECT min(pf.id) FROM streamline_music_files pf
	WHERE pf.track_id = t.id AND pf.missing = 0)`

type browseSong struct {
	id         uint32
	mbid       string
	title      string
	disc       int64
	position   int64
	duration   uint32
	credit     string
	added      string
	albumID    uint32
	albumTitle string
	released   string
	artistID   uint32
	artistName string
	genre      string
	path       string
	size       int64
	format     string
	fileSecs   uint32
	bitrate    int64
}

// browseSongs reads the playable tracks matching q as songs. A track with no
// file on disk is never returned.
func browseSongs(q browseSongQuery) ([]child, error) {
	sql := browseSongSQL + " " + q.join + " WHERE " + q.where
	if q.order != "" {
		sql += " ORDER BY " + q.order
	}
	args := slices.Clone(q.args)
	if q.limit > 0 {
		sql += " LIMIT ? OFFSET ?"
		args = append(args, q.limit, q.offset)
	}
	rows, err := query(sql, args...)
	if err != nil {
		return nil, err
	}
	songs := make([]browseSong, len(rows))
	for i, r := range rows {
		songs[i] = browseSong{
			id: r.uint(0), mbid: r.str(1), title: r.str(2), disc: r.int(3),
			position: r.int(4), duration: r.uint(5), credit: r.str(6),
			added: r.str(7), albumID: r.uint(8), albumTitle: r.str(9),
			released: r.str(10), artistID: r.uint(11), artistName: r.str(12),
			genre: r.str(13), path: r.str(14), size: r.int(15), format: r.str(16),
			fileSecs: r.uint(17), bitrate: r.int(18),
		}
	}
	coArtists, err := browseCoArtists(songs)
	if err != nil {
		return nil, err
	}
	out := make([]child, len(songs))
	for i, s := range songs {
		out[i] = s.toChild(coArtists[s.albumID])
	}
	return out, nil
}

type browseCredited struct {
	id   uint32
	name string
}

// browseCoArtists names the other artists of every album a split release's
// song needs one for: a song whose own credit is not its album's owner.
func browseCoArtists(songs []browseSong) (map[uint32][]browseCredited, error) {
	var albums []uint32
	for _, s := range songs {
		if s.credit != "" && !strings.EqualFold(s.credit, s.artistName) &&
			!slices.Contains(albums, s.albumID) {
			albums = append(albums, s.albumID)
		}
	}
	rows, err := queryIn(
		`SELECT aa.album_id, x.id, x.name FROM streamline_album_artists aa
		JOIN streamline_artists x ON x.id = aa.artist_id
		WHERE aa.owner = 0 AND aa.album_id IN ({in}) ORDER BY x.id`,
		albums,
	)
	if err != nil {
		return nil, err
	}
	out := map[uint32][]browseCredited{}
	for _, r := range rows {
		out[r.uint(0)] = append(
			out[r.uint(0)],
			browseCredited{id: r.uint(1), name: r.str(2)},
		)
	}
	return out, nil
}

func browseSuffix(p, format string) string {
	if s := strings.TrimPrefix(strings.ToLower(path.Ext(p)), "."); s != "" {
		return s
	}
	if format == "other" {
		return ""
	}
	return format
}

func browseContentType(suffix string) string {
	if ct, ok := browseContentTypes[suffix]; ok {
		return ct
	}
	return "application/octet-stream"
}

// browseKbps is the file's probed bitrate, else one estimated from its size
// over duration seconds; 0 when neither is known.
func browseKbps(bitrate, size int64, duration uint32) uint32 {
	if kbps := bitrate / 1000; kbps > 0 || duration == 0 || size <= 0 {
		return uint32(min(max(kbps, 0), math.MaxUint32))
	}
	est := min(size*8/int64(duration)/1000, math.MaxUint32)
	return uint32(est) //nolint:gosec // clamped above
}

func (s browseSong) toChild(co []browseCredited) child {
	suffix := browseSuffix(s.path, s.format)
	duration := s.fileSecs
	if duration == 0 {
		duration = s.duration
	}
	c := child{
		ID:            ids.Track(s.id),
		Parent:        ids.Album(s.albumID),
		Title:         s.title,
		Album:         s.albumTitle,
		Artist:        s.artistName,
		Track:         uint16(min(max(s.position, 0), math.MaxUint16)),
		DiscNumber:    uint8(min(max(s.disc, 0), math.MaxUint8)),
		Year:          browseYear(s.released),
		Genre:         s.genre,
		CoverArt:      ids.Album(s.albumID),
		Size:          s.size,
		ContentType:   browseContentType(suffix),
		Suffix:        suffix,
		Duration:      duration,
		BitRate:       browseKbps(s.bitrate, s.size, duration),
		Created:       timestamp(parseTime(s.added)),
		AlbumID:       ids.Album(s.albumID),
		ArtistID:      ids.Artist(s.artistID),
		Type:          "music",
		MediaType:     "song",
		MusicBrainzID: s.mbid,
	}
	// A song of a split release is named by its own credited artist, with
	// that artist's id when the album is listed under them; one credited to
	// the album's owner keeps it.
	if s.credit == "" || strings.EqualFold(s.credit, s.artistName) {
		return c
	}
	c.Artist, c.ArtistID = s.credit, ""
	for _, other := range co {
		if strings.EqualFold(other.name, s.credit) {
			c.ArtistID = ids.Artist(other.id)
		}
	}
	return c
}

func browseSortKey(a browseArtist) string {
	if a.sortName != "" {
		return a.sortName
	}
	return a.name
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
	return c.respond(&body{Artists: &indexes{Index: browseGroupArtists(artists)}})
}

// getIndexes is the folder-based twin of getArtists: the library has the one
// music folder, so the same artist grouping answers it.
//
// ifModifiedSince is read but never short-circuits: no timestamp here moves
// when an artist is deleted (the newest remaining row can be older than the
// client's copy), so answering "unchanged" could leave a client holding a
// removed artist for good. The full index is always a correct answer.
func getIndexes(c *reqCtx) sdk.Response {
	artists, err := browseSortedArtists()
	if err != nil {
		return c.failErr(err)
	}
	modified, err := browseLibraryModified(artists)
	if err != nil {
		return c.failErr(err)
	}
	out := &folderIndexes{LastModified: modified, Index: []index{}}
	if !browseOtherFolder(c) {
		out.Index = browseGroupArtists(artists)
	}
	return c.respond(&body{Indexes: out})
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
	return browseSongs(browseSongQuery{
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
	songs, err := browseSongs(browseSongQuery{where: "t.id = ?", args: []any{id}})
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

// getGenres lists the artists' genres: MusicBrainz names one per artist, and
// its albums and songs inherit it. An artist with no genre is left out.
func getGenres(c *reqCtx) sdk.Response {
	albumRows, err := query(
		`SELECT ar.genre, count(al.id) FROM streamline_albums al
		JOIN streamline_artists ar ON ar.id = al.artist_id
		WHERE ar.genre <> '' AND ` + browseListedAlbum("al") + `
		GROUP BY ar.genre`,
	)
	if err != nil {
		return c.failErr(err)
	}
	songRows, err := query(
		`SELECT ar.genre, count(t.id) FROM streamline_tracks t
		JOIN streamline_albums al ON al.id = t.album_id
		JOIN streamline_artists ar ON ar.id = al.artist_id
		WHERE ar.genre <> '' AND ` + browsePlayable("t") + `
		GROUP BY ar.genre`,
	)
	if err != nil {
		return c.failErr(err)
	}
	songs := make(map[string]int, len(songRows))
	for _, r := range songRows {
		songs[r.str(0)] = int(r.int(1))
	}
	out := &genres{Genre: make([]genre, 0, len(albumRows))}
	for _, r := range albumRows {
		out.Genre = append(out.Genre, genre{
			Value: r.str(0), SongCount: songs[r.str(0)], AlbumCount: int(r.int(1)),
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

// browseSimilarArtists are other artists in the library sharing ar's genre,
// in a random order: the library knows no closer relation, and a shuffle
// keeps a client's "similar" row from always showing the same few.
func browseSimilarArtists(ar browseArtist, n int) ([]browseArtist, error) {
	if ar.genre == "" || n == 0 {
		return nil, nil
	}
	return browseArtists(
		"ar.genre = ? AND ar.id <> ? AND "+browseListedArtist("ar"),
		[]any{ar.genre, ar.id}, "RANDOM()", n, 0,
	)
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
	return artistInfoBase{Biography: ar.overview, MusicBrainzID: ar.mbid},
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

// browseSimilarSongs is a random mix of the seed artist's songs and those of
// the artists sharing its genre: what a client's "instant mix" plays.
func browseSimilarSongs(c *reqCtx) ([]child, error) {
	ar, err := browseArtistOf(c)
	if err != nil {
		return nil, err
	}
	where, args := browseOfArtist("al"), []any{ar.id}
	if ar.genre != "" {
		where = "(" + where + " OR ar.genre = ?)"
		args = append(args, ar.genre)
	}
	return browseSongs(browseSongQuery{
		where: where,
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

// getTopSongs ranks an artist's songs by plays summed over every listener.
// A song nobody played is not a top song, so a fresh library answers none.
func getTopSongs(c *reqCtx) sdk.Response {
	name := c.param("artist")
	if name == "" {
		return c.failErr(envelope.MissingParam("artist"))
	}
	limit := c.intParam("count", browseSongCount, 1, browseMaxList)
	out := &songs{Song: []child{}}
	artists, err := browseArtists(
		"lower(ar.name) = lower(?)", []any{name}, "ar.id", 1, 0,
	)
	if err != nil {
		return c.failErr(err)
	}
	if len(artists) == 0 {
		return c.respond(&body{TopSongs: out})
	}
	ranked, err := browseSongs(browseSongQuery{
		join: `JOIN (SELECT target_id AS tid, sum(play_count) AS plays
			FROM annotations WHERE target_kind = 'tr' AND play_count > 0
			GROUP BY target_id) pl ON pl.tid = t.id`,
		where: browseOfArtist("al"),
		args:  []any{artists[0].id},
		order: "pl.plays DESC, t.id",
		limit: limit,
	})
	if err != nil {
		return c.failErr(err)
	}
	out.Song = ranked
	return c.respond(&body{TopSongs: out})
}
