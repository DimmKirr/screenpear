package mask

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
)

// Border fill: the deterministic, model-free way to erase a text box.
//
// OCR boxes hug the glyphs, so the pixels just outside a box are the
// background the text sits on. Every pixel inside the box is interpolated
// from four samples on that ring (left, right, above, below, each a
// per-row or per-column median over a band of ring pixels), weighted by
// inverse distance. Flat panels stay flat, vertical and horizontal
// gradients continue unbroken, and a divider or underline that crosses
// the box is carried through because its row interpolates from its own
// left and right neighbours. Cost is O(pixels); no texture is invented.
//
// Ring is the sampled band just outside the box, in pixels. 1 px catches
// the anti-aliased fringe of the glyphs themselves (slightly tinted
// samples), wide bands blur neighbouring UI into the fill; 2 px with a
// median was best on the tuning test (see borderfill_test.go).
const defaultRing = 2

// borderFill paints box in dst from the ring outside it in src.
func borderFill(dst, src *image.RGBA, box image.Rectangle, ring int) {
	bounds := src.Bounds()
	if ring < 1 {
		ring = 1
	}
	hasL := box.Min.X-ring >= bounds.Min.X
	hasR := box.Max.X+ring <= bounds.Max.X
	hasT := box.Min.Y-ring >= bounds.Min.Y
	hasB := box.Max.Y+ring <= bounds.Max.Y

	// Per-row medians of the left and right bands, per-column of top and
	// bottom. Medians reject a stray dark pixel from a neighbouring glyph.
	left := make([]color.RGBA, box.Dy())
	right := make([]color.RGBA, box.Dy())
	for y := box.Min.Y; y < box.Max.Y; y++ {
		if hasL {
			left[y-box.Min.Y] = medianColor(src, image.Rect(box.Min.X-ring, y, box.Min.X, y+1))
		}
		if hasR {
			right[y-box.Min.Y] = medianColor(src, image.Rect(box.Max.X, y, box.Max.X+ring, y+1))
		}
	}
	top := make([]color.RGBA, box.Dx())
	bottom := make([]color.RGBA, box.Dx())
	for x := box.Min.X; x < box.Max.X; x++ {
		if hasT {
			top[x-box.Min.X] = medianColor(src, image.Rect(x, box.Min.Y-ring, x+1, box.Min.Y))
		}
		if hasB {
			bottom[x-box.Min.X] = medianColor(src, image.Rect(x, box.Max.Y, x+1, box.Max.Y+ring))
		}
	}

	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			var r, g, b, wsum float64
			add := func(c color.RGBA, d int) {
				w := 1 / float64(d)
				r += w * float64(c.R)
				g += w * float64(c.G)
				b += w * float64(c.B)
				wsum += w
			}
			if hasL {
				add(left[y-box.Min.Y], x-box.Min.X+1)
			}
			if hasR {
				add(right[y-box.Min.Y], box.Max.X-x)
			}
			if hasT {
				add(top[x-box.Min.X], y-box.Min.Y+1)
			}
			if hasB {
				add(bottom[x-box.Min.X], box.Max.Y-y)
			}
			if wsum == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r/wsum + 0.5), uint8(g/wsum + 0.5), uint8(b/wsum + 0.5), 255})
		}
	}
}

// medianColor is the per-channel median of the pixels in r.
func medianColor(img *image.RGBA, r image.Rectangle) color.RGBA {
	r = r.Intersect(img.Bounds())
	n := r.Dx() * r.Dy()
	if n == 0 {
		return color.RGBA{}
	}
	if n == 1 {
		return img.RGBAAt(r.Min.X, r.Min.Y)
	}
	rs, gs, bs := make([]int, 0, n), make([]int, 0, n), make([]int, 0, n)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			rs = append(rs, int(c.R))
			gs = append(gs, int(c.G))
			bs = append(bs, int(c.B))
		}
	}
	sort.Ints(rs)
	sort.Ints(gs)
	sort.Ints(bs)
	return color.RGBA{uint8(rs[n/2]), uint8(gs[n/2]), uint8(bs[n/2]), 255}
}

