package e2e

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func trackID(t seededTrack) string { return "tr-" + itoa(t.ID) }

// isMP3 reports an ID3 tag or an MPEG audio frame header at the start.
func isMP3(b []byte) bool {
	return bytes.HasPrefix(b, []byte("ID3")) ||
		(len(b) > 1 && b[0] == 0xFF && b[1]&0xE0 == 0xE0)
}

func wantCode(raw rawResponse, code int) {
	GinkgoHelper()
	Expect(raw.Status).To(Equal(http.StatusOK))
	resp := raw.json()
	Expect(resp["status"]).To(Equal("failed"), "%v", resp)
	Expect(errorCode(obj(resp, "error"))).To(Equal(code))
}

// tagFile rewrites an audio file with one more metadata tag, streams copied.
func tagFile(path, key, value string) {
	GinkgoHelper()
	tmp := path + ".tagged" + filepath.Ext(path)
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-i", path, "-c", "copy", "-metadata", key+"="+value, tmp).CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), string(out))
	Expect(os.Rename(tmp, path)).To(Succeed())
}

var _ = Describe("stream and download", Label("e2e"), func() {
	var (
		lib library
		u   *testUser
		bin func(t seededTrack) []byte
	)

	BeforeEach(func() {
		lib = app.sharedLibrary()
		u = app.subsonicUser("media")
		bin = func(t seededTrack) []byte {
			GinkgoHelper()
			b, err := os.ReadFile(t.Path)
			Expect(err).NotTo(HaveOccurred())
			return b
		}
	})

	Describe("stream", func() {
		It("sends the original with range support advertised", func() {
			t := lib.track(0, 0, 0)
			raw := u.get("stream", url.Values{"id": {trackID(t)}})
			Expect(raw.Status).To(Equal(http.StatusOK))
			Expect(raw.Header.Get("Content-Type")).To(Equal("audio/flac"))
			Expect(raw.Header.Get("Accept-Ranges")).To(Equal("bytes"))
			Expect(raw.Body).To(Equal(bin(t)))
		})

		It("answers a range request with 206", func() {
			t := lib.track(0, 0, 0)
			q := u.signed(url.Values{"id": {trackID(t)}})
			raw := fetch(http.MethodGet, "/rest/stream?"+q.Encode(), nil,
				http.Header{"Range": {"bytes=0-3"}})
			Expect(raw.Status).To(Equal(http.StatusPartialContent))
			Expect(raw.Body).To(Equal(bin(t)[:4]))
		})

		It("answers on the .view path too", func() {
			t := lib.track(0, 0, 1)
			raw := u.get("stream.view", url.Values{"id": {trackID(t)}})
			Expect(raw.Body).To(Equal(bin(t)))
		})

		It("sends the original when the client names no conversion", func() {
			t := lib.track(1, 0, 0)
			raw := u.get("stream", url.Values{
				"id": {trackID(t)}, "format": {"raw"}, "maxBitRate": {"0"},
			})
			Expect(raw.Header.Get("Content-Type")).To(Equal("audio/mpeg"))
			Expect(raw.Body).To(Equal(bin(t)))
		})

		It("transcodes a file to the format the client asks for", func() {
			t := lib.track(0, 0, 0)
			raw := u.get("stream", url.Values{
				"id": {trackID(t)}, "format": {"mp3"}, "maxBitRate": {"128"},
			})
			Expect(raw.Status).To(Equal(http.StatusOK))
			Expect(raw.Header.Get("Content-Type")).To(Equal("audio/mpeg"))
			Expect(isMP3(raw.Body)).To(BeTrue(), "not MP3: % x", raw.Body[:8])
			Expect(raw.Body).NotTo(Equal(bin(t)))
		})

		It("transcodes to Opus in Ogg and to AAC in ADTS", func() {
			t := lib.track(0, 0, 0)
			opus := u.get(
				"stream",
				url.Values{"id": {trackID(t)}, "format": {"opus"}},
			)
			Expect(opus.Header.Get("Content-Type")).To(Equal("audio/ogg"))
			Expect(opus.Body).To(HavePrefix("OggS"))
			aac := u.get("stream", url.Values{"id": {trackID(t)}, "format": {"aac"}})
			Expect(aac.Header.Get("Content-Type")).To(Equal("audio/aac"))
			Expect(aac.Body[0]).To(BeEquivalentTo(0xFF))
			Expect(aac.Body[1] & 0xF0).To(BeEquivalentTo(0xF0))
		})

		It("transcodes when the client caps the bitrate under the file's", func() {
			t := lib.track(0, 0, 0)
			raw := u.get("stream", url.Values{
				"id": {trackID(t)}, "maxBitRate": {"64"},
			})
			Expect(raw.Header.Get("Content-Type")).To(Equal("audio/mpeg"))
			Expect(isMP3(raw.Body)).To(BeTrue())
		})

		It("holds an account's own cap even against a request for raw", func() {
			capped := app.subsonicUser("capped")
			app.setSettings(capped, map[string]any{"max_bit_rate": 64})
			t := lib.track(0, 0, 0)
			raw := capped.get("stream", url.Values{
				"id": {trackID(t)}, "format": {"raw"},
			})
			Expect(raw.Header.Get("Content-Type")).To(Equal("audio/mpeg"))
			Expect(raw.Body).NotTo(Equal(bin(t)))
		})

		It("starts a transcode at timeOffset, even when none was asked for", func() {
			long := app.addArtist(artistSpec{
				Name: app.unique("Offset"),
				Albums: []albumSpec{{
					Title: "Long", Year: 2020,
					Tracks: []trackSpec{{Title: "Drone", Position: 1, Seconds: 8}},
				}},
			})
			t := long.Albums[0].Tracks[0]
			whole := u.get("stream", url.Values{
				"id": {trackID(t)}, "format": {"mp3"}, "maxBitRate": {"128"},
			})
			late := u.get("stream", url.Values{
				"id": {trackID(t)}, "format": {"mp3"}, "maxBitRate": {"128"},
				"timeOffset": {"5"},
			})
			Expect(isMP3(late.Body)).To(BeTrue())
			Expect(len(late.Body)).To(BeNumerically("<", len(whole.Body)*3/4))

			bare := u.get("stream", url.Values{
				"id": {trackID(t)}, "timeOffset": {"5"},
			})
			Expect(bare.Status).To(Equal(http.StatusOK))
			Expect(bare.Body).NotTo(Equal(bin(t)))
			Expect(bare.Header.Get("Content-Type")).To(Equal("audio/mpeg"))
		})

		It("refuses a timeOffset when ffmpeg cannot run, rather than play from 0",
			func() {
				app.api(http.MethodPatch, "/api/v1/config/ffmpeg",
					map[string]any{"enabled": false}, http.StatusOK)
				DeferCleanup(func() {
					app.api(http.MethodPatch, "/api/v1/config/ffmpeg",
						map[string]any{"enabled": true}, http.StatusOK)
				})
				t := lib.track(0, 0, 0)
				wantCode(u.get("stream", url.Values{
					"id": {trackID(t)}, "format": {"mp3"}, "timeOffset": {"3"},
				}), 0)
			})

		It("falls back to the original when ffmpeg cannot run", func() {
			app.api(http.MethodPatch, "/api/v1/config/ffmpeg",
				map[string]any{"enabled": false}, http.StatusOK)
			DeferCleanup(func() {
				app.api(http.MethodPatch, "/api/v1/config/ffmpeg",
					map[string]any{"enabled": true}, http.StatusOK)
			})
			t := lib.track(0, 0, 0)
			raw := u.get("stream", url.Values{
				"id": {trackID(t)}, "format": {"mp3"},
			})
			Expect(raw.Body).To(Equal(bin(t)))
		})

		It("answers 70 for a track with no file", func() {
			wantCode(
				u.get("stream", url.Values{"id": {trackID(lib.track(0, 1, 1))}}),
				70,
			)
		})

		It("answers 70 for ids that are missing, malformed, of another kind or gone",
			func() {
				wantCode(u.get("stream", nil), 10)
				wantCode(u.get("stream", url.Values{"id": {"nonsense"}}), 70)
				wantCode(u.get("stream", url.Values{
					"id": {"al-" + itoa(lib.Artists[0].Albums[0].ID)},
				}), 70)
				wantCode(u.get("stream", url.Values{"id": {"tr-999999"}}), 70)
			})

		It("answers 70 when the file is gone from disk", func() {
			own := app.addArtist(artistSpec{
				Name: app.unique("Gone"),
				Albums: []albumSpec{{
					Title: "Vanished", Year: 2020,
					Tracks: []trackSpec{{Title: "Ghost", Position: 1}},
				}},
			})
			t := own.Albums[0].Tracks[0]
			Expect(os.Remove(t.Path)).To(Succeed())
			wantCode(u.get("stream", url.Values{"id": {trackID(t)}}), 70)
			wantCode(u.get("download", url.Values{"id": {trackID(t)}}), 70)
		})

		It("answers 70, not a 500, for a file outside the granted folder", func() {
			own := app.addArtist(artistSpec{
				Name: app.unique("Outside"),
				Albums: []albumSpec{{
					Title: "Elsewhere", Year: 2020,
					Tracks: []trackSpec{{Title: "Away", Position: 1}},
				}},
			})
			t := own.Albums[0].Tracks[0]
			outside := filepath.Join(GinkgoT().TempDir(), "away.flac")
			Expect(os.WriteFile(outside, []byte("not yours"), 0o600)).To(Succeed())
			_, err := app.core.Exec(
				"UPDATE media_files SET path = ? WHERE id = ?", outside, t.FileID,
			)
			Expect(err).NotTo(HaveOccurred())
			for _, endpoint := range []string{"stream", "download"} {
				raw := u.get(endpoint, url.Values{"id": {trackID(t)}})
				wantCode(raw, 70)
				Expect(string(raw.Body)).NotTo(ContainSubstring("not yours"))
			}
		})

		It("refuses an account without the stream role with error 50", func() {
			mute := app.subsonicUser("mute")
			app.setSettings(mute, map[string]any{"can_stream": false})
			wantCode(
				mute.get("stream", url.Values{"id": {trackID(lib.track(0, 0, 0))}}),
				50,
			)
		})

		It("refuses wrong credentials with error 40", func() {
			bad := app.subsonicUser("badpw")
			bad.Password = "nope"
			wantCode(
				bad.get("stream", url.Values{"id": {trackID(lib.track(0, 0, 0))}}),
				40,
			)
		})
	})

	Describe("download", func() {
		It("sends the original as an attachment, never converted", func() {
			t := lib.track(0, 0, 0)
			raw := u.get("download", url.Values{
				"id": {trackID(t)}, "format": {"mp3"}, "maxBitRate": {"32"},
			})
			Expect(raw.Status).To(Equal(http.StatusOK))
			Expect(raw.Body).To(Equal(bin(t)))
			Expect(raw.Header.Get("Content-Disposition")).To(And(
				HavePrefix("attachment"), ContainSubstring(filepath.Base(t.Path)),
			))
			Expect(raw.Header.Get("Content-Type")).To(Equal("audio/flac"))
		})

		It("refuses an account without the download role with error 50", func() {
			locked := app.subsonicUser("nodl")
			app.setSettings(locked, map[string]any{"can_download": false})
			wantCode(
				locked.get(
					"download",
					url.Values{"id": {trackID(lib.track(0, 0, 0))}},
				),
				50,
			)
			Expect(
				locked.get(
					"stream",
					url.Values{"id": {trackID(lib.track(0, 0, 0))}},
				).
					Body,
			).To(Equal(bin(lib.track(0, 0, 0))))
		})

		It("answers 70 for ids that are malformed, of another kind or gone", func() {
			wantCode(u.get("download", url.Values{"id": {"x"}}), 70)
			wantCode(u.get("download", url.Values{"id": {"ar-1"}}), 70)
			wantCode(u.get("download", url.Values{"id": {"tr-999999"}}), 70)
			wantCode(u.get("download", nil), 10)
		})
	})
})

