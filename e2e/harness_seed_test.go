package e2e

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// trackSpec describes one seeded track. A track with a Format has a real audio
// file under the music root; NoFile leaves it with none.
type trackSpec struct {
	Title    string
	Disc     int
	Position int
	// Format is the file's extension and codec: flac, mp3 or opus; flac when
	// empty.
	Format string
	NoFile bool
	// Seconds is the length of the generated audio; 2 when zero.
	Seconds int
	// Credit overrides the track's artist credit.
	Credit string
}

type albumSpec struct {
	Title  string
	Year   int
	Type   string
	Genres []string
	Tracks []trackSpec
	// Cover writes a poster for the album under the artwork folder.
	Cover bool
}

type artistSpec struct {
	Name   string
	Genre  string
	Albums []albumSpec
}

type seededTrack struct {
	ID       uint32
	Title    string
	Position int
	// Path is the audio file on disk, empty for a track with none; FileID is
	// its music_files row.
	Path   string
	FileID uint32
	Format string
}

type seededAlbum struct {
	ID     uint32
	Title  string
	Tracks []seededTrack
}

type seededArtist struct {
	ID     uint32
	Name   string
	Albums []seededAlbum
}

// library is what seedLibrary made.
type library struct {
	Artists []seededArtist
}

// track returns the n-th track of the n-th album of the n-th artist.
func (l library) track(artist, album, track int) seededTrack {
	return l.Artists[artist].Albums[album].Tracks[track]
}

// sharedLibrary is seedLibrary run once per instance, for specs that only read.
// A spec that changes the library (a file removed, a track edited) seeds one of
// its own with seedLibrary or addArtist.
func (i *instance) sharedLibrary() library {
	GinkgoHelper()
	if i.shared == nil {
		lib := i.seedLibrary()
		i.shared = &lib
	}
	return *i.shared
}

// seedLibrary fills the core database and the music root with a small
// library: two artists, three albums, seven tracks, one of them with no file.
//
//	The Seeders (Rock)
//	  First Light (2019, album, Rock/Indie, cover): 3 flac tracks
//	  Second Wind (2021, EP): "Gust" (flac), "Calm" (no file)
//	Ana Lyra (Jazz)
//	  Blue Hour (2015, album, Jazz, no cover): 2 mp3 tracks
func (i *instance) seedLibrary() library {
	GinkgoHelper()
	return library{Artists: []seededArtist{
		i.addArtist(artistSpec{
			Name: "The Seeders", Genre: "Rock",
			Albums: []albumSpec{
				{
					Title: "First Light", Year: 2019, Type: "album",
					Genres: []string{"Rock", "Indie"}, Cover: true,
					Tracks: []trackSpec{
						{Title: "Dawn", Position: 1},
						{Title: "Noon", Position: 2},
						{Title: "Dusk", Position: 3},
					},
				},
				{
					Title: "Second Wind", Year: 2021, Type: "ep",
					Genres: []string{"Rock"},
					Tracks: []trackSpec{
						{Title: "Gust", Position: 1},
						{Title: "Calm", Position: 2, NoFile: true},
					},
				},
			},
		}),
		i.addArtist(artistSpec{
			Name: "Ana Lyra", Genre: "Jazz",
			Albums: []albumSpec{{
				Title: "Blue Hour", Year: 2015, Type: "album",
				Genres: []string{"Jazz"},
				Tracks: []trackSpec{
					{Title: "Indigo", Position: 1, Format: "mp3"},
					{Title: "Slate", Position: 2, Format: "mp3"},
				},
			}},
		}),
	}}
}

// coreTime is a time the way the core database stores one.
func coreTime(t time.Time) string { return t.UTC().String() }

// addArtist inserts the artist, its albums, tracks and files, and generates
// the audio with ffmpeg and the covers.
func (i *instance) addArtist(s artistSpec) seededArtist {
	GinkgoHelper()
	now := coreTime(time.Now())
	mbid := i.unique("artist-mbid")
	res, err := i.core.Exec(
		`INSERT INTO artists(create_time, update_time, mbid, name, sort_name, genre)
		VALUES(?, ?, ?, ?, ?, ?)`,
		now, now, mbid, s.Name, s.Name, s.Genre,
	)
	Expect(err).NotTo(HaveOccurred())
	artistID, err := res.LastInsertId()
	Expect(err).NotTo(HaveOccurred())

	out := seededArtist{ID: uint32(artistID), Name: s.Name}
	for _, a := range s.Albums {
		out.Albums = append(out.Albums, i.addAlbum(out, s, a))
	}
	return out
}

