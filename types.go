package main

import "github.com/streamline-hq/streamline-subsonic/envelope"

// body is the payload a handler sets, at most one field non-nil, serialized
// inside <subsonic-response> / "subsonic-response".
type body struct {
	License                *license                `xml:"license,omitempty"                json:"license,omitempty"`
	MusicFolders           *musicFolders           `xml:"musicFolders,omitempty"           json:"musicFolders,omitempty"`
	Artists                *indexes                `xml:"artists,omitempty"                json:"artists,omitempty"`
	Indexes                *folderIndexes          `xml:"indexes,omitempty"                json:"indexes,omitempty"`
	Directory              *directory              `xml:"directory,omitempty"              json:"directory,omitempty"`
	Genres                 *genres                 `xml:"genres,omitempty"                 json:"genres,omitempty"`
	Artist                 *artistWith             `xml:"artist,omitempty"                 json:"artist,omitempty"`
	Album                  *albumWith              `xml:"album,omitempty"                  json:"album,omitempty"`
	Song                   *child                  `xml:"song,omitempty"                   json:"song,omitempty"`
	ArtistInfo             *artistInfo             `xml:"artistInfo,omitempty"             json:"artistInfo,omitempty"`
	ArtistInfo2            *artistInfo2            `xml:"artistInfo2,omitempty"            json:"artistInfo2,omitempty"`
	AlbumInfo              *albumInfo              `xml:"albumInfo,omitempty"              json:"albumInfo,omitempty"`
	SimilarSongs           *songs                  `xml:"similarSongs,omitempty"           json:"similarSongs,omitempty"`
	SimilarSongs2          *songs                  `xml:"similarSongs2,omitempty"          json:"similarSongs2,omitempty"`
	TopSongs               *songs                  `xml:"topSongs,omitempty"               json:"topSongs,omitempty"`
	AlbumList              *albumList              `xml:"albumList,omitempty"              json:"albumList,omitempty"`
	AlbumList2             *albumList2             `xml:"albumList2,omitempty"             json:"albumList2,omitempty"`
	RandomSongs            *songs                  `xml:"randomSongs,omitempty"            json:"randomSongs,omitempty"`
	SongsByGenre           *songs                  `xml:"songsByGenre,omitempty"           json:"songsByGenre,omitempty"`
	NowPlaying             *nowPlayingList         `xml:"nowPlaying,omitempty"             json:"nowPlaying,omitempty"`
	Starred                *starred                `xml:"starred,omitempty"                json:"starred,omitempty"`
	Starred2               *starred2               `xml:"starred2,omitempty"               json:"starred2,omitempty"`
	SearchResult           *searchResult           `xml:"searchResult,omitempty"           json:"searchResult,omitempty"`
	SearchResult2          *searchResult2          `xml:"searchResult2,omitempty"          json:"searchResult2,omitempty"`
	SearchResult3          *search3                `xml:"searchResult3,omitempty"          json:"searchResult3,omitempty"`
	Playlists              *playlists              `xml:"playlists,omitempty"              json:"playlists,omitempty"`
	Playlist               *playlistWith           `xml:"playlist,omitempty"               json:"playlist,omitempty"`
	Shares                 *shares                 `xml:"shares,omitempty"                 json:"shares,omitempty"`
	Bookmarks              *bookmarks              `xml:"bookmarks,omitempty"              json:"bookmarks,omitempty"`
	PlayQueue              *playQueue              `xml:"playQueue,omitempty"              json:"playQueue,omitempty"`
	PlayQueueByIndex       *playQueueByIndex       `xml:"playQueueByIndex,omitempty"       json:"playQueueByIndex,omitempty"`
	InternetRadioStations  *radioStations          `xml:"internetRadioStations,omitempty"  json:"internetRadioStations,omitempty"`
	ScanStatus             *scanStatus             `xml:"scanStatus,omitempty"             json:"scanStatus,omitempty"`
	User                   *subsonicUser           `xml:"user,omitempty"                   json:"user,omitempty"`
	Users                  *subsonicUsers          `xml:"users,omitempty"                  json:"users,omitempty"`
	Lyrics                 *lyrics                 `xml:"lyrics,omitempty"                 json:"lyrics,omitempty"`
	LyricsList             *lyricsList             `xml:"lyricsList,omitempty"             json:"lyricsList,omitempty"`
	OpenSubsonicExtensions []openSubsonicExtension `xml:"openSubsonicExtensions,omitempty" json:"openSubsonicExtensions,omitempty"`
	Error                  *envelope.Error         `xml:"error,omitempty"                  json:"error,omitempty"`
}

