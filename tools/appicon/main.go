// Command appicon renders Qryvanta's application icon (build/appicon.png) from the same
// geometry as the logo mark in the app header (frontend/src/components/brand/Logo.tsx), so the
// icon can be regenerated and reviewed like code instead of being a hand-edited binary.
//
//	go run ./tools/appicon -o build/appicon.png -preview <dir>
//
// After changing the icon, regenerate the platform icons:
//
//	wails3 task common:generate:icons
//
// This is a separate Go module so the image library is not a dependency of Qryvanta itself.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/vector"
)

// Colors match the dark theme tokens and frontend/public/favicon.svg.
var (
	tileTop    = color.RGBA{0x1d, 0x1f, 0x27, 0xff}
	tileBottom = color.RGBA{0x12, 0x13, 0x18, 0xff}
	tileEdge   = color.RGBA{0x2c, 0x2f, 0x3a, 0xff}
	barColor   = color.RGBA{0x9a, 0xa0, 0xad, 0xff}
	lensColor  = color.RGBA{0x3c, 0xc8, 0xbd, 0xff}
)

// Logo geometry in the 24-unit viewBox of the SVG logo mark.
type rect struct{ x, y, w, h float64 }

var bars = []rect{{2.5, 4, 13, 2.2}, {2.5, 9, 9, 2.2}, {2.5, 14, 6, 2.2}}

const (
	lensCX, lensCY, lensR, lensStroke = 15.5, 13.5, 5.0, 2.2
	handleX1, handleY1                = 19.0, 17.0
	handleX2, handleY2                = 22.0, 20.0
	handleStroke                      = 2.4
)

func main() {
	out := flag.String("o", "build/appicon.png", "output PNG path")
	size := flag.Int("size", 1024, "icon size in pixels")
	preview := flag.String("preview", "", "optional directory for downscaled previews (16-256 px)")
	dmgDir := flag.String("dmg", "", "optional directory for the macOS disk-image assets (dmg-background.png, dmg-file-icon.png)")
	flag.Parse()

	img := render(*size)
	if err := writePNG(*out, img); err != nil {
		fail(err)
	}
	fmt.Println("wrote", *out)

	if *dmgDir != "" {
		for name, im := range map[string]image.Image{
			"dmg-background.png": renderDMGBackground(540, 380),
			"dmg-file-icon.png":  img,
		} {
			p := filepath.Join(*dmgDir, name)
			if err := writePNG(p, im); err != nil {
				fail(err)
			}
			fmt.Println("wrote", p)
		}
	}

	if *preview != "" {
		for _, s := range []int{16, 24, 32, 48, 256} {
			small := image.NewRGBA(image.Rect(0, 0, s, s))
			xdraw.CatmullRom.Scale(small, small.Bounds(), img, img.Bounds(), draw.Src, nil)
			p := filepath.Join(*preview, fmt.Sprintf("appicon-%d.png", s))
			if err := writePNG(p, small); err != nil {
				fail(err)
			}
			fmt.Println("wrote", p)
		}
	}
}

func render(size int) *image.RGBA {
	n := float64(size)
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Tile: a rounded square with a small transparent margin, as desktop icons usually have.
	margin := n * 0.0625
	side := n - 2*margin
	radius := side * 0.22
	tile := newRaster(size)
	roundRect(tile, margin, margin, side, side, radius)
	fillGradient(img, tile, margin, side)

	// A 1-unit-wide inner edge gives the tile definition on dark taskbars.
	edge := newRaster(size)
	roundRect(edge, margin, margin, side, side, radius)
	inset := n * 0.006
	roundRectReverse(edge, margin+inset, margin+inset, side-2*inset, side-2*inset, radius-inset)
	fill(img, edge, tileEdge)

	drawGlyph(img, n/2, n/2, side*0.70)
	return img
}

