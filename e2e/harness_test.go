package e2e

import (
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the harness", Label("e2e"), func() {
	It("seeds a library with real audio, a poster and a track with no file", func() {
		lib := app.sharedLibrary()
		Expect(lib.Artists).To(HaveLen(2))

		first := lib.track(0, 0, 0)
		Expect(first.Title).To(Equal("Dawn"))
		Expect(first.Path).To(HavePrefix(app.MusicDir))
		out, err := exec.Command(
			"ffprobe", "-v", "error", "-show_entries", "format=format_name",
			"-of", "default=nw=1:nk=1", first.Path,
		).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(out))
		Expect(strings.TrimSpace(string(out))).To(Equal("flac"))

		Expect(lib.track(1, 0, 0).Format).To(Equal("mp3"))
		Expect(lib.track(0, 1, 1).Path).To(BeEmpty())
		Expect(lib.track(0, 1, 1).FileID).To(BeZero())

		Expect(
			app.ArtworkDir + "/albums/" + itoa(
				lib.Artists[0].Albums[0].ID,
			) + "/poster.jpg",
		).
			To(BeAnExistingFile())
		Expect(app.ArtworkDir + "/albums/" + itoa(lib.Artists[1].Albums[0].ID)).
			NotTo(BeAnExistingFile())

		var files int
		Expect(app.core.QueryRow(
			"SELECT count(*) FROM media_files WHERE track_media_files IS NOT NULL",
		).Scan(&files)).To(Succeed())
		Expect(files).To(BeNumerically(">=", 6))
	})

	It("starts every spec's users without a Subsonic password", func() {
		u := app.newUser("fresh")
		Expect(u.Password).To(BeEmpty())
		db := app.ext()
		defer db.Close()
		var n int
		Expect(db.QueryRow(
			"SELECT count(*) FROM accounts WHERE user_id = ?", u.ID,
		).Scan(&n)).To(Succeed())
		Expect(n).To(BeZero())
	})

	It("creates the extension's database and its own music root", func() {
		Expect(app.extPath()).To(BeAnExistingFile())
		info, err := os.Stat(app.MusicDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.IsDir()).To(BeTrue())
	})
})
