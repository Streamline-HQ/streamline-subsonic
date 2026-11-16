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

// Lyrics is a song's words, timed when the source was LRC. Lang is the
// language an LRC [la:] tag names, as written; empty when there is none.
type Lyrics struct {
	Synced bool
	Lang   string
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
	// wordStamp is the per-word time of enhanced (A2) LRC, "<mm:ss.xx>";
	// the line keeps only its words.
	wordStamp = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`)
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
		lang    string
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
			rest = stripWordStamps(rest)
			for _, ms := range stamps {
				start := ms
				timed = append(timed, Line{Start: &start, Value: rest})
			}
			continue
		}
		if m := idTag.FindStringSubmatch(rest); m != nil {
			if strings.EqualFold(m[1], "la") {
				lang = strings.TrimSpace(m[2])
			}
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
		return Lyrics{Synced: true, Lang: lang, Lines: timed}
	}
	// Trim blank lines at either end; keep the stanza breaks between.
	for len(untimed) > 0 && untimed[0].Value == "" {
		untimed = untimed[1:]
	}
	for len(untimed) > 0 && untimed[len(untimed)-1].Value == "" {
		untimed = untimed[:len(untimed)-1]
	}
	return Lyrics{Lang: lang, Lines: untimed}
}

// UnknownLang is the lang of lyrics whose language is not known: the value
// taggers write, which OpenSubsonic accepts alongside und.
const UnknownLang = "xxx"

// NormalizeLang makes a language a source names an ISO 639 code: two or
// three letters, lowercased. Anything else, an empty value included, is
// UnknownLang.
func NormalizeLang(raw string) string {
	code := strings.ToLower(strings.TrimSpace(raw))
	if len(code) < 2 || len(code) > 3 {
		return UnknownLang
	}
	for _, r := range code {
		if r < 'a' || r > 'z' {
			return UnknownLang
		}
	}
	return code
}

// stripWordStamps drops enhanced LRC word times from a line, and the spaces
// they leave doubled. A time inside a word marks a syllable, so it joins the
// two halves rather than splitting them.
func stripWordStamps(line string) string {
	if !wordStamp.MatchString(line) {
		return line
	}
	return strings.Join(strings.Fields(wordStamp.ReplaceAllString(line, "")), " ")
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

// Tag is one lyrics tag of a file: its text, and the language its key names
// ("eng" for ffmpeg's "lyrics-eng"), empty when the key names none.
type Tag struct {
	Lang string
	Text string
}

// FromProbe finds the first lyrics tag in the JSON ffprobe printed for
// ProbeArgs. An empty string is none.
func FromProbe(out []byte) string {
	if tags := TagsFromProbe(out); len(tags) > 0 {
		return tags[0].Text
	}
	return ""
}

// TagsFromProbe finds every lyrics tag in the JSON ffprobe printed for
// ProbeArgs: "lyrics", "unsyncedlyrics" or a USLT frame, which ffmpeg names
// "lyrics-<language>" (or "lyrics-<description>-<language>"). The file's tags
// come before its streams', each set in key order; a text met twice, as when
// a container repeats a stream's tags, is listed once.
func TagsFromProbe(out []byte) []Tag {
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return nil
	}
	sources := []probeTags{p.Format.Tags}
	for _, s := range p.Streams {
		sources = append(sources, s.Tags)
	}
	var found []Tag
	seen := map[string]bool{}
	for _, tags := range sources {
		keys := make([]string, 0, len(tags))
		for k := range tags {
			keys = append(keys, k)
		}
		// Map order is random; a fixed one keeps the answer stable.
		sort.Strings(keys)
		for _, k := range keys {
			key := strings.ToLower(k)
			text := tags[k]
			if !isLyricsKey(key) || strings.TrimSpace(text) == "" || seen[text] {
				continue
			}
			seen[text] = true
			found = append(found, Tag{Lang: keyLang(key), Text: text})
		}
	}
	return found
}

// keyLang is the language at the end of a "lyrics-…" or "uslt-…" key.
func keyLang(key string) string {
	_, lang, found := strings.CutLast(key, "-")
	if !found {
		return ""
	}
	return lang
}

func isLyricsKey(k string) bool {
	switch k {
	case "lyrics", "unsyncedlyrics", "unsynced lyrics", "uslt", "syncedlyrics":
		return true
	}
	return strings.HasPrefix(k, "lyrics-") || strings.HasPrefix(k, "uslt-")
}