// drawGlyph draws the logo mark centered at (cx, cy), fitting its bounding box (including
// stroke caps) into a square of the given size.
func drawGlyph(img *image.RGBA, centerX, centerY, glyph float64) {
	size := img.Bounds().Dx()
	minX, minY := 2.5, 4.0
	maxX := handleX2 + handleStroke/2
	maxY := handleY2 + handleStroke/2
	s := glyph / math.Max(maxX-minX, maxY-minY)
	ox := centerX - s*(minX+maxX)/2
	oy := centerY - s*(minY+maxY)/2
	px := func(x float64) float32 { return float32(ox + s*x) }
	py := func(y float64) float32 { return float32(oy + s*y) }

	barR := newRaster(size)
	for _, b := range bars {
		roundRect(barR, float64(px(b.x)), float64(py(b.y)), b.w*s, b.h*s, b.h*s/2)
	}
	fill(img, barR, barColor)

	lens := newRaster(size)
	cx, cy := float64(px(lensCX)), float64(py(lensCY))
	circle(lens, cx, cy, (lensR+lensStroke/2)*s, false)
	circle(lens, cx, cy, (lensR-lensStroke/2)*s, true) // opposite direction cuts the hole
	fill(img, lens, lensColor)

	// The handle is rasterized separately: sharing the ring's rasterizer would let its
	// winding cancel against the hole where the two overlap, leaving a notch.
	handle := newRaster(size)
	capsule(handle, float64(px(handleX1)), float64(py(handleY1)), float64(px(handleX2)), float64(py(handleY2)), handleStroke*s/2)
	fill(img, handle, lensColor)
}

// renderDMGBackground draws the macOS disk-image window background: a light gradient (Finder
// draws dark icon labels on it) with a small logo mark in the lower right corner.
func renderDMGBackground(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	from := color.RGBA{0xe6, 0xe8, 0xee, 0xff}
	to := color.RGBA{0xfb, 0xfb, 0xfd, 0xff}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := (float64(x)/float64(w) + float64(y)/float64(h)) / 2
			img.SetRGBA(x, y, color.RGBA{R: lerp(from.R, to.R, t), G: lerp(from.G, to.G, t), B: lerp(from.B, to.B, t), A: 0xff})
		}
	}
	// The glyph needs a square canvas; draw it on one and copy the corner region over.
	mark := image.NewRGBA(image.Rect(0, 0, w, w))
	draw.Draw(mark, image.Rect(0, 0, w, h), img, image.Point{}, draw.Src)
	drawGlyph(mark, float64(w)-70, float64(h)-62, 72)
	draw.Draw(img, img.Bounds(), mark, image.Point{}, draw.Src)
	return img
}

func newRaster(size int) *vector.Rasterizer { return vector.NewRasterizer(size, size) }

func fill(dst *image.RGBA, mask *vector.Rasterizer, c color.Color) {
	mask.Draw(dst, dst.Bounds(), image.NewUniform(c), image.Point{})
}

// fillGradient fills mask with a vertical gradient from tileTop to tileBottom.
func fillGradient(dst *image.RGBA, mask *vector.Rasterizer, top, height float64) {
	grad := image.NewRGBA(dst.Bounds())
	for y := grad.Rect.Min.Y; y < grad.Rect.Max.Y; y++ {
		t := math.Max(0, math.Min(1, (float64(y)-top)/height))
		c := color.RGBA{
			R: lerp(tileTop.R, tileBottom.R, t),
			G: lerp(tileTop.G, tileBottom.G, t),
			B: lerp(tileTop.B, tileBottom.B, t),
			A: 0xff,
		}
		for x := grad.Rect.Min.X; x < grad.Rect.Max.X; x++ {
			grad.SetRGBA(x, y, c)
		}
	}
	mask.Draw(dst, dst.Bounds(), grad, image.Point{})
}