// response is the whole document: the envelope's header and one body.
type response struct {
	envelope.Head
	body
}

// child is a song, or an album when IsDir is set (folder-style responses).
type child struct {
	ID            string `xml:"id,attr"                      json:"id"`
	Parent        string `xml:"parent,attr,omitempty"        json:"parent,omitempty"`
	IsDir         bool   `xml:"isDir,attr"                   json:"isDir"`
	Title         string `xml:"title,attr"                   json:"title"`
	Album         string `xml:"album,attr,omitempty"         json:"album,omitempty"`
	Artist        string `xml:"artist,attr,omitempty"        json:"artist,omitempty"`
	Track         uint16 `xml:"track,attr,omitempty"         json:"track,omitempty"`
	DiscNumber    uint8  `xml:"discNumber,attr,omitempty"    json:"discNumber,omitempty"`
	Year          uint16 `xml:"year,attr,omitempty"          json:"year,omitempty"`
	Genre         string `xml:"genre,attr,omitempty"         json:"genre,omitempty"`
	CoverArt      string `xml:"coverArt,attr,omitempty"      json:"coverArt,omitempty"`
	Size          int64  `xml:"size,attr,omitempty"          json:"size,omitempty"`
	ContentType   string `xml:"contentType,attr,omitempty"   json:"contentType,omitempty"`
	Suffix        string `xml:"suffix,attr,omitempty"        json:"suffix,omitempty"`
	Duration      uint32 `xml:"duration,attr"                json:"duration"`
	BitRate       uint32 `xml:"bitRate,attr,omitempty"       json:"bitRate,omitempty"`
	Created       string `xml:"created,attr,omitempty"       json:"created,omitempty"`
	Starred       string `xml:"starred,attr,omitempty"       json:"starred,omitempty"`
	UserRating    uint8  `xml:"userRating,attr,omitempty"    json:"userRating,omitempty"`
	PlayCount     uint32 `xml:"playCount,attr,omitempty"     json:"playCount,omitempty"`
	Played        string `xml:"played,attr,omitempty"        json:"played,omitempty"`
	AlbumID       string `xml:"albumId,attr,omitempty"       json:"albumId,omitempty"`
	ArtistID      string `xml:"artistId,attr,omitempty"      json:"artistId,omitempty"`
	Type          string `xml:"type,attr,omitempty"          json:"type,omitempty"`
	MediaType     string `xml:"mediaType,attr,omitempty"     json:"mediaType,omitempty"`
	MusicBrainzID string `xml:"musicBrainzId,attr,omitempty" json:"musicBrainzId,omitempty"`
}

// artistID3 is also the folder-style <artist> element: the two share every
// attribute a client reads.
type artistID3 struct {
	ID            string `xml:"id,attr"                      json:"id"`
	Name          string `xml:"name,attr"                    json:"name"`
	AlbumCount    int    `xml:"albumCount,attr"              json:"albumCount"`
	CoverArt      string `xml:"coverArt,attr,omitempty"      json:"coverArt,omitempty"`
	Starred       string `xml:"starred,attr,omitempty"       json:"starred,omitempty"`
	UserRating    uint8  `xml:"userRating,attr,omitempty"    json:"userRating,omitempty"`
	MusicBrainzID string `xml:"musicBrainzId,attr,omitempty" json:"musicBrainzId,omitempty"`
	SortName      string `xml:"sortName,attr,omitempty"      json:"sortName,omitempty"`
	MediaType     string `xml:"mediaType,attr,omitempty"     json:"mediaType,omitempty"`
}

