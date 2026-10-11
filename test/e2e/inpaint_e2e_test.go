package e2e

import (
	"context"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/devcell-sh/go-textshot"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	mask2 "github.com/automationd/screenpear/internal/mask"
	"github.com/automationd/screenpear/services"
)

// TestInpaintFixtures erases the text of every fixture with MI-GAN and
// then masks it with substituted text on top of that background. For each
// fixture the result dir gets:
//
//	<name>-boxes.json        every box: coordinates, text, angle, erase method, roughness, ink coverage, fringe, overlaps
//	<name>-boxes.png         the original with each box outlined in its own colour and numbered
//	<name>-boxes.svg         the same as vector layers: one <rect> per box, id = line id, colour = erase method
//	<name>-labels.png        label map: every box painted with its own id (never merged white)
//	<name>-masks/<id>.png    the exact stroke mask erased for that box (white = replaced), cropped to the box
//	<name>-model-erased.png  MI-GAN per box (box + 3 px ring, one call per box), for comparison
//	<name>-erased.png        the masker's erase: stroke fills, ring fill, PatchMatch, model only where needed
//	<name>-output.png        substituted text drawn on that background
//	<name>-flat-output.png   the same with the flat fill everywhere, for comparison
func TestInpaintFixtures(t *testing.T) {
	svc := newOCR(t)
	dir := resultDir(t)
	ctx := context.Background()
	// The service keeps its engine private (every exported method becomes
	// a webview binding), so the erase step uses its own engine.
	eng, err := textshot.New(ctx, services.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	for _, path := range inputFixtures(t) {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			a, err := svc.Analyze(path)
			if err != nil {
				t.Fatal(err)
			}
			img := loadImage(t, path)
			var boxes []image.Rectangle
			for _, l := range a.Lines {
				boxes = append(boxes, l.Rect())
			}
			start := time.Now()
			erased, err := eng.InpaintBoxes(ctx, img, boxes, 3)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %d boxes erased in %s", name, len(boxes), time.Since(start).Round(time.Millisecond))
			writePNG(t, filepath.Join(dir, name+"-model-erased.png"), erased)
			erasedBest, infos := mask2.EraseDetailed(img, a.Lines, func(im image.Image, bx []image.Rectangle) (image.Image, error) {
				return eng.InpaintBoxes(ctx, im, bx, 3)
			})
			writePNG(t, filepath.Join(dir, name+"-erased.png"), erasedBest)
			writeJSON(t, filepath.Join(dir, name+"-boxes.json"), infos)
			writePNG(t, filepath.Join(dir, name+"-boxes.png"), annotateBoxes(img, infos))
			writePNG(t, filepath.Join(dir, name+"-labels.png"), labelMap(img.Bounds(), infos))
			if err := os.WriteFile(filepath.Join(dir, name+"-boxes.svg"), []byte(boxesSVG(img.Bounds(), infos)), 0o644); err != nil {
				t.Fatal(err)
			}
			maskDir := filepath.Join(dir, name+"-masks")
			if err := os.MkdirAll(maskDir, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, in := range infos {
				if in.Method == "ring" || in.Method == "tiny-ring" || in.Method == "patch" || in.Method == "model" {
					continue // whole box: the mask is the box itself
				}
				writePNG(t, filepath.Join(maskDir, fmt.Sprintf("%03d.png", in.LineID)), mask2.StrokeMask(img, in.Box, in.Fringe))
			}
			overlapping := 0
			for _, in := range infos {
				if len(in.Overlaps) > 0 {
					overlapping++
				}
			}
			t.Logf("%s: %d boxes, %d overlap another box", name, len(infos), overlapping)

			for _, v := range []struct {
				suffix string
				flat   bool
			}{{"-output.png", false}, {"-flat-output.png", true}} {
				res, err := svc.Mask(services.MaskRequest{
					Path: path, Output: filepath.Join(dir, name+v.suffix),
					Lines: a.Lines, SubstituteAll: true, FlatBackground: v.flat,
				})
				if err != nil {
					t.Fatal(err)
				}
				if len(res.Replaced) == 0 {
					t.Errorf("%s%s: nothing replaced", name, v.suffix)
				}
			}
		})
	}
}

// TestInpaintRingTuning measures, against ground truth, how much ring the
// model should see around a text box. Backgrounds: flat, vertical and
// horizontal gradients, and a real wallpaper crop. Text is drawn on the
// clean background, the box erased with each ring, and the RMS error
// inside the box reported. Also compares the model-free ring
// interpolation the masker falls back to.
func TestInpaintRingTuning(t *testing.T) {
	skipUnlessE2E(t)
	ctx := context.Background()
	eng, err := textshot.New(ctx, services.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	dir := resultDir(t)

	wall := loadImage(t, testdata("input", "macos-01-input.jpg"))
	type bg struct {
		name string
		at   func(x, y int) color.RGBA
	}
	bgs := []bg{
		{"flat", func(x, y int) color.RGBA { return color.RGBA{36, 38, 44, 255} }},
		{"vgrad", func(x, y int) color.RGBA { v := uint8(60 + y); return color.RGBA{v, v, v + 10, 255} }},
		{"hgrad", func(x, y int) color.RGBA { v := uint8(40 + x/2); return color.RGBA{v, v / 2, 200 - v/2, 255} }},
		{"wallpaper", func(x, y int) color.RGBA {
			r, g, b, _ := wall.At(wall.Bounds().Min.X+200+x, wall.Bounds().Min.Y+300+y).RGBA()
			return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255}
		}},
	}
	box := image.Rect(40, 30, 160, 52)
	rings := []int{1, 2, 3, 4, 6, 10}
	for _, b := range bgs {
		clean := image.NewRGBA(image.Rect(0, 0, 200, 80))
		for y := 0; y < 80; y++ {
			for x := 0; x < 200; x++ {
				clean.SetRGBA(x, y, b.at(x, y))
			}
		}
		dirty := image.NewRGBA(clean.Rect)
		copy(dirty.Pix, clean.Pix)
		drawFakeText(dirty, box)
		row := ""
		for _, ring := range rings {
			out, err := eng.InpaintBoxes(ctx, dirty, []image.Rectangle{box}, ring)
			if err != nil {
				t.Fatal(err)
			}
			row += fmt.Sprintf("  ring%-2d=%5.2f", ring, rmsIn(out, clean, box))
			if ring == 3 {
				writePNG(t, filepath.Join(dir, b.name+"-ring3.png"), out)
			}
		}
		fb := mask2.Erase(dirty, []mask2.Line{mask2.FromRect(0, "Settings", 1, box)}, nil)
		t.Logf("%-10s%s  | ring-interp=%5.2f", b.name, row, rmsIn(fb, clean, box))
		writePNG(t, filepath.Join(dir, b.name+"-clean.png"), clean)
		writePNG(t, filepath.Join(dir, b.name+"-dirty.png"), dirty)
	}
}

