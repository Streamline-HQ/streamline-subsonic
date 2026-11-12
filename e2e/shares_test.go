package e2e

import (
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("shares", Label("e2e"), func() {
	var (
		lib   library
		owner *testUser
	)

	BeforeEach(func() {
		lib = app.sharedLibrary()
		owner = app.subsonicUser("sharer")
	})

	createShare := func(u *testUser, extra url.Values) map[string]any {
		GinkgoHelper()
		got := list(obj(u.call("createShare", extra), "shares"), "share")
		Expect(got).To(HaveLen(1))
		return got[0]
	}

	tokenOf := func(s map[string]any) string {
		GinkgoHelper()
		link, ok := s["url"].(string)
		Expect(ok).To(BeTrue())
		u, err := url.Parse(link)
		Expect(err).NotTo(HaveOccurred())
		Expect(u.Path).To(HavePrefix("/share/"))
		return strings.TrimPrefix(u.Path, "/share/")
	}

	visit := func(path string) rawResponse {
		GinkgoHelper()
		return fetch(http.MethodGet, "/share/"+path, nil, nil)
	}

	titles := func(items []map[string]any) []string {
		out := make([]string, 0, len(items))
		for _, it := range items {
			out = append(out, it["title"].(string))
		}
		return out
	}

	idOf := func(s map[string]any) string { return s["id"].(string) }

	insertPlaylist := func(owner *testUser, public bool, tracks ...seededTrack) string {
		GinkgoHelper()
		db := app.ext()
		defer db.Close()
		now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		res, err := db.Exec(
			`INSERT INTO playlists(owner_id, name, public, created_at, changed_at)
			VALUES(?, 'shared', ?, ?, ?)`,
			owner.ID, public, now, now,
		)
		Expect(err).NotTo(HaveOccurred())
		id, err := res.LastInsertId()
		Expect(err).NotTo(HaveOccurred())
		for pos, t := range tracks {
			_, err := db.Exec(
				"INSERT INTO playlist_tracks(playlist_id, position, track_id) VALUES(?, ?, ?)",
				id,
				pos,
				t.ID,
			)
			Expect(err).NotTo(HaveOccurred())
		}
		return "pl-" + itoa(uint32(id))
	}

	setPlaylistPublic := func(id string, public bool) {
		GinkgoHelper()
		db := app.ext()
		defer db.Close()
		_, err := db.Exec(
			"UPDATE playlists SET public = ? WHERE id = ?",
			public, strings.TrimPrefix(id, "pl-"),
		)
		Expect(err).NotTo(HaveOccurred())
	}

	It("shares an album and serves it to a visitor", func() {
		album := lib.Artists[0].Albums[0]
		s := createShare(owner, url.Values{
			"id": {"al-" + itoa(album.ID)}, "description": {"Listen to this"},
		})
		Expect(s["id"]).To(HavePrefix("sh-"))
		Expect(s["url"]).To(HaveSuffix("/share/" + tokenOf(s)))
		Expect(s["username"]).To(Equal(owner.Email))
		Expect(s["description"]).To(Equal("Listen to this"))
		Expect(titles(list(s, "entry"))).To(Equal([]string{"Dawn", "Noon", "Dusk"}))
		token := tokenOf(s)
		Expect(len(token)).To(BeNumerically(">=", 22))

		page := visit(token)
		Expect(page.Status).To(Equal(http.StatusOK))
		Expect(page.Header.Get("Content-Type")).To(HavePrefix("text/html"))
		Expect(page.Header.Get("X-Robots-Tag")).To(ContainSubstring("noindex"))
		html := string(page.Body)
		Expect(html).To(ContainSubstring("Listen to this"))
		Expect(html).To(ContainSubstring("Dawn"))
		Expect(html).NotTo(ContainSubstring(owner.Email))
		Expect(html).NotTo(ContainSubstring("<script"))

		first := album.Tracks[0]
		want, err := os.ReadFile(first.Path)
		Expect(err).NotTo(HaveOccurred())
		audio := visit(token + "/stream/" + itoa(first.ID))
		Expect(audio.Status).To(Equal(http.StatusOK))
		Expect(audio.Body).To(Equal(want))

		cover := visit(token + "/cover/" + itoa(album.ID))
		Expect(cover.Status).To(Equal(http.StatusOK))
		Expect(cover.Header.Get("Content-Type")).To(Equal("image/jpeg"))

		got := list(obj(owner.call("getShares", nil), "shares"), "share")
		Expect(got).To(HaveLen(1))
		Expect(got[0]["visitCount"]).To(BeEquivalentTo(1))
		Expect(got[0]["lastVisited"]).NotTo(BeEmpty())
	})

	It("expands an artist and a playlist and a track in the order shared", func() {
		artist := lib.Artists[0]
		s := createShare(owner, url.Values{"id": {
			"tr-" + itoa(lib.track(1, 0, 0).ID),
			"ar-" + itoa(artist.ID),
		}})
		Expect(titles(list(s, "entry"))).To(Equal(
			[]string{"Indigo", "Dawn", "Noon", "Dusk", "Gust"},
		), "a track with no file is left out")

		p := insertPlaylist(owner, false, lib.track(0, 0, 2), lib.track(0, 0, 0))
		s = createShare(owner, url.Values{"id": {p}})
		Expect(titles(list(s, "entry"))).To(Equal([]string{"Dusk", "Dawn"}))
	})

	It("answers in XML when asked", func() {
		createShare(owner, url.Values{"id": {"tr-" + itoa(lib.track(0, 0, 0).ID)}})
		raw := owner.get("getShares", url.Values{"f": {""}})
		Expect(raw.Header.Get("Content-Type")).To(Equal("text/xml; charset=utf-8"))
		Expect(string(raw.Body)).To(ContainSubstring("<shares>"))
	})

	It("builds an absolute link when a proxy names the origin", func() {
		q := owner.signed(url.Values{"id": {"tr-" + itoa(lib.track(0, 0, 0).ID)}})
		raw := fetch(
			http.MethodGet,
			"/rest/createShare?"+q.Encode(),
			nil,
			http.Header{
				"X-Forwarded-Host":  {"music.example.com"},
				"X-Forwarded-Proto": {"https"},
			},
		)
		got := list(obj(raw.json(), "shares"), "share")
		Expect(got).To(HaveLen(1))
		Expect(got[0]["url"]).To(HavePrefix("https://music.example.com/share/"))
	})

	It("serves only what the share holds", func() {
		s := createShare(
			owner,
			url.Values{"id": {"tr-" + itoa(lib.track(0, 0, 0).ID)}},
		)
		token := tokenOf(s)
		other := lib.track(0, 0, 1)
		Expect(
			visit(token + "/stream/" + itoa(other.ID)).Status,
		).To(Equal(http.StatusNotFound))
		Expect(visit(token + "/stream/" + itoa(lib.track(1, 0, 0).ID)).Status).
			To(Equal(http.StatusNotFound))
		Expect(visit(token + "/cover/" + itoa(lib.Artists[1].Albums[0].ID)).Status).
			To(Equal(http.StatusNotFound))
		Expect(
			visit(token + "/stream/not-a-number").Status,
		).To(Equal(http.StatusNotFound))
		Expect(visit(token + "/other").Status).To(Equal(http.StatusNotFound))
		Expect(visit("AAAAAAAAAAAAAAAAAAAAAA").Status).To(Equal(http.StatusNotFound))
		Expect(visit("AAAAAAAAAAAAAAAAAAAAAA/stream/" + itoa(other.ID)).Status).
			To(Equal(http.StatusNotFound))
	})

	It("never plays a track with no file", func() {
		gone := lib.track(0, 1, 1)
		Expect(gone.Path).To(BeEmpty())
		s := createShare(owner, url.Values{"id": {"tr-" + itoa(gone.ID)}})
		Expect(list(s, "entry")).To(BeEmpty())
		token := tokenOf(s)
		Expect(
			visit(token + "/stream/" + itoa(gone.ID)).Status,
		).To(Equal(http.StatusNotFound))
		Expect(
			string(visit(token).Body),
		).To(ContainSubstring("Nothing in this share"))
	})

	It("answers 404, not 500, for a file outside the music folder", func() {
		artist := app.addArtist(artistSpec{
			Name: app.unique("Outside"),
			Albums: []albumSpec{{
				Title: "Away", Year: 2020,
				Tracks: []trackSpec{{Title: "Far", Position: 1}},
			}},
		})
		track := artist.Albums[0].Tracks[0]
		_, err := app.core.Exec(
			"UPDATE media_files SET path = ? WHERE id = ?",
			"/nonexistent/outside/far.flac", track.FileID,
		)
		Expect(err).NotTo(HaveOccurred())
		s := createShare(owner, url.Values{"id": {"tr-" + itoa(track.ID)}})
		Expect(visit(tokenOf(s) + "/stream/" + itoa(track.ID)).Status).
			To(Equal(http.StatusNotFound))
	})

	It("stops serving an expired or deleted share", func() {
		past := strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)
		track := lib.track(0, 0, 0)
		s := createShare(owner, url.Values{
			"id": {"tr-" + itoa(track.ID)}, "expires": {past},
		})
		token := tokenOf(s)
		Expect(visit(token).Status).To(Equal(http.StatusNotFound))
		Expect(visit(token + "/stream/" + itoa(track.ID)).Status).
			To(Equal(http.StatusNotFound))
		Expect(visit(token + "/cover/" + itoa(lib.Artists[0].Albums[0].ID)).Status).
			To(Equal(http.StatusNotFound))

		owner.call("updateShare", url.Values{"id": {idOf(s)}, "expires": {"0"}})
		Expect(visit(token).Status).To(Equal(http.StatusOK))

		future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
		owner.call("updateShare", url.Values{
			"id": {idOf(s)}, "expires": {future}, "description": {"  later  "},
		})
		got := list(obj(owner.call("getShares", nil), "shares"), "share")
		Expect(got).To(HaveLen(1))
		Expect(got[0]["description"]).To(Equal("later"))
		Expect(got[0]["expires"]).NotTo(BeEmpty())
		Expect(visit(token).Status).To(Equal(http.StatusOK))

		owner.call("deleteShare", url.Values{"id": {idOf(s)}})
		Expect(visit(token).Status).To(Equal(http.StatusNotFound))
		Expect(visit(token + "/stream/" + itoa(track.ID)).Status).
			To(Equal(http.StatusNotFound))
		Expect(
			list(obj(owner.call("getShares", nil), "shares"), "share"),
		).To(BeEmpty())
	})

	It("keeps shares to their owner", func() {
		other := app.subsonicUser("intruder")
		s := createShare(
			owner,
			url.Values{"id": {"tr-" + itoa(lib.track(0, 0, 0).ID)}},
		)
		Expect(
			list(obj(other.call("getShares", nil), "shares"), "share"),
		).To(BeEmpty())
		for _, endpoint := range []string{"deleteShare", "updateShare"} {
			Expect(
				errorCode(other.callError(endpoint, url.Values{"id": {idOf(s)}})),
			).
				To(Equal(70), endpoint)
		}
		Expect(visit(tokenOf(s)).Status).To(Equal(http.StatusOK))

		admin := app.newUserWithRole("shareadmin", "admin")
		app.setSubsonicPassword(admin, "pw-admin")
		Expect(
			list(obj(admin.call("getShares", nil), "shares"), "share"),
		).To(BeEmpty())
		Expect(
			errorCode(admin.callError("deleteShare", url.Values{"id": {idOf(s)}})),
		).
			To(Equal(70))
	})

	It("refuses ids that are missing, malformed, of the wrong kind or gone", func() {
		Expect(errorCode(owner.callError("createShare", nil))).To(Equal(10))
		for _, id := range []string{
			"al-999999", "tr-999999", "ar-999999", "pl-999999", "nonsense", "al-x",
			"sh-1", "rs-1", "al-99999999999",
		} {
			Expect(
				errorCode(owner.callError("createShare", url.Values{"id": {id}})),
			).
				To(Equal(70), id)
		}
		good := "tr-" + itoa(lib.track(0, 0, 0).ID)
		Expect(
			errorCode(
				owner.callError(
					"createShare",
					url.Values{"id": {good, "al-999999"}},
				),
			),
		).
			To(Equal(70), "one bad id refuses the whole call")
		Expect(
			list(obj(owner.call("getShares", nil), "shares"), "share"),
		).To(BeEmpty())

		Expect(errorCode(owner.callError("updateShare", nil))).To(Equal(10))
		Expect(
			errorCode(
				owner.callError("updateShare", url.Values{"id": {"sh-999999"}}),
			),
		).
			To(Equal(70))
		Expect(errorCode(owner.callError("updateShare", url.Values{"id": {good}}))).
			To(Equal(70), "a track id is not a share id")
		Expect(
			errorCode(
				owner.callError("deleteShare", url.Values{"id": {"sh-999999"}}),
			),
		).
			To(Equal(70))
	})

	It("lets nobody share someone else's private playlist", func() {
		other := app.subsonicUser("plowner")
		admin := app.newUserWithRole("pladmin", "admin")
		app.setSubsonicPassword(admin, "pw-admin")
		p := insertPlaylist(other, false, lib.track(0, 0, 0))

		Expect(errorCode(owner.callError("createShare", url.Values{"id": {p}}))).
			To(Equal(70))
		Expect(errorCode(admin.callError("createShare", url.Values{"id": {p}}))).
			To(Equal(50), "an admin reads it but does not publish it")

		setPlaylistPublic(p, true)
		s := createShare(owner, url.Values{"id": {p}})
		Expect(list(s, "entry")).To(HaveLen(1))
	})

	It("drops a shared playlist its owner made private", func() {
		other := app.subsonicUser("plowner")
		p := insertPlaylist(other, true, lib.track(0, 0, 0))
		s := createShare(owner, url.Values{"id": {p}})
		Expect(list(s, "entry")).To(HaveLen(1))
		token := tokenOf(s)

		setPlaylistPublic(p, false)
		got := list(obj(owner.call("getShares", nil), "shares"), "share")
		Expect(got).To(HaveLen(1))
		Expect(list(got[0], "entry")).To(BeEmpty())
		Expect(visit(token + "/stream/" + itoa(lib.track(0, 0, 0).ID)).Status).
			To(Equal(http.StatusNotFound))
	})

	It("refuses a caller without the share permission", func() {
		track := "tr-" + itoa(lib.track(0, 0, 0).ID)
		s := createShare(owner, url.Values{"id": {track}})
		app.setSettings(owner, map[string]any{"can_share": false})
		for endpoint, extra := range map[string]url.Values{
			"createShare": {"id": {track}},
			"updateShare": {"id": {idOf(s)}},
			"deleteShare": {"id": {idOf(s)}},
		} {
			Expect(
				errorCode(owner.callError(endpoint, extra)),
			).To(Equal(50), endpoint)
		}
	})

	It("ends every link once its owner may no longer share", func() {
		track := lib.track(0, 0, 0)
		s := createShare(
			owner,
			url.Values{"id": {"al-" + itoa(lib.Artists[0].Albums[0].ID)}},
		)
		token := tokenOf(s)
		Expect(visit(token).Status).To(Equal(http.StatusOK))

		app.setSettings(owner, map[string]any{"can_share": false})
		Expect(visit(token).Status).To(Equal(http.StatusNotFound))
		Expect(visit(token + "/stream/" + itoa(track.ID)).Status).
			To(Equal(http.StatusNotFound))
		Expect(visit(token + "/cover/" + itoa(lib.Artists[0].Albums[0].ID)).Status).
			To(Equal(http.StatusNotFound))

		app.setSettings(owner, map[string]any{"can_share": true})
		Expect(visit(token).Status).To(Equal(http.StatusOK))
	})

	It("serves no audio through a link whose owner may not stream", func() {
		track := lib.track(0, 0, 0)
		album := "al-" + itoa(lib.Artists[0].Albums[0].ID)
		s := createShare(owner, url.Values{"id": {album}})
		token := tokenOf(s)
		app.setSettings(owner, map[string]any{"can_stream": false})

		Expect(visit(token).Status).To(Equal(http.StatusOK))
		Expect(visit(token + "/stream/" + itoa(track.ID)).Status).
			To(Equal(http.StatusNotFound))
		Expect(errorCode(owner.callError("createShare", url.Values{"id": {album}}))).
			To(Equal(50))
	})

	It("holds a share stream to its owner's bitrate cap", func() {
		track := lib.track(1, 0, 0)
		want, err := os.ReadFile(track.Path)
		Expect(err).NotTo(HaveOccurred())
		s := createShare(owner, url.Values{"id": {"tr-" + itoa(track.ID)}})
		stream := tokenOf(s) + "/stream/" + itoa(track.ID)

		plain := visit(stream)
		Expect(plain.Status).To(Equal(http.StatusOK))
		Expect(plain.Body).To(Equal(want), "no cap: the original")
		Expect(plain.Header.Get("Content-Type")).To(Equal("audio/mpeg"))

		app.setSettings(owner, map[string]any{"max_bit_rate": 64})
		capped := visit(stream)
		Expect(capped.Status).To(Equal(http.StatusOK))
		Expect(capped.Header.Get("Content-Type")).To(Equal("audio/mpeg"))
		Expect(capped.Body).NotTo(BeEmpty())
		Expect(
			capped.Body,
		).NotTo(Equal(want), "a 64 kbps conversion of a 128 kbps file")
		Expect(len(capped.Body)).To(BeNumerically("<", len(want)))

		app.setSettings(owner, map[string]any{"max_bit_rate": 320})
		Expect(visit(stream).Body).To(Equal(want), "a cap above the file's rate")
	})
})