func lerp(a, b uint8, t float64) uint8 {
	return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

// kappa approximates a quarter circle with a cubic Bézier curve.
const kappa = 0.5522847498

// roundRect adds a clockwise rounded rectangle.
func roundRect(r *vector.Rasterizer, x, y, w, h, rad float64) {
	k := rad * kappa
	f := func(v float64) float32 { return float32(v) }
	r.MoveTo(f(x+rad), f(y))
	r.LineTo(f(x+w-rad), f(y))
	r.CubeTo(f(x+w-rad+k), f(y), f(x+w), f(y+rad-k), f(x+w), f(y+rad))
	r.LineTo(f(x+w), f(y+h-rad))
	r.CubeTo(f(x+w), f(y+h-rad+k), f(x+w-rad+k), f(y+h), f(x+w-rad), f(y+h))
	r.LineTo(f(x+rad), f(y+h))
	r.CubeTo(f(x+rad-k), f(y+h), f(x), f(y+h-rad+k), f(x), f(y+h-rad))
	r.LineTo(f(x), f(y+rad))
	r.CubeTo(f(x), f(y+rad-k), f(x+rad-k), f(y), f(x+rad), f(y))
	r.ClosePath()
}

// roundRectReverse adds a counter-clockwise rounded rectangle (a hole when nested).
func roundRectReverse(r *vector.Rasterizer, x, y, w, h, rad float64) {
	k := rad * kappa
	f := func(v float64) float32 { return float32(v) }
	r.MoveTo(f(x+rad), f(y))
	r.CubeTo(f(x+rad-k), f(y), f(x), f(y+rad-k), f(x), f(y+rad))
	r.LineTo(f(x), f(y+h-rad))
	r.CubeTo(f(x), f(y+h-rad+k), f(x+rad-k), f(y+h), f(x+rad), f(y+h))
	r.LineTo(f(x+w-rad), f(y+h))
	r.CubeTo(f(x+w-rad+k), f(y+h), f(x+w), f(y+h-rad+k), f(x+w), f(y+h-rad))
	r.LineTo(f(x+w), f(y+rad))
	r.CubeTo(f(x+w), f(y+rad-k), f(x+w-rad+k), f(y), f(x+w-rad), f(y))
	r.ClosePath()
}

// circle adds a circle; reverse selects counter-clockwise winding.
func circle(r *vector.Rasterizer, cx, cy, rad float64, reverse bool) {
	k := rad * kappa
	f := func(v float64) float32 { return float32(v) }
	if !reverse {
		r.MoveTo(f(cx+rad), f(cy))
		r.CubeTo(f(cx+rad), f(cy+k), f(cx+k), f(cy+rad), f(cx), f(cy+rad))
		r.CubeTo(f(cx-k), f(cy+rad), f(cx-rad), f(cy+k), f(cx-rad), f(cy))
		r.CubeTo(f(cx-rad), f(cy-k), f(cx-k), f(cy-rad), f(cx), f(cy-rad))
		r.CubeTo(f(cx+k), f(cy-rad), f(cx+rad), f(cy-k), f(cx+rad), f(cy))
	} else {
		r.MoveTo(f(cx+rad), f(cy))
		r.CubeTo(f(cx+rad), f(cy-k), f(cx+k), f(cy-rad), f(cx), f(cy-rad))
		r.CubeTo(f(cx-k), f(cy-rad), f(cx-rad), f(cy-k), f(cx-rad), f(cy))
		r.CubeTo(f(cx-rad), f(cy+k), f(cx-k), f(cy+rad), f(cx), f(cy+rad))
		r.CubeTo(f(cx+k), f(cy+rad), f(cx+rad), f(cy+k), f(cx+rad), f(cy))
	}
	r.ClosePath()
}

// capsule adds a line segment with round caps (a stroked line with stroke-linecap: round).
func capsule(r *vector.Rasterizer, x1, y1, x2, y2, half float64) {
	angle := math.Atan2(y2-y1, x2-x1)
	const steps = 48
	f := func(v float64) float32 { return float32(v) }
	// Cap around the end point, from +90° to -90° relative to the direction.
	for i := 0; i <= steps; i++ {
		a := angle + math.Pi/2 - math.Pi*float64(i)/steps
		x, y := x2+half*math.Cos(a), y2+half*math.Sin(a)
		if i == 0 {
			r.MoveTo(f(x), f(y))
		} else {
			r.LineTo(f(x), f(y))
		}
	}
	// Cap around the start point, from -90° to -270°.
	for i := 0; i <= steps; i++ {
		a := angle - math.Pi/2 - math.Pi*float64(i)/steps
		r.LineTo(f(x1+half*math.Cos(a)), f(y1+half*math.Sin(a)))
	}
	r.ClosePath()
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "appicon:", err)
	os.Exit(1)
}