type albumID3 struct {
	ID            string `xml:"id,attr"                      json:"id"`
	Name          string `xml:"name,attr"                    json:"name"`
	Artist        string `xml:"artist,attr"                  json:"artist"`
	ArtistID      string `xml:"artistId,attr"                json:"artistId"`
	CoverArt      string `xml:"coverArt,attr"                json:"coverArt"`
	SongCount     int    `xml:"songCount,attr"               json:"songCount"`
	Duration      uint32 `xml:"duration,attr"                json:"duration"`
	Year          uint16 `xml:"year,attr,omitempty"          json:"year,omitempty"`
	Genre         string `xml:"genre,attr,omitempty"         json:"genre,omitempty"`
	Created       string `xml:"created,attr"                 json:"created"`
	Starred       string `xml:"starred,attr,omitempty"       json:"starred,omitempty"`
	UserRating    uint8  `xml:"userRating,attr,omitempty"    json:"userRating,omitempty"`
	PlayCount     uint32 `xml:"playCount,attr,omitempty"     json:"playCount,omitempty"`
	Played        string `xml:"played,attr,omitempty"        json:"played,omitempty"`
	MusicBrainzID string `xml:"musicBrainzId,attr,omitempty" json:"musicBrainzId,omitempty"`
	MediaType     string `xml:"mediaType,attr,omitempty"     json:"mediaType,omitempty"`
}

type artistWith struct {
	artistID3
	Album []albumID3 `xml:"album" json:"album"`
}

type albumWith struct {
	albumID3
	Song []child `xml:"song" json:"song"`
}

type directory struct {
	ID         string  `xml:"id,attr"                   json:"id"`
	Parent     string  `xml:"parent,attr,omitempty"     json:"parent,omitempty"`
	Name       string  `xml:"name,attr"                 json:"name"`
	Starred    string  `xml:"starred,attr,omitempty"    json:"starred,omitempty"`
	UserRating uint8   `xml:"userRating,attr,omitempty" json:"userRating,omitempty"`
	PlayCount  uint32  `xml:"playCount,attr,omitempty"  json:"playCount,omitempty"`
	Played     string  `xml:"played,attr,omitempty"     json:"played,omitempty"`
	Child      []child `xml:"child"                     json:"child"`
}

type index struct {
	Name   string      `xml:"name,attr" json:"name"`
	Artist []artistID3 `xml:"artist"    json:"artist"`
}

type indexes struct {
	IgnoredArticles string  `xml:"ignoredArticles,attr" json:"ignoredArticles"`
	Index           []index `xml:"index"                json:"index"`
}

type folderIndexes struct {
	IgnoredArticles string  `xml:"ignoredArticles,attr" json:"ignoredArticles"`
	LastModified    int64   `xml:"lastModified,attr"    json:"lastModified"`
	Index           []index `xml:"index"                json:"index"`
}

type genre struct {
	Value      string `xml:",chardata"       json:"value"`
	SongCount  int    `xml:"songCount,attr"  json:"songCount"`
	AlbumCount int    `xml:"albumCount,attr" json:"albumCount"`
}

type genres struct {
	Genre []genre `xml:"genre" json:"genre"`
}

type artistInfoBase struct {
	Biography     string `xml:"biography,omitempty"     json:"biography,omitempty"`
	MusicBrainzID string `xml:"musicBrainzId,omitempty" json:"musicBrainzId,omitempty"`
}

type artistInfo struct {
	artistInfoBase
	SimilarArtist []artistID3 `xml:"similarArtist" json:"similarArtist"`
}

type artistInfo2 struct {
	artistInfoBase
	SimilarArtist []artistID3 `xml:"similarArtist" json:"similarArtist"`
}

type albumInfo struct {
	Notes         string `xml:"notes,omitempty"         json:"notes,omitempty"`
	MusicBrainzID string `xml:"musicBrainzId,omitempty" json:"musicBrainzId,omitempty"`
}

type songs struct {
	Song []child `xml:"song" json:"song"`
}

type albumList struct {
	Album []child `xml:"album" json:"album"`
}

type albumList2 struct {
	Album []albumID3 `xml:"album" json:"album"`
}

type nowPlayingEntry struct {
	child
	Username   string `xml:"username,attr"   json:"username"`
	MinutesAgo int    `xml:"minutesAgo,attr" json:"minutesAgo"`
	PlayerID   int    `xml:"playerId,attr"   json:"playerId"`
	PlayerName string `xml:"playerName,attr" json:"playerName"`
}