// RowFill fills box by horizontal continuation only: every row of the box
// is a linear blend between the median of `ring` pixels just left of the
// box and just right of it. This is "the same gradient as next to the
// text": exact for backgrounds that vary along x only (a row stripe, a
// horizontal gradient), and it ignores everything above and below.
func RowFill(dst, src *image.RGBA, box image.Rectangle, ring int) {
	bounds := src.Bounds()
	hasL := box.Min.X-ring >= bounds.Min.X
	hasR := box.Max.X+ring <= bounds.Max.X
	for y := box.Min.Y; y < box.Max.Y; y++ {
		var l, r color.RGBA
		if hasL {
			l = medianColor(src, image.Rect(box.Min.X-ring, y, box.Min.X, y+1))
		}
		if hasR {
			r = medianColor(src, image.Rect(box.Max.X, y, box.Max.X+ring, y+1))
		}
		if !hasL {
			l = r
		}
		if !hasR {
			r = l
		}
		for x := box.Min.X; x < box.Max.X; x++ {
			f := float64(x-box.Min.X+1) / float64(box.Dx()+1)
			dst.SetRGBA(x, y, color.RGBA{
				uint8(float64(l.R)*(1-f) + float64(r.R)*f + 0.5),
				uint8(float64(l.G)*(1-f) + float64(r.G)*f + 0.5),
				uint8(float64(l.B)*(1-f) + float64(r.B)*f + 0.5), 255})
		}
	}
}

