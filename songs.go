package main

import (
	"math"
	"path"
	"slices"
	"strings"

	"github.com/streamline-hq/streamline-subsonic/ids"
)

var songContentTypes = map[string]string{
	"flac": "audio/flac",
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/opus",
	"aac":  "audio/aac",
}

func songContentType(suffix string) string {
	if ct, ok := songContentTypes[suffix]; ok {
		return ct
	}
	return "application/octet-stream"
}

func songSuffix(filePath, format string) string {
	if s := strings.TrimPrefix(strings.ToLower(path.Ext(filePath)), "."); s != "" {
		return s
	}
	if format == "other" {
		return ""
	}
	return format
}

// songKbps is the file's probed bitrate (bits per second) in kbps, else one
// estimated from its size over duration seconds; 0 when neither is known.
func songKbps(bitrate, size int64, duration uint32) uint32 {
	if kbps := bitrate / 1000; kbps > 0 || duration == 0 || size <= 0 {
		return uint32(min(max(kbps, 0), math.MaxUint32))
	}
	est := min(size*8/int64(duration)/1000, math.MaxUint32)
	return uint32(est) //nolint:gosec // clamped above
}

// songQuery selects playable tracks: those with a file on disk, the first of
// which is the one offered.
type songQuery struct {
	join  string
	where string
	args  []any
	order string
	limit int
	// offset applies with limit.
	offset int
}

// The first file a track still has on disk is the one a client is offered.
const songSQL = `SELECT t.id, t.mbid, t.title, t.disc, t.position, t.duration,
	t.artist_credit, f.added_at,
	al.id, al.title, al.release_date,
	ar.id, ar.name, ar.genre,
	f.path, f.size, f.format, f.duration, f.bitrate
FROM streamline_tracks t
JOIN streamline_albums al ON al.id = t.album_id
JOIN streamline_artists ar ON ar.id = al.artist_id
JOIN streamline_music_files f ON f.id = (
	SELECT min(pf.id) FROM streamline_music_files pf
	WHERE pf.track_id = t.id AND pf.missing = 0)`

// song is a track as every response renders it, with the facts a share page
// needs besides.
type song struct {
	child
	trackID uint32
	albumID uint32
	// owner is the album's artist, which a split release's song is not
	// named by.
	owner string
}

// loadSongs reads the playable tracks matching q. A track with no file on
// disk is never returned.
func loadSongs(q songQuery) ([]song, error) {
	sql := songSQL + " " + q.join + " WHERE " + q.where
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
	return songsOfRows(rows)
}

// loadSongsByID reads the playable tracks among trackIDs.
func loadSongsByID(trackIDs []uint32) ([]song, error) {
	rows, err := queryIn(songSQL+" WHERE t.id IN ({in})", trackIDs)
	if err != nil {
		return nil, err
	}
	return songsOfRows(rows)
}

func songsOfRows(rows []row) ([]song, error) {
	out := make([]song, len(rows))
	var split []uint32
	for i, r := range rows {
		albumID, owner := r.uint(8), r.str(12)
		suffix := songSuffix(r.str(14), r.str(16))
		duration := r.uint(17)
		if duration == 0 {
			duration = r.uint(5)
		}
		size := r.int(15)
		c := child{
			ID:            ids.Track(r.uint(0)),
			Parent:        ids.Album(albumID),
			Title:         r.str(2),
			Album:         r.str(9),
			Artist:        owner,
			Track:         uint16(min(max(r.int(4), 0), math.MaxUint16)),
			DiscNumber:    uint8(min(max(r.int(3), 0), math.MaxUint8)),
			Year:          browseYear(r.str(10)),
			Genre:         r.str(13),
			CoverArt:      ids.Album(albumID),
			Size:          size,
			ContentType:   songContentType(suffix),
			Suffix:        suffix,
			Duration:      duration,
			BitRate:       songKbps(r.int(18), size, duration),
			Created:       timestamp(r.time(7)),
			AlbumID:       ids.Album(albumID),
			ArtistID:      ids.Artist(r.uint(11)),
			Type:          "music",
			MediaType:     "song",
			MusicBrainzID: r.str(1),
		}
		out[i] = song{child: c, trackID: r.uint(0), albumID: albumID, owner: owner}
		if credit := r.str(6); songSplit(credit, owner) {
			split = append(split, albumID)
		}
	}
	if len(split) == 0 {
		return out, nil
	}
	return out, creditSplitSongs(rows, out, split)
}

// songSplit holds for a song whose own credit is not its album's owner.
func songSplit(credit, owner string) bool {
	return credit != "" && !strings.EqualFold(credit, owner)
}

// creditSplitSongs names a song of a split release by its own credited
// artist, with that artist's id when the album is listed under them; a song
// credited to the album's owner keeps it.
func creditSplitSongs(rows []row, songs []song, albumIDs []uint32) error {
	slices.Sort(albumIDs)
	albumIDs = slices.Compact(albumIDs)
	co, err := queryIn(
		`SELECT aa.album_id, x.id, x.name FROM streamline_album_artists aa
		JOIN streamline_artists x ON x.id = aa.artist_id
		WHERE aa.owner = 0 AND aa.album_id IN ({in}) ORDER BY x.id`,
		albumIDs,
	)
	if err != nil {
		return err
	}
	for i, r := range rows {
		credit := r.str(6)
		if !songSplit(credit, songs[i].owner) {
			continue
		}
		songs[i].Artist, songs[i].ArtistID = credit, ""
		for _, k := range co {
			if k.uint(0) == songs[i].albumID && strings.EqualFold(k.str(2), credit) {
				songs[i].ArtistID = ids.Artist(k.uint(1))
			}
		}
	}
	return nil
}

// songChildren reads the playable tracks matching q as plain songs.
func songChildren(q songQuery) ([]child, error) {
	found, err := loadSongs(q)
	if err != nil {
		return nil, err
	}
	out := make([]child, len(found))
	for i, s := range found {
		out[i] = s.child
	}
	return out, nil
}

// songsByID renders the playable tracks among trackIDs, keyed by track id; a
// track with no file on disk is absent.
func songsByID(trackIDs []uint32) (map[uint32]child, error) {
	found, err := loadSongsByID(trackIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]child, len(found))
	for _, s := range found {
		out[s.trackID] = s.child
	}
	return out, nil
}

// songsInOrder is the playable tracks among want, in the order of want.
func songsInOrder(want []uint32) ([]song, error) {
	if len(want) == 0 {
		return nil, nil
	}
	found, err := loadSongsByID(want)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint32]song, len(found))
	for _, s := range found {
		byID[s.trackID] = s
	}
	out := make([]song, 0, len(want))
	for _, id := range want {
		if s, ok := byID[id]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}
