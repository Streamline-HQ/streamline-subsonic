package main

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"net/http"
	"os"
	"path"
	"strconv"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
	"github.com/streamline-hq/streamline-subsonic/mosaic"
)

const playlistCoverDir = "playlist-covers"

// servePlaylistCover answers getCoverArt for a playlist id: the covers of its
// first four distinct albums that have one, as a 2x2 grid. A playlist with
// fewer than two decodable covers gets the first album cover it has, as it is.
// One with no cover at all is error 70, as for an album.
// getCoverArt calls it for an id of kind "pl".
func servePlaylistCover(c *reqCtx, id uint32) sdk.Response {
	p, err := c.loadPlaylist(id)
	if err != nil {
		return c.failErr(err)
	}
	stored, err := playlistTrackIDs([]uint32{p.id})
	if err != nil {
		return c.failErr(err)
	}
	songs, err := songsByID(stored[p.id])
	if err != nil {
		return c.failErr(err)
	}
	entries := playlistEntries(stored[p.id], songs)
	if len(entries) == 0 {
		return c.failErr(envelope.NotFound())
	}
	albums := playlistAlbums(entries)
	artwork, ok := c.req.Folder("artwork")
	if !ok {
		return c.failErr(envelope.NotFound())
	}
	covers := playlistCoverFiles(artwork.GuestPath, albums)
	if len(covers) == 0 {
		return c.failErr(envelope.NotFound())
	}
	cached, cachePath := playlistCachedCover(c, covers)
	if cached != nil {
		return coverBytes(c, "image/jpeg", cached)
	}

	var squares []image.Image
	// asIs is the cover served when no grid can be drawn: the first one that
	// decodes, or the first one at all.
	asIs := covers[0].path
	for _, f := range covers {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		if sq, decoded := mosaic.Square(data); decoded {
			if len(squares) == 0 {
				asIs = f.path
			}
			squares = append(squares, sq)
			if len(squares) == mosaic.MaxCovers {
				break
			}
		}
	}
	grid, drawn, err := mosaic.Grid(squares)
	if err != nil {
		return c.failErr(err)
	}
	if !drawn {
		return playlistAsIs(c, asIs)
	}
	if cachePath != "" {
		playlistStoreCover(cachePath, grid)
	}
	return coverBytes(c, "image/jpeg", grid)
}

type playlistCoverFile struct {
	albumID uint32
	path    string
	size    int64
	mtime   int64
}

// playlistAlbums are the distinct album ids of the entries, in order.
func playlistAlbums(entries []child) []uint32 {
	var out []uint32
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.AlbumID] {
			continue
		}
		seen[e.AlbumID] = true
		_, id, err := ids.Parse(e.AlbumID)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}

func playlistCoverPath(artworkRoot string, albumID uint32) string {
	return path.Join(
		artworkRoot,
		"albums",
		strconv.FormatUint(uint64(albumID), 10),
		"poster.jpg",
	)
}

// playlistCoverFiles lists the albums' covers that exist, in album order.
func playlistCoverFiles(root string, albums []uint32) []playlistCoverFile {
	var out []playlistCoverFile
	for _, id := range albums {
		p := playlistCoverPath(root, id)
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, playlistCoverFile{
			albumID: id,
			path:    p,
			size:    info.Size(),
			mtime:   info.ModTime().UnixNano(),
		})
	}
	return out
}

// playlistCachedCover reads the drawn grid for these covers from the cache
// folder. The key names every cover's identity, size and mtime, so a changed
// cover or playlist draws a new grid. cachePath is "" when there is no cache.
func playlistCachedCover(
	c *reqCtx,
	covers []playlistCoverFile,
) (data []byte, cachePath string) {
	cache, ok := c.req.Folder("cache")
	if !ok {
		return nil, ""
	}
	h := sha256.New()
	for _, f := range covers {
		h.Write([]byte(strconv.FormatUint(uint64(f.albumID), 10) + ":" +
			strconv.FormatInt(
				f.size,
				10,
			) + ":" + strconv.FormatInt(f.mtime, 10) + "\n"))
	}
	cachePath = path.Join(
		cache.GuestPath,
		playlistCoverDir,
		hex.EncodeToString(h.Sum(nil))+".jpg",
	)
	//nolint:gosec // a path under the cache folder, named by a hash
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, cachePath
	}
	return data, cachePath
}

// playlistStoreCover keeps a drawn cover (a grid, or one scaled to a size),
// best effort: a cache that cannot be written costs a redraw, not an answer.
func playlistStoreCover(cachePath string, grid []byte) {
	// cachePath is under the cache folder, named by a hash of what it holds.
	//nolint:gosec // see above
	if err := os.MkdirAll(path.Dir(cachePath), 0o750); err != nil {
		sdk.Log("warn", "creating the cover cache failed", "error", err.Error())
		return
	}
	tmp := cachePath + ".tmp"
	err := os.WriteFile(tmp, grid, 0o600) //nolint:gosec // see playlistCachedCover
	if err != nil {
		sdk.Log("warn", "caching a playlist cover failed", "error", err.Error())
		return
	}
	//nolint:gosec // see the MkdirAll above
	if err = os.Rename(tmp, cachePath); err != nil {
		sdk.Log("warn", "caching a playlist cover failed", "error", err.Error())
	}
}

// playlistAsIs serves an album cover of the playlist, unredrawn, typed as
// mediaPoster types it.
func playlistAsIs(c *reqCtx, coverPath string) sdk.Response {
	data, err := os.ReadFile(coverPath) //nolint:gosec // under the artwork folder
	if err != nil {
		return c.failErr(envelope.NotFound())
	}
	return coverBytes(c, coverContentType(data), data)
}

func coverResponse(contentType string, data []byte) sdk.Response {
	return sdk.Response{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type":  {contentType},
			"Cache-Control": {coverCacheControl},
		},
		Body: data,
	}
}
