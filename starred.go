package main

import (
	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/ids"
)

func init() {
	register("getStarred", getStarred)
	register("getStarred2", getStarred2)
}

// starredSet is everything the caller starred, newest star first, with only
// what a client can list: artists and albums holding a playable track, and
// playable tracks.
type starredSet struct {
	artists []artistID3
	albums  []albumID3
	songs   []child
}

func loadStarred(c *reqCtx) (*starredSet, error) {
	rows, err := query(
		`SELECT target_kind, target_id FROM annotations
		WHERE user_id = ? AND starred_at IS NOT NULL
		ORDER BY starred_at DESC, target_kind, target_id`,
		c.user.ID,
	)
	if err != nil {
		return nil, err
	}
	var arIDs, alIDs, trIDs []uint32
	for _, r := range rows {
		switch r.str(0) {
		case ids.KindArtist:
			arIDs = append(arIDs, r.uint(1))
		case ids.KindAlbum:
			alIDs = append(alIDs, r.uint(1))
		case ids.KindTrack:
			trIDs = append(trIDs, r.uint(1))
		}
	}
	artists, err := starredArtists(arIDs)
	if err != nil {
		return nil, err
	}
	albums, err := starredAlbums(alIDs)
	if err != nil {
		return nil, err
	}
	songs, err := marksSongs(trIDs)
	if err != nil {
		return nil, err
	}
	set := &starredSet{
		artists: make([]artistID3, 0, len(arIDs)),
		albums:  make([]albumID3, 0, len(alIDs)),
		songs:   make([]child, 0, len(trIDs)),
	}
	for _, id := range arIDs {
		if a, ok := artists[id]; ok {
			set.artists = append(set.artists, a)
		}
	}
	for _, id := range alIDs {
		if a, ok := albums[id]; ok {
			set.albums = append(set.albums, a)
		}
	}
	for _, id := range trIDs {
		if s, ok := songs[id]; ok {
			set.songs = append(set.songs, s)
		}
	}
	return set, nil
}

func starredArtists(artistIDs []uint32) (map[uint32]artistID3, error) {
	rows, err := queryIn(
		`SELECT ar.id, ar.mbid, ar.name, ar.sort_name,
			(SELECT count(DISTINCT aa.album_id)
			 FROM streamline_album_artists aa
			 WHERE aa.artist_id = ar.id AND EXISTS (
				SELECT 1 FROM streamline_tracks t
				WHERE t.album_id = aa.album_id AND `+marksLiveTrack+`))
		FROM streamline_artists ar WHERE ar.id IN ({in})`,
		artistIDs,
	)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]artistID3, len(rows))
	for _, r := range rows {
		if r.int(4) == 0 {
			continue
		}
		out[r.uint(0)] = artistID3{
			ID:            ids.Artist(r.uint(0)),
			Name:          r.str(2),
			AlbumCount:    int(r.int(4)),
			CoverArt:      ids.Artist(r.uint(0)),
			MusicBrainzID: r.str(1),
			SortName:      r.str(3),
			MediaType:     "artist",
		}
	}
	return out, nil
}

func starredAlbums(albumIDs []uint32) (map[uint32]albumID3, error) {
	rows, err := queryIn(
		`SELECT a.id, a.mbid, a.title, a.release_date, a.added_at,
			ar.id, ar.name, ar.genre,
			(SELECT count(*) FROM streamline_tracks t
			 WHERE t.album_id = a.id AND `+marksLiveTrack+`),
			(SELECT COALESCE(sum(t.duration), 0) FROM streamline_tracks t
			 WHERE t.album_id = a.id AND `+marksLiveTrack+`)
		FROM streamline_albums a JOIN streamline_artists ar ON ar.id = a.artist_id
		WHERE a.id IN ({in})`,
		albumIDs,
	)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]albumID3, len(rows))
	for _, r := range rows {
		if r.int(8) == 0 {
			continue
		}
		out[r.uint(0)] = albumID3{
			ID:            ids.Album(r.uint(0)),
			Name:          r.str(2),
			Artist:        r.str(6),
			ArtistID:      ids.Artist(r.uint(5)),
			CoverArt:      ids.Album(r.uint(0)),
			SongCount:     int(r.int(8)),
			Duration:      r.uint(9),
			Year:          marksYear(r.str(3)),
			Genre:         r.str(7),
			Created:       timestamp(r.time(4)),
			MusicBrainzID: r.str(1),
			MediaType:     "album",
		}
	}
	return out, nil
}

// albumAsChild is an album in the folder-style responses: a directory under
// its artist.
func albumAsChild(a albumID3) child {
	return child{
		ID:            a.ID,
		Parent:        a.ArtistID,
		IsDir:         true,
		Title:         a.Name,
		Album:         a.Name,
		Artist:        a.Artist,
		Year:          a.Year,
		Genre:         a.Genre,
		CoverArt:      a.CoverArt,
		Duration:      a.Duration,
		Created:       a.Created,
		AlbumID:       a.ID,
		ArtistID:      a.ArtistID,
		MediaType:     "album",
		MusicBrainzID: a.MusicBrainzID,
	}
}

func getStarred(c *reqCtx) sdk.Response {
	set, err := loadStarred(c)
	if err != nil {
		return c.failErr(err)
	}
	albums := make([]child, 0, len(set.albums))
	for _, a := range set.albums {
		albums = append(albums, albumAsChild(a))
	}
	return c.respond(&body{Starred: &starred{
		Artist: set.artists, Album: albums, Song: set.songs,
	}})
}

func getStarred2(c *reqCtx) sdk.Response {
	set, err := loadStarred(c)
	if err != nil {
		return c.failErr(err)
	}
	return c.respond(&body{Starred2: &starred2{
		Artist: set.artists, Album: set.albums, Song: set.songs,
	}})
}
