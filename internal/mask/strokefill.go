package mask

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	cv "github.com/malcolmston/opencv"
	"github.com/malcolmston/opencv/inpaint"
)

// Stroke erase: the crisp way to remove text.
//
// An OCR box is mostly background: the glyph strokes cover a quarter of it
// or less. Erasing the whole box throws that background away and asks a
// fill to reinvent it, which is where smudges come from. Instead the ink
// map of the crop (textshot.InkMap) gives the strokes themselves; those,
// grown by one pixel to take the anti-aliased fringe, are the hole, and
// every background pixel between and around the letters is kept as is.
// The hole is then one to three pixels wide everywhere, so Telea's fast
// marching fill closes it from the immediate neighbours: gradients,
// stripes and wallpaper continue through because the known pixels are
// right there on both sides of every stroke.
//
// Tuned by TestStrokeFillTuning (fringe 1 px, radius 3) and by the fixture
// outputs in test/results/.
const (
	strokeLevel    = 0.06 // fraction of the strongest ink pixel counted as glyph; measured against the interpolated background, so low is safe and takes JPEG undershoot too
	strokeLevelInk = 15   // strokeLevel on the 0..255 scale, for strokeFillWith
	strokeInk      = 64   // legacy ink-map level for coverage reporting (25%)
	strokeFringe   = 1    // dilation of the stroke mask, px
	strokeRadius   = 3    // Telea neighbourhood, px
	// A box whose ink covers more than this is not text on background
	// (a filled pill, a selected row): fall back to the ring fill.
	strokeMaxCoverage = 0.6
	// Below this box height a stroke is one or two pixels and its halo is
	// as wide as the stroke: a stroke mask leaves a ghost, the whole-box
	// ring fill does not.
	strokeMinHeight = 14
)

// inkAgainstBackground is the ink mask of box measured against the ring
// interpolation of its own background rather than one border colour:
// gradients, stripes and vignettes then measure as zero ink, so the
// threshold can be low enough to take the whole anti-aliased halo and the
// JPEG ringing with it. Level is the fraction of the strongest ink pixel.
func inkAgainstBackground(orig *image.RGBA, box image.Rectangle, level float64, fringe int) (*inpaint.Mask, int) {
	bg := image.NewRGBA(box)
	draw.Draw(bg, box, orig, box.Min, draw.Src)
	borderFill(bg, orig, box, defaultRing)
	w, h := box.Dx(), box.Dy()
	diff := make([]float64, w*h)
	peak := 0.0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := math.Abs(luma(orig.RGBAAt(box.Min.X+x, box.Min.Y+y)) - luma(bg.RGBAAt(box.Min.X+x, box.Min.Y+y)))
			diff[y*w+x] = d
			peak = math.Max(peak, d)
		}
	}
	m := inpaint.NewMask(h, w)
	n := 0
	if peak < 0.08 {
		return m, 0 // nothing darker or lighter than the background: no text here
	}
	for i, d := range diff {
		if d >= level*peak {
			m.Data[i] = true
			n++
		}
	}
	if fringe > 0 {
		m = m.Dilate(fringe)
	}
	return m, n
}

