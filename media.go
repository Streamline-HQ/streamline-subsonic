package main

import (
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
	"github.com/streamline-hq/streamline-subsonic/transcode"
)

func init() {
	register("stream", mediaStream)
	register("download", mediaDownload)
	register("getCoverArt", mediaCoverArt)
	register("getAvatar", mediaAvatar)
}

// mediaFile is the first file on disk of a track.
type mediaFile struct {
	path          string
	format        string
	size          int64
	bitrate       int64
	fileSeconds   uint32
	trackDuration uint32
}

func mediaTrackFile(trackID uint32) (*mediaFile, error) {
	r, found, err := queryRow(
		`SELECT f.path, f.format, f.size, f.bitrate, f.duration, t.duration
		FROM streamline_music_files f
		JOIN streamline_tracks t ON t.id = f.track_id
		WHERE f.track_id = ? AND f.missing = 0
		ORDER BY f.id LIMIT 1`,
		trackID,
	)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, envelope.NotFound()
	}
	return &mediaFile{
		path:          r.str(0),
		format:        r.str(1),
		size:          r.int(2),
		bitrate:       r.int(3),
		fileSeconds:   r.uint(4),
		trackDuration: r.uint(5),
	}, nil
}

func (f *mediaFile) suffix() string { return songSuffix(f.path, f.format) }

// kbps is the bitrate a song listing reports: the probed one, else an
// estimate from the size and length, so an unprobed file is not taken for one
// of unknown bitrate and transcoded needlessly.
func (f *mediaFile) kbps() uint32 {
	duration := f.fileSeconds
	if duration == 0 {
		duration = f.trackDuration
	}
	return songKbps(f.bitrate, f.size, duration)
}

func mediaFileMissing() error {
	return &envelope.Error{Code: envelope.CodeNotFound, Message: "file missing"}
}

// check confirms the file can be opened where the extension has it: a library
// row whose path lies outside the granted folder, or that is gone from disk,
// answers like a row that does not exist.
func (f *mediaFile) check(c *reqCtx) error {
	guest, ok := c.req.GuestPath(f.path)
	if !ok {
		return mediaFileMissing()
	}
	info, err := os.Stat(guest)
	if err != nil || info.IsDir() {
		return mediaFileMissing()
	}
	return nil
}

func (f *mediaFile) original(attachment bool) sdk.Response {
	sf := &sdk.SendFile{Path: f.path, ContentType: songContentType(f.suffix())}
	if attachment {
		sf.Filename = path.Base(f.path)
	}
	return sdk.Response{SendFile: sf}
}

// mediaLimits is the format and bitrate cap a stream honours: the client's,
// tightened by the account's own cap, which also overrides a request for the
// raw file.
func mediaLimits(c *reqCtx) (format string, maxKbps int) {
	format = c.param("format")
	maxKbps = c.intParam("maxBitRate", 0, 0, 1<<20)
	if limit := int(c.user.MaxBitRate); limit > 0 {
		if maxKbps == 0 || maxKbps > limit {
			maxKbps = limit
		}
		if strings.EqualFold(strings.TrimSpace(format), "raw") {
			format = ""
		}
	}
	return format, maxKbps
}

// mediaStream sends the original file with range support, or a transcode when
// transcode.Plan asks for one. A transcode cannot seek by range; timeOffset
// starts it that many seconds in instead (OpenSubsonic transcodeOffset), and
// only a transcode can, so an offset always takes one.
func mediaStream(c *reqCtx) sdk.Response {
	if err := c.require(permStream); err != nil {
		return c.failErr(err)
	}
	id, err := c.idParam("id", ids.KindTrack)
	if err != nil {
		return c.failErr(err)
	}
	mf, err := mediaTrackFile(id)
	if err != nil {
		return c.failErr(err)
	}
	if err := mf.check(c); err != nil {
		return c.failErr(err)
	}
	format, maxKbps := mediaLimits(c)
	plan, convert := transcode.Plan(mf.suffix(), mf.kbps(), format, maxKbps)
	offset := c.intParam("timeOffset", 0, 0, 1<<20)
	if offset == 0 && !convert {
		return mf.original(false)
	}
	fallback := mf.original(false).SendFile
	if offset > 0 {
		// The client counts from the offset, so the original from 0 would play
		// at the wrong position: transcode or refuse, never fall back.
		if !convert {
			plan = transcode.OffsetPlan(mf.suffix(), mf.kbps())
		}
		if !mediaTranscodeAvailable() {
			return c.fail(
				envelope.CodeGeneric,
				"timeOffset needs a transcode and none can run now",
			)
		}
		plan.OffsetSec = offset
		fallback = nil
	}
	args, err := transcode.Args(mf.path, plan)
	if err != nil {
		return c.failErr(err)
	}
	return sdk.Response{StreamFFmpeg: &sdk.StreamFFmpeg{
		Args:        args,
		ContentType: transcode.Formats[plan.Format].ContentType,
		Fallback:    fallback,
	}}
}

