package mosaic

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func solid(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

func encodePNG(img image.Image) []byte {
	g.GinkgoHelper()
	var b bytes.Buffer
	Expect(png.Encode(&b, img)).To(Succeed())
	return b.Bytes()
}

func near(got color.Color, want color.RGBA) {
	g.GinkgoHelper()
	r, gg, b, _ := got.RGBA()
	Expect(int(r >> 8)).To(BeNumerically("~", int(want.R), 12))
	Expect(int(gg >> 8)).To(BeNumerically("~", int(want.G), 12))
	Expect(int(b >> 8)).To(BeNumerically("~", int(want.B), 12))
}

var (
	red   = color.RGBA{R: 255, A: 255}
	green = color.RGBA{G: 255, A: 255}
	blue  = color.RGBA{B: 255, A: 255}
	white = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

var _ = g.Describe("mosaic", g.Label("unit"), func() {
	g.Describe("Square", func() {
		g.It("crops to the centre square and scales to one tile", func() {
			wide := image.NewRGBA(image.Rect(0, 0, 600, 200))
			for y := range 200 {
				for x := range 600 {
					switch {
					case x < 200:
						wide.Set(x, y, red)
					case x < 400:
						wide.Set(x, y, green)
					default:
						wide.Set(x, y, blue)
					}
				}
			}
			sq, ok := Square(encodePNG(wide))
			Expect(ok).To(BeTrue())
			Expect(sq.Bounds().Dx()).To(Equal(Tile))
			Expect(sq.Bounds().Dy()).To(Equal(Tile))
			near(sq.At(10, 10), green)
			near(sq.At(Tile-10, Tile-10), green)
		})

		g.It("scales a small cover up to a tile", func() {
			sq, ok := Square(encodePNG(solid(16, 16, red)))
			Expect(ok).To(BeTrue())
			Expect(sq.Bounds().Dx()).To(Equal(Tile))
			near(sq.At(150, 150), red)
		})

		g.It("refuses data that is not a decodable image", func() {
			_, ok := Square([]byte("RIFF....WEBPVP8 "))
			Expect(ok).To(BeFalse())
			_, ok = Square(nil)
			Expect(ok).To(BeFalse())
		})
	})

	g.Describe("Grid", func() {
		tile := func(c color.RGBA) image.Image { return solid(Tile, Tile, c) }
		corners := func(data []byte) [4]color.Color {
			g.GinkgoHelper()
			img, err := jpeg.Decode(bytes.NewReader(data))
			Expect(err).NotTo(HaveOccurred())
			Expect(img.Bounds().Dx()).To(Equal(2 * Tile))
			Expect(img.Bounds().Dy()).To(Equal(2 * Tile))
			return [4]color.Color{
				img.At(Tile/2, Tile/2), img.At(Tile+Tile/2, Tile/2),
				img.At(Tile/2, Tile+Tile/2), img.At(Tile+Tile/2, Tile+Tile/2),
			}
		}

		g.It("puts four covers on the four cells", func() {
			out, ok, err := Grid(
				[]image.Image{tile(red), tile(green), tile(blue), tile(white)},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			got := corners(out)
			near(got[0], red)
			near(got[1], green)
			near(got[2], blue)
			near(got[3], white)
		})

		g.It("puts two covers on the diagonals", func() {
			out, ok, err := Grid([]image.Image{tile(red), tile(green)})
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			got := corners(out)
			near(got[0], red)
			near(got[1], green)
			near(got[2], green)
			near(got[3], red)
		})

		g.It("repeats the first of three covers", func() {
			out, ok, err := Grid([]image.Image{tile(red), tile(green), tile(blue)})
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			got := corners(out)
			near(got[0], red)
			near(got[1], green)
			near(got[2], blue)
			near(got[3], red)
		})

		g.It("leaves one cover, none and too many to the caller", func() {
			for _, n := range []int{0, 1, 5} {
				covers := make([]image.Image, n)
				for i := range covers {
					covers[i] = tile(red)
				}
				_, ok, err := Grid(covers)
				Expect(err).NotTo(HaveOccurred())
				Expect(ok).To(BeFalse(), "%d covers", n)
			}
		})
	})
})