type nowPlayingList struct {
	Entry []nowPlayingEntry `xml:"entry" json:"entry"`
}

type starred struct {
	Artist []artistID3 `xml:"artist" json:"artist"`
	Album  []child     `xml:"album"  json:"album"`
	Song   []child     `xml:"song"   json:"song"`
}

type starred2 struct {
	Artist []artistID3 `xml:"artist" json:"artist"`
	Album  []albumID3  `xml:"album"  json:"album"`
	Song   []child     `xml:"song"   json:"song"`
}

type searchResult struct {
	Offset    int     `xml:"offset,attr"    json:"offset"`
	TotalHits int     `xml:"totalHits,attr" json:"totalHits"`
	Match     []child `xml:"match"          json:"match"`
}

type searchResult2 struct {
	Artist []artistID3 `xml:"artist" json:"artist"`
	Album  []child     `xml:"album"  json:"album"`
	Song   []child     `xml:"song"   json:"song"`
}

type search3 struct {
	Artist []artistID3 `xml:"artist" json:"artist"`
	Album  []albumID3  `xml:"album"  json:"album"`
	Song   []child     `xml:"song"   json:"song"`
}

type playlistInfo struct {
	ID        string `xml:"id,attr"                 json:"id"`
	Name      string `xml:"name,attr"               json:"name"`
	Comment   string `xml:"comment,attr,omitempty"  json:"comment,omitempty"`
	Owner     string `xml:"owner,attr"              json:"owner"`
	Public    bool   `xml:"public,attr"             json:"public"`
	SongCount int    `xml:"songCount,attr"          json:"songCount"`
	Duration  uint32 `xml:"duration,attr"           json:"duration"`
	Created   string `xml:"created,attr"            json:"created"`
	Changed   string `xml:"changed,attr"            json:"changed"`
	CoverArt  string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
}

type playlistWith struct {
	playlistInfo
	Entry []child `xml:"entry" json:"entry"`
}

type playlists struct {
	Playlist []playlistInfo `xml:"playlist" json:"playlist"`
}

type shareInfo struct {
	ID          string  `xml:"id,attr"                    json:"id"`
	URL         string  `xml:"url,attr"                   json:"url"`
	Description string  `xml:"description,attr,omitempty" json:"description,omitempty"`
	Username    string  `xml:"username,attr"              json:"username"`
	Created     string  `xml:"created,attr"               json:"created"`
	Expires     string  `xml:"expires,attr,omitempty"     json:"expires,omitempty"`
	LastVisited string  `xml:"lastVisited,attr,omitempty" json:"lastVisited,omitempty"`
	VisitCount  uint32  `xml:"visitCount,attr"            json:"visitCount"`
	Entry       []child `xml:"entry"                      json:"entry"`
}

type shares struct {
	Share []shareInfo `xml:"share" json:"share"`
}

type bookmarkInfo struct {
	Position int64  `xml:"position,attr"          json:"position"`
	Username string `xml:"username,attr"          json:"username"`
	Comment  string `xml:"comment,attr,omitempty" json:"comment,omitempty"`
	Created  string `xml:"created,attr"           json:"created"`
	Changed  string `xml:"changed,attr"           json:"changed"`
	Entry    child  `xml:"entry"                  json:"entry"`
}

type bookmarks struct {
	Bookmark []bookmarkInfo `xml:"bookmark" json:"bookmark"`
}

type playQueue struct {
	Current   string  `xml:"current,attr,omitempty" json:"current,omitempty"`
	Position  int64   `xml:"position,attr"          json:"position"`
	Username  string  `xml:"username,attr"          json:"username"`
	Changed   string  `xml:"changed,attr"           json:"changed"`
	ChangedBy string  `xml:"changedBy,attr"         json:"changedBy"`
	Entry     []child `xml:"entry"                  json:"entry"`
}

