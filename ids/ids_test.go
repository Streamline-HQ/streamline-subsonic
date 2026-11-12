package ids

import (
	"math"
	"strconv"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("ids", g.Label("unit"), func() {
	g.It("round-trips every kind", func() {
		for kind, format := range map[string]func(uint32) string{
			KindArtist:   Artist,
			KindAlbum:    Album,
			KindTrack:    Track,
			KindPlaylist: Playlist,
			KindShare:    Share,
			KindRadio:    Radio,
		} {
			s := format(42)
			Expect(s).To(Equal(kind + "-42"))
			gotKind, gotID, err := Parse(s)
			Expect(err).NotTo(HaveOccurred())
			Expect(gotKind).To(Equal(kind))
			Expect(gotID).To(Equal(uint32(42)))
		}
	})

	g.It("accepts the largest row id", func() {
		_, id, err := Parse(Track(math.MaxUint32))
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(uint32(math.MaxUint32)))
	})

	g.DescribeTable("rejects a malformed id",
		func(s string) {
			_, _, err := Parse(s)
			Expect(err).To(HaveOccurred())
		},
		g.Entry("empty", ""),
		g.Entry("no separator", "tr42"),
		g.Entry("unknown kind", "xx-1"),
		g.Entry("no number", "tr-"),
		g.Entry("not a number", "tr-abc"),
		g.Entry("negative", "tr--1"),
		g.Entry("signed", "tr-+1"),
		g.Entry("overflows uint32", "tr-"+strconv.FormatUint(math.MaxUint32+1, 10)),
		g.Entry("a bare number", "42"),
	)
})
