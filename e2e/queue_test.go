package e2e

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("bookmarks", Label("e2e"), func() {
	var (
		u        *testUser
		withFile seededTrack
	)

	BeforeEach(func() {
		withFile = app.sharedLibrary().track(0, 0, 0)
		u = app.subsonicUser("bookmarks")
	})

	It("creates, moves and deletes a bookmark", func() {
		id := trackRef(withFile)
		u.call("createBookmark", url.Values{
			"id": {id}, "position": {"61000"}, "comment": {"chorus"},
		})
		u.call("createBookmark", url.Values{"id": {id}, "position": {"90000"}})

		bs := list(obj(u.call("getBookmarks", nil), "bookmarks"), "bookmark")
		Expect(bs).To(HaveLen(1))
		Expect(bs[0]["position"]).To(BeEquivalentTo(90000))
		Expect(bs[0]["username"]).To(Equal(u.Email))
		Expect(bs[0]["entry"].(map[string]any)["id"]).To(Equal(id))
		Expect(bs[0]["entry"].(map[string]any)["title"]).To(Equal(withFile.Title))

		u.call("deleteBookmark", url.Values{"id": {id}})
		Expect(list(obj(u.call("getBookmarks", nil), "bookmarks"), "bookmark")).
			To(BeEmpty())
	})

	It("keeps each user's bookmarks to themselves", func() {
		other := app.subsonicUser("bookmarks-other")
		id := trackRef(withFile)
		u.call("createBookmark", url.Values{"id": {id}, "position": {"1000"}})
		other.call("createBookmark", url.Values{"id": {id}, "position": {"2000"}})

		Expect(list(obj(other.call("getBookmarks", nil), "bookmarks"), "bookmark")).
			To(HaveLen(1))
		other.call("deleteBookmark", url.Values{"id": {id}})
		bs := list(obj(u.call("getBookmarks", nil), "bookmarks"), "bookmark")
		Expect(bs).To(HaveLen(1))
		Expect(bs[0]["position"]).To(BeEquivalentTo(1000))
	})

	It("refuses a missing position, and a track that does not exist", func() {
		id := trackRef(withFile)
		Expect(errorCode(u.callError("createBookmark", url.Values{"id": {id}}))).
			To(Equal(10))
		Expect(errorCode(u.callError("createBookmark", url.Values{
			"id": {"tr-999999"}, "position": {"1"},
		}))).To(Equal(70))
		Expect(errorCode(u.callError("createBookmark", url.Values{
			"id": {"al-1"}, "position": {"1"},
		}))).To(Equal(70))
		Expect(errorCode(u.callError("createBookmark", url.Values{
			"id": {"nonsense"}, "position": {"1"},
		}))).To(Equal(70))
		Expect(errorCode(u.callError("createBookmark", url.Values{
			"id": {id}, "position": {"-5"},
		}))).To(Equal(0))
		Expect(errorCode(u.callError("deleteBookmark", nil))).To(Equal(10))
	})
})

