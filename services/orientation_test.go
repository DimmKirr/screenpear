package services

import (
	"image"
	"image/color"
	"testing"
)

func TestApplyOrientation(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})     // top-left red
	src.Set(2, 1, color.RGBA{0, 0, 255, 255})     // bottom-right blue
	out := applyOrientation(src, 6).(*image.RGBA) // 90 CW: 2x3, red goes top-right
	if out.Bounds().Dx() != 2 || out.Bounds().Dy() != 3 {
		t.Fatalf("size %v", out.Bounds())
	}
	if out.RGBAAt(1, 0) != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("red not at top-right: %v", out.RGBAAt(1, 0))
	}
	if out.RGBAAt(0, 2) != (color.RGBA{0, 0, 255, 255}) {
		t.Fatalf("blue not at bottom-left: %v", out.RGBAAt(0, 2))
	}
	if exifOrientation([]byte{0xFF, 0xD8, 0xFF, 0xDA}) != 1 {
		t.Fatal("no EXIF must read as upright")
	}
}

func TestUnrotateBoxRoundTrip(t *testing.T) {
	// A 100x40 image. A box at (10,5)-(50,20) on the original.
	w, h := 100, 40
	orig := image.Rect(10, 5, 50, 20)
	// Turned CCW (angle 90): x' = y, y' = w-1-x  =>  box' = (5, 50)-(20, 90)
	if got := unrotateBox(image.Rect(5, 50, 20, 90), w, h, 90); got != orig {
		t.Fatalf("CCW: got %v want %v", got, orig)
	}
	// Turned CW (angle -90): x' = h-1-y, y' = x  =>  box' = (20, 10)-(35, 50)
	if got := unrotateBox(image.Rect(20, 10, 35, 50), w, h, -90); got != orig {
		t.Fatalf("CW: got %v want %v", got, orig)
	}
}
