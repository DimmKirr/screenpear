package e2e

import (
	"fmt"
	"image"
	"math"
	"path/filepath"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"

	mask2 "github.com/automationd/screenpear/internal/mask"
	"github.com/automationd/screenpear/services"
)

// TestEraseFinderSmall is the focused erase test on the small Finder
// screenshot (flat dark rows, light text, JPEG). It writes, into
// test/results/<ts>-TestEraseFinderSmall/:
//
//	erased.png          the masker's erase (stroke fills)
//	rowfill.png         every box filled by horizontal-only continuation of the row beyond the text
//	crops/*.png         the file list zone of the original and both erases, 3x, side by side comparable
//	boxes.json          per-box report
//
// and logs, per box, the mean luminance of the background just left and
// right of the text versus inside the box after each erase, and the
// residual roughness inside the box: that is the number that says whether
// an erase "continues the same gradient as next to the text".
func TestEraseFinderSmall(t *testing.T) {
	svc := newOCR(t)
	dir := resultDir(t)
	path := testdata("input", "macos-01-input-small.jpg")
	a, err := svc.Analyze(path)
	if err != nil {
		t.Fatal(err)
	}
	src := loadImage(t, path)
	orig := image.NewRGBA(src.Bounds())
	xdraw.Draw(orig, orig.Bounds(), src, src.Bounds().Min, xdraw.Src)

	erased, infos := mask2.EraseDetailed(src, a.Lines, nil)
	writePNG(t, filepath.Join(dir, "erased.png"), erased)
	writeJSON(t, filepath.Join(dir, "boxes.json"), infos)

	// The full mask (substituted text in the detected fonts), with its
	// per-box font report.
	res, err := svc.Mask(services.MaskRequest{Path: path, Output: filepath.Join(dir, "masked.png"), Lines: a.Lines, SubstituteAll: true})
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "mask.json"), res)

	rowfill := image.NewRGBA(orig.Rect)
	copy(rowfill.Pix, orig.Pix)
	for _, in := range infos {
		mask2.RowFill(rowfill, orig, in.Box, 3)
	}
	writePNG(t, filepath.Join(dir, "rowfill.png"), rowfill)

	// Per box: brightness of the background beside the text vs inside the
	// box after the erase, and grain (mean neighbour step) of the filled
	// pixels vs the untouched background pixels of the same rows. A fill
	// that matches the style has grainIn close to grainOut; a smooth fill
	// in a grainy row reads as a smudge even when the brightness is right.
	t.Logf("%-24s %-14s %6s %6s | %6s %6s | %7s %7s %6s", "text", "method", "left", "right", "erased", "diff", "grainIn", "grainOut", "ratio")
	var worstDiff, worstRatio float64
	for _, in := range infos {
		if in.Box.Dy() < 14 {
			continue
		}
		l := meanLuma(orig, image.Rect(in.Box.Min.X-10, in.Box.Min.Y, in.Box.Min.X-2, in.Box.Max.Y))
		r := meanLuma(orig, image.Rect(in.Box.Max.X+2, in.Box.Min.Y, in.Box.Max.X+10, in.Box.Max.Y))
		fileRow := in.Box.Min.X > 290 && in.Box.Min.Y >= 170 && in.Box.Max.Y <= 340
		if fileRow {
			// The empty Size column at the right end of the same rows is the
			// untouched reference for brightness and grain.
			r = meanLuma(orig, image.Rect(1100, in.Box.Min.Y, 1230, in.Box.Max.Y))
		}
		m := mask2.StrokeMask(src, in.Box, in.Fringe)
		filled := func(x, y int) bool { return m.GrayAt(x-in.Box.Min.X, y-in.Box.Min.Y).Y >= 128 }
		gIn := grain(erased, in.Box, filled)
		// Reference grain: untouched background 3+ px from any filled pixel.
		far := func(x, y int) bool {
			for d := -3; d <= 3; d++ {
				if (image.Point{x + d, y}).In(in.Box) && filled(x+d, y) {
					return false
				}
			}
			return (image.Point{x, y}).In(in.Box)
		}
		gOut := grain(erased, in.Box, far)
		if fileRow {
			gOut = grain(orig, image.Rect(1100, in.Box.Min.Y, 1230, in.Box.Max.Y), func(x, y int) bool { return true })
		}
		ratio := 1.0
		switch {
		case gOut >= 0.001:
			ratio = gIn / gOut
		case gIn >= 0.0015:
			ratio = 0 // flat reference, grainy fill
		}
		diff := meanLuma(erased, in.Box) - r
		t.Logf("%-24s %-14s %6.3f %6.3f | %6.3f %+6.3f | %7.4f %7.4f %6.2f", trunc(in.Text, 24), in.Method, l, r, meanLuma(erased, in.Box), diff, gIn, gOut, ratio)
		if in.Method == "stroke-interp" && fileRow && len([]rune(strings.TrimSpace(in.Text))) > 1 { // the file list rows, words only
			worstDiff = math.Max(worstDiff, math.Abs(diff))
			worstRatio = math.Max(worstRatio, math.Abs(ratio-1))
		}
	}
	t.Logf("file list rows: worst brightness diff %.4f, worst grain ratio deviation %.2f", worstDiff, worstRatio)
	if worstDiff > 0.004 {
		t.Errorf("erased brightness differs from the row background by %.4f, want <= 0.004", worstDiff)
	}
	if worstRatio > 0.35 {
		t.Errorf("filled pixels' grain differs from the row's by %.0f%%, want <= 35%%", worstRatio*100)
	}

	// Crops of the file list zone, 3x, for eyeballing.
	zone := image.Rect(290, 170, 1236, 340).Intersect(orig.Rect)
	for name, img := range map[string]*image.RGBA{"original": orig, "erased": erased, "rowfill": rowfill} {
		crop := image.NewRGBA(image.Rect(0, 0, zone.Dx()*3, zone.Dy()*3))
		xdraw.NearestNeighbor.Scale(crop, crop.Bounds(), img, zone, xdraw.Src, nil)
		writePNG(t, filepath.Join(dir, "crop-"+name+".png"), crop)
	}
}

// grain is the mean luminance step between horizontal neighbours, over
// pairs where both pixels satisfy sel.
func grain(img *image.RGBA, r image.Rectangle, sel func(x, y int) bool) float64 {
	r = r.Intersect(img.Bounds())
	sum, n := 0.0, 0
	lum := func(x, y int) float64 {
		c := img.RGBAAt(x, y)
		return (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X-1; x++ {
			if sel(x, y) && sel(x+1, y) {
				sum += math.Abs(lum(x, y) - lum(x+1, y))
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func meanLuma(img *image.RGBA, r image.Rectangle) float64 {
	r = r.Intersect(img.Bounds())
	sum, n := 0.0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			sum += (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// roughness is the mean luminance step between neighbouring pixels.
func roughness(img *image.RGBA, r image.Rectangle) float64 {
	r = r.Intersect(img.Bounds())
	sum, n := 0.0, 0
	lum := func(x, y int) float64 {
		c := img.RGBAAt(x, y)
		return (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
	}
	for y := r.Min.Y; y < r.Max.Y-1; y++ {
		for x := r.Min.X; x < r.Max.X-1; x++ {
			sum += math.Abs(lum(x, y)-lum(x+1, y)) + math.Abs(lum(x, y)-lum(x, y+1))
			n += 2
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

var _ = fmt.Sprintf
var _ = strings.TrimSpace