var _ = Describe("the play queue", Label("e2e"), func() {
	var (
		u        *testUser
		withFile seededTrack
		noFile   seededTrack
	)

	BeforeEach(func() {
		lib := app.sharedLibrary()
		withFile, noFile = lib.track(0, 0, 0), lib.track(0, 1, 1)
		u = app.subsonicUser("queue")
	})

	It("saves and restores the queue by id and by index", func() {
		Expect(u.call("getPlayQueue", nil)).NotTo(HaveKey("playQueue"))

		// noFile has no file, so the saved queue is reduced to playable entries.
		other := app.sharedLibrary().track(0, 0, 1)
		u.call("savePlayQueue", url.Values{
			"id": {
				trackRef(withFile), trackRef(other), trackRef(withFile),
			},
			"current":  {trackRef(other)},
			"position": {"12000"},
			"c":        {"Tempo"},
		})
		q := obj(u.call("getPlayQueue", nil), "playQueue")
		Expect(q["current"]).To(Equal(trackRef(other)))
		Expect(q["position"]).To(BeEquivalentTo(12000))
		Expect(q["changedBy"]).To(Equal("Tempo"))
		Expect(q["username"]).To(Equal(u.Email))
		Expect(ids(list(q, "entry"))).To(Equal(
			[]string{trackRef(withFile), trackRef(other), trackRef(withFile)},
		))

		u.call("savePlayQueueByIndex", url.Values{
			"id": {
				trackRef(withFile), trackRef(other), trackRef(withFile),
			},
			"currentIndex": {"2"},
		})
		qi := obj(u.call("getPlayQueueByIndex", nil), "playQueueByIndex")
		Expect(qi["currentIndex"]).To(BeEquivalentTo(2))
		q = obj(u.call("getPlayQueue", nil), "playQueue")
		Expect(q["current"]).To(Equal(trackRef(withFile)))

		u.call("savePlayQueue", nil)
		Expect(u.call("getPlayQueue", nil)).NotTo(HaveKey("playQueue"))
		Expect(u.call("getPlayQueueByIndex", nil)).
			NotTo(HaveKey("playQueueByIndex"))
	})

	It("refuses a current entry that is not in the queue", func() {
		Expect(errorCode(u.callError("savePlayQueue", url.Values{
			"id": {trackRef(withFile)}, "current": {trackRef(noFile)},
		}))).To(Equal(0))
		Expect(errorCode(u.callError("savePlayQueueByIndex", url.Values{
			"id": {trackRef(withFile)}, "currentIndex": {"3"},
		}))).To(Equal(0))
		Expect(errorCode(u.callError("savePlayQueue", url.Values{
			"id": {trackRef(withFile)}, "position": {"-1"},
		}))).To(Equal(0))
	})

	It("refuses an id that is malformed, of another kind or gone", func() {
		for _, id := range []string{"junk", "al-1", "pl-1", "tr-999999"} {
			Expect(errorCode(u.callError("savePlayQueue", url.Values{
				"id": {trackRef(withFile), id},
			}))).To(Equal(70), id)
		}
		Expect(errorCode(u.callError("savePlayQueue", url.Values{
			"id": {trackRef(withFile)}, "current": {"junk"},
		}))).To(Equal(70))
		Expect(u.call("getPlayQueue", nil)).NotTo(HaveKey("playQueue"))
	})

	It("moves the current entry past one that has no file", func() {
		u.call("savePlayQueueByIndex", url.Values{
			"id":           {trackRef(noFile), trackRef(withFile)},
			"currentIndex": {"1"},
		})
		qi := obj(u.call("getPlayQueueByIndex", nil), "playQueueByIndex")
		Expect(qi["currentIndex"]).To(BeEquivalentTo(0))
		Expect(ids(list(qi, "entry"))).To(Equal([]string{trackRef(withFile)}))
	})

	It("drops an entry whose file went missing", func() {
		lib := app.seedLibrary()
		a, b := lib.track(0, 0, 0), lib.track(0, 0, 1)
		u.call("savePlayQueueByIndex", url.Values{
			"id":           {trackRef(a), trackRef(b)},
			"currentIndex": {"1"},
			"position":     {"500"},
		})
		_, err := app.core.Exec(
			"UPDATE media_files SET missing_since = ? WHERE id = ?",
			coreTime(time.Now()), b.FileID,
		)
		Expect(err).NotTo(HaveOccurred())

		q := obj(u.call("getPlayQueue", nil), "playQueue")
		Expect(ids(list(q, "entry"))).To(Equal([]string{trackRef(a)}))
		Expect(q).NotTo(HaveKey("current"))
		Expect(q["position"]).To(BeEquivalentTo(0))
	})

	It("never shows one user's queue to another", func() {
		other := app.subsonicUser("queue-other")
		u.call("savePlayQueue", url.Values{"id": {trackRef(withFile)}})
		Expect(other.call("getPlayQueue", nil)).NotTo(HaveKey("playQueue"))

		other.call("savePlayQueue", url.Values{"id": {trackRef(withFile)}})
		other.call("savePlayQueue", nil)
		Expect(
			ids(list(obj(u.call("getPlayQueue", nil), "playQueue"), "entry")),
		).To(Equal([]string{trackRef(withFile)}))
	})

	It("never splits a character when it trims the client name", func() {
		name := strings.Repeat("a", 63) + "é"
		u.call("savePlayQueue", url.Values{
			"id": {trackRef(withFile)}, "c": {name},
		})
		q := obj(u.call("getPlayQueue", nil), "playQueue")
		changedBy := q["changedBy"].(string)
		Expect(utf8.ValidString(changedBy)).To(BeTrue(), "%q", changedBy)
		Expect(len(changedBy)).To(BeNumerically("<=", 64))
	})
})

