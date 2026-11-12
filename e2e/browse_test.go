package e2e

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func browseAr(id uint32) string { return "ar-" + itoa(id) }
func browseAl(id uint32) string { return "al-" + itoa(id) }
func browseTr(id uint32) string { return "tr-" + itoa(id) }

// browseBand is a one-album artist of a spec's own: a song with a file and one
// without, under names and a genre no other spec shares.
type browseBand struct {
	Name, Genre, Album string
	Artist             seededArtist
	// WithFile and NoFile are the album's two tracks.
	WithFile, NoFile seededTrack
}

func (b browseBand) albumID() uint32 { return b.Artist.Albums[0].ID }

func newBrowseBand() browseBand {
	GinkgoHelper()
	name, genre, album := app.unique("Nirvana"), app.unique("Grunge"),
		app.unique("Nevermind")
	ar := app.addArtist(artistSpec{
		Name: name, Genre: genre,
		Albums: []albumSpec{{
			Title: album, Year: 1991, Type: "album", Genres: []string{genre},
			Tracks: []trackSpec{
				{Title: "Smells Like Teen Spirit", Position: 1},
				{Title: "In Bloom", Position: 2, NoFile: true},
			},
		}},
	})
	return browseBand{
		Name: name, Genre: genre, Album: album, Artist: ar,
		WithFile: ar.Albums[0].Tracks[0], NoFile: ar.Albums[0].Tracks[1],
	}
}

// browseFile gives a track a row in media_files; the path need not exist for
// the browse endpoints.
func browseFile(trackID uint32, path string) {
	GinkgoHelper()
	now := coreTime(time.Now())
	res, err := app.core.Exec(
		`INSERT INTO media_files(create_time, update_time, path, size, format,
			quality, duration_seconds, bitrate, audio_channels, track_media_files)
		VALUES(?, ?, ?, 1000, 'flac', 'lossless', 60, 800000, 2, ?)`,
		now, now, path, trackID,
	)
	Expect(err).NotTo(HaveOccurred())
	Expect(res.LastInsertId()).To(BeNumerically(">", 0))
}

func browseCore(query string, args ...any) {
	GinkgoHelper()
	_, err := app.core.Exec(query, args...)
	Expect(err).NotTo(HaveOccurred())
}

func browseNames(artists map[string]any) []string {
	GinkgoHelper()
	var names []string
	for _, idx := range list(artists, "index") {
		for _, a := range list(idx, "artist") {
			names = append(names, a["name"].(string))
		}
	}
	return names
}

