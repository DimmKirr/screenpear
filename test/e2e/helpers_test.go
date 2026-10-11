// Package e2e runs the real OCR pipeline over test/testdata and, with the
// gui tag, the built binary. Gated by SCREENPEAR_E2E=1 because the first run
// downloads ONNX Runtime and the PP-OCR models (~40 MB) into the user cache.
//
//	SCREENPEAR_E2E=1 go test -v ./test/e2e/                 # OCR + mask over fixtures
//	SCREENPEAR_E2E=1 go test -v -tags gui -run TestGUI ./test/e2e/   # launches bin/screenpear
//
// Every test writes its artifacts to test/results/<timestamp>-<TestName>/
// so output can be inspected after the run.
package e2e

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	resultMu   sync.Mutex
	resultDirs = map[string]string{}
	runStamp   = time.Now().UTC().Format("20060102T150405Z")
)

func skipUnlessE2E(t testing.TB) {
	t.Helper()
	if os.Getenv("SCREENPEAR_E2E") != "1" {
		t.Skip("set SCREENPEAR_E2E=1 to run model-backed tests")
	}
}

// projectRoot is two levels above this file (test/e2e/).
func projectRoot() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(f)))
}

func testdata(parts ...string) string {
	return filepath.Join(append([]string{projectRoot(), "test", "testdata"}, parts...)...)
}

// resultDir is a durable per-test directory under test/results/.
func resultDir(t testing.TB) string {
	t.Helper()
	resultMu.Lock()
	defer resultMu.Unlock()
	if d, ok := resultDirs[t.Name()]; ok {
		return d
	}
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dir := filepath.Join(projectRoot(), "test", "results", runStamp+"-"+name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("result dir: %v", err)
	}
	resultDirs[t.Name()] = dir
	t.Logf("artifacts -> %s", dir)
	return dir
}

// inputFixtures lists test/testdata/input/*.{png,jpg}.
func inputFixtures(t testing.TB) []string {
	t.Helper()
	var out []string
	for _, pattern := range []string{"*.png", "*.jpg", "*.jpeg"} {
		m, _ := filepath.Glob(testdata("input", pattern))
		out = append(out, m...)
	}
	if len(out) == 0 {
		t.Fatal("no fixtures under test/testdata/input")
	}
	return out
}

func writeJSON(t testing.TB, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadImage(t testing.TB, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return img
}

// annotate draws each box on a copy of img, for eyeballing OCR coverage.
func annotate(img image.Image, boxes []image.Rectangle, c color.RGBA) *image.RGBA {
	dst := image.NewRGBA(img.Bounds())
	draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Src)
	for _, b := range boxes {
		for x := b.Min.X; x < b.Max.X; x++ {
			for _, y := range []int{b.Min.Y, b.Max.Y - 1} {
				if (image.Point{x, y}).In(dst.Bounds()) {
					dst.SetRGBA(x, y, c)
				}
			}
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for _, x := range []int{b.Min.X, b.Max.X - 1} {
				if (image.Point{x, y}).In(dst.Bounds()) {
					dst.SetRGBA(x, y, c)
				}
			}
		}
	}
	return dst
}

func writePNG(t testing.TB, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