var _ = Describe("getCoverArt", Label("e2e"), func() {
	var (
		lib library
		u   *testUser
	)

	poster := func(kind string, id uint32) string {
		return filepath.Join(app.ArtworkDir, kind, itoa(id), "poster.jpg")
	}

	BeforeEach(func() {
		lib = app.sharedLibrary()
		u = app.subsonicUser("cover")
	})

	It("serves an album's poster as stored", func() {
		album := lib.Artists[0].Albums[0]
		want, err := os.ReadFile(poster("albums", album.ID))
		Expect(err).NotTo(HaveOccurred())
		raw := u.get("getCoverArt", url.Values{"id": {"al-" + itoa(album.ID)}})
		Expect(raw.Status).To(Equal(http.StatusOK))
		Expect(raw.Header.Get("Content-Type")).To(Equal("image/jpeg"))
		Expect(raw.Body).To(Equal(want))
	})

	It("serves a song's album cover", func() {
		album := lib.Artists[0].Albums[0]
		want, err := os.ReadFile(poster("albums", album.ID))
		Expect(err).NotTo(HaveOccurred())
		raw := u.get("getCoverArt", url.Values{"id": {trackID(album.Tracks[0])}})
		Expect(raw.Body).To(Equal(want))
	})

	It("serves an artist's photo, and a plain 404 when it has none", func() {
		artist := lib.Artists[1]
		raw := u.get("getCoverArt", url.Values{"id": {"ar-" + itoa(artist.ID)}})
		Expect(raw.Status).To(Equal(http.StatusNotFound))

		src, err := os.ReadFile(poster("albums", lib.Artists[0].Albums[0].ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(os.MkdirAll(filepath.Dir(poster("artists", artist.ID)), 0o755)).
			To(Succeed())
		Expect(os.WriteFile(poster("artists", artist.ID), src, 0o600)).To(Succeed())
		DeferCleanup(os.RemoveAll, filepath.Dir(poster("artists", artist.ID)))
		raw = u.get("getCoverArt", url.Values{"id": {"ar-" + itoa(artist.ID)}})
		Expect(raw.Status).To(Equal(http.StatusOK))
		Expect(raw.Body).To(Equal(src))
	})

	It("answers a plain 404 for an album with no poster", func() {
		raw := u.get("getCoverArt", url.Values{
			"id": {"al-" + itoa(lib.Artists[1].Albums[0].ID)},
		})
		Expect(raw.Status).To(Equal(http.StatusNotFound))
	})

	It("never serves a body that is not an image as one", func() {
		own := app.addArtist(artistSpec{
			Name: app.unique("Hostile"),
			Albums: []albumSpec{{
				Title: "Page", Year: 2020,
				Tracks: []trackSpec{{Title: "Html", Position: 1}},
			}},
		})
		album := own.Albums[0]
		Expect(os.MkdirAll(filepath.Dir(poster("albums", album.ID)), 0o755)).
			To(Succeed())
		Expect(os.WriteFile(poster("albums", album.ID),
			[]byte("<html><script>alert(1)</script></html>"), 0o600)).To(Succeed())
		raw := u.get("getCoverArt", url.Values{"id": {"al-" + itoa(album.ID)}})
		Expect(raw.Header.Get("Content-Type")).To(Equal("image/jpeg"))
	})

	It("answers 70 for ids that are malformed, of another kind or gone", func() {
		wantCode(u.get("getCoverArt", nil), 10)
		wantCode(u.get("getCoverArt", url.Values{"id": {"nonsense"}}), 70)
		wantCode(u.get("getCoverArt", url.Values{"id": {"tr-999999"}}), 70)
		wantCode(u.get("getCoverArt", url.Values{"id": {"al-999999"}}), 70)
		wantCode(u.get("getCoverArt", url.Values{"id": {"ar-999999"}}), 70)
		wantCode(u.get("getCoverArt", url.Values{"id": {"rs-1"}}), 70)
	})
})

var _ = Describe("getAvatar", Label("e2e"), func() {
	It("has no avatars", func() {
		u := app.subsonicUser("avatar")
		wantCode(u.get("getAvatar", nil), 10)
		Expect(
			errorCode(u.callError("getAvatar", url.Values{"username": {u.Email}})),
		).
			To(Equal(70))
	})
})

var _ = Describe("lyrics", Label("e2e"), func() {
	var (
		u     *testUser
		track seededTrack
		name  string
	)

	BeforeEach(func() {
		u = app.subsonicUser("lyrics")
		name = app.unique("Singer")
		own := app.addArtist(artistSpec{
			Name: name,
			Albums: []albumSpec{{
				Title: "Words", Year: 2020,
				Tracks: []trackSpec{
					{Title: "Verse", Position: 1},
					{Title: "Mute", Position: 2, Format: "mp3"},
				},
			}},
		})
		track = own.Albums[0].Tracks[0]
	})

	structured := func(id string) []map[string]any {
		GinkgoHelper()
		resp := u.call("getLyricsBySongId", url.Values{"id": {id}})
		return list(obj(resp, "lyricsList"), "structuredLyrics")
	}

	It("reads timed lines from a sidecar .lrc", func() {
		lrc := strings.TrimSuffix(track.Path, filepath.Ext(track.Path)) + ".lrc"
		Expect(os.WriteFile(lrc,
			[]byte("[00:01.00]Load up on guns\n[00:03.00]Bring your friends\n"),
			0o600)).To(Succeed())

		sl := structured(trackID(track))
		Expect(sl).To(HaveLen(1))
		Expect(sl[0]["synced"]).To(BeTrue())
		Expect(sl[0]["displayArtist"]).To(Equal(name))
		Expect(sl[0]["displayTitle"]).To(Equal("Verse"))
		lines := list(sl[0], "line")
		Expect(lines).To(HaveLen(2))
		Expect(lines[1]["start"]).To(BeEquivalentTo(3000))
		Expect(lines[1]["value"]).To(Equal("Bring your friends"))

		ly := obj(u.call("getLyrics", url.Values{
			"artist": {strings.ToUpper(name)}, "title": {"verse"},
		}), "lyrics")
		Expect(ly["value"]).To(Equal("Load up on guns\nBring your friends"))
		Expect(ly["artist"]).To(Equal(name))
		Expect(ly["title"]).To(Equal("Verse"))
	})

	It("reads lyrics embedded in a FLAC and in an MP3", func() {
		tagFile(track.Path, "LYRICS", "[00:02.00]Hello from the tag")
		sl := structured(trackID(track))
		Expect(sl).To(HaveLen(1))
		Expect(sl[0]["synced"]).To(BeTrue())
		Expect(list(sl[0], "line")[0]["value"]).To(Equal("Hello from the tag"))

		own := app.addArtist(artistSpec{
			Name: app.unique("Tagged"),
			Albums: []albumSpec{{
				Title: "Frames", Year: 2020,
				Tracks: []trackSpec{{Title: "Uslt", Position: 1, Format: "mp3"}},
			}},
		})
		mp3 := own.Albums[0].Tracks[0]
		tagFile(mp3.Path, "lyrics", "Plain words\nSecond line")
		sl = structured(trackID(mp3))
		Expect(sl).To(HaveLen(1))
		Expect(sl[0]["synced"]).To(BeFalse())
		Expect(list(sl[0], "line")).To(HaveLen(2))
	})

	It("prefers the sidecar over the tag", func() {
		tagFile(track.Path, "LYRICS", "from the tag")
		lrc := strings.TrimSuffix(track.Path, filepath.Ext(track.Path)) + ".lrc"
		Expect(os.WriteFile(lrc, []byte("from the sidecar"), 0o600)).To(Succeed())
		Expect(list(structured(trackID(track))[0], "line")[0]["value"]).
			To(Equal("from the sidecar"))
	})

	It("answers empty lyrics when there are none", func() {
		Expect(structured(trackID(track))).To(BeEmpty())
		ly := obj(u.call("getLyrics", url.Values{"title": {"Mute"}}), "lyrics")
		Expect(ly["value"]).To(BeEmpty())
		ly = obj(u.call("getLyrics", nil), "lyrics")
		Expect(ly["value"]).To(BeEmpty())
	})

	It("finds a song by its title only for the artist named", func() {
		tagFile(track.Path, "LYRICS", "words")
		other := obj(u.call("getLyrics", url.Values{
			"artist": {"Somebody Else"}, "title": {"Verse"},
		}), "lyrics")
		Expect(other["value"]).To(BeEmpty())
		Expect(obj(u.call("getLyrics", url.Values{
			"artist": {name}, "title": {"Verse"},
		}), "lyrics")["value"]).To(Equal("words"))
	})

	It(
		"answers 70 for a song id that is malformed, of another kind or gone",
		func() {
			wantCode(u.get("getLyricsBySongId", nil), 10)
			wantCode(u.get("getLyricsBySongId", url.Values{"id": {"nonsense"}}), 70)
			wantCode(u.get("getLyricsBySongId", url.Values{"id": {"al-1"}}), 70)
			wantCode(u.get("getLyricsBySongId", url.Values{"id": {"tr-999999"}}), 70)
		},
	)

	It("refuses wrong credentials with error 40", func() {
		u.Password = "nope"
		wantCode(u.get("getLyrics", url.Values{"title": {"Verse"}}), 40)
	})
})
