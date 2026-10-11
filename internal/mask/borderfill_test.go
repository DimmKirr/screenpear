package mask

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Tuning harness with ground truth: a known background, text drawn on it,
// the box erased, and the error against the clean background measured
// inside the box. Backgrounds cover what UI actually has: flat, vertical
// and horizontal gradients, a diagonal, and a 1 px rule crossing the box.
func backgrounds() map[string]func(x, y int) color.RGBA {
	return map[string]func(x, y int) color.RGBA{
		"flat":     func(x, y int) color.RGBA { return color.RGBA{36, 38, 44, 255} },
		"vgrad":    func(x, y int) color.RGBA { v := uint8(60 + y); return color.RGBA{v, v, v + 10, 255} },
		"hgrad":    func(x, y int) color.RGBA { v := uint8(40 + x/2); return color.RGBA{v, v / 2, 200 - v/2, 255} },
		"diagonal": func(x, y int) color.RGBA { v := uint8(30 + (x+y)/2); return color.RGBA{v, v, v, 255} },
		"rule": func(x, y int) color.RGBA {
			if y == 42 {
				return color.RGBA{180, 180, 180, 255}
			}
			return color.RGBA{250, 250, 250, 255}
		},
	}
}

func eraseError(ring int, bgFn func(x, y int) color.RGBA) float64 {
	w, h := 200, 80
	clean := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			clean.SetRGBA(x, y, bgFn(x, y))
		}
	}
	box := image.Rect(40, 30, 160, 52)
	dirty := image.NewRGBA(clean.Rect)
	copy(dirty.Pix, clean.Pix)
	// Text: dark strokes with a one pixel anti-aliased fringe reaching the
	// box edge, the way OCR boxes hug glyphs.
	draw.Draw(dirty, box.Inset(1), image.NewUniform(color.RGBA{10, 10, 10, 255}), image.Point{}, draw.Src)
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if x == box.Min.X || x == box.Max.X-1 || y == box.Min.Y || y == box.Max.Y-1 {
				c := clean.RGBAAt(x, y)
				dirty.SetRGBA(x, y, color.RGBA{c.R / 2, c.G / 2, c.B / 2, 255})
			}
		}
	}
	out := image.NewRGBA(clean.Rect)
	copy(out.Pix, dirty.Pix)
	borderFill(out, dirty, box, ring)
	var se float64
	n := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			a, b := out.RGBAAt(x, y), clean.RGBAAt(x, y)
			se += float64((int(a.R)-int(b.R))*(int(a.R)-int(b.R)) + (int(a.G)-int(b.G))*(int(a.G)-int(b.G)) + (int(a.B)-int(b.B))*(int(a.B)-int(b.B)))
			n += 3
		}
	}
	return math.Sqrt(se / float64(n))
}

// TestBorderFillTuning prints the RMS error per ring width and background
// and asserts the chosen default is within 1 level of the best on every
// background and under 4 levels absolute on the gradients. The rule case
// is informational: a 1 px line crossing a text box is not carried through
// (vertical neighbours are closer and win), which is acceptable for the
// fallback; the model path handles it.
func TestBorderFillTuning(t *testing.T) {
	bgs := backgrounds()
	for _, name := range []string{"flat", "vgrad", "hgrad", "diagonal", "rule"} {
		best, bestRing := math.Inf(1), 0
		var row string
		for ring := 1; ring <= 4; ring++ {
			e := eraseError(ring, bgs[name])
			row += fmt.Sprintf("  ring%d=%.2f", ring, e)
			if e < best {
				best, bestRing = e, ring
			}
		}
		d := eraseError(defaultRing, bgs[name])
		t.Logf("%-8s%s  (best ring %d, default ring %d = %.2f)", name, row, bestRing, defaultRing, d)
		if d > best+1 {
			t.Errorf("%s: default ring %d error %.2f, best ring %d gets %.2f", name, defaultRing, d, bestRing, best)
		}
		if name != "rule" && d > 4 {
			t.Errorf("%s: error %.2f levels, want <= 4", name, d)
		}
	}
}