// interiorFill paints the masked pixels of box from the box's own
// unmasked pixels: for every row a straight line (per channel) is fitted
// through the background pixels left and right of the strokes, and
// masked pixels take the line's value at their x. This is literally "the
// same gradient as next to the text": exact for flat rows, horizontal
// and vertical gradients and diagonals, and immune to whatever sits
// outside the box (icons, dividers, the neighbouring row stripe), because
// nothing outside the box is read. Rows with too few background pixels
// borrow the fit of the nearest row that has enough.
func interiorFill(dst, orig *image.RGBA, box image.Rectangle, masked func(x, y int) bool) {
	type fit struct {
		ok   bool
		a, b [3]float64 // value = a + b*x per channel
		// residuals of the background pixels that fit the line: the row's
		// own grain (JPEG noise, dithering), reused in the filled pixels so
		// the fill has the same texture as its surroundings instead of
		// being the one perfectly smooth patch in a grainy row.
		res [][3]float64
	}
	fits := make([]fit, box.Dy())
	for y := box.Min.Y; y < box.Max.Y; y++ {
		// Two passes: fit every unmasked pixel, then refit without the
		// pixels that sit far off that line. Background pixels, gradient or
		// not, lie on the line; an icon the OCR box swallowed, a cursor or
		// a divider do not and are dropped. A median band would clip the
		// ends of a steep gradient; residuals do not.
		type sample struct {
			x   float64
			v   [3]float64
			lum float64
		}
		var pts []sample
		for x := box.Min.X; x < box.Max.X; x++ {
			if masked(x, y) {
				continue
			}
			c := orig.RGBAAt(x, y)
			pts = append(pts, sample{float64(x - box.Min.X), [3]float64{float64(c.R), float64(c.G), float64(c.B)}, luma(c)})
		}
		width := float64(box.Dx())
		solve := func(keep func(sample) bool) (fit, int) {
			var n, sx, sxx float64
			var sy, sxy [3]float64
			minX, maxX := math.Inf(1), math.Inf(-1)
			for _, p := range pts {
				if !keep(p) {
					continue
				}
				n++
				sx += p.x
				sxx += p.x * p.x
				minX, maxX = math.Min(minX, p.x), math.Max(maxX, p.x)
				for ch := 0; ch < 3; ch++ {
					sy[ch] += p.v[ch]
					sxy[ch] += p.x * p.v[ch]
				}
			}
			f := fit{}
			// A line needs support across the box: a few pixels huddled at
			// one edge (the fringe of an icon the box swallowed) would be
			// extrapolated into a colour drift over the whole row.
			if n >= 4 && maxX-minX >= 0.4*width {
				f.ok = true
				den := n*sxx - sx*sx
				for ch := 0; ch < 3; ch++ {
					if den > 1e-9 {
						f.b[ch] = (n*sxy[ch] - sx*sy[ch]) / den
					}
					f.a[ch] = (sy[ch] - f.b[ch]*sx) / n
				}
			}
			return f, int(n)
		}
		f, _ := solve(func(sample) bool { return true })
		if f.ok {
			const residualBand = 0.08 // luminance
			const channelBand = 20.0  // levels, per channel: a coloured icon pixel can match the panel's brightness
			lumAt := func(p sample) float64 {
				r, g, b := f.a[0]+f.b[0]*p.x, f.a[1]+f.b[1]*p.x, f.a[2]+f.b[2]*p.x
				return (0.299*r + 0.587*g + 0.114*b) / 255
			}
			keep := func(p sample) bool {
				if math.Abs(p.lum-lumAt(p)) > residualBand {
					return false
				}
				for ch := 0; ch < 3; ch++ {
					if math.Abs(p.v[ch]-(f.a[ch]+f.b[ch]*p.x)) > channelBand {
						return false
					}
				}
				return true
			}
			if f2, n2 := solve(keep); f2.ok && n2 >= 4 {
				f = f2
			}
			// Grain pool: residuals of pixels that are background beyond
			// doubt, at least 3 px from any masked pixel in this row (the
			// anti-aliased fringe and JPEG ringing live closer than that),
			// with the largest residuals dropped (3x the median absolute).
			var cand []sample
			var absRes []float64
			for _, p := range pts {
				if !keep(p) {
					continue
				}
				x := box.Min.X + int(p.x)
				near := false
				for d := -3; d <= 3 && !near; d++ {
					if x+d >= box.Min.X && x+d < box.Max.X && masked(x+d, y) {
						near = true
					}
				}
				if near {
					continue
				}
				cand = append(cand, p)
				absRes = append(absRes, math.Abs(p.lum-lumAt(p)))
			}
			sorted := append([]float64(nil), absRes...)
			sort.Float64s(sorted)
			limit := math.Inf(1)
			if len(sorted) > 0 {
				limit = 3*sorted[len(sorted)/2] + 1e-6
			}
			for i, p := range cand {
				if absRes[i] <= limit {
					f.res = append(f.res, [3]float64{p.v[0] - (f.a[0] + f.b[0]*p.x), p.v[1] - (f.a[1] + f.b[1]*p.x), p.v[2] - (f.a[2] + f.b[2]*p.x)})
				}
			}
		}
		fits[y-box.Min.Y] = f
	}
	nearest := func(i int) (fit, bool) {
		for d := 0; d < len(fits); d++ {
			if i-d >= 0 && fits[i-d].ok {
				return fits[i-d], true
			}
			if i+d < len(fits) && fits[i+d].ok {
				return fits[i+d], true
			}
		}
		return fit{}, false
	}
	// Residuals are reused in x order, tiled: consecutive filled pixels get
	// consecutive background residuals, so the grain keeps its spatial
	// correlation (random draws would double the neighbour step).
	// Is this row background flat or grainy away from the text? JPEG puts
	// ringing around glyphs but leaves flat areas flat, so the pixels 3+ px
	// from any stroke tell the row's real style. Flat: every background
	// pixel in the box is set to the fitted line too, which removes the
	// ringing the text left behind and makes the box indistinguishable from
	// the untouched part of the row. Grainy: background pixels are kept and
	// the strokes get the row's grain tiled in.
	flatten := math.Min(farGrain(orig, box, masked), besideGrain(orig, box)) < 0.0015

	pool := func(i int) [][3]float64 {
		for d := 0; d < len(fits); d++ {
			if i-d >= 0 && len(fits[i-d].res) >= 8 {
				return fits[i-d].res
			}
			if i+d < len(fits) && len(fits[i+d].res) >= 8 {
				return fits[i+d].res
			}
		}
		return nil
	}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		f, ok := nearest(y - box.Min.Y)
		if !ok {
			// No row has enough background to fit (ink spans the box):
			// fall back to the ring interpolation for the masked pixels.
			ring := image.NewRGBA(box)
			draw.Draw(ring, box, orig, box.Min, draw.Src)
			borderFill(ring, orig, box, defaultRing)
			for yy := box.Min.Y; yy < box.Max.Y; yy++ {
				for x := box.Min.X; x < box.Max.X; x++ {
					if masked(x, yy) {
						dst.SetRGBA(x, yy, ring.RGBAAt(x, yy))
					}
				}
			}
			return
		}
		res := pool(y - box.Min.Y)
		for x := box.Min.X; x < box.Max.X; x++ {
			fx := float64(x - box.Min.X)
			if !masked(x, y) {
				if !flatten {
					continue
				}
				// Background pixel on a flat row: snap to the line unless it
				// is something else (icon, divider) sitting off the line.
				c := orig.RGBAAt(x, y)
				lineLum := (0.299*(f.a[0]+f.b[0]*fx) + 0.587*(f.a[1]+f.b[1]*fx) + 0.114*(f.a[2]+f.b[2]*fx)) / 255
				if math.Abs(luma(c)-lineLum) > 0.08 ||
					math.Abs(float64(c.R)-(f.a[0]+f.b[0]*fx)) > 20 || math.Abs(float64(c.G)-(f.a[1]+f.b[1]*fx)) > 20 || math.Abs(float64(c.B)-(f.a[2]+f.b[2]*fx)) > 20 {
					continue
				}
			}
			var grain [3]float64
			if !flatten && len(res) > 0 {
				grain = res[(x-box.Min.X)%len(res)]
			}
			var out [3]uint8
			for ch := 0; ch < 3; ch++ {
				v := f.a[ch] + f.b[ch]*fx + grain[ch]
				out[ch] = uint8(math.Max(0, math.Min(255, v+0.5)))
			}
			dst.SetRGBA(x, y, color.RGBA{out[0], out[1], out[2], 255})
		}
	}
}