// mediaTranscodeAvailable asks the host whether ffmpeg can run now: it is
// granted, enabled and has a free slot.
func mediaTranscodeAvailable() bool {
	res, err := sdk.FFmpeg("ffmpeg", "-hide_banner", "-version")
	return err == nil && res.Error == "" && res.ExitCode == 0
}

// mediaDownload sends the original file as an attachment, never transcoded.
func mediaDownload(c *reqCtx) sdk.Response {
	if err := c.require(permDownload); err != nil {
		return c.failErr(err)
	}
	id, err := c.idParam("id", ids.KindTrack)
	if err != nil {
		return c.failErr(err)
	}
	mf, err := mediaTrackFile(id)
	if err != nil {
		return c.failErr(err)
	}
	if err := mf.check(c); err != nil {
		return c.failErr(err)
	}
	return mf.original(true)
}

// mediaCoverArt serves an album's cover for an album or a song, a grid of its
// albums' covers for a playlist, and an artist's photo.
func mediaCoverArt(c *reqCtx) sdk.Response {
	kind, id, err := c.anyIDParam("id")
	if err != nil {
		return c.failErr(err)
	}
	switch kind {
	case ids.KindAlbum:
		return mediaPoster(c, "albums", "streamline_albums", id)
	case ids.KindArtist:
		return mediaPoster(c, "artists", "streamline_artists", id)
	case ids.KindTrack:
		r, found, err := queryRow(
			"SELECT album_id FROM streamline_tracks WHERE id = ?", id,
		)
		if err != nil {
			return c.failErr(err)
		}
		if !found {
			return c.failErr(envelope.NotFound())
		}
		return mediaPoster(c, "albums", "streamline_albums", r.uint(0))
	case ids.KindPlaylist:
		return servePlaylistCover(c, id)
	}
	return c.failErr(envelope.NotFound())
}

// mediaPoster serves <artwork>/<kind>/<id>/poster.jpg, Streamline's cached
// art, as stored. A row that is gone, or one with no art, is a plain 404, the
// cue for a client to draw its own placeholder.
func mediaPoster(c *reqCtx, kind, table string, id uint32) sdk.Response {
	_, found, err := queryRow("SELECT id FROM "+table+" WHERE id = ?", id)
	if err != nil {
		return c.failErr(err)
	}
	if !found {
		return notFoundResponse()
	}
	folder, ok := c.req.Folder("artwork")
	if !ok {
		return notFoundResponse()
	}
	rel := path.Join(kind, strconv.FormatUint(uint64(id), 10), "poster.jpg")
	contentType, ok := posterContentType(path.Join(folder.GuestPath, rel))
	if !ok {
		return notFoundResponse()
	}
	return sdk.Response{SendFile: &sdk.SendFile{
		Path:        path.Join(folder.HostPath, rel),
		ContentType: contentType,
	}}
}

// posterContentType sniffs the cached cover at guestPath. The file is always
// named poster.jpg, whatever the source sent, so anything that does not sniff
// as an image stays image/jpeg, and a hostile body is never served as a type a
// browser would render as a page. ok is false when there is no readable file.
func posterContentType(guestPath string) (contentType string, ok bool) {
	f, err := os.Open(guestPath) //nolint:gosec // built from a kind and a uint32
	if err != nil {
		return "", false
	}
	defer f.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && n == 0 {
		return "", false
	}
	contentType = http.DetectContentType(head[:n])
	if !strings.HasPrefix(contentType, "image/") {
		contentType = "image/jpeg"
	}
	return contentType, true
}

// mediaAvatar has nothing to serve: Streamline keeps no user pictures.
func mediaAvatar(c *reqCtx) sdk.Response {
	if c.param("username") == "" {
		return c.failErr(envelope.MissingParam("username"))
	}
	return c.failErr(envelope.NotFound())
}
