package main

import (
	"io"
	"os"
	"path"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
	"github.com/streamline-hq/streamline-subsonic/envelope"
	"github.com/streamline-hq/streamline-subsonic/ids"
	"github.com/streamline-hq/streamline-subsonic/lrc"
)

func init() {
	register("getLyrics", lyricsByName)
	register("getLyricsBySongId", lyricsBySongID)
}

// creditedArtist is the artist a song is named by: its own credit, else the
// album owner.
func creditedArtist(credit, owner string) string {
	if credit == "" {
		return owner
	}
	return credit
}

// lyricsMaxSongs bounds how many songs of one name getLyrics reads files for.
const lyricsMaxSongs = 100

// lyricsOf reads a song's lyrics: a sidecar .lrc of the same name first, the
// tag embedded in the file second. Either may be LRC-timed.
func lyricsOf(c *reqCtx, hostPath string) (lrc.Lyrics, bool) {
	sidecar := strings.TrimSuffix(hostPath, path.Ext(hostPath)) + ".lrc"
	if text, ok := lyricsSidecar(c, sidecar); ok {
		if p := lrc.Parse(text); len(p.Lines) > 0 {
			return p, true
		}
	}
	res, err := sdk.FFmpeg("ffprobe", lrc.ProbeArgs(hostPath)...)
	if err != nil || res.Error != "" || res.ExitCode != 0 {
		return lrc.Lyrics{}, false
	}
	p := lrc.Parse(lrc.FromProbe(res.Stdout))
	return p, len(p.Lines) > 0
}

func lyricsSidecar(c *reqCtx, hostPath string) (string, bool) {
	guest, ok := c.req.GuestPath(hostPath)
	if !ok {
		return "", false
	}
	f, err := os.Open(guest) //nolint:gosec // a mapped library path
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, lrc.MaxFile))
	if err != nil || len(buf) == 0 {
		return "", false
	}
	return string(buf), true
}

// lyricsFile is the path of a track's first file on disk, if it has one.
func lyricsFile(trackID uint32) (string, bool, error) {
	r, found, err := queryRow(
		`SELECT path FROM streamline_music_files
		WHERE track_id = ? AND missing = 0 ORDER BY id LIMIT 1`,
		trackID,
	)
	if err != nil || !found {
		return "", false, err
	}
	return r.str(0), true, nil
}

// lyricsByName finds a song by artist and title and answers its lyrics as
// plain text; an empty element when there are none.
func lyricsByName(c *reqCtx) sdk.Response {
	artistName := strings.TrimSpace(c.param("artist"))
	title := strings.TrimSpace(c.param("title"))
	out := &lyrics{}
	if title == "" {
		return c.ok(&body{Lyrics: out})
	}
	sql := `SELECT t.id, t.title, ar.name, t.artist_credit
		FROM streamline_tracks t
		JOIN streamline_albums al ON al.id = t.album_id
		JOIN streamline_artists ar ON ar.id = al.artist_id
		WHERE lower(t.title) = lower(?)
		AND EXISTS (SELECT 1 FROM streamline_music_files f
			WHERE f.track_id = t.id AND f.missing = 0)`
	args := []any{title}
	if artistName != "" {
		sql += " AND lower(ar.name) = lower(?)"
		args = append(args, artistName)
	}
	rows, err := query(sql+" ORDER BY t.id LIMIT ?", append(args, lyricsMaxSongs)...)
	if err != nil {
		return c.failErr(err)
	}
	for _, r := range rows {
		hostPath, ok, err := lyricsFile(r.uint(0))
		if err != nil {
			return c.failErr(err)
		}
		if !ok {
			continue
		}
		if lyr, found := lyricsOf(c, hostPath); found {
			out = &lyrics{
				Artist: creditedArtist(r.str(3), r.str(2)),
				Title:  r.str(1),
				Value:  lyr.Plain(),
			}
			break
		}
	}
	return c.ok(&body{Lyrics: out})
}

// lyricsBySongID is the OpenSubsonic songLyrics endpoint: the song's lyrics as
// lines, timed when the source is LRC.
func lyricsBySongID(c *reqCtx) sdk.Response {
	id, err := c.idParam("id", ids.KindTrack)
	if err != nil {
		return c.failErr(err)
	}
	r, found, err := queryRow(
		`SELECT t.title, ar.name, t.artist_credit
		FROM streamline_tracks t
		JOIN streamline_albums al ON al.id = t.album_id
		JOIN streamline_artists ar ON ar.id = al.artist_id
		WHERE t.id = ?`,
		id,
	)
	if err != nil {
		return c.failErr(err)
	}
	if !found {
		return c.failErr(envelope.NotFound())
	}
	out := &lyricsList{StructuredLyrics: []structuredLyrics{}}
	hostPath, ok, err := lyricsFile(id)
	if err != nil {
		return c.failErr(err)
	}
	if ok {
		if lyr, found := lyricsOf(c, hostPath); found {
			lines := make([]lyricLine, 0, len(lyr.Lines))
			for _, l := range lyr.Lines {
				lines = append(lines, lyricLine{Start: l.Start, Value: l.Value})
			}
			out.StructuredLyrics = append(out.StructuredLyrics, structuredLyrics{
				DisplayArtist: creditedArtist(r.str(2), r.str(1)),
				DisplayTitle:  r.str(0),
				Lang:          "xxx",
				Synced:        lyr.Synced,
				Line:          lines,
			})
		}
	}
	return c.ok(&body{LyricsList: out})
}
