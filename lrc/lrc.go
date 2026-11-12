// Package lrc reads a song's lyrics from LRC text and from ffprobe's
// output. It holds no host calls, so it runs natively under test.
package lrc

import (
	"bufio"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// MaxFile bounds a sidecar .lrc read: real ones are a few KB.
const MaxFile = 256 << 10

// Line is one line of lyrics; Start is milliseconds, nil for an untimed one.
type Line struct {
	Start *int64
	Value string
}

// Lyrics is a song's words, timed when the source was LRC.
type Lyrics struct {
	Synced bool
	Lines  []Line
}

// Plain is the lyrics as text, timestamps dropped.
func (l Lyrics) Plain() string {
	out := make([]string, 0, len(l.Lines))
	for _, line := range l.Lines {
		out = append(out, line.Value)
	}
	return strings.Join(out, "\n")
}

var (
	timeStamp = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	// The LRC ID tags only: a section header such as "[Chorus: Rihanna]" in
	// plain lyrics is text and stays.
	idTag = regexp.MustCompile(
		`(?i)^\[(ar|al|ti|au|by|offset|re|ve|length|tool|la|#)\s*:(.*)\]\s*$`,
	)
)

// Parse reads LRC ("[01:02.34]words", several stamps allowed on a line,
// [offset:+ms] honoured) into timed lines; text with no stamp at all is
// returned line by line, untimed.
func Parse(text string) Lyrics {
	var (
		timed   []Line
		untimed []Line
		offset  int64
	)
	sc := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(text, "\ufeff")))
	sc.Buffer(make([]byte, 0, 64<<10), MaxFile)
	for sc.Scan() {
		rest := strings.TrimSpace(strings.TrimRight(sc.Text(), "\r"))
		var stamps []int64
		for {
			m := timeStamp.FindStringSubmatch(rest)
			if m == nil {
				break
			}
			stamps = append(stamps, millis(m))
			rest = strings.TrimSpace(rest[len(m[0]):])
		}
		if len(stamps) > 0 {
			for _, ms := range stamps {
				start := ms
				timed = append(timed, Line{Start: &start, Value: rest})
			}
			continue
		}
		if m := idTag.FindStringSubmatch(rest); m != nil {
			if strings.EqualFold(m[1], "offset") {
				if n, err := strconv.ParseInt(
					strings.TrimSpace(m[2]),
					10,
					64,
				); err == nil {
					offset = n
				}
			}
			continue
		}
		untimed = append(untimed, Line{Value: rest})
	}
	if len(timed) > 0 {
		sort.SliceStable(timed, func(i, j int) bool {
			return *timed[i].Start < *timed[j].Start
		})
		for i := range timed {
			// A positive offset shows the words earlier.
			*timed[i].Start = max(*timed[i].Start-offset, 0)
		}
		return Lyrics{Synced: true, Lines: timed}
	}
	// Trim blank lines at either end; keep the stanza breaks between.
	for len(untimed) > 0 && untimed[0].Value == "" {
		untimed = untimed[1:]
	}
	for len(untimed) > 0 && untimed[len(untimed)-1].Value == "" {
		untimed = untimed[:len(untimed)-1]
	}
	return Lyrics{Lines: untimed}
}

func millis(m []string) int64 {
	minutes, _ := strconv.ParseInt(m[1], 10, 64)
	seconds, _ := strconv.ParseInt(m[2], 10, 64)
	var frac int64
	if m[3] != "" {
		frac, _ = strconv.ParseInt(m[3], 10, 64)
		switch len(m[3]) {
		case 1:
			frac *= 100
		case 2:
			frac *= 10
		}
	}
	return (minutes*60+seconds)*1000 + frac
}

// ProbeArgs is the ffprobe command line that reports a file's tags, host path
// in src.
func ProbeArgs(src string) []string {
	return []string{
		"-v", "quiet", "-print_format", "json",
		"-show_entries", "format_tags:stream_tags", src,
	}
}

type probeTags map[string]string

type probeOutput struct {
	Format  struct{ Tags probeTags } `json:"format"`
	Streams []struct{ Tags probeTags }
}

// FromProbe finds the lyrics tag in the JSON ffprobe printed for ProbeArgs:
// "lyrics", "unsyncedlyrics" or a USLT frame, which ffmpeg names
// "lyrics-<language>". An empty string is none.
func FromProbe(out []byte) string {
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return ""
	}
	sources := []probeTags{p.Format.Tags}
	for _, s := range p.Streams {
		sources = append(sources, s.Tags)
	}
	for _, tags := range sources {
		keys := make([]string, 0, len(tags))
		for k := range tags {
			keys = append(keys, k)
		}
		// Map order is random; a fixed one keeps the answer stable.
		sort.Strings(keys)
		for _, k := range keys {
			if isLyricsKey(strings.ToLower(k)) && strings.TrimSpace(tags[k]) != "" {
				return tags[k]
			}
		}
	}
	return ""
}

func isLyricsKey(k string) bool {
	switch k {
	case "lyrics", "unsyncedlyrics", "unsynced lyrics", "uslt", "syncedlyrics":
		return true
	}
	return strings.HasPrefix(k, "lyrics-") || strings.HasPrefix(k, "uslt-")
}
