package e2e

import (
	"context"
	"image"
	"image/color"
	_ "image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/automationd/screenpear/internal/mask"
	"github.com/automationd/screenpear/services"
)

func newOCR(t testing.TB) *services.OCRService {
	t.Helper()
	skipUnlessE2E(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	svc := services.NewOCRService(services.DefaultOptions(), logger)
	t.Cleanup(func() { _ = svc.ServiceShutdown(context.Background()) })
	return svc
}

// TestAnalyzeFixtures runs text detection over every input fixture and keeps
// the lines (txt + json) and an annotated screenshot per fixture.
func TestAnalyzeFixtures(t *testing.T) {
	svc := newOCR(t)
	dir := resultDir(t)

	for _, path := range inputFixtures(t) {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			a, err := svc.Analyze(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %d lines in %s (%dx%d)", name, len(a.Lines), time.Since(start).Round(time.Millisecond), a.Width, a.Height)
			if len(a.Lines) == 0 {
				t.Errorf("%s: no text detected", name)
			}

			writeJSON(t, filepath.Join(dir, name+".json"), a)
			var lines, boxes = []string{}, []image.Rectangle{}
			for _, l := range a.Lines {
				lines = append(lines, l.Text)
				boxes = append(boxes, l.Rect())
				if l.Rect().Empty() || !l.Rect().In(image.Rect(0, 0, a.Width, a.Height)) {
					t.Errorf("%s: line %d box %v outside %dx%d", name, l.ID, l.Rect(), a.Width, a.Height)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			writePNG(t, filepath.Join(dir, name+"-boxes.png"), annotate(loadImage(t, path), boxes, color.RGBA{0, 200, 255, 255}))
		})
	}
}

// TestMaskNutcorp masks the three NutCorp fixtures with the default rules:
// the dictionary was written for exactly these screenshots, so every one
// must produce at least one replacement.
func TestMaskNutcorp(t *testing.T) {
	svc := newOCR(t)
	dir := resultDir(t)

	fixtures, _ := filepath.Glob(testdata("input", "nutcorp-*-input.png"))
	if len(fixtures) == 0 {
		t.Fatal("no nutcorp fixtures")
	}
	for _, path := range fixtures {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			a, err := svc.Analyze(path)
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, services.DefaultOutputPath(filepath.Base(path)))
			res, err := svc.Mask(services.MaskRequest{
				Path:   path,
				Output: out,
				Rules:  mask.DefaultRules(),
				Lines:  a.Lines,
			})
			if err != nil {
				t.Fatal(err)
			}
			writeJSON(t, filepath.Join(dir, name+"-mask.json"), res)
			t.Logf("%s: %d replacements -> %s", name, len(res.Replaced), filepath.Base(res.Output))
			if len(res.Replaced) == 0 {
				t.Errorf("%s: default rules matched nothing; lines: %q", name, mask.Line(a.Lines[0]).Text)
			}
			for _, r := range res.Replaced {
				if strings.Contains(r.To, "NutCorp") || strings.Contains(r.To, "nutcorp") {
					t.Errorf("%s: %q still contains the masked term", name, r.To)
				}
			}
			if _, err := os.Stat(res.Output); err != nil {
				t.Fatalf("output not written: %v", err)
			}
		})
	}
}
