// Package transcode decides when a stream is converted and builds the ffmpeg
// command line for it. It holds no host calls, so it runs natively under test.
package transcode

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrUnknownFormat is a Format outside Formats.
var ErrUnknownFormat = errors.New("unknown audio format")

// Options is one conversion of an audio file to a stream.
type Options struct {
	// Format is one of Formats.
	Format string
	// BitrateKbps is the target bitrate.
	BitrateKbps int
	// OffsetSec starts the output that many seconds into the source.
	OffsetSec int
}

// Format describes a stream format ffmpeg can produce here.
type Format struct {
	ContentType string
	codec       string
	muxer       string
}

// Formats are keyed by the name a Subsonic client asks for. Each muxer writes
// a stream that plays as it arrives, with no index to seek back and patch at
// the end.
var Formats = map[string]Format{
	"mp3":  {ContentType: "audio/mpeg", codec: "libmp3lame", muxer: "mp3"},
	"opus": {ContentType: "audio/ogg", codec: "libopus", muxer: "ogg"},
	"aac":  {ContentType: "audio/aac", codec: "aac", muxer: "adts"},
}

// Bitrate bounds for a conversion, in kbps.
const (
	MinKbps = 32
	MaxKbps = 320
)

// defaultKbps applies when the client names a format but no maxBitRate.
var defaultKbps = map[string]int{"mp3": 192, "opus": 128, "aac": 192}

// Args is the ffmpeg command line for o, minus the binary.
func Args(src string, o Options) ([]string, error) {
	f, ok := Formats[o.Format]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownFormat, o.Format)
	}
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error"}
	if o.OffsetSec > 0 {
		// Before -i: an input seek, which jumps instead of decoding up to it.
		args = append(args, "-ss", strconv.Itoa(o.OffsetSec))
	}
	return append(args,
		"-i", src,
		"-map", "0:a:0",
		"-vn", "-sn", "-dn",
		"-map_metadata", "-1",
		"-c:a", f.codec,
		"-b:a", strconv.Itoa(o.BitrateKbps)+"k",
		"-f", f.muxer,
		"pipe:1",
	), nil
}

// Plan decides whether a stream converts a file, and to what. A client gets
// the original unless it asks for another format this server can make, or
// caps the bitrate below the file's (or a file whose bitrate is unknown).
// "raw" always means the original.
func Plan(
	suffix string,
	srcKbps uint32,
	format string,
	maxKbps int,
) (Options, bool) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "raw" {
		return Options{}, false
	}
	_, known := Formats[format]
	overCap := maxKbps > 0 && (srcKbps == 0 || int(srcKbps) > maxKbps)
	sameFormat := format == "" || format == suffix || !known
	if sameFormat && !overCap {
		return Options{}, false
	}
	target := format
	if !known {
		target = "mp3"
	}
	kbps := defaultKbps[target]
	if maxKbps > 0 {
		kbps = min(maxKbps, MaxKbps)
	}
	return Options{Format: target, BitrateKbps: max(kbps, MinKbps)}, true
}

// OffsetPlan is the conversion a timeOffset needs when Plan asked for none:
// the file's own format when this server can make it, else mp3, at the file's
// bitrate within the encoder's bounds.
func OffsetPlan(suffix string, srcKbps uint32) Options {
	format := suffix
	if _, ok := Formats[format]; !ok {
		format = "mp3"
	}
	kbps := defaultKbps[format]
	if srcKbps > 0 {
		kbps = min(int(srcKbps), MaxKbps)
	}
	return Options{Format: format, BitrateKbps: max(kbps, MinKbps)}
}
