package e2e

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	xdraw "golang.org/x/image/draw"

	"github.com/automationd/screenpear/services"
)

// TestFilelistCrop crops the file-list panel (Applications, Library, System,
// Users + dates) from macos-01-input-small.jpg, runs Analyze + SubstituteAll
// mask on the crop, and writes crop-original.png and crop-masked.png for
// visual inspection. All items share the same dark background, so GroupBoxes
// should put them in one group and consensus should give them identical font
// face and size.
func TestFilelistCrop(t *testing.T) {
	svc := newOCR(t)
	dir := resultDir(t)

	srcPath := testdata("input", "macos-01-input-small.jpg")
	src := loadImage(t, srcPath)

	cropRect := image.Rect(310, 165, 1050, 340)
	cropped := image.NewRGBA(image.Rect(0, 0, cropRect.Dx(), cropRect.Dy()))
	xdraw.Draw(cropped, cropped.Bounds(), src, cropRect.Min, xdraw.Src)

	writePNG(t, filepath.Join(dir, "crop-original.png"), cropped)

	cropFile := filepath.Join(dir, "crop-input.png")
	f, err := os.Create(cropFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, cropped); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	a, err := svc.Analyze(cropFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("crop: %dx%d, %d lines detected", a.Width, a.Height, len(a.Lines))
	for _, l := range a.Lines {
		t.Logf("  [%d] %q  box=(%d,%d)-(%d,%d) kind=%s",
			l.ID, l.Text, l.X0, l.Y0, l.X1, l.Y1, l.Kind)
	}
	writeJSON(t, filepath.Join(dir, "crop-analysis.json"), a)

	// Annotated original with green box boundaries.
	var boxes []image.Rectangle
	for _, l := range a.Lines {
		boxes = append(boxes, image.Rect(l.X0, l.Y0, l.X1, l.Y1))
	}
	writePNG(t, filepath.Join(dir, "crop-boxes.png"),
		annotate(cropped, boxes, color.RGBA{R: 0, G: 255, B: 0, A: 255}))

	if len(a.Lines) == 0 {
		t.Fatal("no text detected in crop")
	}

	maskedPath := filepath.Join(dir, "crop-masked.png")
	res, err := svc.Mask(services.MaskRequest{
		Path:          cropFile,
		Output:        maskedPath,
		Lines:         a.Lines,
		SubstituteAll: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("masked: %d replacements", len(res.Replaced))

	type lineReport struct {
		ID   int     `json:"id"`
		From string  `json:"from"`
		To   string  `json:"to"`
		Face string  `json:"face,omitempty"`
		Size float64 `json:"size,omitempty"`
	}
	var report []lineReport
	for _, r := range res.Replaced {
		lr := lineReport{ID: r.LineID, From: r.From, To: r.To}
		if r.Font != nil {
			lr.Face = r.Font.Face
			lr.Size = r.Font.SizePx
			t.Logf("  [%d] %q -> %q  face=%s size=%.1f", r.LineID, r.From, r.To, r.Font.Face, r.Font.SizePx)
		} else {
			t.Logf("  [%d] %q -> %q  (no font detected)", r.LineID, r.From, r.To)
		}
		report = append(report, lr)
	}
	writeJSON(t, filepath.Join(dir, "crop-mask.json"), report)

	// Check font consistency: all detected fonts should have the same face.
	faces := map[string]int{}
	for _, r := range report {
		if r.Face != "" {
			faces[r.Face]++
		}
	}
	if len(faces) > 1 {
		t.Logf("WARNING: multiple faces detected:")
		for face, count := range faces {
			t.Logf("  %s: %d lines", face, count)
		}
	} else if len(faces) == 1 {
		for face := range faces {
			t.Logf("all lines use consensus face: %s", face)
		}
	}

	// Check size consistency.
	var sizes []float64
	for _, r := range report {
		if r.Size > 0 {
			sizes = append(sizes, r.Size)
		}
	}
	if len(sizes) >= 2 {
		min, max := sizes[0], sizes[0]
		for _, s := range sizes[1:] {
			if s < min {
				min = s
			}
			if s > max {
				max = s
			}
		}
		spread := max - min
		t.Logf("size range: %.1f - %.1f (spread=%.1f)", min, max, spread)
		if spread > 2.0 {
			t.Errorf("font sizes vary too much: spread=%.1f (want <2.0)", spread)
		}
	}

	fmt.Fprintf(os.Stderr, "\n  crop-original: %s\n  crop-masked:   %s\n\n",
		filepath.Join(dir, "crop-original.png"), maskedPath)
}
