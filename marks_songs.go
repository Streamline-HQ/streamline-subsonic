package main

import (
	"math"
	"path"
	"slices"
	"strings"

	"github.com/streamline-hq/streamline-subsonic/ids"
)

var marksContentTypes = map[string]string{
	"flac": "audio/flac",
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/opus",
	"aac":  "audio/aac",
}

// marksLiveTrack holds for a track t with a file on disk.
const marksLiveTrack = `EXISTS (SELECT 1 FROM streamline_music_files f
	WHERE f.track_id = t.id AND f.missing = 0)`

func marksYear(releaseDate string) uint16 {
	t := parseTime(releaseDate)
	if t.IsZero() {
		return 0
	}
	return uint16(min(max(t.Year(), 0), math.MaxUint16))
}

func marksSuffix(filePath, format string) string {
	if s := strings.TrimPrefix(strings.ToLower(path.Ext(filePath)), "."); s != "" {
		return s
	}
	if format == "other" {
		return ""
	}
	return format
}

// marksKbps is the file's probed bitrate (bits per second) in kbps, else one
// estimated from its size over duration seconds; 0 when neither is known.
func marksKbps(bitrate, size int64, duration uint32) uint32 {
	kbps := bitrate / 1000
	if kbps <= 0 && duration != 0 && size > 0 {
		kbps = size * 8 / int64(duration) / 1000
	}
	return uint32(min(max(kbps, 0), math.MaxUint32))
}

// marksSongs renders the playable tracks among trackIDs as songs, keyed by
// track id; a track with no file on disk is absent. A file's creation time is
// not in the music_files view, so Created is the track's own.
func marksSongs(trackIDs []uint32) (map[uint32]child, error) {
	rows, err := queryIn(
		`SELECT t.id, t.mbid, t.title, t.disc, t.position, t.duration,
			t.artist_credit, t.added_at,
			a.id, a.title, a.release_date,
			ar.id, ar.name, ar.genre,
			f.path, f.format, f.size, f.duration, f.bitrate
		FROM streamline_tracks t
		JOIN streamline_albums a ON a.id = t.album_id
		JOIN streamline_artists ar ON ar.id = a.artist_id
		JOIN streamline_music_files f ON f.id = (
			SELECT MIN(id) FROM streamline_music_files
			WHERE track_id = t.id AND missing = 0)
		WHERE t.id IN ({in})`,
		trackIDs,
	)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]child, len(rows))
	var creditAlbums []uint32
	for _, r := range rows {
		albumID, owner := r.uint(8), r.str(12)
		credit := r.str(6)
		if credit != "" && !strings.EqualFold(credit, owner) {
			creditAlbums = append(creditAlbums, albumID)
		}
		suffix := marksSuffix(r.str(14), r.str(15))
		contentType := marksContentTypes[suffix]
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		duration := r.uint(17)
		if duration == 0 {
			duration = r.uint(5)
		}
		size := r.int(16)
		bitRate := marksKbps(r.int(18), size, duration)
		out[r.uint(0)] = child{
			ID:            ids.Track(r.uint(0)),
			Parent:        ids.Album(albumID),
			Title:         r.str(2),
			Album:         r.str(9),
			Artist:        owner,
			Track:         uint16(min(max(r.int(4), 0), math.MaxUint16)),
			DiscNumber:    uint8(min(max(r.int(3), 0), math.MaxUint8)),
			Year:          marksYear(r.str(10)),
			Genre:         r.str(13),
			CoverArt:      ids.Album(albumID),
			Size:          size,
			ContentType:   contentType,
			Suffix:        suffix,
			Duration:      duration,
			BitRate:       bitRate,
			Created:       timestamp(r.time(7)),
			AlbumID:       ids.Album(albumID),
			ArtistID:      ids.Artist(r.uint(11)),
			Type:          "music",
			MediaType:     "song",
			MusicBrainzID: r.str(1),
		}
	}
	if len(creditAlbums) == 0 {
		return out, nil
	}
	return out, marksCredit(rows, out, creditAlbums)
}

// marksCredit names a song of a split release by its own credited artist,
// with that artist's id when the album is also listed under them; a song
// credited to the album's owner keeps it.
func marksCredit(rows []row, out map[uint32]child, albumIDs []uint32) error {
	slices.Sort(albumIDs)
	albumIDs = slices.Compact(albumIDs)
	co, err := queryIn(
		`SELECT aa.album_id, ar.id, ar.name
		FROM streamline_album_artists aa
		JOIN streamline_artists ar ON ar.id = aa.artist_id
		WHERE aa.owner = 0 AND aa.album_id IN ({in})`,
		albumIDs,
	)
	if err != nil {
		return err
	}
	for _, r := range rows {
		credit := r.str(6)
		id := r.uint(0)
		if credit == "" || strings.EqualFold(credit, r.str(12)) {
			continue
		}
		c := out[id]
		c.Artist, c.ArtistID = credit, ""
		for _, k := range co {
			if k.uint(0) == r.uint(8) && strings.EqualFold(k.str(2), credit) {
				c.ArtistID = ids.Artist(k.uint(1))
			}
		}
		out[id] = c
	}
	return nil
}