// StrokeMask is the erase mask the stroke fill uses for box: glyph pixels
// (ink map at the stroke ink level) grown by fringe. White = erased. For
// inspection and tests; the fill computes the same thing internally.
func StrokeMask(src image.Image, box image.Rectangle, fringe int) *image.Gray {
	b := src.Bounds()
	box = box.Intersect(b)
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	m, _ := inkAgainstBackground(rgba, box.Sub(b.Min), strokeLevel, fringe)
	w, h := box.Dx(), box.Dy()
	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if m.At(y, x) {
				out.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	return out
}

// FringeFor is the fringe the stroke fill applies to a box of this height
// on smooth surroundings (textured ones use strokeFringe).
func FringeFor(height int, textured bool) int {
	if textured {
		return strokeFringe
	}
	return max(strokeFringe+1, height/10)
}

// strokeFill erases the text in box of orig into dst. Returns false when
// the crop is not text-like, leaving dst untouched.
func strokeFill(dst, orig *image.RGBA, box image.Rectangle) bool {
	if box.Dy() < strokeMinHeight {
		return false
	}
	// Flat and gradient surroundings: the ring interpolation is noise-free
	// by construction, so stroke pixels take its value and a wider fringe
	// also swallows JPEG ringing around the glyphs. Textured surroundings:
	// Telea from the immediate neighbours, then a 3x3 smoothing of the
	// filled pixels only, so compression noise around the strokes does not
	// get rebuilt as mottling.
	if ringStdDev(orig, box) <= textureThreshold {
		// Fringe grows with text size: JPEG ringing and sub-pixel halos scale
		// with the stroke width (2 px at 14-23 px lines, 3 px at Retina sizes).
		return strokeFillWith(dst, orig, box, strokeLevelInk, max(strokeFringe+1, box.Dy()/10))
	}
	return strokeFillWith(dst, orig, box, strokeLevelInk, strokeFringe)
}

func strokeFillWith(dst, orig *image.RGBA, box image.Rectangle, inkLevel uint8, fringe int) bool {
	w, h := box.Dx(), box.Dy()
	m, n := inkAgainstBackground(orig, box, float64(inkLevel)/255, fringe)
	if n == 0 || float64(n)/float64(w*h) > strokeMaxCoverage {
		return false
	}

	if ringStdDev(orig, box) <= textureThreshold {
		// Smooth surroundings: continue the row's own background through
		// the strokes (per-row linear fit of the unmasked pixels). Nothing
		// outside the box is read, so an icon beside the label or the
		// neighbouring row stripe cannot tint the fill.
		interiorFill(dst, orig, box, func(x, y int) bool { return m.At(y-box.Min.Y, x-box.Min.X) })
		return true
	}

	// Work on a crop with a margin so strokes at the box edge have known
	// neighbours outside the box too.
	margin := strokeRadius + fringe
	crop := box.Inset(-margin).Intersect(orig.Bounds())
	src := cv.NewMat(crop.Dy(), crop.Dx(), 3)
	hole := inpaint.NewMask(crop.Dy(), crop.Dx())
	for y := crop.Min.Y; y < crop.Max.Y; y++ {
		for x := crop.Min.X; x < crop.Max.X; x++ {
			c := orig.RGBAAt(x, y)
			i := ((y-crop.Min.Y)*crop.Dx() + (x - crop.Min.X)) * 3
			src.Data[i], src.Data[i+1], src.Data[i+2] = c.R, c.G, c.B
			if (image.Point{x, y}).In(box) && m.At(y-box.Min.Y, x-box.Min.X) {
				hole.Data[(y-crop.Min.Y)*crop.Dx()+(x-crop.Min.X)] = true
			}
		}
	}
	out := inpaint.InpaintTelea(src, hole, strokeRadius)
	// Filled pixels get a 3x3 mean over the filled crop, which evens out the
	// neighbour noise Telea copies in; unmasked pixels are never written.
	at := func(x, y int) (int, int, int) {
		i := ((y-crop.Min.Y)*crop.Dx() + (x - crop.Min.X)) * 3
		return int(out.Data[i]), int(out.Data[i+1]), int(out.Data[i+2])
	}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if !m.At(y-box.Min.Y, x-box.Min.X) {
				continue
			}
			var r, g, b, n int
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if (image.Point{x + dx, y + dy}).In(crop) {
						cr, cg, cb := at(x+dx, y+dy)
						r, g, b, n = r+cr, g+cg, b+cb, n+1
					}
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255})
		}
	}
	return true
}

// patchFill erases the whole box from a neighbourhood of real pixels with
// PatchMatch: for textured backgrounds where a stroke-level fill is not
// available (coverage too high) and a model would invent content.
func patchFill(dst, orig *image.RGBA, box image.Rectangle) {
	margin := int(math.Max(float64(box.Dy())*2, 16))
	crop := box.Inset(-margin).Intersect(orig.Bounds())
	src := cv.NewMat(crop.Dy(), crop.Dx(), 3)
	hole := inpaint.NewMask(crop.Dy(), crop.Dx())
	for y := crop.Min.Y; y < crop.Max.Y; y++ {
		for x := crop.Min.X; x < crop.Max.X; x++ {
			c := orig.RGBAAt(x, y)
			i := ((y-crop.Min.Y)*crop.Dx() + (x - crop.Min.X)) * 3
			src.Data[i], src.Data[i+1], src.Data[i+2] = c.R, c.G, c.B
			if (image.Point{x, y}).In(box) {
				hole.Data[(y-crop.Min.Y)*crop.Dx()+(x-crop.Min.X)] = true
			}
		}
	}
	out := inpaint.InpaintPatchMatch(src, hole, inpaint.DefaultPatchMatchOptions())
	patch := image.NewRGBA(box)
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			i := ((y-crop.Min.Y)*crop.Dx() + (x - crop.Min.X)) * 3
			patch.SetRGBA(x, y, color.RGBA{out.Data[i], out.Data[i+1], out.Data[i+2], 255})
		}
	}
	draw.Draw(dst, box, patch, box.Min, draw.Src)
}