var _ = Describe("internet radio", Label("e2e"), func() {
	It("lets every user list stations and only an admin change them", func() {
		member := app.subsonicUser("radio")
		admin := app.newUserWithRole("radio-admin", "admin")
		app.setSubsonicPassword(admin, "pw-"+admin.Email)
		name := app.unique("FIP")

		Expect(errorCode(member.callError("createInternetRadioStation", url.Values{
			"name": {name}, "streamUrl": {"https://icecast.example/fip.aac"},
		}))).To(Equal(50))

		admin.call("createInternetRadioStation", url.Values{
			"name":        {name},
			"streamUrl":   {"https://icecast.example/fip.aac"},
			"homepageUrl": {"https://www.example.org/fip"},
		})
		find := func() map[string]any {
			stations := list(
				obj(
					member.call("getInternetRadioStations", nil),
					"internetRadioStations",
				),
				"internetRadioStation",
			)
			for _, s := range stations {
				if s["name"] == name || s["name"] == name+" Rock" {
					return s
				}
			}
			return nil
		}
		s := find()
		Expect(s).NotTo(BeNil())
		id := s["id"].(string)
		Expect(id).To(HavePrefix("rs-"))
		Expect(s["homePageUrl"]).To(Equal("https://www.example.org/fip"))

		Expect(errorCode(member.callError("updateInternetRadioStation", url.Values{
			"id": {id}, "name": {"x"},
		}))).To(Equal(50))
		Expect(errorCode(member.callError("deleteInternetRadioStation", url.Values{
			"id": {id},
		}))).To(Equal(50))

		admin.call("updateInternetRadioStation", url.Values{
			"id": {id}, "name": {name + " Rock"},
			"streamUrl": {"https://example.org/rock"},
		})
		s = find()
		Expect(s["name"]).To(Equal(name + " Rock"))
		Expect(s["streamUrl"]).To(Equal("https://example.org/rock"))
		Expect(s["homePageUrl"]).To(Equal("https://www.example.org/fip"))

		Expect(errorCode(admin.callError("createInternetRadioStation", url.Values{
			"name": {"Bad"}, "streamUrl": {"javascript:alert(1)"},
		}))).To(Equal(0))
		Expect(errorCode(admin.callError("createInternetRadioStation", url.Values{
			"name": {"Bad"},
		}))).To(Equal(10))
		Expect(errorCode(admin.callError("updateInternetRadioStation", url.Values{
			"id": {id}, "homepageUrl": {"file:///etc/passwd"},
		}))).To(Equal(0))

		admin.call("deleteInternetRadioStation", url.Values{"id": {id}})
		Expect(find()).To(BeNil())
		Expect(errorCode(admin.callError("deleteInternetRadioStation", url.Values{
			"id": {id},
		}))).To(Equal(70))
		Expect(errorCode(admin.callError("updateInternetRadioStation", url.Values{
			"id": {id}, "name": {"x"},
		}))).To(Equal(70))
		Expect(errorCode(admin.callError("deleteInternetRadioStation", url.Values{
			"id": {"tr-1"},
		}))).To(Equal(70))
	})
})