func (i *instance) addAlbum(
	artist seededArtist,
	as artistSpec,
	s albumSpec,
) seededAlbum {
	GinkgoHelper()
	now := coreTime(time.Now())
	genres, err := json.Marshal(s.Genres)
	Expect(err).NotTo(HaveOccurred())
	albumType := s.Type
	if albumType == "" {
		albumType = "album"
	}
	released := time.Date(s.Year, time.June, 1, 0, 0, 0, 0, time.UTC)
	res, err := i.core.Exec(
		`INSERT INTO albums(create_time, update_time, mbid, title, type,
			release_date, original_date, genres, status, artist_albums)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, 'available', ?)`,
		now, now, i.unique("album-mbid"), s.Title, albumType,
		coreTime(released), fmt.Sprint(s.Year), string(genres), artist.ID,
	)
	Expect(err).NotTo(HaveOccurred())
	albumID, err := res.LastInsertId()
	Expect(err).NotTo(HaveOccurred())

	out := seededAlbum{ID: uint32(albumID), Title: s.Title}
	albumDir := filepath.Join(i.MusicDir, safeName(as.Name), safeName(s.Title))
	for _, t := range s.Tracks {
		out.Tracks = append(out.Tracks, i.addTrack(out.ID, as.Name, albumDir, t))
	}
	if s.Cover {
		i.writeCover(out.ID)
	}
	return out
}

func (i *instance) addTrack(
	albumID uint32,
	artistName, albumDir string,
	s trackSpec,
) seededTrack {
	GinkgoHelper()
	now := coreTime(time.Now())
	disc := max(s.Disc, 1)
	seconds := s.Seconds
	if seconds == 0 {
		seconds = 2
	}
	credit := s.Credit
	if credit == "" {
		credit = artistName
	}
	res, err := i.core.Exec(
		`INSERT INTO tracks(create_time, update_time, title, disc, position,
			duration, album_tracks, artist_credit)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		now, now, s.Title, disc, s.Position, seconds, albumID, credit,
	)
	Expect(err).NotTo(HaveOccurred())
	trackID, err := res.LastInsertId()
	Expect(err).NotTo(HaveOccurred())

	out := seededTrack{ID: uint32(trackID), Title: s.Title, Position: s.Position}
	if s.NoFile {
		return out
	}
	format := s.Format
	if format == "" {
		format = "flac"
	}
	path := filepath.Join(
		albumDir,
		fmt.Sprintf("%02d - %s.%s", s.Position, safeName(s.Title), format),
	)
	size := generateAudio(path, format, seconds)
	res, err = i.core.Exec(
		`INSERT INTO media_files(create_time, update_time, path, size, format,
			quality, duration_seconds, bitrate, audio_channels,
			track_media_files)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, 2, ?)`,
		now, now, path, size, format, qualityOf(format), seconds,
		size*8/int64(seconds)/1000, trackID,
	)
	Expect(err).NotTo(HaveOccurred())
	fileID, err := res.LastInsertId()
	Expect(err).NotTo(HaveOccurred())
	out.Path, out.FileID, out.Format = path, uint32(fileID), format
	return out
}

func qualityOf(format string) string {
	if format == "flac" {
		return "lossless"
	}
	return "high"
}

func safeName(s string) string {
	out := []rune(s)
	for n, r := range out {
		if r == '/' || r == 0 {
			out[n] = '_'
		}
	}
	return string(out)
}

// generateAudio writes a sine tone with the real ffmpeg and returns its size.
func generateAudio(path, format string, seconds int) int64 {
	GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	codec := map[string][]string{
		"flac": {"-c:a", "flac"},
		"mp3":  {"-c:a", "libmp3lame", "-b:a", "128k"},
		"opus": {"-c:a", "libopus", "-b:a", "96k"},
	}[format]
	Expect(codec).NotTo(BeEmpty(), "no codec for %q", format)
	args := append([]string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%d", seconds),
		"-ac", "2",
	}, codec...)
	args = append(args, path)
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), string(out))
	info, err := os.Stat(path)
	Expect(err).NotTo(HaveOccurred())
	return info.Size()
}

// writeCover puts a small JPEG where Streamline keeps an album's poster.
func (i *instance) writeCover(albumID uint32) {
	GinkgoHelper()
	dir := filepath.Join(i.ArtworkDir, "albums", itoa(albumID))
	Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := range 16 {
		for y := range 16 {
			img.Set(
				x,
				y,
				color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 255},
			)
		}
	}
	f, err := os.Create(filepath.Join(dir, "poster.jpg"))
	Expect(err).NotTo(HaveOccurred())
	defer f.Close()
	Expect(jpeg.Encode(f, img, nil)).To(Succeed())
}
