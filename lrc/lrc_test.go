package lrc

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parse", Label("unit"), func() {
	It(
		"reads timed lines, several stamps per line, sorted, with the offset",
		func() {
			p := Parse(
				"\ufeff[ar:Nirvana]\n[offset:+500]\n[00:12.50]Load up\n[00:05.00][01:00]Hello\n",
			)
			Expect(p.Synced).To(BeTrue())
			starts := make([]int64, 0, len(p.Lines))
			words := make([]string, 0, len(p.Lines))
			for _, l := range p.Lines {
				starts = append(starts, *l.Start)
				words = append(words, l.Value)
			}
			Expect(starts).To(Equal([]int64{4500, 12000, 59500}))
			Expect(words).To(Equal([]string{"Hello", "Load up", "Hello"}))
		},
	)

	It("keeps untimed text line by line, stanza breaks included", func() {
		p := Parse("\nFirst line\n\nSecond verse\n\n")
		Expect(p.Synced).To(BeFalse())
		Expect(p.Plain()).To(Equal("First line\n\nSecond verse"))
	})

	It("reads one- and three-digit fractions", func() {
		p := Parse("[00:01.5]a\n[00:02.250]b")
		Expect(*p.Lines[0].Start).To(BeEquivalentTo(1500))
		Expect(*p.Lines[1].Start).To(BeEquivalentTo(2250))
	})

	It("drops the word times of enhanced LRC and keeps the words", func() {
		p := Parse("[00:01.00] <00:01.00> When <00:01.40> the <00:01.90>\n" +
			"[00:03.00]<00:03.00>Hel<00:03.20>lo<00:03.50> world\n" +
			"[00:05.00]Plain  spacing <kept>\n")
		Expect(p.Synced).To(BeTrue())
		values := make([]string, 0, len(p.Lines))
		for _, l := range p.Lines {
			values = append(values, l.Value)
		}
		Expect(values).To(Equal([]string{
			"When the", "Hello world", "Plain  spacing <kept>",
		}))
		Expect(*p.Lines[1].Start).To(BeEquivalentTo(3000))
	})

	It("reads the language of an [la:] tag", func() {
		Expect(Parse("[la:fr]\n[00:01.00]Salut").Lang).To(Equal("fr"))
		Expect(Parse("[la: eng ]\nUntimed").Lang).To(Equal("eng"))
		Expect(Parse("[00:01.00]No tag").Lang).To(BeEmpty())
	})

	It("keeps a section header in plain lyrics and drops the ID tags", func() {
		p := Parse("[ar: Rihanna]\n[ti:Work]\n[Chorus: Rihanna]\nwork work\n")
		values := make([]string, 0, len(p.Lines))
		for _, l := range p.Lines {
			values = append(values, l.Value)
		}
		Expect(values).To(Equal([]string{"[Chorus: Rihanna]", "work work"}))
	})
})

var _ = Describe("NormalizeLang", Label("unit"), func() {
	It(
		"keeps an ISO 639 code, lowercased, and answers xxx for anything else",
		func() {
			Expect(NormalizeLang("eng")).To(Equal("eng"))
			Expect(NormalizeLang(" FR ")).To(Equal("fr"))
			Expect(NormalizeLang("und")).To(Equal("und"))
			Expect(NormalizeLang("XXX")).To(Equal("xxx"))
			for _, raw := range []string{"", "e", "english", "en-US", "e1g", "日本"} {
				Expect(NormalizeLang(raw)).To(Equal(UnknownLang), raw)
			}
		},
	)
})

var _ = Describe("FromProbe", Label("unit"), func() {
	It("finds a Vorbis LYRICS comment", func() {
		out := `{"format":{"tags":{"TITLE":"x","LYRICS":"[00:01.00]hi"}}}`
		Expect(FromProbe([]byte(out))).To(Equal("[00:01.00]hi"))
	})

	It("finds an ID3 USLT frame ffmpeg names with a language", func() {
		out := `{"format":{"tags":{"lyrics-eng":"words"}}}`
		Expect(FromProbe([]byte(out))).To(Equal("words"))
	})

	It("looks in the stream's tags too", func() {
		out := `{"streams":[{"tags":{"unsyncedlyrics":"words"}}]}`
		Expect(FromProbe([]byte(out))).To(Equal("words"))
	})

	It("lists every lyrics tag with the language its key names", func() {
		out := `{"format":{"tags":{"lyrics-fra":"Bonjour","lyrics-eng":"Hello",
			"LYRICS":"Plain"}},"streams":[{"tags":{"lyrics-eng":"Hello"}}]}`
		Expect(TagsFromProbe([]byte(out))).To(Equal([]Tag{
			{Lang: "", Text: "Plain"},
			{Lang: "eng", Text: "Hello"},
			{Lang: "fra", Text: "Bonjour"},
		}))
		Expect(FromProbe([]byte(out))).To(Equal("Plain"))
	})

	It("answers nothing for no tag, a blank one or output that is not JSON", func() {
		Expect(FromProbe([]byte(`{"format":{"tags":{"title":"x"}}}`))).To(BeEmpty())
		Expect(
			FromProbe([]byte(`{"format":{"tags":{"lyrics":"  "}}}`)),
		).To(BeEmpty())
		Expect(FromProbe([]byte("nope"))).To(BeEmpty())
	})
})