// farGrain is the mean horizontal luminance step between background pixels
// of box that are at least 3 px from any masked pixel on their row.
func farGrain(orig *image.RGBA, box image.Rectangle, masked func(x, y int) bool) float64 {
	far := func(x, y int) bool {
		for d := -3; d <= 3; d++ {
			if x+d >= box.Min.X && x+d < box.Max.X && masked(x+d, y) {
				return false
			}
		}
		return true
	}
	sum, n := 0.0, 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X-1; x++ {
			if far(x, y) && far(x+1, y) {
				sum += math.Abs(luma(orig.RGBAAt(x, y)) - luma(orig.RGBAAt(x+1, y)))
				n++
			}
		}
	}
	if n == 0 {
		return 1
	}
	return sum / float64(n)
}

// besideGrain is the horizontal grain of the same rows just beyond the
// box (4 to 12 px left and right): the row's background where no text
// ever was. Dense lines leave few far pixels inside the box, so this is
// the more reliable read of the row's style.
func besideGrain(orig *image.RGBA, box image.Rectangle) float64 {
	best := 1.0
	for _, band := range []image.Rectangle{
		image.Rect(box.Min.X-12, box.Min.Y, box.Min.X-4, box.Max.Y),
		image.Rect(box.Max.X+4, box.Min.Y, box.Max.X+12, box.Max.Y),
	} {
		band = band.Intersect(orig.Bounds())
		sum, n := 0.0, 0
		for y := band.Min.Y; y < band.Max.Y; y++ {
			for x := band.Min.X; x < band.Max.X-1; x++ {
				sum += math.Abs(luma(orig.RGBAAt(x, y)) - luma(orig.RGBAAt(x+1, y)))
				n++
			}
		}
		// The calmer side: an icon or the next column on one side must not
		// hide a flat row on the other.
		if n > 0 {
			best = math.Min(best, sum/float64(n))
		}
	}
	return best
}
