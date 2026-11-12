// Package ids is the Subsonic id scheme: every id the server hands out is
// "<kind>-<row id>", so one parameter can name any kind and a handler can
// refuse the wrong one.
package ids

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	KindArtist   = "ar"
	KindAlbum    = "al"
	KindTrack    = "tr"
	KindPlaylist = "pl"
	KindShare    = "sh"
	KindRadio    = "rs"
)

var knownKinds = map[string]struct{}{
	KindArtist: {}, KindAlbum: {}, KindTrack: {},
	KindPlaylist: {}, KindShare: {}, KindRadio: {},
}

func Format(kind string, id uint32) string {
	return fmt.Sprintf("%s-%d", kind, id)
}

func Artist(id uint32) string   { return Format(KindArtist, id) }
func Album(id uint32) string    { return Format(KindAlbum, id) }
func Track(id uint32) string    { return Format(KindTrack, id) }
func Playlist(id uint32) string { return Format(KindPlaylist, id) }
func Share(id uint32) string    { return Format(KindShare, id) }
func Radio(id uint32) string    { return Format(KindRadio, id) }

// Parse splits an id into its kind and row id. A kind this server never hands
// out, a missing or non-numeric tail and a tail that overflows uint32 are all
// malformed.
func Parse(s string) (kind string, id uint32, err error) {
	kind, tail, found := strings.Cut(s, "-")
	if _, known := knownKinds[kind]; !found || !known {
		return "", 0, fmt.Errorf("malformed id %q", s)
	}
	n, err := strconv.ParseUint(tail, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("malformed id %q: %w", s, err)
	}
	return kind, uint32(n), nil
}
