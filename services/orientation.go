package services

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"io"
)

// exifOrientation reads the EXIF Orientation tag (0x0112) from a JPEG's
// APP1 segment. Returns 1 (upright) when absent or unreadable. Go's
// image.Decode ignores EXIF, so a portrait phone photo decodes sideways;
// the browser and UIImage apply the tag, so boxes would not line up unless
// the Go side normalises too.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	r := bytes.NewReader(data[2:])
	for {
		var marker [2]byte
		if _, err := io.ReadFull(r, marker[:]); err != nil || marker[0] != 0xFF {
			return 1
		}
		if marker[1] == 0xDA || marker[1] == 0xD9 { // SOS / EOI: no APP1 before image data
			return 1
		}
		var size uint16
		if err := binary.Read(r, binary.BigEndian, &size); err != nil || size < 2 {
			return 1
		}
		seg := make([]byte, int(size)-2)
		if _, err := io.ReadFull(r, seg); err != nil {
			return 1
		}
		if marker[1] != 0xE1 || len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
			continue
		}
		tiff := seg[6:]
		var order binary.ByteOrder
		switch string(tiff[:2]) {
		case "II":
			order = binary.LittleEndian
		case "MM":
			order = binary.BigEndian
		default:
			return 1
		}
		ifd := int(order.Uint32(tiff[4:8]))
		if ifd+2 > len(tiff) {
			return 1
		}
		n := int(order.Uint16(tiff[ifd : ifd+2]))
		for i := 0; i < n; i++ {
			e := ifd + 2 + i*12
			if e+12 > len(tiff) {
				return 1
			}
			if order.Uint16(tiff[e:e+2]) == 0x0112 {
				v := int(order.Uint16(tiff[e+8 : e+10]))
				if v >= 1 && v <= 8 {
					return v
				}
				return 1
			}
		}
		return 1
	}
}

// applyOrientation returns img rotated/flipped so that it displays upright
// for the given EXIF orientation value.
func applyOrientation(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	var dst *image.RGBA
	if o >= 5 {
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2: // mirror horizontal
				nx, ny = w-1-x, y
			case 3: // rotate 180
				nx, ny = w-1-x, h-1-y
			case 4: // mirror vertical
				nx, ny = x, h-1-y
			case 5: // transpose
				nx, ny = y, x
			case 6: // rotate 90 CW
				nx, ny = h-1-y, x
			case 7: // transverse
				nx, ny = h-1-y, w-1-x
			case 8: // rotate 90 CCW
				nx, ny = y, w-1-x
			}
			dst.SetRGBA(nx, ny, src.RGBAAt(x, y))
		}
	}
	return dst
}
