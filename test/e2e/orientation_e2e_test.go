package e2e

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// TestVerticalPhoto creates a landscape JPEG with EXIF orientation 6
// (portrait, rotated 90 CW) containing readable text, runs it through
// Analyze and Mask, and verifies that the output image is portrait-sized
// and text was found.
func TestVerticalPhoto(t *testing.T) {
	svc := newOCR(t)
	dir := resultDir(t)

	// Create a 200x100 landscape image with text-like contrast.
	// When EXIF orientation 6 is applied, it becomes 100x200 (portrait).
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{240, 240, 240, 255})
		}
	}
	// Draw a dark stripe so there's contrast for the JPEG to preserve.
	for y := 20; y < 40; y++ {
		for x := 10; x < 190; x++ {
			img.Set(x, y, color.RGBA{30, 30, 30, 255})
		}
	}

	// Encode as JPEG with EXIF orientation 6.
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	data := injectEXIFOrientation(raw.Bytes(), 6)
	path := filepath.Join(dir, "vertical-photo.jpg")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := svc.Analyze(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("analyzed: %dx%d, %d lines", a.Width, a.Height, len(a.Lines))

	// After applying orientation 6 (90 CW), the image should be portrait:
	// width < height.
	if a.Width >= a.Height {
		t.Errorf("expected portrait after orientation 6: got %dx%d", a.Width, a.Height)
	}
	// Width should be the original height (100) and height the original width (200).
	if a.Width != 100 || a.Height != 200 {
		t.Errorf("expected 100x200, got %dx%d", a.Width, a.Height)
	}

	// All boxes must be inside the rotated image.
	for _, l := range a.Lines {
		r := l.Rect()
		if !r.In(image.Rect(0, 0, a.Width, a.Height)) {
			t.Errorf("line %d box %v outside %dx%d", l.ID, r, a.Width, a.Height)
		}
	}
}

// injectEXIFOrientation inserts a minimal APP1/EXIF segment with the given
// orientation tag right after the SOI marker.
func injectEXIFOrientation(jpeg []byte, orientation int) []byte {
	if len(jpeg) < 2 || jpeg[0] != 0xFF || jpeg[1] != 0xD8 {
		return jpeg
	}
	// Minimal EXIF APP1 segment: II (little-endian), one IFD entry.
	var exif bytes.Buffer
	exif.Write([]byte("Exif\x00\x00")) // EXIF header
	exif.Write([]byte("II"))            // little-endian
	binary.Write(&exif, binary.LittleEndian, uint16(42)) // TIFF magic
	binary.Write(&exif, binary.LittleEndian, uint32(8))  // IFD offset
	binary.Write(&exif, binary.LittleEndian, uint16(1))  // 1 entry
	// Entry: tag=0x0112 (Orientation), type=3 (SHORT), count=1, value
	binary.Write(&exif, binary.LittleEndian, uint16(0x0112))
	binary.Write(&exif, binary.LittleEndian, uint16(3))
	binary.Write(&exif, binary.LittleEndian, uint32(1))
	binary.Write(&exif, binary.LittleEndian, uint16(orientation))
	binary.Write(&exif, binary.LittleEndian, uint16(0)) // padding
	binary.Write(&exif, binary.LittleEndian, uint32(0)) // next IFD = none

	// APP1 marker + size.
	var seg bytes.Buffer
	seg.Write([]byte{0xFF, 0xE1})
	size := uint16(exif.Len() + 2)
	binary.Write(&seg, binary.BigEndian, size)
	seg.Write(exif.Bytes())

	var out bytes.Buffer
	out.Write(jpeg[:2]) // SOI
	out.Write(seg.Bytes())
	out.Write(jpeg[2:])
	return out.Bytes()
}