func drawFakeText(img *image.RGBA, box image.Rectangle) {
	// Dark strokes with a one pixel half-tone fringe at the box edge.
	draw.Draw(img, box.Inset(1), image.NewUniform(color.RGBA{10, 10, 10, 255}), image.Point{}, draw.Src)
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if x == box.Min.X || x == box.Max.X-1 || y == box.Min.Y || y == box.Max.Y-1 {
				c := img.RGBAAt(x, y)
				img.SetRGBA(x, y, color.RGBA{c.R / 2, c.G / 2, c.B / 2, 255})
			}
		}
	}
}

func rmsIn(a, b *image.RGBA, box image.Rectangle) float64 {
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

// annotateBoxes outlines every box in a distinct colour with its line id
// drawn at the top-left corner, so tight, overlapping or merged boxes are
// visible at a glance.
func annotateBoxes(src image.Image, infos []mask2.EraseInfo) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	palette := []color.RGBA{{255, 64, 64, 255}, {64, 200, 64, 255}, {64, 128, 255, 255}, {255, 180, 0, 255}, {200, 64, 255, 255}, {0, 200, 220, 255}}
	f, _ := opentype.Parse(gobold.TTF)
	face, _ := opentype.NewFace(f, &opentype.FaceOptions{Size: 11, DPI: 72})
	defer face.Close()
	for i, in := range infos {
		c := palette[i%len(palette)]
		b := in.Box
		for x := b.Min.X; x < b.Max.X; x++ {
			out.SetRGBA(x, b.Min.Y, c)
			out.SetRGBA(x, b.Max.Y-1, c)
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			out.SetRGBA(b.Min.X, y, c)
			out.SetRGBA(b.Max.X-1, y, c)
		}
		label := strconv.Itoa(in.LineID)
		lw := font.MeasureString(face, label).Ceil() + 4
		tag := image.Rect(b.Min.X, b.Min.Y-12, b.Min.X+lw, b.Min.Y)
		if tag.Min.Y < 0 {
			tag = tag.Add(image.Pt(0, 12))
		}
		draw.Draw(out, tag, image.NewUniform(c), image.Point{}, draw.Src)
		d := &font.Drawer{Dst: out, Src: image.Black, Face: face, Dot: fixed.P(tag.Min.X+2, tag.Max.Y-2)}
		d.DrawString(label)
	}
	return out
}

// labelMap paints each box with its own gray level (1..255 cycling) so
// adjacent boxes stay visibly separate, unlike a merged binary mask.
func labelMap(bounds image.Rectangle, infos []mask2.EraseInfo) *image.Gray {
	out := image.NewGray(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for i, in := range infos {
		v := uint8(40 + (i*47)%216)
		draw.Draw(out, in.Box.Sub(bounds.Min), image.NewUniform(color.Gray{Y: v}), image.Point{}, draw.Src)
	}
	return out
}

var methodColor = map[string]string{
	"stroke-interp": "#4caf50", "stroke-telea": "#2196f3", "ring": "#ff9800", "tiny-ring": "#ffc107", "patch": "#9c27b0", "model": "#f44336",
}

// boxesSVG is one <rect> per box in a <g> per erase method: a vector,
// layered record of exactly what was erased where. Opens in any browser or
// vector editor; ids are the line ids.
func boxesSVG(bounds image.Rectangle, infos []mask2.EraseInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\">\n", bounds.Dx(), bounds.Dy(), bounds.Dx(), bounds.Dy())
	byMethod := map[string][]mask2.EraseInfo{}
	var order []string
	for _, in := range infos {
		if _, ok := byMethod[in.Method]; !ok {
			order = append(order, in.Method)
		}
		byMethod[in.Method] = append(byMethod[in.Method], in)
	}
	for _, m := range order {
		fmt.Fprintf(&b, "  <g id=\"%s\" fill=\"%s\" fill-opacity=\"0.35\" stroke=\"%s\" stroke-width=\"1\">\n", m, methodColor[m], methodColor[m])
		for _, in := range byMethod[m] {
			r := in.Box.Sub(bounds.Min)
			fmt.Fprintf(&b, "    <rect id=\"line-%d\" x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\"><title>%d: %s</title></rect>\n",
				in.LineID, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), in.LineID, html.EscapeString(in.Text))
		}
		b.WriteString("  </g>\n")
	}
	b.WriteString("</svg>\n")
	return b.String()
}