var _ = Describe("browsing the library", Label("e2e"), func() {
	var (
		u    *testUser
		band browseBand
	)

	BeforeEach(func() {
		u = app.subsonicUser("browse")
		band = newBrowseBand()
	})

	Describe("artists", func() {
		It("groups getArtists by the first letter of the sort name", func() {
			artists := obj(u.call("getArtists", nil), "artists")
			Expect(artists).To(HaveKey("ignoredArticles"))
			var entry map[string]any
			for _, idx := range list(artists, "index") {
				for _, a := range list(idx, "artist") {
					if a["id"] == browseAr(band.Artist.ID) {
						entry = a
						Expect(idx["name"]).To(Equal("N"))
					}
				}
			}
			Expect(entry).NotTo(BeNil())
			Expect(entry["name"]).To(Equal(band.Name))
			Expect(entry["albumCount"]).To(BeEquivalentTo(1))
			Expect(entry["coverArt"]).To(Equal(browseAr(band.Artist.ID)))
		})

		It("falls back to the name when there is no sort name", func() {
			browseCore(
				"UPDATE artists SET sort_name = '' WHERE id = ?",
				band.Artist.ID,
			)
			artists := obj(u.call("getArtists", nil), "artists")
			for _, idx := range list(artists, "index") {
				if len(list(idx, "artist")) > 0 &&
					ids(list(idx, "artist"))[0] == browseAr(band.Artist.ID) {
					Expect(idx["name"]).To(Equal("N"))
				}
			}
			Expect(browseNames(artists)).To(ContainElement(band.Name))
		})

		It("sorts a name that is no letter under #", func() {
			odd := app.addArtist(artistSpec{
				Name: "9" + app.unique("Lives"), Genre: "Pop",
				Albums: []albumSpec{{
					Title: app.unique("Cat"), Year: 1999,
					Tracks: []trackSpec{{Title: "One", Position: 1}},
				}},
			})
			artists := obj(u.call("getArtists", nil), "artists")
			found := false
			for _, idx := range list(artists, "index") {
				if slices.Contains(ids(list(idx, "artist")), browseAr(odd.ID)) {
					found = true
					Expect(idx["name"]).To(Equal("#"))
				}
			}
			Expect(found).To(BeTrue())
		})

		It("answers getIndexes with the same grouping and a lastModified", func() {
			ix := obj(u.call("getIndexes", nil), "indexes")
			Expect(ix["lastModified"]).To(BeNumerically(">", 0))
			Expect(ix).To(HaveKey("ignoredArticles"))
			Expect(browseNames(ix)).To(ContainElement(band.Name))
		})

		It(
			"filters getIndexes by music folder and never by ifModifiedSince",
			func() {
				Expect(obj(u.call("getIndexes", url.Values{"musicFolderId": {"2"}}),
					"indexes")["index"]).To(BeEmpty())
				Expect(browseNames(obj(u.call("getIndexes",
					url.Values{"musicFolderId": {"1"}}), "indexes"))).
					To(ContainElement(band.Name))
				for _, since := range []string{"99999999999999", "0"} {
					got := obj(u.call("getIndexes",
						url.Values{"ifModifiedSince": {since}}), "indexes")
					Expect(browseNames(got)).To(ContainElement(band.Name), since)
				}
				Expect(obj(u.call("getArtists", url.Values{"musicFolderId": {"2"}}),
					"artists")["index"]).To(BeEmpty())
			},
		)

		It("lists an artist's albums with the songs it can play", func() {
			got := obj(u.call("getArtist",
				url.Values{"id": {browseAr(band.Artist.ID)}}), "artist")
			Expect(got["name"]).To(Equal(band.Name))
			albums := list(got, "album")
			Expect(albums).To(HaveLen(1))
			Expect(albums[0]["id"]).To(Equal(browseAl(band.albumID())))
			Expect(albums[0]["songCount"]).To(BeEquivalentTo(1))
			Expect(albums[0]["coverArt"]).To(Equal(browseAl(band.albumID())))
			Expect(albums[0]["year"]).To(BeEquivalentTo(1991))
			Expect(albums[0]["artist"]).To(Equal(band.Name))
		})

		It("leaves out the albums and artists it holds no file for", func() {
			empty := app.addArtist(artistSpec{
				Name: app.unique("Melvins"), Genre: "Sludge",
				Albums: []albumSpec{{
					Title: "Houdini", Year: 1993,
					Tracks: []trackSpec{{Title: "Hooch", Position: 1, NoFile: true}},
				}},
			})
			epTitle := app.unique("Hormoaning")
			ep := browseAddAlbum(band, albumSpec{
				Title:  epTitle,
				Year:   1992,
				Type:   "ep",
				Genres: []string{band.Genre},
				Tracks: []trackSpec{
					{Title: "Turnaround", Position: 1, NoFile: true},
				},
			})

			artists := obj(u.call("getArtists", nil), "artists")
			Expect(browseNames(artists)).To(ContainElement(band.Name))
			Expect(browseNames(artists)).NotTo(ContainElement(empty.Name))

			got := obj(u.call("getArtist",
				url.Values{"id": {browseAr(band.Artist.ID)}}), "artist")
			Expect(got["albumCount"]).To(BeEquivalentTo(1))
			Expect(
				ids(list(got, "album")),
			).To(Equal([]string{browseAl(band.albumID())}))

			res := obj(u.call("search3", url.Values{"query": {band.Name}}),
				"searchResult3")
			Expect(
				ids(list(res, "album")),
			).To(Equal([]string{browseAl(band.albumID())}))

			for _, typ := range []string{"newest", "alphabeticalByName", "random"} {
				albums := list(obj(u.call("getAlbumList2", url.Values{
					"type": {typ}, "size": {"500"},
				}), "albumList2"), "album")
				Expect(ids(albums)).NotTo(ContainElement(browseAl(ep.ID)), typ)
				Expect(ids(albums)).To(ContainElement(browseAl(band.albumID())), typ)
			}

			By("listing the album once a file lands, newest first by that file")
			browseFile(ep.Tracks[0].ID, "/music/ep/01.flac")
			albums := list(obj(u.call("getAlbumList2", url.Values{
				"type": {"newest"}, "size": {"2"},
			}), "albumList2"), "album")
			Expect(ids(albums)).To(Equal(
				[]string{browseAl(ep.ID), browseAl(band.albumID())}))
		})
	})

	Describe("a co-credited artist", func() {
		var guest seededArtist

		BeforeEach(func() {
			guest = app.addArtist(
				artistSpec{Name: app.unique("Guest"), Genre: "Pop"},
			)
			browseCore(
				"INSERT INTO artist_co_albums(album_id, artist_id) VALUES(?, ?)",
				band.albumID(),
				guest.ID,
			)
		})

		It("lists a co-album under getArtist, keeping its owner", func() {
			got := obj(u.call("getArtist",
				url.Values{"id": {browseAr(guest.ID)}}), "artist")
			Expect(got["albumCount"]).To(BeEquivalentTo(1))
			albums := list(got, "album")
			Expect(albums).To(HaveLen(1))
			Expect(albums[0]["id"]).To(Equal(browseAl(band.albumID())))
			Expect(albums[0]["artistId"]).To(Equal(browseAr(band.Artist.ID)))
			Expect(albums[0]["artist"]).To(Equal(band.Name))

			By("listing it in the folder view under the co-artist")
			dir := obj(u.call("getMusicDirectory",
				url.Values{"id": {browseAr(guest.ID)}}), "directory")
			child := list(dir, "child")
			Expect(child).To(HaveLen(1))
			Expect(child[0]["parent"]).To(Equal(browseAr(guest.ID)))
		})

		It("lists the artist among the artists", func() {
			Expect(browseNames(obj(u.call("getArtists", nil), "artists"))).
				To(ContainElement(guest.Name))
		})

		It("names a song by its own credited artist on a split release", func() {
			song := func() map[string]any {
				GinkgoHelper()
				return obj(u.call("getSong",
					url.Values{"id": {browseTr(band.WithFile.ID)}}), "song")
			}
			Expect(song()["artist"]).To(Equal(band.Name))

			browseCore("UPDATE tracks SET artist_credit = ? WHERE id = ?",
				guest.Name, band.WithFile.ID)
			got := song()
			Expect(got["artist"]).To(Equal(guest.Name))
			Expect(got["artistId"]).To(Equal(browseAr(guest.ID)))
			Expect(got["albumId"]).To(Equal(browseAl(band.albumID())))

			browseCore(
				"UPDATE tracks SET artist_credit = 'Somebody Else' WHERE id = ?",
				band.WithFile.ID,
			)
			got = song()
			Expect(got["artist"]).To(Equal("Somebody Else"))
			Expect(got).NotTo(HaveKey("artistId"))
		})

		It("orders owned and co-albums together by date", func() {
			later := app.addArtist(artistSpec{
				Name: guest.Name + " Own", Genre: "Pop",
				Albums: []albumSpec{{
					Title: "Own", Year: 2000, Type: "album",
					Tracks: []trackSpec{{Title: "Mine", Position: 1}},
				}},
			})
			browseCore("UPDATE albums SET artist_albums = ? WHERE id = ?",
				guest.ID, later.Albums[0].ID)
			albums := list(obj(u.call("getArtist",
				url.Values{"id": {browseAr(guest.ID)}}), "artist"), "album")
			Expect(ids(albums)).To(Equal([]string{
				browseAl(band.albumID()), browseAl(later.Albums[0].ID),
			}))
		})

		It("counts a co-album's songs under the artist", func() {
			got := obj(u.call("getSimilarSongs2", url.Values{
				"id": {browseAr(guest.ID)}, "count": {"500"},
			}), "similarSongs2")
			Expect(
				ids(list(got, "song")),
			).To(ContainElement(browseTr(band.WithFile.ID)))
		})
	})

	Describe("albums and songs", func() {
		It("lists an album's songs, omitting tracks with no file", func() {
			lib := app.sharedLibrary()
			first := lib.Artists[0].Albums[0]
			got := obj(
				u.call("getAlbum", url.Values{"id": {browseAl(first.ID)}}),
				"album",
			)
			Expect(got["name"]).To(Equal("First Light"))
			Expect(got["songCount"]).To(BeEquivalentTo(3))
			Expect(got["artistId"]).To(Equal(browseAr(lib.Artists[0].ID)))
			Expect(got["genre"]).To(Equal("Rock"))
			Expect(got["year"]).To(BeEquivalentTo(2019))
			songs := list(got, "song")
			Expect(songs).To(HaveLen(3))
			Expect(songs[0]["id"]).To(Equal(browseTr(first.Tracks[0].ID)))
			Expect(songs[0]["title"]).To(Equal("Dawn"))
			Expect(songs[0]["track"]).To(BeEquivalentTo(1))
			Expect(songs[0]["discNumber"]).To(BeEquivalentTo(1))
			Expect(songs[0]["suffix"]).To(Equal("flac"))
			Expect(songs[0]["contentType"]).To(Equal("audio/flac"))
			Expect(songs[0]["duration"]).To(BeEquivalentTo(2))
			Expect(songs[0]["size"]).To(BeNumerically(">", 0))
			Expect(songs[0]["isDir"]).To(BeFalse())
			Expect(songs[0]["type"]).To(Equal("music"))

			second := lib.Artists[0].Albums[1]
			got = obj(
				u.call("getAlbum", url.Values{"id": {browseAl(second.ID)}}),
				"album",
			)
			Expect(
				ids(list(got, "song")),
			).To(Equal([]string{browseTr(second.Tracks[0].ID)}))

			blue := lib.Artists[1].Albums[0]
			got = obj(
				u.call("getAlbum", url.Values{"id": {browseAl(blue.ID)}}),
				"album",
			)
			Expect(list(got, "song")[0]["contentType"]).To(Equal("audio/mpeg"))
		})

		It("falls back to the track's duration when the file has none", func() {
			browseCore(
				"UPDATE tracks SET duration = 222 WHERE id = ?",
				band.WithFile.ID,
			)
			browseCore("UPDATE media_files SET duration_seconds = 0 WHERE id = ?",
				band.WithFile.FileID)
			got := obj(u.call("getSong",
				url.Values{"id": {browseTr(band.WithFile.ID)}}), "song")
			Expect(got["duration"]).To(BeEquivalentTo(222))
		})

		It(
			"estimates a bit rate from size and duration when none was probed",
			func() {
				browseCore(
					"UPDATE media_files SET bitrate = 0, size = 240000, duration_seconds = 2 WHERE id = ?",
					band.WithFile.FileID,
				)
				got := obj(u.call("getSong",
					url.Values{"id": {browseTr(band.WithFile.ID)}}), "song")
				Expect(got["bitRate"]).To(BeEquivalentTo(960))
			},
		)

		It("answers getSong, and 70 for a track with no file", func() {
			got := obj(u.call("getSong",
				url.Values{"id": {browseTr(band.WithFile.ID)}}), "song")
			Expect(got["title"]).To(Equal("Smells Like Teen Spirit"))
			Expect(got["album"]).To(Equal(band.Album))
			Expect(got["artist"]).To(Equal(band.Name))
			Expect(got["genre"]).To(Equal(band.Genre))

			Expect(errorCode(u.callError("getSong",
				url.Values{"id": {browseTr(band.NoFile.ID)}}))).To(Equal(70))
		})

		It("skips files flagged missing", func() {
			browseCore("UPDATE media_files SET missing_since = ? WHERE id = ?",
				coreTime(time.Now()), band.WithFile.FileID)
			Expect(errorCode(u.callError("getSong",
				url.Values{"id": {browseTr(band.WithFile.ID)}}))).To(Equal(70))
			albums := list(obj(u.call("getAlbumList2", url.Values{
				"type": {"newest"}, "size": {"500"},
			}), "albumList2"), "album")
			Expect(ids(albums)).NotTo(ContainElement(browseAl(band.albumID())))
		})

		It("offers the first file a track still has", func() {
			browseCore("UPDATE media_files SET missing_since = ? WHERE id = ?",
				coreTime(time.Now()), band.WithFile.FileID)
			browseFile(band.WithFile.ID, "/music/second.mp3")
			got := obj(u.call("getSong",
				url.Values{"id": {browseTr(band.WithFile.ID)}}), "song")
			Expect(got["suffix"]).To(Equal("mp3"))
		})

		It("answers 70 for ids that are bad and 10 for none", func() {
			for _, id := range []string{
				"al-999999", "xx-1", "al-99999999999", "al-", "al", "-1",
				browseAr(band.Artist.ID), browseTr(band.WithFile.ID),
			} {
				Expect(errorCode(u.callError("getAlbum",
					url.Values{"id": {id}}))).To(Equal(70), id)
			}
			Expect(errorCode(u.callError("getAlbum", nil))).To(Equal(10))
			for endpoint, wrong := range map[string]string{
				"getArtist": browseAl(band.albumID()),
				"getSong":   browseAl(band.albumID()),
			} {
				Expect(errorCode(u.callError(endpoint,
					url.Values{"id": {wrong}}))).To(Equal(70), endpoint)
				Expect(errorCode(u.callError(endpoint,
					url.Values{"id": {"ar-999999"}}))).To(Equal(70), endpoint)
			}
			for _, endpoint := range []string{
				"getMusicDirectory", "getArtistInfo", "getArtistInfo2",
				"getAlbumInfo", "getAlbumInfo2", "getSimilarSongs", "getSimilarSongs2",
			} {
				Expect(errorCode(u.callError(endpoint, nil))).To(Equal(10), endpoint)
				Expect(errorCode(u.callError(endpoint,
					url.Values{"id": {"zz-1"}}))).To(Equal(70), endpoint)
				Expect(errorCode(u.callError(endpoint,
					url.Values{"id": {"ar-999999"}}))).To(Equal(70), endpoint)
			}
		})

		It("serializes the album tree as XML attributes by default", func() {
			raw := u.get("getAlbum.view", url.Values{
				"id": {browseAl(band.albumID())}, "f": {""},
			})
			body := string(raw.Body)
			Expect(body).To(ContainSubstring(`<album id="al-`))
			Expect(body).To(ContainSubstring(`<song id="tr-`))
			Expect(body).To(ContainSubstring(`suffix="flac"`))
		})
	})

	Describe("the folder view", func() {
		It("walks from artist to album to songs", func() {
			dir := obj(u.call("getMusicDirectory",
				url.Values{"id": {browseAr(band.Artist.ID)}}), "directory")
			Expect(dir["name"]).To(Equal(band.Name))
			albums := list(dir, "child")
			Expect(albums).To(HaveLen(1))
			Expect(albums[0]["isDir"]).To(BeTrue())
			Expect(albums[0]["id"]).To(Equal(browseAl(band.albumID())))
			Expect(albums[0]["parent"]).To(Equal(browseAr(band.Artist.ID)))

			dir = obj(u.call("getMusicDirectory",
				url.Values{"id": {browseAl(band.albumID())}}), "directory")
			Expect(dir["parent"]).To(Equal(browseAr(band.Artist.ID)))
			Expect(
				ids(list(dir, "child")),
			).To(Equal([]string{browseTr(band.WithFile.ID)}))

			Expect(errorCode(u.callError("getMusicDirectory",
				url.Values{"id": {browseTr(band.WithFile.ID)}}))).To(Equal(70))
		})

		It("serves the folder-style album list", func() {
			resp := obj(u.call("getAlbumList", url.Values{
				"type": {"byGenre"}, "genre": {band.Genre},
			}), "albumList")
			albums := list(resp, "album")
			Expect(albums).To(HaveLen(1))
			Expect(albums[0]["isDir"]).To(BeTrue())
			Expect(albums[0]["parent"]).To(Equal(browseAr(band.Artist.ID)))
		})
	})

	Describe("genres", func() {
		It("lists genres and filters by them", func() {
			var found map[string]any
			for _, g := range list(obj(u.call("getGenres", nil), "genres"), "genre") {
				if g["value"] == band.Genre {
					found = g
				}
			}
			Expect(found).NotTo(BeNil())
			Expect(found["albumCount"]).To(BeEquivalentTo(1))
			Expect(found["songCount"]).To(BeEquivalentTo(1))

			byGenre := obj(u.call("getSongsByGenre", url.Values{
				"genre": {strings.ToLower(band.Genre)},
			}), "songsByGenre")
			Expect(ids(list(byGenre, "song"))).To(Equal(
				[]string{browseTr(band.WithFile.ID)}))
			Expect(list(byGenre, "song")[0]["genre"]).To(Equal(band.Genre))

			albums := obj(u.call("getAlbumList2", url.Values{
				"type": {"byGenre"}, "genre": {band.Genre},
			}), "albumList2")
			Expect(list(albums, "album")).To(HaveLen(1))
			albums = obj(u.call("getAlbumList2", url.Values{
				"type": {"byGenre"}, "genre": {"No Such Genre"},
			}), "albumList2")
			Expect(list(albums, "album")).To(BeEmpty())
			Expect(errorCode(u.callError("getAlbumList2",
				url.Values{"type": {"byGenre"}}))).To(Equal(10))
			Expect(errorCode(u.callError("getSongsByGenre", nil))).To(Equal(10))
		})

		It("counts nothing for an album that holds no file", func() {
			genre := app.unique("Cool")
			app.addArtist(artistSpec{
				Name: app.unique("Miles"), Genre: genre,
				Albums: []albumSpec{
					{
						Title: "Kind of Blue",
						Year:  1959,
						Tracks: []trackSpec{
							{Title: "So What", Position: 1, NoFile: true},
						},
					},
				},
			})
			for _, g := range list(obj(u.call("getGenres", nil), "genres"), "genre") {
				Expect(g["value"]).NotTo(Equal(genre))
			}
		})

		It("pages songs by genre", func() {
			second := app.addArtist(artistSpec{
				Name: app.unique("Mudhoney"), Genre: band.Genre,
				Albums: []albumSpec{{
					Title: "Superfuzz", Year: 1988,
					Tracks: []trackSpec{{Title: "Touch Me", Position: 1}},
				}},
			})
			got := func(offset string) []string {
				GinkgoHelper()
				return ids(list(obj(u.call("getSongsByGenre", url.Values{
					"genre": {band.Genre}, "count": {"1"}, "offset": {offset},
				}), "songsByGenre"), "song"))
			}
			Expect(got("0")).To(Equal([]string{browseTr(band.WithFile.ID)}))
			Expect(
				got("1"),
			).To(Equal([]string{browseTr(second.Albums[0].Tracks[0].ID)}))
			Expect(got("2")).To(BeEmpty())
		})
	})

	Describe("album lists", func() {
		It("serves them by type and falls back to alphabetical", func() {
			app.sharedLibrary()
			for _, typ := range []string{"newest", "alphabeticalByName", "random", "bogus"} {
				albums := list(obj(u.call("getAlbumList2", url.Values{
					"type": {typ}, "size": {"1"},
				}), "albumList2"), "album")
				Expect(albums).To(HaveLen(1), typ)
			}
			albums := list(obj(u.call("getAlbumList2", url.Values{
				"type": {"alphabeticalByName"}, "size": {"500"},
			}), "albumList2"), "album")
			titles := make([]string, 0, len(albums))
			for _, a := range albums {
				titles = append(titles, a["name"].(string))
			}
			Expect(titles).To(BeComparableTo(sortedCopy(titles)))
			offset := list(obj(u.call("getAlbumList2", url.Values{
				"type": {"alphabeticalByName"}, "size": {"1"}, "offset": {"1"},
			}), "albumList2"), "album")
			Expect(offset[0]["name"]).To(Equal(titles[1]))
		})

		It("orders by artist, newest first and by year", func() {
			lib := app.sharedLibrary()
			byArtist := list(obj(u.call("getAlbumList2", url.Values{
				"type": {"alphabeticalByArtist"}, "size": {"500"},
			}), "albumList2"), "album")
			Expect(ids(byArtist)).To(ContainElements(
				browseAl(lib.Artists[1].Albums[0].ID),
				browseAl(lib.Artists[0].Albums[0].ID),
			))
			Expect(indexOf(ids(byArtist), browseAl(lib.Artists[1].Albums[0].ID))).
				To(BeNumerically("<", indexOf(ids(byArtist),
					browseAl(lib.Artists[0].Albums[0].ID))))

			for _, r := range [][2]string{{"1990", "1992"}, {"1992", "1990"}} {
				resp := obj(u.call("getAlbumList2", url.Values{
					"type": {"byYear"}, "fromYear": {r[0]}, "toYear": {r[1]},
					"size": {"500"},
				}), "albumList2")
				Expect(ids(list(resp, "album"))).To(
					ContainElement(browseAl(band.albumID())), r[0]+"-"+r[1])
			}
			resp := obj(u.call("getAlbumList2", url.Values{
				"type": {"byYear"}, "fromYear": {"2100"}, "toYear": {"2110"},
			}), "albumList2")
			Expect(list(resp, "album")).To(BeEmpty())

			Expect(errorCode(u.callError("getAlbumList2",
				url.Values{"type": {"byYear"}, "toYear": {"2000"}}))).To(Equal(10))
			Expect(errorCode(u.callError("getAlbumList2",
				url.Values{"type": {"byYear"}, "fromYear": {"2000"}}))).To(Equal(10))

			By("running a reversed range newest first")
			other := app.addArtist(artistSpec{
				Name: app.unique("Old"), Genre: "Pop",
				Albums: []albumSpec{{
					Title: app.unique("Older"), Year: 1951,
					Tracks: []trackSpec{{Title: "One", Position: 1}},
				}, {
					Title: app.unique("Newer"), Year: 1955,
					Tracks: []trackSpec{{Title: "Two", Position: 1}},
				}},
			})
			asc := ids(list(obj(u.call("getAlbumList2", url.Values{
				"type": {"byYear"}, "fromYear": {"1951"}, "toYear": {"1955"},
			}), "albumList2"), "album"))
			desc := ids(list(obj(u.call("getAlbumList2", url.Values{
				"type": {"byYear"}, "fromYear": {"1955"}, "toYear": {"1951"},
			}), "albumList2"), "album"))
			Expect(asc).To(Equal([]string{
				browseAl(other.Albums[0].ID), browseAl(other.Albums[1].ID),
			}))
			Expect(desc).To(Equal([]string{
				browseAl(other.Albums[1].ID), browseAl(other.Albums[0].ID),
			}))
		})

		It("answers an unknown music folder with nothing", func() {
			Expect(list(obj(u.call("getAlbumList2", url.Values{
				"type": {"newest"}, "musicFolderId": {"2"},
			}), "albumList2"), "album")).To(BeEmpty())
			Expect(list(obj(u.call(
				"getRandomSongs",
				url.Values{
					"musicFolderId": {"2"},
				},
			), "randomSongs"), "song")).To(BeEmpty())
		})

		It("lists what the caller starred, rated and played", func() {
			album := browseAl(band.albumID())
			db := app.ext()
			defer db.Close()
			now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
			_, err := db.Exec(
				`INSERT INTO annotations(user_id, target_kind, target_id, starred_at,
					rating, play_count, played_at) VALUES(?, 'al', ?, ?, 4, 3, ?)`,
				u.ID, band.albumID(), now, now,
			)
			Expect(err).NotTo(HaveOccurred())
			for _, typ := range []string{"starred", "highest", "frequent", "recent"} {
				got := ids(list(obj(u.call("getAlbumList2", url.Values{
					"type": {typ},
				}), "albumList2"), "album"))
				Expect(got).To(Equal([]string{album}), typ)
			}

			By("leaving another listener's marks out")
			other := app.subsonicUser("browse-other")
			for _, typ := range []string{"starred", "highest", "frequent", "recent"} {
				Expect(list(obj(other.call("getAlbumList2", url.Values{
					"type": {typ},
				}), "albumList2"), "album")).To(BeEmpty(), typ)
			}
		})
	})

	Describe("info and mixes", func() {
		It("answers artist and album info", func() {
			other := app.addArtist(artistSpec{
				Name: app.unique("Mudhoney"), Genre: band.Genre,
				Albums: []albumSpec{{
					Title: "Superfuzz Bigmuff", Year: 1988,
					Tracks: []trackSpec{{Title: "Touch Me", Position: 1}},
				}},
			})
			browseCore("UPDATE artists SET overview = 'Seattle band.' WHERE id = ?",
				band.Artist.ID)
			info := obj(u.call("getArtistInfo2", url.Values{
				"id": {browseTr(band.WithFile.ID)},
			}), "artistInfo2")
			Expect(info["biography"]).To(Equal("Seattle band."))
			Expect(info["musicBrainzId"]).NotTo(BeEmpty())
			Expect(
				ids(list(info, "similarArtist")),
			).To(Equal([]string{browseAr(other.ID)}))

			browseCore(
				"UPDATE artists SET overview = '', overview_fr = 'Groupe de Seattle.' WHERE id = ?",
				band.Artist.ID,
			)
			info = obj(u.call("getArtistInfo2", url.Values{
				"id": {browseAr(band.Artist.ID)},
			}), "artistInfo2")
			Expect(info["biography"]).To(Equal("Groupe de Seattle."))

			info = obj(u.call("getArtistInfo", url.Values{
				"id": {browseAr(band.Artist.ID)}, "count": {"0"},
			}), "artistInfo")
			Expect(list(info, "similarArtist")).To(BeEmpty())

			for _, endpoint := range []string{"getAlbumInfo", "getAlbumInfo2"} {
				album := obj(u.call(endpoint, url.Values{
					"id": {browseAl(band.albumID())},
				}), "albumInfo")
				Expect(album["musicBrainzId"]).NotTo(BeEmpty(), endpoint)
			}
			track := obj(u.call("getAlbumInfo", url.Values{
				"id": {browseTr(band.WithFile.ID)},
			}), "albumInfo")
			Expect(track["musicBrainzId"]).NotTo(BeEmpty())
			Expect(errorCode(u.callError("getAlbumInfo", url.Values{
				"id": {browseAr(band.Artist.ID)},
			}))).To(Equal(70))
		})

		It("mixes similar and random songs from playable tracks only", func() {
			for endpoint, key := range map[string]string{
				"getSimilarSongs": "similarSongs", "getSimilarSongs2": "similarSongs2",
			} {
				got := obj(u.call(endpoint, url.Values{
					"id": {browseAr(band.Artist.ID)}, "count": {"500"},
				}), key)
				Expect(ids(list(got, "song"))).To(ContainElement(
					browseTr(band.WithFile.ID)), endpoint)
				Expect(ids(list(got, "song"))).NotTo(ContainElement(
					browseTr(band.NoFile.ID)), endpoint)
			}
			random := obj(u.call("getRandomSongs", url.Values{
				"size": {"500"}, "genre": {band.Genre},
			}), "randomSongs")
			Expect(
				ids(list(random, "song")),
			).To(Equal([]string{browseTr(band.WithFile.ID)}))

			random = obj(u.call("getRandomSongs", url.Values{
				"fromYear": {"2100"}, "genre": {band.Genre},
			}), "randomSongs")
			Expect(list(random, "song")).To(BeEmpty())
			random = obj(u.call("getRandomSongs", url.Values{
				"fromYear": {"1991"}, "toYear": {"1991"}, "genre": {band.Genre},
			}), "randomSongs")
			Expect(list(random, "song")).To(HaveLen(1))
			random = obj(u.call("getRandomSongs", url.Values{
				"toYear": {"1990"}, "genre": {band.Genre},
			}), "randomSongs")
			Expect(list(random, "song")).To(BeEmpty())
		})

		It("has no top songs before anyone plays", func() {
			top := obj(
				u.call("getTopSongs", url.Values{"artist": {band.Name}}),
				"topSongs",
			)
			Expect(list(top, "song")).To(BeEmpty())
			Expect(errorCode(u.callError("getTopSongs", nil))).To(Equal(10))
			top = obj(u.call("getTopSongs", url.Values{"artist": {"Nobody At All"}}),
				"topSongs")
			Expect(list(top, "song")).To(BeEmpty())
		})

		It("ranks top songs by plays over every listener", func() {
			popular := app.addArtist(artistSpec{
				Name: app.unique("Charts"), Genre: "Pop",
				Albums: []albumSpec{{
					Title: app.unique("Hits"), Year: 2001,
					Tracks: []trackSpec{
						{Title: "Quiet", Position: 1},
						{Title: "Loud", Position: 2},
						{Title: "Unplayed", Position: 3},
						{Title: "Gone", Position: 4, NoFile: true},
					},
				}},
			})
			tracks := popular.Albums[0].Tracks
			other := app.subsonicUser("browse-top")
			db := app.ext()
			defer db.Close()
			for _, play := range []struct {
				user  *testUser
				track seededTrack
				count int
			}{
				{u, tracks[0], 1},
				{u, tracks[1], 2},
				{other, tracks[1], 5},
				{other, tracks[3], 9},
			} {
				_, err := db.Exec(
					`INSERT INTO annotations(user_id, target_kind, target_id, play_count)
					VALUES(?, 'tr', ?, ?)`,
					play.user.ID,
					play.track.ID,
					play.count,
				)
				Expect(err).NotTo(HaveOccurred())
			}
			top := obj(u.call("getTopSongs", url.Values{
				"artist": {strings.ToUpper(popular.Name)},
			}), "topSongs")
			Expect(ids(list(top, "song"))).To(Equal([]string{
				browseTr(tracks[1].ID), browseTr(tracks[0].ID),
			}))
			top = obj(u.call("getTopSongs", url.Values{
				"artist": {popular.Name}, "count": {"1"},
			}), "topSongs")
			Expect(
				ids(list(top, "song")),
			).To(Equal([]string{browseTr(tracks[1].ID)}))
		})
	})

	Describe("search", func() {
		It("searches artists, albums and songs", func() {
			res := obj(
				u.call("search3", url.Values{"query": {strings.ToLower(band.Name)}}),
				"searchResult3",
			)
			Expect(
				ids(list(res, "artist")),
			).To(Equal([]string{browseAr(band.Artist.ID)}))
			By("listing the artist's albums and songs under their name")
			Expect(
				ids(list(res, "album")),
			).To(Equal([]string{browseAl(band.albumID())}))
			Expect(
				ids(list(res, "song")),
			).To(Equal([]string{browseTr(band.WithFile.ID)}))

			res = obj(
				u.call(
					"search3",
					url.Values{"query": {"smells like"}, "songCount": {"500"}},
				),
				"searchResult3",
			)
			Expect(
				ids(list(res, "song")),
			).To(ContainElement(browseTr(band.WithFile.ID)))
			Expect(
				ids(list(res, "artist")),
			).NotTo(ContainElement(browseAr(band.Artist.ID)))
		})

		It("treats % and _ in a query as plain characters", func() {
			res := obj(
				u.call("search3", url.Values{"query": {"%"}}),
				"searchResult3",
			)
			Expect(list(res, "artist")).To(BeEmpty())
			Expect(list(res, "song")).To(BeEmpty())
			res = obj(u.call("search3", url.Values{"query": {"_"}}), "searchResult3")
			Expect(list(res, "artist")).To(BeEmpty())
		})

		It("searches with search2, search and a quoted empty query", func() {
			res := obj(
				u.call("search2", url.Values{"query": {band.Album}}),
				"searchResult2",
			)
			albums := list(res, "album")
			Expect(albums).To(HaveLen(1))
			Expect(albums[0]["isDir"]).To(BeTrue())

			res = obj(u.call("search", url.Values{
				"artist": {strings.ToLower(band.Name)}, "title": {"smells"},
			}), "searchResult")
			Expect(res["totalHits"]).To(BeEquivalentTo(1))
			Expect(
				ids(list(res, "match")),
			).To(Equal([]string{browseTr(band.WithFile.ID)}))
			res = obj(
				u.call("search", url.Values{"any": {band.Album}}),
				"searchResult",
			)
			Expect(res["totalHits"]).To(BeEquivalentTo(1))
			Expect(res["offset"]).To(BeEquivalentTo(0))

			res = obj(
				u.call("search3", url.Values{"query": {`""`}}),
				"searchResult3",
			)
			Expect(list(res, "artist")).NotTo(BeEmpty())
			Expect(list(res, "song")).NotTo(BeEmpty())
		})

		It("restricts the old search to tracks added since newerThan", func() {
			future := time.Now().Add(24 * time.Hour).UnixMilli()
			res := obj(u.call("search", url.Values{
				"artist": {band.Name}, "newerThan": {fmt.Sprint(future)},
			}), "searchResult")
			Expect(res["totalHits"]).To(BeEquivalentTo(0))
			past := time.Now().Add(-24 * time.Hour).UnixMilli()
			res = obj(u.call("search", url.Values{
				"artist": {band.Name}, "newerThan": {fmt.Sprint(past)},
			}), "searchResult")
			Expect(res["totalHits"]).To(BeEquivalentTo(1))
		})

		It("reads a file's added time in the zone the core wrote it", func() {
			added := time.Date(
				2020,
				3,
				4,
				12,
				0,
				0,
				0,
				time.FixedZone("CEST", 2*3600),
			)
			Expect(added.String()).To(Equal("2020-03-04 12:00:00 +0200 CEST"))
			browseCore("UPDATE media_files SET create_time = ? WHERE id = ?",
				added.String(), band.WithFile.FileID)

			song := obj(u.call("getSong",
				url.Values{"id": {browseTr(band.WithFile.ID)}}), "song")
			Expect(song["created"]).To(Equal("2020-03-04T10:00:00Z"))

			since := func(t time.Time) any {
				return obj(u.call("search", url.Values{
					"artist": {band.Name}, "newerThan": {fmt.Sprint(t.UnixMilli())},
				}), "searchResult")["totalHits"]
			}
			utc := added.UTC()
			Expect(since(utc.Add(-time.Minute))).To(BeEquivalentTo(1))
			Expect(since(utc.Add(time.Minute))).To(BeEquivalentTo(0))
		})

		It("pages a search past 100 hits", func() {
			prefix := app.unique("Bulk Band")
			now := coreTime(time.Now())
			for i := range 150 {
				res, err := app.core.Exec(
					`INSERT INTO artists(create_time, update_time, mbid, name, sort_name)
					VALUES(?, ?, ?, ?, ?)`,
					now,
					now,
					fmt.Sprintf("%s-%03d", prefix, i),
					fmt.Sprintf(
						"%s %03d",
						prefix,
						i,
					),
					fmt.Sprintf("%s %03d", prefix, i),
				)
				Expect(err).NotTo(HaveOccurred())
				artistID, err := res.LastInsertId()
				Expect(err).NotTo(HaveOccurred())
				res, err = app.core.Exec(
					`INSERT INTO albums(create_time, update_time, mbid, title, type,
						genres, status, artist_albums)
					VALUES(?, ?, ?, 'Demo', 'album', '[]', 'available', ?)`,
					now, now, fmt.Sprintf("%s-al-%03d", prefix, i), artistID,
				)
				Expect(err).NotTo(HaveOccurred())
				albumID, err := res.LastInsertId()
				Expect(err).NotTo(HaveOccurred())
				res, err = app.core.Exec(
					`INSERT INTO tracks(create_time, update_time, title, disc, position,
						duration, album_tracks, artist_credit)
					VALUES(?, ?, 'Demo', 1, 1, 60, ?, '')`,
					now,
					now,
					albumID,
				)
				Expect(err).NotTo(HaveOccurred())
				trackID, err := res.LastInsertId()
				Expect(err).NotTo(HaveOccurred())
				browseFile(uint32(trackID), fmt.Sprintf("/music/bulk/%03d.flac", i))
			}

			res := obj(u.call("search3", url.Values{
				"query": {prefix}, "artistCount": {"150"},
				"albumCount": {"0"}, "songCount": {"0"},
			}), "searchResult3")
			Expect(list(res, "artist")).To(HaveLen(150))
			res = obj(u.call("search3", url.Values{
				"query": {prefix}, "artistCount": {"100"}, "artistOffset": {"100"},
				"albumCount": {"0"}, "songCount": {"0"},
			}), "searchResult3")
			Expect(list(res, "artist")).To(HaveLen(50))
		})
	})

	It("answers error 0 for an endpoint it has not got", func() {
		Expect(errorCode(u.callError("getPodcasts", nil))).To(Equal(0))
	})

	It("refuses every browse endpoint to a caller who did not sign in", func() {
		for _, endpoint := range []string{
			"getArtists", "getIndexes", "getArtist", "getAlbum", "getSong",
			"getMusicDirectory", "getGenres", "getArtistInfo2", "getAlbumInfo",
			"getSimilarSongs", "getTopSongs", "getAlbumList2", "getRandomSongs",
			"getSongsByGenre", "search3",
		} {
			bad := *u
			bad.Password = "wrong"
			Expect(errorCode(bad.callError(endpoint, url.Values{
				"id": {browseAr(band.Artist.ID)},
			}))).To(Equal(40), endpoint)
		}
	})
})

func indexOf(haystack []string, needle string) int {
	return slices.Index(haystack, needle)
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

// browseAddAlbum adds another album to an artist the spec already made.
func browseAddAlbum(b browseBand, s albumSpec) seededAlbum {
	GinkgoHelper()
	return app.addAlbum(b.Artist, artistSpec{Name: b.Name, Genre: b.Genre}, s)
}
