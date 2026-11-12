package e2e

import (
	"net/url"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func markTrack(t seededTrack) string   { return "tr-" + itoa(t.ID) }
func markAlbum(a seededAlbum) string   { return "al-" + itoa(a.ID) }
func markArtist(a seededArtist) string { return "ar-" + itoa(a.ID) }

// requireEndpoint skips the spec until an endpoint of another domain is
// there: it reads the library through it.
func marksRequireEndpoint(u *testUser, name string, extra url.Values) {
	GinkgoHelper()
	resp := u.get(name, extra).json()
	if e, ok := resp["error"].(map[string]any); ok &&
		e["message"] == "Not implemented" {
		Skip(name + " is not served yet")
	}
}

func marksStarred2(u *testUser) map[string]any {
	GinkgoHelper()
	return obj(u.call("getStarred2", nil), "starred2")
}

func marksNowPlaying(u *testUser) []map[string]any {
	GinkgoHelper()
	return list(obj(u.call("getNowPlaying", nil), "nowPlaying"), "entry")
}

var _ = Describe("stars", Label("e2e"), func() {
	var (
		lib library
		u   *testUser
	)

	BeforeEach(func() {
		lib = app.sharedLibrary()
		u = app.subsonicUser("stars")
	})

	It("stars and unstars songs, albums and artists and shows it back", func() {
		song, album, artist := lib.track(
			0,
			0,
			0,
		), lib.Artists[0].Albums[0], lib.Artists[0]
		u.call("star", url.Values{
			"id":       {markTrack(song)},
			"albumId":  {markAlbum(album)},
			"artistId": {markArtist(artist)},
		})

		got := marksStarred2(u)
		Expect(ids(list(got, "song"))).To(ConsistOf(markTrack(song)))
		Expect(ids(list(got, "album"))).To(ConsistOf(markAlbum(album)))
		Expect(ids(list(got, "artist"))).To(ConsistOf(markArtist(artist)))
		Expect(list(got, "song")[0]["starred"]).NotTo(BeEmpty())
		Expect(list(got, "album")[0]["starred"]).NotTo(BeEmpty())
		Expect(list(got, "artist")[0]["starred"]).NotTo(BeEmpty())

		Expect(list(got, "song")[0]).To(And(
			HaveKeyWithValue("title", song.Title),
			HaveKeyWithValue("album", album.Title),
			HaveKeyWithValue("artist", artist.Name),
			HaveKeyWithValue("albumId", markAlbum(album)),
			HaveKeyWithValue("artistId", markArtist(artist)),
			HaveKeyWithValue("suffix", "flac"),
			HaveKeyWithValue("contentType", "audio/flac"),
		))
		Expect(list(got, "album")[0]).To(And(
			HaveKeyWithValue("name", album.Title),
			HaveKeyWithValue("songCount", BeNumerically("==", 3)),
		))
		Expect(
			list(got, "artist")[0],
		).To(HaveKeyWithValue("albumCount", BeNumerically("==", 2)))

		folder := obj(u.call("getStarred", nil), "starred")
		Expect(list(folder, "album")[0]["isDir"]).To(BeTrue())
		Expect(ids(list(folder, "song"))).To(ConsistOf(markTrack(song)))

		u.call("unstar", url.Values{"id": {markTrack(song), markAlbum(album)}})
		got = marksStarred2(u)
		Expect(list(got, "song")).To(BeEmpty())
		Expect(list(got, "album")).To(BeEmpty())
		Expect(list(got, "artist")).To(HaveLen(1))
	})

	It("lists the newest star first", func() {
		first, second := lib.track(0, 0, 0), lib.track(0, 0, 1)
		u.call("star", url.Values{"id": {markTrack(first)}})
		time.Sleep(20 * time.Millisecond)
		u.call("star", url.Values{"id": {markTrack(second)}})
		Expect(
			ids(list(marksStarred2(u), "song")),
		).To(Equal([]string{markTrack(second), markTrack(first)}))
	})

	It("keeps a star's time when it is starred again", func() {
		song := lib.track(0, 0, 0)
		u.call("star", url.Values{"id": {markTrack(song)}})
		at := list(marksStarred2(u), "song")[0]["starred"]
		time.Sleep(20 * time.Millisecond)
		u.call("star", url.Values{"id": {markTrack(song)}})
		Expect(list(marksStarred2(u), "song")[0]["starred"]).To(Equal(at))
	})

	It("keeps each listener's marks to themselves", func() {
		other := app.subsonicUser("stars-other")
		song := lib.track(0, 0, 0)
		other.call("star", url.Values{"id": {markTrack(song)}})
		other.call("setRating", url.Values{"id": {markTrack(song)}, "rating": {"5"}})

		Expect(list(marksStarred2(u), "song")).To(BeEmpty())
		u.call("star", url.Values{"id": {markTrack(song)}})
		Expect(list(marksStarred2(u), "song")[0]).NotTo(HaveKey("userRating"))

		other.call("unstar", url.Values{"id": {markTrack(song)}})
		Expect(ids(list(marksStarred2(u), "song"))).To(ConsistOf(markTrack(song)))
		Expect(list(marksStarred2(other), "song")).To(BeEmpty())
	})

	It("leaves out a starred track with no file", func() {
		gone := lib.track(0, 1, 1)
		Expect(gone.Path).To(BeEmpty())
		u.call("star", url.Values{"id": {markTrack(gone)}})
		Expect(list(marksStarred2(u), "song")).To(BeEmpty())
	})

	It("refuses a star on something that does not exist", func() {
		Expect(
			errorCode(u.callError("star", url.Values{"id": {"tr-999999"}})),
		).To(Equal(70))
		Expect(
			errorCode(u.callError("star", url.Values{"id": {"al-999999"}})),
		).To(Equal(70))
		Expect(
			errorCode(u.callError("star", url.Values{"artistId": {"ar-999999"}})),
		).
			To(Equal(70))
		Expect(
			errorCode(u.callError("unstar", url.Values{"id": {"tr-999999"}})),
		).To(Equal(70))
	})

	It("refuses a malformed id or one of a kind that cannot be starred", func() {
		song := lib.track(0, 0, 0)
		for _, id := range []string{"nonsense", "tr-", "tr-abc", "pl-1", "sh-1", "rs-1"} {
			Expect(
				errorCode(u.callError("star", url.Values{"id": {id}})),
			).To(Equal(70), id)
		}
		Expect(
			errorCode(u.callError("star", url.Values{"albumId": {markTrack(song)}})),
		).
			To(Equal(70))
		Expect(
			errorCode(
				u.callError(
					"star",
					url.Values{"artistId": {markAlbum(lib.Artists[0].Albums[0])}},
				),
			),
		).
			To(Equal(70))
		Expect(errorCode(u.callError("star", nil))).To(Equal(10))
	})

	It("stars nothing when one target of the call is refused", func() {
		song := lib.track(0, 0, 0)
		Expect(
			errorCode(
				u.callError(
					"star",
					url.Values{"id": {markTrack(song), "tr-999999"}},
				),
			),
		).
			To(Equal(70))
		Expect(list(marksStarred2(u), "song")).To(BeEmpty())
	})

	It("refuses a caller with the wrong password", func() {
		u.Password = "nope"
		Expect(
			errorCode(u.callError("star", url.Values{"id": {"tr-1"}})),
		).To(Equal(40))
		Expect(errorCode(u.callError("getStarred2", nil))).To(Equal(40))
		Expect(errorCode(u.callError("getNowPlaying", nil))).To(Equal(40))
	})

	It("reads the starred ids of a playable library through the album list", func() {
		album := lib.Artists[0].Albums[0]
		marksRequireEndpoint(u, "getAlbumList2", url.Values{"type": {"starred"}})
		u.call("star", url.Values{"id": {markAlbum(album)}})
		got := obj(
			u.call("getAlbumList2", url.Values{"type": {"starred"}}),
			"albumList2",
		)
		Expect(ids(list(got, "album"))).To(ConsistOf(markAlbum(album)))
	})
})

var _ = Describe("ratings", Label("e2e"), func() {
	var (
		lib library
		u   *testUser
	)

	BeforeEach(func() {
		lib = app.sharedLibrary()
		u = app.subsonicUser("rating")
	})

	It("rates and clears a rating on a song, an album and an artist", func() {
		song, album, artist := lib.track(
			0,
			0,
			0,
		), lib.Artists[0].Albums[0], lib.Artists[0]
		u.call("star", url.Values{
			"id": {
				markTrack(song),
			},
			"albumId":  {markAlbum(album)},
			"artistId": {markArtist(artist)},
		})
		u.call("setRating", url.Values{"id": {markTrack(song)}, "rating": {"3"}})
		u.call("setRating", url.Values{"id": {markAlbum(album)}, "rating": {"4"}})
		u.call("setRating", url.Values{"id": {markArtist(artist)}, "rating": {"5"}})

		got := marksStarred2(u)
		Expect(list(got, "song")[0]["userRating"]).To(BeEquivalentTo(3))
		Expect(list(got, "album")[0]["userRating"]).To(BeEquivalentTo(4))
		Expect(list(got, "artist")[0]["userRating"]).To(BeEquivalentTo(5))

		u.call("setRating", url.Values{"id": {markAlbum(album)}, "rating": {"0"}})
		Expect(list(marksStarred2(u), "album")[0]).NotTo(HaveKey("userRating"))
		Expect(list(marksStarred2(u), "album")[0]["starred"]).NotTo(BeEmpty())
	})

	It("lists the highest rated albums", func() {
		album := lib.Artists[0].Albums[0]
		marksRequireEndpoint(u, "getAlbumList2", url.Values{"type": {"highest"}})
		u.call("setRating", url.Values{"id": {markAlbum(album)}, "rating": {"4"}})
		highest := obj(
			u.call("getAlbumList2", url.Values{"type": {"highest"}}),
			"albumList2",
		)
		Expect(ids(list(highest, "album"))).To(ConsistOf(markAlbum(album)))
		Expect(list(highest, "album")[0]["userRating"]).To(BeEquivalentTo(4))

		u.call("setRating", url.Values{"id": {markAlbum(album)}, "rating": {"0"}})
		highest = obj(
			u.call("getAlbumList2", url.Values{"type": {"highest"}}),
			"albumList2",
		)
		Expect(list(highest, "album")).To(BeEmpty())
	})

	It("shows the rating on a song read through getSong", func() {
		song := lib.track(0, 0, 0)
		marksRequireEndpoint(u, "getSong", url.Values{"id": {markTrack(song)}})
		u.call("setRating", url.Values{"id": {markTrack(song)}, "rating": {"2"}})
		got := obj(u.call("getSong", url.Values{"id": {markTrack(song)}}), "song")
		Expect(got["userRating"]).To(BeEquivalentTo(2))
	})

	It("refuses a rating outside 0 to 5 or one that is not a number", func() {
		album := markAlbum(lib.Artists[0].Albums[0])
		for _, bad := range []string{"6", "7", "-1", "x", "99999999999999999999"} {
			Expect(errorCode(u.callError("setRating", url.Values{
				"id": {album}, "rating": {bad},
			}))).To(Equal(0), bad)
		}
		Expect(
			errorCode(u.callError("setRating", url.Values{"id": {album}})),
		).To(Equal(10))
		Expect(
			errorCode(u.callError("setRating", url.Values{"rating": {"3"}})),
		).To(Equal(10))
	})

	It("refuses an id that is malformed, the wrong kind, or gone", func() {
		for _, id := range []string{"nonsense", "pl-1", "tr-999999", "al-999999", "ar-999999"} {
			Expect(errorCode(u.callError("setRating", url.Values{
				"id": {id}, "rating": {"3"},
			}))).To(Equal(70), id)
		}
	})

	It("refuses a caller without the rating permission", func() {
		app.setSettings(u, map[string]any{"can_rate": false})
		Expect(errorCode(u.callError("setRating", url.Values{
			"id": {markAlbum(lib.Artists[0].Albums[0])}, "rating": {"3"},
		}))).To(Equal(50))
	})

	It("still lets a caller without the rating permission star", func() {
		app.setSettings(u, map[string]any{"can_rate": false})
		u.call("star", url.Values{"id": {markTrack(lib.track(0, 0, 0))}})
		Expect(list(marksStarred2(u), "song")).To(HaveLen(1))
	})
})

var _ = Describe("scrobble", Label("e2e"), func() {
	var (
		lib library
		u   *testUser
	)

	BeforeEach(func() {
		lib = app.sharedLibrary()
		u = app.subsonicUser("scrobble")
	})

	It("counts a scrobble on the song and keeps the time it was played", func() {
		song := lib.track(0, 0, 0)
		at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
		u.call("star", url.Values{"id": {markTrack(song)}})
		u.call("scrobble", url.Values{
			"id":   {markTrack(song)},
			"time": {strconv.FormatInt(at.UnixMilli(), 10)},
		})
		got := list(marksStarred2(u), "song")[0]
		Expect(got["playCount"]).To(BeEquivalentTo(1))
		Expect(got["played"]).To(Equal("2026-03-01T12:00:00Z"))

		u.call("scrobble", url.Values{"id": {markTrack(song)}})
		got = list(marksStarred2(u), "song")[0]
		Expect(got["playCount"]).To(BeEquivalentTo(2))
		Expect(got["played"]).NotTo(Equal("2026-03-01T12:00:00Z"))
	})

	It("keeps the latest play time when an older play arrives later", func() {
		song := lib.track(0, 0, 1)
		u.call("star", url.Values{"id": {markTrack(song)}})
		late := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		early := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		u.call(
			"scrobble",
			url.Values{"id": {markTrack(song)}, "time": {marksMillis(late)}},
		)
		u.call(
			"scrobble",
			url.Values{"id": {markTrack(song)}, "time": {marksMillis(early)}},
		)
		got := list(marksStarred2(u), "song")[0]
		Expect(got["playCount"]).To(BeEquivalentTo(2))
		Expect(got["played"]).To(Equal("2026-06-01T00:00:00Z"))
	})

	It("counts several songs in one call, each with its own time", func() {
		a, b := lib.track(0, 0, 0), lib.track(0, 0, 1)
		u.call("star", url.Values{"id": {markTrack(a), markTrack(b)}})
		u.call("scrobble", url.Values{
			"id": {markTrack(a), markTrack(b)},
			"time": {
				marksMillis(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
				marksMillis(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
			},
		})
		byID := map[string]map[string]any{}
		for _, s := range list(marksStarred2(u), "song") {
			byID[s["id"].(string)] = s
		}
		Expect(byID[markTrack(a)]["played"]).To(Equal("2026-01-01T00:00:00Z"))
		Expect(byID[markTrack(b)]["played"]).To(Equal("2026-02-01T00:00:00Z"))
	})

	It("counts the play on the album and the artist as well", func() {
		song, album, artist := lib.track(
			0,
			0,
			0,
		), lib.Artists[0].Albums[0], lib.Artists[0]
		u.call("star", url.Values{"albumId": {markAlbum(album)}})
		u.call("scrobble", url.Values{"id": {markTrack(song)}})
		u.call("scrobble", url.Values{"id": {markTrack(lib.track(0, 0, 1))}})
		got := list(marksStarred2(u), "album")[0]
		Expect(got["playCount"]).To(BeEquivalentTo(2))
		Expect(got["played"]).NotTo(BeEmpty())

		marksRequireEndpoint(u, "getAlbumList2", url.Values{"type": {"frequent"}})
		for _, typ := range []string{"frequent", "recent"} {
			resp := obj(
				u.call("getAlbumList2", url.Values{"type": {typ}}),
				"albumList2",
			)
			Expect(ids(list(resp, "album"))).To(ConsistOf(markAlbum(album)), typ)
		}

		marksRequireEndpoint(u, "getTopSongs", url.Values{"artist": {artist.Name}})
		top := obj(
			u.call("getTopSongs", url.Values{"artist": {artist.Name}}),
			"topSongs",
		)
		Expect(
			ids(list(top, "song")),
		).To(ConsistOf(markTrack(song), markTrack(lib.track(0, 0, 1))))
	})

	It("ranks only playable songs among the top songs", func() {
		artist := app.addArtist(artistSpec{
			Name: app.unique("Toppers"), Genre: "Rock",
			Albums: []albumSpec{{
				Title: "Charts", Year: 2020, Type: "album", Genres: []string{"Rock"},
				Tracks: []trackSpec{
					{Title: "Hit", Position: 1},
					{Title: "Lost", Position: 2, NoFile: true},
				},
			}},
		})
		hit, lost := artist.Albums[0].Tracks[0], artist.Albums[0].Tracks[1]
		marksRequireEndpoint(u, "getTopSongs", url.Values{"artist": {artist.Name}})
		u.call("scrobble", url.Values{"id": {markTrack(hit)}})
		for range 3 {
			u.call("scrobble", url.Values{"id": {markTrack(lost)}})
		}
		top := obj(u.call("getTopSongs", url.Values{
			"artist": {artist.Name}, "count": {"1"},
		}), "topSongs")
		Expect(ids(list(top, "song"))).To(Equal([]string{markTrack(hit)}))
	})

	It("keeps each listener's plays to themselves", func() {
		other := app.subsonicUser("scrobble-other")
		song := lib.track(0, 0, 2)
		u.call("scrobble", url.Values{"id": {markTrack(song)}})
		other.call("star", url.Values{"id": {markTrack(song)}})
		Expect(list(marksStarred2(other), "song")[0]).NotTo(HaveKey("playCount"))
	})

	It("refuses a missing, malformed, wrong-kind or unknown id", func() {
		Expect(errorCode(u.callError("scrobble", nil))).To(Equal(10))
		for _, id := range []string{"nonsense", "tr-999999", markAlbum(lib.Artists[0].Albums[0]), "pl-1"} {
			Expect(
				errorCode(u.callError("scrobble", url.Values{"id": {id}})),
			).To(Equal(70), id)
			Expect(errorCode(u.callError("scrobble", url.Values{
				"id": {id}, "submission": {"false"},
			}))).To(Equal(70), id)
		}
	})

	It("counts nothing when one id of the call is unknown", func() {
		song := lib.track(0, 0, 0)
		u.call("star", url.Values{"id": {markTrack(song)}})
		Expect(errorCode(u.callError("scrobble", url.Values{
			"id": {markTrack(song), "tr-999999"},
		}))).To(Equal(70))
		Expect(list(marksStarred2(u), "song")[0]).NotTo(HaveKey("playCount"))
	})
})

var _ = Describe("getNowPlaying", Label("e2e"), func() {
	var (
		lib library
		u   *testUser
	)

	BeforeEach(func() {
		lib = app.sharedLibrary()
		u = app.subsonicUser("playing")
	})

	It(
		"reports now playing without counting a play, and a final scrobble closes it",
		func() {
			song := lib.track(0, 0, 0)
			u.call("star", url.Values{"id": {markTrack(song)}})
			u.call("scrobble", url.Values{
				"id": {markTrack(song)}, "submission": {"false"}, "c": {"Symfonium"},
			})
			playing := marksNowPlaying(u)
			Expect(playing).To(ContainElement(And(
				HaveKeyWithValue("id", markTrack(song)),
				HaveKeyWithValue("username", u.Email),
				HaveKeyWithValue("playerName", "Symfonium"),
				HaveKeyWithValue("minutesAgo", BeNumerically("==", 0)),
				HaveKeyWithValue("title", song.Title),
				HaveKeyWithValue("starred", Not(BeEmpty())),
				Not(HaveKey("playCount")),
			)))
			Expect(list(marksStarred2(u), "song")[0]).NotTo(HaveKey("playCount"))

			u.call(
				"scrobble",
				url.Values{"id": {markTrack(song)}, "c": {"Symfonium"}},
			)
			Expect(
				marksNowPlaying(u),
			).NotTo(ContainElement(HaveKeyWithValue("id", markTrack(song))))
		},
	)

	It("keeps an entry when the final scrobble is another song", func() {
		a, b := lib.track(0, 0, 0), lib.track(0, 0, 1)
		u.call(
			"scrobble",
			url.Values{"id": {markTrack(a)}, "submission": {"false"}, "c": {"p"}},
		)
		u.call("scrobble", url.Values{"id": {markTrack(b)}, "c": {"p"}})
		Expect(
			marksNowPlaying(u),
		).To(ContainElement(HaveKeyWithValue("id", markTrack(a))))
	})

	It("keeps one entry per client and the last song of a call", func() {
		a, b := lib.track(0, 0, 0), lib.track(0, 0, 1)
		u.call(
			"scrobble",
			url.Values{
				"id":         {markTrack(a)},
				"submission": {"false"},
				"c":          {"phone"},
			},
		)
		u.call("scrobble", url.Values{
			"id": {
				markTrack(a),
				markTrack(b),
			},
			"submission": {"false"},
			"c":          {"phone"},
		})
		mine := marksEntriesOf(marksNowPlaying(u), u.Email)
		Expect(mine).To(HaveLen(1))
		Expect(mine[0]["id"]).To(Equal(markTrack(b)))
	})

	It("reads submission like every other boolean", func() {
		song := lib.track(0, 0, 0)
		u.call("star", url.Values{"id": {markTrack(song)}})
		u.call("scrobble", url.Values{
			"id": {markTrack(song)}, "submission": {"0"}, "c": {"Symfonium"},
		})
		Expect(marksEntriesOf(marksNowPlaying(u), u.Email)).To(HaveLen(1))
		Expect(list(marksStarred2(u), "song")[0]).NotTo(HaveKey("playCount"))
	})

	It("shows every listener's entries to any signed-in user", func() {
		other := app.subsonicUser("playing-other")
		song := lib.track(0, 0, 1)
		other.call(
			"scrobble",
			url.Values{
				"id":         {markTrack(song)},
				"submission": {"false"},
				"c":          {"tv"},
			},
		)
		Expect(marksEntriesOf(marksNowPlaying(u), other.Email)).To(HaveLen(1))
	})

	It("expires an entry nobody closed", func() {
		song := lib.track(0, 0, 0)
		u.call(
			"scrobble",
			url.Values{
				"id":         {markTrack(song)},
				"submission": {"false"},
				"c":          {"old"},
			},
		)
		Expect(marksEntriesOf(marksNowPlaying(u), u.Email)).To(HaveLen(1))

		db := app.ext()
		defer db.Close()
		_, err := db.Exec(
			"UPDATE now_playing SET expires_at = '2000-01-01T00:00:00.000Z' WHERE user_id = ?",
			u.ID,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(marksEntriesOf(marksNowPlaying(u), u.Email)).To(BeEmpty())
	})

	It("lives for the song's length plus ten minutes", func() {
		song := lib.track(0, 0, 0)
		u.call(
			"scrobble",
			url.Values{
				"id":         {markTrack(song)},
				"submission": {"false"},
				"c":          {"ttl"},
			},
		)
		db := app.ext()
		defer db.Close()
		var started, expires string
		Expect(db.QueryRow(
			"SELECT started_at, expires_at FROM now_playing WHERE user_id = ?", u.ID,
		).Scan(&started, &expires)).To(Succeed())
		from, err := time.Parse("2006-01-02T15:04:05.000Z", started)
		Expect(err).NotTo(HaveOccurred())
		until, err := time.Parse("2006-01-02T15:04:05.000Z", expires)
		Expect(err).NotTo(HaveOccurred())
		Expect(until.Sub(from)).To(BeNumerically(">=", 10*time.Minute))
		Expect(until.Sub(from)).To(BeNumerically("<", 11*time.Minute))
	})

	It("skips a song with no file", func() {
		gone := lib.track(0, 1, 1)
		u.call(
			"scrobble",
			url.Values{"id": {markTrack(gone)}, "submission": {"false"}},
		)
		Expect(marksEntriesOf(marksNowPlaying(u), u.Email)).To(BeEmpty())
	})

	It("bounds one account's entries to its newest", func() {
		song := lib.track(0, 0, 0)
		for i := range 12 {
			u.call("scrobble", url.Values{
				"id": {
					markTrack(song),
				},
				"submission": {"false"},
				"c":          {"player-" + strconv.Itoa(i)},
			})
			time.Sleep(5 * time.Millisecond)
		}
		mine := marksEntriesOf(marksNowPlaying(u), u.Email)
		Expect(mine).To(HaveLen(8))
		names := make([]any, len(mine))
		for i, m := range mine {
			names[i] = m["playerName"]
		}
		Expect(names).To(ContainElement("player-11"))
		Expect(names).NotTo(ContainElement("player-0"))
	})

	It("lists the newest entry first", func() {
		a, b := lib.track(0, 0, 0), lib.track(0, 0, 1)
		u.call(
			"scrobble",
			url.Values{"id": {markTrack(a)}, "submission": {"false"}, "c": {"one"}},
		)
		time.Sleep(10 * time.Millisecond)
		u.call(
			"scrobble",
			url.Values{"id": {markTrack(b)}, "submission": {"false"}, "c": {"two"}},
		)
		mine := marksEntriesOf(marksNowPlaying(u), u.Email)
		Expect(mine[0]["playerName"]).To(Equal("two"))
		Expect(mine[1]["playerName"]).To(Equal("one"))
	})
})

func marksMillis(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

func marksEntriesOf(entries []map[string]any, username string) []map[string]any {
	var out []map[string]any
	for _, e := range entries {
		if e["username"] == username {
			out = append(out, e)
		}
	}
	return out
}
