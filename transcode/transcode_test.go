package transcode

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Args", Label("unit"), func() {
	It("seeks before the input and encodes the first audio stream", func() {
		args, err := Args("/m/a.flac", Options{
			Format: "opus", BitrateKbps: 96, OffsetSec: 30,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal([]string{
			"-hide_banner", "-nostdin", "-loglevel", "error",
			"-ss", "30",
			"-i", "/m/a.flac",
			"-map", "0:a:0",
			"-vn", "-sn", "-dn",
			"-map_metadata", "-1",
			"-c:a", "libopus",
			"-b:a", "96k",
			"-f", "ogg",
			"pipe:1",
		}))
	})

	It("omits the seek at offset zero", func() {
		args, err := Args("/m/a.flac", Options{Format: "mp3", BitrateKbps: 192})
		Expect(err).NotTo(HaveOccurred())
		Expect(args).NotTo(ContainElement("-ss"))
		Expect(args).To(ContainElements("libmp3lame", "192k", "mp3"))
	})

	It("writes AAC as ADTS", func() {
		args, err := Args("/m/a.flac", Options{Format: "aac", BitrateKbps: 128})
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(ContainElements("aac", "adts"))
	})

	It("refuses a format it cannot produce", func() {
		_, err := Args("/m/a.flac", Options{Format: "flac"})
		Expect(err).To(MatchError(ErrUnknownFormat))
	})
})

var _ = DescribeTable(
	"Plan",
	Label("unit"),
	func(suffix string, srcKbps int, format string, maxKbps int, want *Options) {
		got, convert := Plan(suffix, uint32(srcKbps), format, maxKbps)
		if want == nil {
			Expect(convert).To(BeFalse())
			return
		}
		Expect(convert).To(BeTrue())
		Expect(got).To(Equal(*want))
	},
	Entry("no format, no cap: the original", "flac", 900, "", 0, nil),
	Entry("raw always wins", "flac", 900, "raw", 128, nil),
	Entry("the file's own format", "mp3", 320, "mp3", 0, nil),
	Entry("a format this server cannot make", "mp3", 320, "flac", 0, nil),
	Entry("under the cap", "mp3", 128, "", 192, nil),
	Entry("over the cap", "flac", 900, "", 192,
		&Options{Format: "mp3", BitrateKbps: 192}),
	Entry("unknown bitrate under a cap", "flac", 0, "", 256,
		&Options{Format: "mp3", BitrateKbps: 256}),
	Entry("another format at its default", "flac", 900, "opus", 0,
		&Options{Format: "opus", BitrateKbps: 128}),
	Entry("a format and a cap", "flac", 900, "OPUS", 96,
		&Options{Format: "opus", BitrateKbps: 96}),
	Entry("a cap past the encoder's ceiling", "flac", 900, "aac", 1411,
		&Options{Format: "aac", BitrateKbps: 320}),
	Entry("a cap below the floor", "flac", 900, "", 8,
		&Options{Format: "mp3", BitrateKbps: 32}),
	Entry(
		"an unknown format over the cap falls back to mp3",
		"flac",
		900,
		"wma",
		128,
		&Options{Format: "mp3", BitrateKbps: 128},
	),
)

var _ = Describe("OffsetPlan", Label("unit"), func() {
	It("keeps the file's format at its bitrate", func() {
		Expect(
			OffsetPlan("opus", 96),
		).To(Equal(Options{Format: "opus", BitrateKbps: 96}))
	})

	It("falls back to mp3 for a format it cannot make", func() {
		Expect(
			OffsetPlan("flac", 900),
		).To(Equal(Options{Format: "mp3", BitrateKbps: 320}))
	})

	It("uses the format's default when the bitrate is unknown", func() {
		Expect(
			OffsetPlan("flac", 0),
		).To(Equal(Options{Format: "mp3", BitrateKbps: 192}))
	})

	It("stays above the encoder's floor", func() {
		Expect(
			OffsetPlan("mp3", 8),
		).To(Equal(Options{Format: "mp3", BitrateKbps: 32}))
	})
})
