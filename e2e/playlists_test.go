package e2e

import (
	"net/url"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func trackRef(t seededTrack) string { return "tr-" + itoa(t.ID) }

func createPlaylistFor(u *testUser, name string, songs ...string) map[string]any {
	GinkgoHelper()
	return obj(u.call("createPlaylist", url.Values{
		"name": {name}, "songId": songs,
	}), "playlist")
}

func playlistNames(u *testUser, extra url.Values) []string {
	GinkgoHelper()
	items := list(obj(u.call("getPlaylists", extra), "playlists"), "playlist")
	out := make([]string, 0, len(items))
	for _, p := range items {
		out = append(out, p["name"].(string))
	}
	return out
}

func entryIDs(u *testUser, playlistID string) []string {
	GinkgoHelper()
	p := obj(u.call("getPlaylist", url.Values{"id": {playlistID}}), "playlist")
	return ids(list(p, "entry"))
}

func adminUser(prefix string) *testUser {
	GinkgoHelper()
	u := app.newUserWithRole(prefix, "admin")
	app.setSubsonicPassword(u, "pw-"+u.Email)
	return u
}

// loseFile marks the track's file missing in core; restore brings it back.
func loseFile(t seededTrack) {
	GinkgoHelper()
	_, err := app.core.Exec(
		"UPDATE media_files SET missing_since = ? WHERE id = ?",
		coreTime(time.Now()), t.FileID,
	)
	Expect(err).NotTo(HaveOccurred())
}

func restoreFile(t seededTrack) {
	GinkgoHelper()
	_, err := app.core.Exec(
		"UPDATE media_files SET missing_since = NULL WHERE id = ?", t.FileID,
	)
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("playlists", Label("e2e"), func() {
	var (
		lib  library
		dawn seededTrack
		noon seededTrack
		calm seededTrack
	)

	BeforeEach(func() {
		lib = app.sharedLibrary()
		dawn, noon, calm = lib.track(0, 0, 0), lib.track(0, 0, 1), lib.track(0, 1, 1)
	})

	It("creates, lists, reads and deletes a playlist", func() {
		u := app.subsonicUser("pl")
		name := app.unique("Mix")
		p := createPlaylistFor(
			u,
			name,
			trackRef(dawn),
			trackRef(calm),
			trackRef(dawn),
		)

		Expect(p["name"]).To(Equal(name))
		Expect(p["owner"]).To(Equal(u.Email))
		Expect(p["public"]).To(BeFalse())
		Expect(p["coverArt"]).To(Equal(p["id"]))
		Expect(p["created"]).NotTo(BeEmpty())
		Expect(p["changed"]).NotTo(BeEmpty())
		// Calm has no file: invisible, so two of the three entries show.
		Expect(p["songCount"]).To(BeEquivalentTo(2))
		Expect(
			ids(list(p, "entry")),
		).To(Equal([]string{trackRef(dawn), trackRef(dawn)}))

		all := list(obj(u.call("getPlaylists", nil), "playlists"), "playlist")
		Expect(all).To(HaveLen(1))
		Expect(all[0]).NotTo(HaveKey("entry"))
		Expect(all[0]["songCount"]).To(BeEquivalentTo(2))

		id := p["id"].(string)
		got := obj(u.call("getPlaylist", url.Values{"id": {id}}), "playlist")
		Expect(got["duration"]).To(BeNumerically(">", 0))
		entry := list(got, "entry")[0]
		Expect(entry).To(HaveKeyWithValue("title", "Dawn"))
		Expect(entry).To(HaveKeyWithValue("album", "First Light"))
		Expect(entry).To(HaveKeyWithValue("artist", "The Seeders"))
		Expect(entry).To(HaveKeyWithValue("suffix", "flac"))
		Expect(entry).To(HaveKeyWithValue("contentType", "audio/flac"))
		Expect(
			entry,
		).To(HaveKeyWithValue("albumId", "al-"+itoa(lib.Artists[0].Albums[0].ID)))
		Expect(entry).To(HaveKeyWithValue("year", BeEquivalentTo(2019)))

		u.call("deletePlaylist", url.Values{"id": {id}})
		Expect(
			errorCode(u.callError("getPlaylist", url.Values{"id": {id}})),
		).To(Equal(70))
		Expect(playlistNames(u, nil)).To(BeEmpty())
	})

	It("accepts a form body", func() {
		u := app.subsonicUser("pl")
		raw := u.postForm("createPlaylist", url.Values{
			"name": {"Form"}, "songId": {trackRef(dawn), trackRef(noon)},
		})
		p := obj(raw.json(), "playlist")
		Expect(p["songCount"]).To(BeEquivalentTo(2))
	})

	It(
		"refuses a song that does not exist, a malformed id and the wrong kind",
		func() {
			u := app.subsonicUser("pl")
			for _, bad := range []string{"tr-999999", "tr-x", "nonsense", "al-1", ""} {
				Expect(errorCode(u.callError("createPlaylist", url.Values{
					"name": {"Bad"}, "songId": {bad},
				}))).To(Equal(70), bad)
			}
			Expect(errorCode(u.callError("createPlaylist", nil))).To(Equal(10))
			Expect(errorCode(u.callError("getPlaylist", nil))).To(Equal(10))
			Expect(
				errorCode(u.callError("getPlaylist", url.Values{"id": {"al-1"}})),
			).To(Equal(70))
			Expect(
				errorCode(
					u.callError("getPlaylist", url.Values{"id": {"pl-999999"}}),
				),
			).To(Equal(70))
			Expect(playlistNames(u, nil)).To(BeEmpty())
		},
	)

	It("edits by visible index, renames, comments and publishes", func() {
		u := app.subsonicUser("pl")
		p := createPlaylistFor(u, "Mix", trackRef(dawn), trackRef(noon))
		id := p["id"].(string)
		u.call("updatePlaylist", url.Values{
			"playlistId":        {id},
			"name":              {"Renamed"},
			"comment":           {"for the road"},
			"public":            {"true"},
			"songIndexToRemove": {"0"},
			"songIdToAdd":       {trackRef(dawn)},
		})
		got := obj(u.call("getPlaylist", url.Values{"id": {id}}), "playlist")
		Expect(got["name"]).To(Equal("Renamed"))
		Expect(got["comment"]).To(Equal("for the road"))
		Expect(got["public"]).To(BeTrue())
		Expect(
			ids(list(got, "entry")),
		).To(Equal([]string{trackRef(noon), trackRef(dawn)}))

		Expect(errorCode(u.callError("updatePlaylist", url.Values{
			"playlistId": {id}, "songIndexToRemove": {"9"},
		}))).To(Equal(0))
		Expect(errorCode(u.callError("updatePlaylist", url.Values{
			"playlistId": {id}, "songIndexToRemove": {"-1"},
		}))).To(Equal(0))
		Expect(errorCode(u.callError("updatePlaylist", url.Values{
			"playlistId": {id}, "name": {"  "},
		}))).To(Equal(0))
		Expect(errorCode(u.callError("updatePlaylist", url.Values{
			"playlistId": {id}, "songIdToAdd": {"tr-999999"},
		}))).To(Equal(70))
		Expect(entryIDs(u, id)).To(Equal([]string{trackRef(noon), trackRef(dawn)}))
		u.call("deletePlaylist", url.Values{"id": {id}})
	})

	It("leaves the songs alone when only the header changes", func() {
		u := app.subsonicUser("pl")
		id := createPlaylistFor(u, "Mix", trackRef(dawn), trackRef(noon))["id"].(string)
		u.call("updatePlaylist", url.Values{"playlistId": {id}, "comment": {"hi"}})
		Expect(entryIDs(u, id)).To(Equal([]string{trackRef(dawn), trackRef(noon)}))
	})

	It("replaces the songs through createPlaylist with a playlistId", func() {
		u := app.subsonicUser("pl")
		id := createPlaylistFor(u, "Mix", trackRef(dawn))["id"].(string)
		got := obj(u.call("createPlaylist", url.Values{
			"playlistId": {id}, "songId": {trackRef(noon)},
		}), "playlist")
		Expect(got["id"]).To(Equal(id))
		Expect(got["name"]).To(Equal("Mix"))
		Expect(ids(list(got, "entry"))).To(Equal([]string{trackRef(noon)}))

		renamed := obj(u.call("createPlaylist", url.Values{
			"playlistId": {id}, "name": {"Again"}, "songId": {trackRef(dawn)},
		}), "playlist")
		Expect(renamed["name"]).To(Equal("Again"))
		Expect(playlistNames(u, nil)).To(Equal([]string{"Again"}))
	})

	It("skips entries whose file went missing and brings them back", func() {
		extra := app.addArtist(
			artistSpec{Name: app.unique("Lost"), Genre: "Rock", Albums: []albumSpec{{
				Title: "Gone", Year: 2020, Tracks: []trackSpec{
					{Title: "One", Position: 1}, {Title: "Two", Position: 2},
				},
			}}},
		)
		one, two := extra.Albums[0].Tracks[0], extra.Albums[0].Tracks[1]
		u := app.subsonicUser("pl")
		id := createPlaylistFor(u, "Mix", trackRef(one), trackRef(two))["id"].(string)

		loseFile(two)
		Expect(entryIDs(u, id)).To(Equal([]string{trackRef(one)}))
		restoreFile(two)
		Expect(entryIDs(u, id)).To(Equal([]string{trackRef(one), trackRef(two)}))
	})

	It("keeps a missing entry through an unrelated edit", func() {
		extra := app.addArtist(
			artistSpec{Name: app.unique("Lost"), Genre: "Rock", Albums: []albumSpec{{
				Title: "Gone", Year: 2020, Tracks: []trackSpec{
					{Title: "One", Position: 1}, {Title: "Two", Position: 2},
				},
			}}},
		)
		one, two := extra.Albums[0].Tracks[0], extra.Albums[0].Tracks[1]
		u := app.subsonicUser("pl")
		id := createPlaylistFor(u, "Mix", trackRef(one), trackRef(two))["id"].(string)

		loseFile(two)
		u.call("updatePlaylist", url.Values{
			"playlistId": {id}, "songIdToAdd": {trackRef(one)},
		})
		restoreFile(two)
		Expect(entryIDs(u, id)).To(Equal([]string{
			trackRef(one), trackRef(two), trackRef(one),
		}))
	})

	It("removes by the index the client saw, past a missing entry", func() {
		extra := app.addArtist(
			artistSpec{Name: app.unique("Lost"), Genre: "Rock", Albums: []albumSpec{{
				Title: "Gone", Year: 2020, Tracks: []trackSpec{
					{Title: "One", Position: 1}, {Title: "Two", Position: 2},
				},
			}}},
		)
		one, two := extra.Albums[0].Tracks[0], extra.Albums[0].Tracks[1]
		u := app.subsonicUser("pl")
		id := createPlaylistFor(u, "Mix", trackRef(two), trackRef(one), trackRef(two))["id"].(string)

		loseFile(two)
		// The client sees only "One", at index 0.
		u.call("updatePlaylist", url.Values{
			"playlistId": {id}, "songIndexToRemove": {"0"},
		})
		restoreFile(two)
		Expect(entryIDs(u, id)).To(Equal([]string{trackRef(two), trackRef(two)}))
	})

	It("counts every playlist's playable songs when listing", func() {
		u := app.subsonicUser("pl")
		createPlaylistFor(u, "A", trackRef(dawn))
		createPlaylistFor(u, "B", trackRef(dawn), trackRef(calm))
		createPlaylistFor(u, "C", trackRef(calm), trackRef(noon), trackRef(calm))

		counts := map[string]any{}
		for _, p := range list(obj(u.call("getPlaylists", nil), "playlists"), "playlist") {
			counts[p["name"].(string)] = p["songCount"]
		}
		Expect(counts).To(Equal(map[string]any{
			"A": float64(1), "B": float64(1), "C": float64(1),
		}))
		Expect(playlistNames(u, nil)).To(Equal([]string{"A", "B", "C"}))
	})

	Describe("who may see and change what", func() {
		It(
			"hides a private playlist and lets only its owner or an admin change it",
			func() {
				owner := app.subsonicUser("pl")
				other := app.subsonicUser("pl")
				admin := adminUser("pl")
				name := app.unique("Mine")
				id := createPlaylistFor(owner, name, trackRef(dawn))["id"].(string)

				Expect(playlistNames(other, nil)).NotTo(ContainElement(name))
				Expect(
					errorCode(
						other.callError("getPlaylist", url.Values{"id": {id}}),
					),
				).To(Equal(70))
				Expect(errorCode(other.callError("updatePlaylist", url.Values{
					"playlistId": {id}, "name": {"Taken"},
				}))).To(Equal(70))
				Expect(
					errorCode(
						other.callError("deletePlaylist", url.Values{"id": {id}}),
					),
				).To(Equal(70))
				Expect(errorCode(other.callError("createPlaylist", url.Values{
					"playlistId": {id}, "songId": {trackRef(noon)},
				}))).To(Equal(70))

				owner.call(
					"updatePlaylist",
					url.Values{"playlistId": {id}, "public": {"true"}},
				)
				Expect(playlistNames(other, nil)).To(ContainElement(name))
				public := obj(
					other.call("getPlaylist", url.Values{"id": {id}}),
					"playlist",
				)
				Expect(public["owner"]).To(Equal(owner.Email))
				Expect(
					ids(list(public, "entry")),
				).To(Equal([]string{trackRef(dawn)}))

				// Public is read only for everyone but the owner and an admin.
				Expect(
					errorCode(
						other.callError("deletePlaylist", url.Values{"id": {id}}),
					),
				).To(Equal(50))
				Expect(errorCode(other.callError("updatePlaylist", url.Values{
					"playlistId": {id}, "name": {"Taken"},
				}))).To(Equal(50))
				Expect(errorCode(other.callError("createPlaylist", url.Values{
					"playlistId": {id}, "songId": {trackRef(noon)},
				}))).To(Equal(50))
				Expect(entryIDs(owner, id)).To(Equal([]string{trackRef(dawn)}))

				admin.call(
					"updatePlaylist",
					url.Values{"playlistId": {id}, "name": {"Admin"}},
				)
				Expect(
					obj(owner.call("getPlaylist", url.Values{"id": {id}}), "playlist")["name"],
				).
					To(Equal("Admin"))
				admin.call("deletePlaylist", url.Values{"id": {id}})
				Expect(
					errorCode(
						owner.callError("getPlaylist", url.Values{"id": {id}}),
					),
				).To(Equal(70))
			},
		)

		It("lets only an admin list another user's playlists, in any case", func() {
			owner := app.subsonicUser("pl")
			other := app.subsonicUser("pl")
			admin := adminUser("pl")
			createPlaylistFor(owner, "Private", trackRef(dawn))

			Expect(errorCode(other.callError("getPlaylists", url.Values{
				"username": {owner.Email},
			}))).To(Equal(50))
			Expect(playlistNames(owner, url.Values{"username": {owner.Email}})).
				To(Equal([]string{"Private"}))
			Expect(
				playlistNames(
					admin,
					url.Values{"username": {strings.ToUpper(owner.Email)}},
				),
			).
				To(Equal([]string{"Private"}))
			Expect(
				playlistNames(admin, url.Values{"username": {"nobody@example.com"}}),
			).
				To(BeEmpty())
		})

		It("refuses writes to a caller without the playlist permission", func() {
			u := app.subsonicUser("pl")
			id := createPlaylistFor(u, "Mine", trackRef(dawn))["id"].(string)
			app.setSettings(u, map[string]any{"can_manage_playlists": false})

			Expect(errorCode(u.callError("createPlaylist", url.Values{
				"name": {"New"}, "songId": {trackRef(dawn)},
			}))).To(Equal(50))
			Expect(errorCode(u.callError("updatePlaylist", url.Values{
				"playlistId": {id}, "name": {"New"},
			}))).To(Equal(50))
			Expect(
				errorCode(u.callError("deletePlaylist", url.Values{"id": {id}})),
			).To(Equal(50))

			Expect(playlistNames(u, nil)).To(Equal([]string{"Mine"}))
			Expect(entryIDs(u, id)).To(Equal([]string{trackRef(dawn)}))
		})
	})

	PIt("serves a mosaic for getCoverArt of a playlist id", func() {
		// Blocked on the media domain: getCoverArt does not route a "pl" id to
		// servePlaylistCover yet. The integration step enables it.
	})
})
