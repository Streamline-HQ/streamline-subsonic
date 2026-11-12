// Package mosaic draws a playlist's cover: the covers of its first albums as
// a 2x2 grid. It has no host dependency, so it runs natively under Ginkgo.
package mosaic

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif" // a cached cover can be any type a source hands back
	"image/jpeg"
	_ "image/png"
	"math"
)

const (
	// Tile is the side of one cell in pixels.
	Tile = 300
	// MaxCovers is how many covers the grid takes.
	MaxCovers   = 4
	jpegQuality = 85
)

// layouts places n covers on the four cells: two go on the diagonals, three
// repeat the first, so no layout leaves a cell empty.
var layouts = map[int][4]int{
	2: {0, 1, 1, 0},
	3: {0, 1, 2, 0},
	4: {0, 1, 2, 3},
}

// Square decodes a cover, crops it to its centre square and scales it to one
// tile. It reports false for data that is not an image the standard library
// decodes (WebP, for one).
func Square(data []byte) (image.Image, bool) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	if side == 0 {
		return nil, false
	}
	at := b.Min.Add(image.Pt((b.Dx()-side)/2, (b.Dy()-side)/2))
	rgba := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(rgba, rgba.Bounds(), img, at, draw.Src)
	return downscale(rgba, Tile), true
}

// Grid draws covers (squares from Square) as a JPEG. It reports false when
// there are fewer than two, or more than MaxCovers: one cover is served as it
// is, not redrawn.
func Grid(covers []image.Image) ([]byte, bool, error) {
	layout, ok := layouts[len(covers)]
	if !ok {
		return nil, false, nil
	}
	grid := image.NewRGBA(image.Rect(0, 0, 2*Tile, 2*Tile))
	for cell, c := range layout {
		at := image.Pt(cell%2*Tile, cell/2*Tile)
		draw.Draw(
			grid,
			image.Rectangle{Min: at, Max: at.Add(image.Pt(Tile, Tile))},
			covers[c],
			image.Point{},
			draw.Src,
		)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(
		&out,
		grid,
		&jpeg.Options{Quality: jpegQuality},
	); err != nil {
		return nil, false, err
	}
	return out.Bytes(), true, nil
}

// downscale averages each destination pixel over the source pixels it covers:
// a box filter, which for a reduction is as good as a player shows a cover
// at. A source smaller than the tile is stretched by repetition.
func downscale(src *image.RGBA, side int) *image.RGBA {
	sw := src.Bounds().Dx()
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	for dy := range side {
		y0, y1 := dy*sw/side, max((dy+1)*sw/side, dy*sw/side+1)
		for dx := range side {
			x0, x1 := dx*sw/side, max((dx+1)*sw/side, dx*sw/side+1)
			var sum [4]uint64
			var n uint64
			for y := y0; y < y1; y++ {
				row := src.Pix[y*src.Stride:]
				for x := x0; x < x1; x++ {
					for k := range 4 {
						sum[k] += uint64(row[x*4+k])
					}
					n++
				}
			}
			o := dst.Pix[dy*dst.Stride+dx*4:]
			for k := range 4 {
				o[k] = byte(min(sum[k]/n, math.MaxUint8))
			}
		}
	}
	return dst
}
