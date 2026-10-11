package e2e

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/automationd/screenpear/services"
)

// TestMaskVertical rotates a fixture 90 degrees so every line is vertical,
// then analyzes and masks it. It records what the OCR engine makes of
// rotated text and that the masker repaints along the line's own axis;
// output in test/results/<ts>-TestMaskVertical/ for inspection.
func TestMaskVertical(t *testing.T) {
	svc := newOCR(t)
	dir := resultDir(t)
	src := loadImage(t, testdata("input", "nutcorp-01-input.png"))
	b := src.Bounds()
	rot := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			rot.Set(b.Max.Y-1-y, x-b.Min.X, src.At(x, y)) // 90 CW: text reads top to bottom
		}
	}
	in := filepath.Join(dir, "nutcorp-01-vertical-input.png")
	f, err := os.Create(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, rot); err != nil {
		t.Fatal(err)
	}
	f.Close()

	a, err := svc.Analyze(in)
	if err != nil {
		t.Fatal(err)
	}
	vertical := 0
	for _, l := range a.Lines {
		if l.Vertical() {
			vertical++
		}
	}
	t.Logf("rotated fixture: %d lines, %d detected as vertical", len(a.Lines), vertical)
	writeJSON(t, filepath.Join(dir, "analyze.json"), a)
	res, err := svc.Mask(services.MaskRequest{Path: in, Output: filepath.Join(dir, "nutcorp-01-vertical-output.png"), Lines: a.Lines, SubstituteAll: true})
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "nutcorp-01-vertical-mask.json"), res)
	t.Logf("%d replacements", len(res.Replaced))
	if len(a.Lines) == 0 {
		t.Skip("OCR engine returned no lines on rotated text; vertical repaint not exercised end to end")
	}
	// The original words must be gone: OCR on the output must read none of them.
	after, err := svc.Analyze(res.Output)
	if err != nil {
		t.Fatal(err)
	}
	was := map[string]bool{}
	for _, l := range a.Lines {
		was[strings.ToLower(l.Text)] = true
	}
	for _, l := range after.Lines {
		if was[strings.ToLower(l.Text)] {
			t.Errorf("original text still readable after masking: %q", l.Text)
		}
	}
	t.Logf("output reads %d lines, none of the originals", len(after.Lines))
}