// playQueueByIndex is the OpenSubsonic indexBasedQueue twin of playQueue.
type playQueueByIndex struct {
	CurrentIndex *int    `xml:"currentIndex,attr,omitempty" json:"currentIndex,omitempty"`
	Position     int64   `xml:"position,attr"               json:"position"`
	Username     string  `xml:"username,attr"               json:"username"`
	Changed      string  `xml:"changed,attr"                json:"changed"`
	ChangedBy    string  `xml:"changedBy,attr"              json:"changedBy"`
	Entry        []child `xml:"entry"                       json:"entry"`
}

type radioStation struct {
	ID          string `xml:"id,attr"                    json:"id"`
	Name        string `xml:"name,attr"                  json:"name"`
	StreamURL   string `xml:"streamUrl,attr"             json:"streamUrl"`
	HomePageURL string `xml:"homePageUrl,attr,omitempty" json:"homePageUrl,omitempty"`
}

type radioStations struct {
	Station []radioStation `xml:"internetRadioStation" json:"internetRadioStation"`
}

type scanStatus struct {
	Scanning bool `xml:"scanning,attr" json:"scanning"`
	Count    int  `xml:"count,attr"    json:"count"`
}

type subsonicUser struct {
	Username            string `xml:"username,attr"             json:"username"`
	Email               string `xml:"email,attr"                json:"email"`
	ScrobblingEnabled   bool   `xml:"scrobblingEnabled,attr"    json:"scrobblingEnabled"`
	MaxBitRate          uint16 `xml:"maxBitRate,attr,omitempty" json:"maxBitRate,omitempty"`
	AdminRole           bool   `xml:"adminRole,attr"            json:"adminRole"`
	SettingsRole        bool   `xml:"settingsRole,attr"         json:"settingsRole"`
	DownloadRole        bool   `xml:"downloadRole,attr"         json:"downloadRole"`
	UploadRole          bool   `xml:"uploadRole,attr"           json:"uploadRole"`
	PlaylistRole        bool   `xml:"playlistRole,attr"         json:"playlistRole"`
	CoverArtRole        bool   `xml:"coverArtRole,attr"         json:"coverArtRole"`
	CommentRole         bool   `xml:"commentRole,attr"          json:"commentRole"`
	PodcastRole         bool   `xml:"podcastRole,attr"          json:"podcastRole"`
	StreamRole          bool   `xml:"streamRole,attr"           json:"streamRole"`
	JukeboxRole         bool   `xml:"jukeboxRole,attr"          json:"jukeboxRole"`
	ShareRole           bool   `xml:"shareRole,attr"            json:"shareRole"`
	VideoConversionRole bool   `xml:"videoConversionRole,attr"  json:"videoConversionRole"`
	Folder              []int  `xml:"folder"                    json:"folder"`
}

type subsonicUsers struct {
	User []subsonicUser `xml:"user" json:"user"`
}

type lyrics struct {
	Artist string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Title  string `xml:"title,attr,omitempty"  json:"title,omitempty"`
	Value  string `xml:",chardata"             json:"value"`
}

type lyricLine struct {
	Start *int64 `xml:"start,attr,omitempty" json:"start,omitempty"`
	Value string `xml:",chardata"            json:"value"`
}

type structuredLyrics struct {
	DisplayArtist string      `xml:"displayArtist,attr,omitempty" json:"displayArtist,omitempty"`
	DisplayTitle  string      `xml:"displayTitle,attr,omitempty"  json:"displayTitle,omitempty"`
	Lang          string      `xml:"lang,attr"                    json:"lang"`
	Synced        bool        `xml:"synced,attr"                  json:"synced"`
	Line          []lyricLine `xml:"line"                         json:"line"`
}

type lyricsList struct {
	StructuredLyrics []structuredLyrics `xml:"structuredLyrics" json:"structuredLyrics"`
}

type openSubsonicExtension struct {
	Name     string `xml:"name,attr" json:"name"`
	Versions []int  `xml:"versions"  json:"versions"`
}

type musicFolder struct {
	ID   int    `xml:"id,attr"   json:"id"`
	Name string `xml:"name,attr" json:"name"`
}

type musicFolders struct {
	MusicFolder []musicFolder `xml:"musicFolder" json:"musicFolder"`
}

type license struct {
	Valid bool `xml:"valid,attr" json:"valid"`
}
