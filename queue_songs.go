package main

import (
	"path/filepath"
	"strings"

	"github.com/streamline-hq/streamline-subsonic/ids"
)

var queueContentTypes = map[string]string{
	"flac": "audio/flac",
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/opus",
	"aac":  "audio/aac",
}

func queueSuffix(path, format string) string {
	if s := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."); s != "" {
		return s
	}
	if format == "other" {
		return ""
	}
	return format
}

// queueClamp is n as a T, saturating at 0 and at T's largest value.
func queueClamp[T uint8 | uint16 | uint32](n int64) T {
	var zero T
	return T(min(max(n, 0), int64(^zero)))
}

// queueSongs renders the given tracks as songs, for every one that still has a
// file on disk; the others are absent from the result.
func queueSongs(trackIDs []uint32) (map[uint32]child, error) {
	rows, err := queryIn(`SELECT t.id, t.mbid, t.title, t.disc, t.position,
			t.duration, t.artist_credit, f.added_at,
			al.id, al.title, al.release_date,
			ar.id, ar.name, ar.genre,
			f.path, f.format, f.size, f.duration, f.bitrate
		FROM streamline_tracks t
		JOIN streamline_albums al ON al.id = t.album_id
		JOIN streamline_artists ar ON ar.id = al.artist_id
		JOIN streamline_music_files f ON f.id = (
			SELECT min(id) FROM streamline_music_files
			WHERE track_id = t.id AND missing = 0)
		WHERE t.id IN ({in})`, trackIDs)
	if err != nil {
		return nil, err
	}
	songs := make(map[uint32]child, len(rows))
	albumIDs := make([]uint32, 0, len(rows))
	for _, r := range rows {
		suffix := queueSuffix(r.str(14), r.str(15))
		duration := queueClamp[uint32](r.int(17))
		if duration == 0 {
			duration = r.uint(5)
		}
		size := r.int(16)
		kbps := queueClamp[uint32](r.int(18) / 1000)
		if kbps == 0 && duration > 0 && size > 0 {
			kbps = queueClamp[uint32](size * 8 / int64(duration) / 1000)
		}
		contentType := queueContentTypes[suffix]
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		var year uint16
		if d := r.time(10); !d.IsZero() {
			year = queueClamp[uint16](int64(d.Year()))
		}
		songs[r.uint(0)] = child{
			ID:            ids.Track(r.uint(0)),
			Parent:        ids.Album(r.uint(8)),
			Title:         r.str(2),
			Album:         r.str(9),
			Artist:        r.str(12),
			Track:         queueClamp[uint16](r.int(4)),
			DiscNumber:    queueClamp[uint8](r.int(3)),
			Year:          year,
			Genre:         r.str(13),
			CoverArt:      ids.Album(r.uint(8)),
			Size:          size,
			ContentType:   contentType,
			Suffix:        suffix,
			Duration:      duration,
			BitRate:       kbps,
			Created:       optTimestamp(r.time(7)),
			AlbumID:       ids.Album(r.uint(8)),
			ArtistID:      ids.Artist(r.uint(11)),
			Type:          "music",
			MediaType:     "song",
			MusicBrainzID: r.str(1),
		}
		albumIDs = append(albumIDs, r.uint(8))
	}
	if len(songs) == 0 {
		return songs, nil
	}
	return songs, queueCreditSplitSongs(rows, songs, albumIDs)
}

// queueCreditSplitSongs names a song of a split release by its own credited
// artist, with that artist's id when the album is listed under them; a song
// credited to the album's owner keeps it.
func queueCreditSplitSongs(
	rows []row,
	songs map[uint32]child,
	albumIDs []uint32,
) error {
	co, err := queryIn(`SELECT aa.album_id, ar.id, ar.name
		FROM streamline_album_artists aa
		JOIN streamline_artists ar ON ar.id = aa.artist_id
		WHERE aa.owner = 0 AND aa.album_id IN ({in})`, albumIDs)
	if err != nil {
		return err
	}
	for _, r := range rows {
		credit := r.str(6)
		song := songs[r.uint(0)]
		if credit == "" || strings.EqualFold(credit, r.str(12)) {
			continue
		}
		song.Artist, song.ArtistID = credit, ""
		for _, c := range co {
			if c.uint(0) == r.uint(8) && strings.EqualFold(c.str(2), credit) {
				song.ArtistID = ids.Artist(c.uint(1))
			}
		}
		songs[r.uint(0)] = song
	}
	return nil
}
