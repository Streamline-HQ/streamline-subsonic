package main

// markCollector gathers every annotatable element of a response by pointer,
// so the decorator can fill them in place after one read per kind.
type markCollector struct {
	children []*child
	albums   []*albumID3
	artists  []*artistID3
	dir      *directory
}

func (c *markCollector) kids(s []child) {
	for i := range s {
		c.children = append(c.children, &s[i])
	}
}

func (c *markCollector) albumSlice(s []albumID3) {
	for i := range s {
		c.albums = append(c.albums, &s[i])
	}
}

func (c *markCollector) artistSlice(s []artistID3) {
	for i := range s {
		c.artists = append(c.artists, &s[i])
	}
}

func (c *markCollector) songsOf(s *songs) {
	if s != nil {
		c.kids(s.Song)
	}
}

func collectMarkTargets(b *body) *markCollector {
	c := &markCollector{}
	if b.Artists != nil {
		for i := range b.Artists.Index {
			c.artistSlice(b.Artists.Index[i].Artist)
		}
	}
	if b.Indexes != nil {
		for i := range b.Indexes.Index {
			c.artistSlice(b.Indexes.Index[i].Artist)
		}
	}
	if b.Directory != nil {
		c.dir = b.Directory
		c.kids(b.Directory.Child)
	}
	if b.Artist != nil {
		c.artists = append(c.artists, &b.Artist.artistID3)
		c.albumSlice(b.Artist.Album)
	}
	if b.Album != nil {
		c.albums = append(c.albums, &b.Album.albumID3)
		c.kids(b.Album.Song)
	}
	if b.Song != nil {
		c.children = append(c.children, b.Song)
	}
	if b.ArtistInfo != nil {
		c.artistSlice(b.ArtistInfo.SimilarArtist)
	}
	if b.ArtistInfo2 != nil {
		c.artistSlice(b.ArtistInfo2.SimilarArtist)
	}
	c.songsOf(b.SimilarSongs)
	c.songsOf(b.SimilarSongs2)
	c.songsOf(b.TopSongs)
	c.songsOf(b.RandomSongs)
	c.songsOf(b.SongsByGenre)
	if b.AlbumList != nil {
		c.kids(b.AlbumList.Album)
	}
	if b.AlbumList2 != nil {
		c.albumSlice(b.AlbumList2.Album)
	}
	if b.NowPlaying != nil {
		for i := range b.NowPlaying.Entry {
			c.children = append(c.children, &b.NowPlaying.Entry[i].child)
		}
	}
	if b.Starred != nil {
		c.artistSlice(b.Starred.Artist)
		c.kids(b.Starred.Album)
		c.kids(b.Starred.Song)
	}
	if b.Starred2 != nil {
		c.artistSlice(b.Starred2.Artist)
		c.albumSlice(b.Starred2.Album)
		c.kids(b.Starred2.Song)
	}
	if b.SearchResult != nil {
		c.kids(b.SearchResult.Match)
	}
	if b.SearchResult2 != nil {
		c.artistSlice(b.SearchResult2.Artist)
		c.kids(b.SearchResult2.Album)
		c.kids(b.SearchResult2.Song)
	}
	if b.SearchResult3 != nil {
		c.artistSlice(b.SearchResult3.Artist)
		c.albumSlice(b.SearchResult3.Album)
		c.kids(b.SearchResult3.Song)
	}
	if b.Playlist != nil {
		c.kids(b.Playlist.Entry)
	}
	if b.Shares != nil {
		for i := range b.Shares.Share {
			c.kids(b.Shares.Share[i].Entry)
		}
	}
	if b.Bookmarks != nil {
		for i := range b.Bookmarks.Bookmark {
			c.children = append(c.children, &b.Bookmarks.Bookmark[i].Entry)
		}
	}
	if b.PlayQueue != nil {
		c.kids(b.PlayQueue.Entry)
	}
	if b.PlayQueueByIndex != nil {
		c.kids(b.PlayQueueByIndex.Entry)
	}
	return c
}
