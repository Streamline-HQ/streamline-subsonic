package main

import (
	"math"
	"path"
	"slices"
	"strings"

	"github.com/streamline-hq/streamline-subsonic/ids"
)

var playlistContentTypes = map[string]string{
	"flac": "audio/flac",
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/opus",
	"aac":  "audio/aac",
}

// playlistSongs renders the tracks among trackIDs that have a file on disk,
// keyed by track id. A track with no live file is absent: it is what a
// playlist skips when it lists its entries.
func playlistSongs(trackIDs []uint32) (map[uint32]child, error) {
	uniq := slices.Clone(trackIDs)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	rows, err := queryIn(`SELECT t.id, t.mbid, t.title, t.disc, t.position,
			t.duration, t.artist_credit, mf.added_at,
			al.id, al.title, al.release_date,
			ar.id, ar.name, ar.genre,
			mf.path, mf.format, mf.size, mf.duration, mf.bitrate
		FROM streamline_tracks t
		JOIN streamline_albums al ON al.id = t.album_id
		JOIN streamline_artists ar ON ar.id = al.artist_id
		JOIN streamline_music_files mf ON mf.track_id = t.id AND mf.missing = 0
			AND mf.id = (SELECT min(f.id) FROM streamline_music_files f
				WHERE f.track_id = t.id AND f.missing = 0)
		WHERE t.id IN ({in})`, uniq)
	if err != nil {
		return nil, err
	}
	var split []uint32
	for _, r := range rows {
		if credit := r.str(
			6,
		); credit != "" &&
			!strings.EqualFold(credit, r.str(12)) {
			split = append(split, r.uint(8))
		}
	}
	coArtists, err := playlistCoArtists(split)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]child, len(rows))
	for _, r := range rows {
		out[r.uint(0)] = playlistSong(r, coArtists)
	}
	return out, nil
}

// playlistCoArtists maps an album id to its co-credited artists' name and id.
func playlistCoArtists(albumIDs []uint32) (map[uint32]map[string]uint32, error) {
	out := map[uint32]map[string]uint32{}
	if len(albumIDs) == 0 {
		return out, nil
	}
	uniq := slices.Clone(albumIDs)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	rows, err := queryIn(`SELECT aa.album_id, a.id, a.name
		FROM streamline_album_artists aa
		JOIN streamline_artists a ON a.id = aa.artist_id
		WHERE aa.owner = 0 AND aa.album_id IN ({in})`, uniq)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if out[r.uint(0)] == nil {
			out[r.uint(0)] = map[string]uint32{}
		}
		out[r.uint(0)][strings.ToLower(r.str(2))] = r.uint(1)
	}
	return out, nil
}

func playlistSong(r row, coArtists map[uint32]map[string]uint32) child {
	suffix := strings.TrimPrefix(strings.ToLower(path.Ext(r.str(14))), ".")
	if suffix == "" && r.str(15) != "other" {
		suffix = r.str(15)
	}
	contentType, ok := playlistContentTypes[suffix]
	if !ok {
		contentType = "application/octet-stream"
	}
	duration := r.uint(17)
	if duration == 0 {
		duration = r.uint(5)
	}
	size := r.int(16)
	var year uint16
	if released := r.time(10); !released.IsZero() {
		year = uint16(min(max(released.Year(), 0), math.MaxUint16))
	}
	song := child{
		ID:            ids.Track(r.uint(0)),
		Parent:        ids.Album(r.uint(8)),
		Title:         r.str(2),
		Album:         r.str(9),
		Artist:        r.str(12),
		Track:         uint16(min(max(r.int(4), 0), math.MaxUint16)),
		DiscNumber:    uint8(min(max(r.int(3), 0), math.MaxUint8)),
		Year:          year,
		Genre:         r.str(13),
		CoverArt:      ids.Album(r.uint(8)),
		Size:          size,
		ContentType:   contentType,
		Suffix:        suffix,
		Duration:      duration,
		BitRate:       playlistBitRate(r.int(18), size, duration),
		Created:       optTimestamp(r.time(7)),
		AlbumID:       ids.Album(r.uint(8)),
		ArtistID:      ids.Artist(r.uint(11)),
		Type:          "music",
		MediaType:     "song",
		MusicBrainzID: r.str(1),
	}
	// A song of a split release is named by its own credited artist, with that
	// artist's id when the album is listed under them; one credited to the
	// album's owner keeps the owner.
	if credit := r.str(6); credit != "" && !strings.EqualFold(credit, r.str(12)) {
		song.Artist = credit
		song.ArtistID = ""
		if id, found := coArtists[r.uint(8)][strings.ToLower(credit)]; found {
			song.ArtistID = ids.Artist(id)
		}
	}
	return song
}

// playlistBitRate is the probed rate in kbps, else the one the size and
// length imply.
func playlistBitRate(bitrate, size int64, duration uint32) uint32 {
	if kbps := bitrate / 1000; kbps > 0 || duration == 0 || size <= 0 {
		return uint32(min(max(kbps, 0), math.MaxUint32))
	}
	return uint32(min(max(size*8/int64(duration)/1000, 0), math.MaxUint32))
}