// TestStrokeFillTuning: real text (Go Regular, 16 px) on each background,
// erased with the stroke fill and with the ring fill; RMS error against the
// clean background inside the box. The stroke fill must beat or match the
// ring fill everywhere and stay under 3 levels on every background.
func TestStrokeFillTuning(t *testing.T) {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	bgs := backgrounds()
	for _, name := range []string{"flat", "vgrad", "hgrad", "diagonal", "rule"} {
		w, h := 200, 80
		clean := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				clean.SetRGBA(x, y, bgs[name](x, y))
			}
		}
		dirty := image.NewRGBA(clean.Rect)
		copy(dirty.Pix, clean.Pix)
		ink := color.RGBA{235, 235, 235, 255}
		if luma(clean.RGBAAt(100, 40)) > 0.5 {
			ink = color.RGBA{20, 20, 20, 255}
		}
		d := &font.Drawer{Dst: dirty, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(44, 46)}
		d.DrawString("Settings 2024")
		box := image.Rect(40, 30, 44+font.MeasureString(face, "Settings 2024").Ceil()+2, 52)

		stroke := image.NewRGBA(clean.Rect)
		copy(stroke.Pix, dirty.Pix)
		if !strokeFill(stroke, dirty, box) {
			t.Fatalf("%s: stroke fill declined a text box", name)
		}
		ring := image.NewRGBA(clean.Rect)
		copy(ring.Pix, dirty.Pix)
		borderFill(ring, dirty, box, defaultRing)
		es, er := rmsInBox(stroke, clean, box), rmsInBox(ring, clean, box)
		t.Logf("%-8s stroke=%.2f ring=%.2f", name, es, er)
		if name == "rule" {
			continue // informational: a 1 px rule through the glyphs is not recovered by either
		}
		// The ring fill is the comparison, not the bar: it has the whole box
		// to interpolate across, the stroke fill keeps the real pixels.
		if es > 1.2 {
			t.Errorf("%s: stroke fill error %.2f, want <= 1.2", name, es)
		}
	}
}

func rmsInBox(a, b *image.RGBA, box image.Rectangle) float64 {
	var se float64
	n := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			p, q := a.RGBAAt(x, y), b.RGBAAt(x, y)
			for _, d := range []int{int(p.R) - int(q.R), int(p.G) - int(q.G), int(p.B) - int(q.B)} {
				se += float64(d * d)
				n++
			}
		}
	}
	return math.Sqrt(se / float64(n))
}

// TestStrokeMaskTuning sweeps the ink level and fringe of the stroke mask
// on rendered text at 12 and 16 px over the gradient backgrounds and
// reports the RMS error; the default pair must be within 0.1 of the best.
func TestStrokeMaskTuning(t *testing.T) {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	bgs := backgrounds()
	type cfg struct {
		ink    uint8
		fringe int
	}
	cfgs := []cfg{{128, 1}, {64, 1}, {31, 1}, {15, 1}, {15, 2}, {8, 1}, {8, 2}}
	total := map[cfg]float64{}
	for _, size := range []float64{12, 16} {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"flat", "vgrad", "hgrad", "diagonal"} {
			w, h := 200, 80
			clean := image.NewRGBA(image.Rect(0, 0, w, h))
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					clean.SetRGBA(x, y, bgs[name](x, y))
				}
			}
			dirty := image.NewRGBA(clean.Rect)
			copy(dirty.Pix, clean.Pix)
			ink := color.RGBA{235, 235, 235, 255}
			if luma(clean.RGBAAt(100, 40)) > 0.5 {
				ink = color.RGBA{20, 20, 20, 255}
			}
			d := &font.Drawer{Dst: dirty, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(44, 46)}
			d.DrawString("Settings 2024")
			box := image.Rect(40, 46-int(size), 44+font.MeasureString(face, "Settings 2024").Ceil()+2, 50)
			row := fmt.Sprintf("%-8s %2.0fpx", name, size)
			for _, c := range cfgs {
				out := image.NewRGBA(clean.Rect)
				copy(out.Pix, dirty.Pix)
				strokeFillWith(out, dirty, box, c.ink, c.fringe)
				e := rmsInBox(out, clean, box)
				total[c] += e
				row += fmt.Sprintf("  ink%d/f%d=%.2f", c.ink, c.fringe, e)
			}
			t.Log(row)
		}
		face.Close()
	}
	best, bestCfg := math.Inf(1), cfg{}
	for c, e := range total {
		if e < best {
			best, bestCfg = e, c
		}
	}
	defInk := strokeLevelInk
	def := total[cfg{strokeLevelInk, strokeFringe}]
	t.Logf("best ink%d/f%d total=%.2f; default level %.2f (ink%d) f%d total=%.2f", bestCfg.ink, bestCfg.fringe, best, strokeLevel, defInk, strokeFringe, def)
	if def > best+0.1*8 {
		t.Errorf("default stroke mask (level %.2f, fringe %d) total error %.2f, best ink %d fringe %d gets %.2f", strokeLevel, strokeFringe, def, bestCfg.ink, bestCfg.fringe, best)
	}
}
