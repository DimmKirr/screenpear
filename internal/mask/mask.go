package mask

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/devcell-sh/go-textshot"
)

// Inter Medium, the same face the web frontend ships, so masked text looks
// alike on every platform regardless of what fonts the host has.
//
//go:embed Inter-Medium.ttf
var interMedium []byte

var (
	fontOnce sync.Once
	fontFace *opentype.Font
	fontErr  error
)

func loadFont() (*opentype.Font, error) {
	fontOnce.Do(func() {
		fontFace, fontErr = opentype.Parse(interMedium)
	})
	return fontFace, fontErr
}

var (
	candOnce   sync.Once
	candidates []textshot.Face
)

// faceCandidates are the faces a repaint may use: Inter Medium (the UI
// font) plus the Go family for regular/bold/italic/mono coverage.
// textshot.DetectTypeface picks whichever reproduces the original pixels
// best; it never names a face outside this list.
func faceCandidates() []textshot.Face {
	candOnce.Do(func() {
		candidates = append([]textshot.Face{{Name: "Inter Medium", Family: "Inter", TTF: interMedium}}, textshot.GoFaces()...)
	})
	return candidates
}

// Line is one recognized text line with its box in image pixels.
type Line struct {
	ID    int             `json:"id"`
	Text  string          `json:"text"`
	Score float32         `json:"score"`
	Box   image.Rectangle `json:"-"`
	X0    int             `json:"x0"`
	Y0    int             `json:"y0"`
	X1    int             `json:"x1"`
	Y1    int             `json:"y1"`
	// Angle is the reading direction in degrees: 0 left-to-right, 90
	// top-to-bottom (rotated clockwise), -90 bottom-to-top. Set by the
	// orientation sweep in services; the Swift lanes carry the same field.
	Angle float64 `json:"angle,omitempty"`
	// Kind is "text" or "symbol". OCR reads glyph-sized icons as single
	// characters (a chevron as "V", a star as "☆", an avatar as "O"); those
	// are symbols and are not rewritten unless the caller selects them.
	Kind string `json:"kind,omitempty"`
}

// Classify sets Kind from what the OCR engine reported: a line with no
// letter or digit, a single character, or at most two characters read
// with low confidence is a symbol (an icon or a glyph the detector cut
// out), everything else is text.
func (l *Line) Classify() {
	runes := 0
	alnum := 0
	for _, r := range l.Text {
		if unicode.IsSpace(r) {
			continue
		}
		runes++
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alnum++
		}
	}
	switch {
	case alnum == 0, runes <= 1, runes <= 2 && l.Score < 0.8:
		l.Kind = "symbol"
	default:
		l.Kind = "text"
	}
}

// IsSymbol reports a line that should not be rewritten by default.
func (l Line) IsSymbol() bool { return l.Kind == "symbol" }

// FromRect builds a Line whose flat x0..y1 fields mirror Box, which is what
// crosses the JSON boundary to the web and SwiftUI frontends.
func FromRect(id int, text string, score float32, r image.Rectangle) Line {
	return Line{ID: id, Text: text, Score: score, Box: r, X0: r.Min.X, Y0: r.Min.Y, X1: r.Max.X, Y1: r.Max.Y}
}

// Vertical reports a line of rotated text: either tagged by the
// orientation sweep (Angle of +-90) or, for engines that only report
// boxes, a box much taller than wide holding more than a couple of
// characters. Horizontal OCR boxes are at most about as tall as one glyph
// row, so the heuristic never fires on short words.
func (l Line) Vertical() bool {
	if l.Angle >= 45 || l.Angle <= -45 {
		return true
	}
	r := l.Rect()
	runes := 0
	for _, c := range l.Text {
		if c != ' ' {
			runes++
		}
	}
	return runes >= 3 && r.Dy() > 2*r.Dx()
}

// upright turns a vertical line's crop so its text reads left-to-right,
// and returns the inverse to put a patch back. Default reading direction
// is top-to-bottom (Angle 90); -90 reads bottom-to-top.
func (l Line) upright(crop image.Image) (*image.RGBA, func(image.Image) *image.RGBA) {
	if l.Angle <= -45 {
		return rotateCW(crop), rotateCCW
	}
	return rotateCCW(crop), rotateCW
}

// Rect returns the box, preferring Box and falling back to the flat fields
// for values that arrived over JSON.
func (l Line) Rect() image.Rectangle {
	if !l.Box.Empty() {
		return l.Box
	}
	return image.Rect(l.X0, l.Y0, l.X1, l.Y1)
}

// Font is what textshot.DetectTypeface measured on the original line and
// what the repaint was drawn with.
type Font struct {
	Face       string  `json:"face"`
	Family     string  `json:"family"`
	Bold       bool    `json:"bold"`
	Italic     bool    `json:"italic"`
	SizePx     float64 `json:"sizePx"`
	SlantDeg   float64 `json:"slantDeg"`
	Stroke     float64 `json:"stroke"`
	Similarity float64 `json:"similarity"`
}

// Replacement records one line that Apply rewrote: which box, what text,
// and the font the original was measured as.
type Replacement struct {
	LineID int     `json:"lineId"`
	From   string  `json:"from"`
	To     string  `json:"to"`
	X0     int     `json:"x0"`
	Y0     int     `json:"y0"`
	X1     int     `json:"x1"`
	Y1     int     `json:"y1"`
	Angle  float64 `json:"angle,omitempty"`
	Font   *Font   `json:"font,omitempty"` // nil when detection failed and the Inter fallback was used
}

func fontOf(t textshot.Typeface) *Font {
	return &Font{Face: t.Face, Family: t.Family, Bold: t.Bold, Italic: t.Italic, SizePx: t.SizePx, SlantDeg: t.SlantDeg, Stroke: t.Stroke, Similarity: t.Similarity}
}

// Options tunes Apply. SubstituteUnmatched replaces lines no rule changes
// with generated same-shape text (Substitute) instead of leaving them.
// Background, when set, is called once with every box about to be
// repainted and must return the image with those boxes filled from their
// surroundings (content-aware inpainting); the text is then drawn on that.
// When nil or failing, each box gets the flat border colour instead.
type Options struct {
	SubstituteUnmatched bool
	Background          func(img image.Image, boxes []image.Rectangle) (image.Image, error)
}

// Apply paints every line whose text a rule changes. only, when non-empty,
// restricts the candidates to those line IDs (the UI's per-line checkboxes).
// The source image is never modified.
func Apply(src image.Image, lines []Line, rules []Rule, only []int) (*image.RGBA, []Replacement, error) {
	return ApplyWith(src, lines, rules, only, Options{})
}

// ApplyWith is Apply with Options.
func ApplyWith(src image.Image, lines []Line, rules []Rule, only []int, opt Options) (*image.RGBA, []Replacement, error) {
	face, err := loadFont()
	if err != nil {
		return nil, nil, fmt.Errorf("mask: font: %w", err)
	}

	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	// Detection reads the original pixels even after neighbours were painted.
	orig := image.NewRGBA(dst.Rect)
	copy(orig.Pix, dst.Pix)

	allowed := map[int]bool{}
	for _, id := range only {
		allowed[id] = true
	}

	// Pass 1: what changes. Needed up front so the background can be
	// inpainted in one call for every box before any text is drawn.
	type job struct {
		line    Line
		newText string
		box     image.Rectangle
	}
	var jobs []job
	var boxes []image.Rectangle
	var boxLines []Line
	for _, line := range lines {
		if len(allowed) > 0 && !allowed[line.ID] {
			continue
		}
		if line.IsSymbol() && !allowed[line.ID] {
			continue // icons read as characters: left alone unless selected
		}
		newText, changed := Rewrite(line.Text, rules)
		if !changed && opt.SubstituteUnmatched && hasWordChars(line.Text) {
			newText, changed = Substitute(line.Text, line.ID), true
		}
		if !changed {
			continue
		}
		box := line.Rect().Intersect(dst.Bounds())
		if box.Empty() {
			continue
		}
		jobs = append(jobs, job{line, newText, box})
		boxes = append(boxes, box)
		boxLines = append(boxLines, line)
	}
	filledBoxes := eraseBoxes(dst, orig, boxes, boxLines, opt.Background)

	// Pass 2: detect typeface for every job. Vertical lines are measured
	// in their upright orientation.
	type detected struct {
		style textshot.Typeface
		ok    bool
	}
	styles := make([]detected, len(jobs))
	for i, j := range jobs {
		if j.line.Vertical() {
			up, _ := j.line.upright(orig.SubImage(j.box))
			s, err := textshot.DetectTypeface(up, up.Bounds(), j.line.Text, faceCandidates())
			if err == nil {
				styles[i] = detected{s, true}
			}
		} else {
			s, err := textshot.DetectTypeface(orig, j.box, j.line.Text, faceCandidates())
			if err == nil {
				styles[i] = detected{s, true}
			}
		}
	}

	// Pass 3: group jobs by spatial zone and apply consensus font.
	// Within each group the face with the highest total similarity wins;
	// individual size is preserved unless it is an outlier within the
	// group (>15% from the group's median size).
	groupIDs := textshot.GroupBoxes(boxes, orig)
	groups := map[int][]int{}
	for i, g := range groupIDs {
		groups[g] = append(groups[g], i)
	}
	for _, members := range groups {
		// Vote: accumulate similarity per face name.
		votes := map[string]float64{}
		var bestFace string
		for _, m := range members {
			if !styles[m].ok {
				continue
			}
			votes[styles[m].style.Face] += styles[m].style.Similarity
		}
		bestSim := 0.0
		for f, s := range votes {
			if s > bestSim {
				bestSim = s
				bestFace = f
			}
		}
		if bestFace == "" {
			continue
		}

		// Collect sizes for median within the group.
		var sizes []float64
		for _, m := range members {
			if styles[m].ok {
				sizes = append(sizes, styles[m].style.SizePx)
			}
		}
		sort.Float64s(sizes)
		medianSize := 0.0
		if len(sizes) > 0 {
			medianSize = sizes[len(sizes)/2]
		}
		sizeTol := math.Max(2, 0.15*medianSize)

		// Apply consensus: override face and size for members whose size
		// is close to the group median. Outliers (headings, section labels)
		// get their own sub-consensus among same-sized outliers so that
		// e.g. "Favorites" and "iCloud" section headers agree with each other.
		var outliers []int
		for _, m := range members {
			if !styles[m].ok {
				continue
			}
			if math.Abs(styles[m].style.SizePx-medianSize) > sizeTol {
				outliers = append(outliers, m)
				continue
			}
			styles[m].style.SizePx = medianSize
			if styles[m].style.Face != bestFace {
				for _, m2 := range members {
					if styles[m2].ok && styles[m2].style.Face == bestFace {
						styles[m].style.Face = bestFace
						styles[m].style.Family = styles[m2].style.Family
						styles[m].style.Bold = styles[m2].style.Bold
						styles[m].style.Italic = styles[m2].style.Italic
						break
					}
				}
			}
		}
		// Sub-consensus for outliers: cluster by size similarity and
		// apply face+size consensus within each sub-group.
		for len(outliers) > 0 {
			cluster := []int{outliers[0]}
			pivot := styles[outliers[0]].style.SizePx
			rest := outliers[1:]
			outliers = nil
			for _, m := range rest {
				if math.Abs(styles[m].style.SizePx-pivot) <= sizeTol {
					cluster = append(cluster, m)
				} else {
					outliers = append(outliers, m)
				}
			}
			if len(cluster) < 2 {
				continue
			}
			// Consensus within the sub-group.
			subVotes := map[string]float64{}
			var subSizes []float64
			for _, m := range cluster {
				subVotes[styles[m].style.Face] += styles[m].style.Similarity
				subSizes = append(subSizes, styles[m].style.SizePx)
			}
			sort.Float64s(subSizes)
			subMedian := subSizes[len(subSizes)/2]
			var subBestFace string
			subBestSim := 0.0
			for f, s := range subVotes {
				if s > subBestSim {
					subBestSim = s
					subBestFace = f
				}
			}
			for _, m := range cluster {
				styles[m].style.SizePx = subMedian
				if styles[m].style.Face != subBestFace {
					for _, m2 := range cluster {
						if styles[m2].style.Face == subBestFace {
							styles[m].style.Face = subBestFace
							styles[m].style.Family = styles[m2].style.Family
							styles[m].style.Bold = styles[m2].style.Bold
							styles[m].style.Italic = styles[m2].style.Italic
							break
						}
					}
				}
			}
		}
	}

	// Pass 4: paint.
	var replaced []Replacement
	for i, j := range jobs {
		line, newText, box := j.line, j.newText, j.box
		_ = filledBoxes[box]
		bg := borderColor(orig, box)
		fg := contrastColor(orig, box, bg)
		if line.Vertical() {
			upright, back := line.upright(orig.SubImage(box))
			ubox := upright.Bounds()
			patch := image.NewRGBA(ubox)
			draw.Draw(patch, ubox, upright, ubox.Min, draw.Src)
			filledUp, _ := line.upright(dst.SubImage(box))
			draw.Draw(patch, ubox, filledUp, ubox.Min, draw.Src)
			rep := Replacement{LineID: line.ID, From: line.Text, To: newText, X0: box.Min.X, Y0: box.Min.Y, X1: box.Max.X, Y1: box.Max.Y, Angle: line.Angle}
			if styles[i].ok {
				err = drawFitted(patch, upright, ubox, newText, fg, styles[i].style)
				rep.Font = fontOf(styles[i].style)
			} else {
				err = drawText(patch, face, ubox, newText, fg)
			}
			if err != nil {
				return nil, nil, err
			}
			draw.Draw(dst, box, back(patch), image.Point{}, draw.Src)
			replaced = append(replaced, rep)
			continue
		}
		rep := Replacement{LineID: line.ID, From: line.Text, To: newText, X0: box.Min.X, Y0: box.Min.Y, X1: box.Max.X, Y1: box.Max.Y, Angle: line.Angle}
		if styles[i].ok {
			err = drawFitted(dst, orig, box, newText, fg, styles[i].style)
			rep.Font = fontOf(styles[i].style)
		} else {
			err = drawText(dst, face, box, newText, fg)
		}
		if err != nil {
			return nil, nil, err
		}
		replaced = append(replaced, rep)
	}
	return dst, replaced, nil
}

// borderColor is the most common colour on a one pixel ring just outside the
// box: the page background the text sits on. OCR boxes hug the glyphs, so
// the ring is almost never text.
func borderColor(img *image.RGBA, box image.Rectangle) color.RGBA {
	ring := box.Inset(-2).Intersect(img.Bounds())
	counts := map[color.RGBA]int{}
	for y := ring.Min.Y; y < ring.Max.Y; y++ {
		for x := ring.Min.X; x < ring.Max.X; x++ {
			if (image.Point{x, y}).In(box) {
				continue
			}
			counts[img.RGBAAt(x, y)]++
		}
	}
	if len(counts) == 0 {
		return color.RGBA{255, 255, 255, 255}
	}
	type kv struct {
		c color.RGBA
		n int
	}
	all := make([]kv, 0, len(counts))
	for c, n := range counts {
		all = append(all, kv{c, n})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return luma(all[i].c) < luma(all[j].c)
	})
	return all[0].c
}

// contrastColor is the pixel inside the box farthest in luminance from the
// background: the ink colour of the original text.
func contrastColor(img *image.RGBA, box image.Rectangle, bg color.RGBA) color.RGBA {
	best := bg
	bestDelta := 0.0
	bgL := luma(bg)
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if d := abs(luma(c) - bgL); d > bestDelta {
				bestDelta, best = d, c
			}
		}
	}
	if bestDelta < 0.15 {
		if bgL > 0.5 {
			return color.RGBA{0x22, 0x22, 0x22, 0xff}
		}
		return color.RGBA{0xee, 0xee, 0xee, 0xff}
	}
	return best
}

// drawFitted draws text in the detected face at the detected size, with the
// ink top aligned to the original ink top and letter spacing adjusted so
// the line spans the full box width. Text wider than the box is scaled
// down to fit; text narrower gets tracking, capped so it still reads as
// one word run.
func drawFitted(dst, orig *image.RGBA, box image.Rectangle, text string, fg color.RGBA, style textshot.Typeface) error {
	var cand *textshot.Face
	for i := range faceCandidates() {
		if faceCandidates()[i].Name == style.Face {
			cand = &faceCandidates()[i]
		}
	}
	if cand == nil {
		return fmt.Errorf("mask: unknown face %q", style.Face)
	}
	f, err := cand.Font()
	if err != nil {
		return err
	}
	size := style.SizePx
	m, err := textshot.MeasureFace(cand, text, size)
	if err != nil {
		return err
	}
	runes := []rune(text)
	tracking := 0.0
	switch {
	case m.Width > float64(box.Dx()):
		size *= float64(box.Dx()) / m.Width
		if m, err = textshot.MeasureFace(cand, text, size); err != nil {
			return err
		}
	case len(runes) > 1:
		// OCR boxes carry padding, so only track up to 0.12 em; whatever is
		// left is split evenly on both sides so the run stays centred where
		// the original sat.
		tracking = math.Min((float64(box.Dx())-m.Width)/float64(len(runes)-1), 0.12*size)
	}
	spanned := m.Width + tracking*float64(len(runes)-1)
	left := float64(box.Min.X) + math.Max(0, (float64(box.Dx())-spanned)/2)
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return err
	}
	defer face.Close()

	// Ink top of the original inside the box.
	inkTop := box.Min.Y + typoInkTop(orig.SubImage(box))
	baseline := inkTop - m.InkTop
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(fg), Face: face, Dot: fixed.Point26_6{X: fixed.Int26_6(left * 64), Y: fixed.I(baseline)}}
	for _, r := range runes {
		d.DrawString(string(r))
		d.Dot.X += fixed.Int26_6(tracking * 64)
	}
	return nil
}

// EraseInfo records how one box was erased, for test output and tuning.
type EraseInfo struct {
	LineID   int             `json:"lineId"`
	Text     string          `json:"text"`
	Box      image.Rectangle `json:"-"`
	X0       int             `json:"x0"`
	Y0       int             `json:"y0"`
	X1       int             `json:"x1"`
	Y1       int             `json:"y1"`
	Angle    float64         `json:"angle,omitempty"`
	Method   string          `json:"method"`             // stroke-interp, stroke-telea, ring, patch, model, tiny-ring
	RingStd  float64         `json:"ringStd"`            // luminance std dev of the surroundings
	Coverage float64         `json:"coverage"`           // fraction of the box that is ink
	Overlaps []int           `json:"overlaps,omitempty"` // other line ids whose boxes intersect this one
	Fringe   int             `json:"fringe"`             // stroke-mask growth used, px (0 for whole-box fills)
}

// Erase returns src with every line's box erased and no text drawn: the
// "remove text" result on its own, with the same fill rules as Apply.
func Erase(src image.Image, lines []Line, background func(image.Image, []image.Rectangle) (image.Image, error)) *image.RGBA {
	out, _ := EraseDetailed(src, lines, background)
	return out
}

// EraseDetailed is Erase plus a per-box report.
func EraseDetailed(src image.Image, lines []Line, background func(image.Image, []image.Rectangle) (image.Image, error)) (*image.RGBA, []EraseInfo) {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	orig := image.NewRGBA(dst.Rect)
	copy(orig.Pix, dst.Pix)
	var boxes []image.Rectangle
	var boxLines []Line
	var infos []EraseInfo
	for _, l := range lines {
		if l.IsSymbol() {
			continue // icons read as characters are not text to erase
		}
		box := l.Rect().Intersect(dst.Bounds())
		if box.Empty() {
			continue
		}
		boxes = append(boxes, box)
		boxLines = append(boxLines, l)
		info := EraseInfo{LineID: l.ID, Text: l.Text, Box: box, X0: box.Min.X, Y0: box.Min.Y, X1: box.Max.X, Y1: box.Max.Y, Angle: l.Angle,
			RingStd: ringStdDev(orig, box), Coverage: inkCoverage(orig, box)}
		for _, o := range lines {
			if o.ID != l.ID && o.Rect().Overlaps(box) {
				info.Overlaps = append(info.Overlaps, o.ID)
			}
		}
		infos = append(infos, info)
	}
	methods := eraseBoxesReport(dst, orig, boxes, boxLines, background)
	for i := range infos {
		infos[i].Method = methods[infos[i].Box]
		if strings.HasPrefix(infos[i].Method, "stroke") {
			infos[i].Fringe = FringeFor(infos[i].Box.Dy(), infos[i].Method == "stroke-telea")
		}
	}
	return dst, infos
}

// inkCoverage is the fraction of the box's pixels above the stroke ink level.
func inkCoverage(orig *image.RGBA, box image.Rectangle) float64 {
	ink := textshot.InkMap(orig.SubImage(box))
	n := 0
	for _, v := range ink.Pix {
		if v >= strokeInk {
			n++
		}
	}
	return float64(n) / float64(len(ink.Pix))
}

// textureThreshold is the ring roughness above which a box's surroundings
// count as textured (wallpaper, photo, noise). Roughness is the mean
// luminance step between adjacent ring pixels, so a smooth gradient
// (a toolbar, a sheet) measures near zero and stays on the interpolation
// path, while texture of any kind measures high.
const textureThreshold = 0.02

// eraseBoxes erases boxes in dst, crispest option first:
//
//  1. stroke fill: only the glyph pixels are replaced, from their immediate
//     neighbours (any background, flat or textured);
//  2. if the box is not text-like (a filled pill): ring interpolation on
//     flat surroundings, PatchMatch from real nearby pixels on textured ones;
//  3. background, when given, is the caller's model fill and is asked only
//     for boxes that are both textured and not text-like, i.e. where
//     invention is the only option left.
//
// Returns the set of boxes the caller's background filled.
func eraseBoxes(dst, orig *image.RGBA, boxes []image.Rectangle, lines []Line, background func(image.Image, []image.Rectangle) (image.Image, error)) map[image.Rectangle]bool {
	filled := map[image.Rectangle]bool{}
	for box, m := range eraseBoxesReport(dst, orig, boxes, lines, background) {
		if m == "model" {
			filled[box] = true
		}
	}
	return filled
}

// eraseBoxesReport is eraseBoxes returning the method used per box. lines
// is parallel to boxes and carries the reading angle: a vertical line is
// erased in its upright orientation (every rule here reads along rows).
func eraseBoxesReport(dst, orig *image.RGBA, boxes []image.Rectangle, lines []Line, background func(image.Image, []image.Rectangle) (image.Image, error)) map[image.Rectangle]string {
	methods := map[image.Rectangle]string{}
	var forModel []image.Rectangle
	for i, box := range boxes {
		if i < len(lines) && lines[i].Vertical() {
			methods[box] = eraseUpright(dst, orig, box, lines[i])
			continue
		}
		if box.Dy() < strokeMinHeight {
			borderFill(dst, orig, box, defaultRing)
			methods[box] = "tiny-ring"
			continue
		}
		flat := ringStdDev(orig, box) <= textureThreshold
		if strokeFill(dst, orig, box) {
			if flat {
				methods[box] = "stroke-interp"
			} else {
				methods[box] = "stroke-telea"
			}
			continue
		}
		if flat {
			borderFill(dst, orig, box, defaultRing)
			methods[box] = "ring"
			continue
		}
		if background != nil {
			forModel = append(forModel, box)
			continue
		}
		patchFill(dst, orig, box)
		methods[box] = "patch"
	}
	if len(forModel) == 0 {
		return methods
	}
	out, err := background(orig, forModel)
	for _, box := range forModel {
		if err != nil || out == nil {
			patchFill(dst, orig, box)
			methods[box] = "patch"
			continue
		}
		draw.Draw(dst, box, out, box.Min, draw.Src)
		methods[box] = "model"
	}
	return methods
}

// eraseUpright erases a vertical line by turning a margin-padded crop
// upright, running the horizontal erase rules on it, and turning the
// result back. Returns the method used, suffixed "-vertical".
func eraseUpright(dst, orig *image.RGBA, box image.Rectangle, line Line) string {
	const margin = 12
	outer := box.Inset(-margin).Intersect(orig.Bounds())
	up, back := line.upright(orig.SubImage(outer))
	// The box in upright coordinates: rotate its corners the same way.
	var ub image.Rectangle
	if line.Angle <= -45 { // rotateCW: x' = h-1-y, y' = x  (h = outer height)
		h := outer.Dy()
		ub = image.Rect(h-(box.Max.Y-outer.Min.Y), box.Min.X-outer.Min.X, h-(box.Min.Y-outer.Min.Y), box.Max.X-outer.Min.X)
	} else { // rotateCCW: x' = y, y' = w-1-x  (w = outer width)
		w := outer.Dx()
		ub = image.Rect(box.Min.Y-outer.Min.Y, w-(box.Max.X-outer.Min.X), box.Max.Y-outer.Min.Y, w-(box.Min.X-outer.Min.X))
	}
	work := image.NewRGBA(up.Rect)
	copy(work.Pix, up.Pix)
	method := "ring"
	switch {
	case ub.Dy() < strokeMinHeight:
		borderFill(work, up, ub, defaultRing)
		method = "tiny-ring"
	case strokeFill(work, up, ub):
		if ringStdDev(up, ub) <= textureThreshold {
			method = "stroke-interp"
		} else {
			method = "stroke-telea"
		}
	case ringStdDev(up, ub) <= textureThreshold:
		borderFill(work, up, ub, defaultRing)
	default:
		patchFill(work, up, ub)
		method = "patch"
	}
	restored := back(work)
	draw.Draw(dst, box, restored, box.Min.Sub(outer.Min), draw.Src)
	return method + "-vertical"
}

// ringStdDev is the mean luminance step between horizontally and
// vertically adjacent pixels on a ring two to six pixels outside the box:
// the local roughness of the surroundings. The name is historical; the
// JSON report exposes it as ringStd.
func ringStdDev(img *image.RGBA, box image.Rectangle) float64 {
	outer := box.Inset(-6).Intersect(img.Bounds())
	inner := box.Inset(-2)
	var sum float64
	n := 0
	for y := outer.Min.Y; y < outer.Max.Y-1; y++ {
		for x := outer.Min.X; x < outer.Max.X-1; x++ {
			if (image.Point{x, y}).In(inner) {
				continue
			}
			l := luma(img.RGBAAt(x, y))
			sum += math.Abs(l-luma(img.RGBAAt(x+1, y))) + math.Abs(l-luma(img.RGBAAt(x, y+1)))
			n += 2
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// rotateCCW turns an image 90 degrees counter-clockwise (vertical
// top-to-bottom text becomes left-to-right), rooted at (0,0).
func rotateCCW(src image.Image) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

// rotateCW is the inverse of rotateCCW.
func rotateCW(src image.Image) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

// typoInkTop is the first ink row of a crop, relative to the crop: the
// row where the crop's luminance departs from its border background.
func typoInkTop(crop image.Image) int {
	b := crop.Bounds()
	bg := luma(colorAt(crop, b.Min.X, b.Min.Y))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if abs(luma(colorAt(crop, x, y))-bg) > 0.3 {
				return y - b.Min.Y
			}
		}
	}
	return 0
}

func colorAt(img image.Image, x, y int) color.RGBA {
	r, g, b, _ := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255}
}

// drawText fits text into box: the font size follows the box height and
// shrinks until the string fits the box width, then draws it left-aligned
// and vertically centred.
func drawText(dst *image.RGBA, f *opentype.Font, box image.Rectangle, text string, fg color.RGBA) error {
	size := float64(box.Dy()) * 0.82
	if size < 6 {
		size = 6
	}
	for {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return fmt.Errorf("mask: face: %w", err)
		}
		width := font.MeasureString(face, text).Ceil()
		if width <= box.Dx() || size <= 6 {
			metrics := face.Metrics()
			ascent := metrics.Ascent.Ceil()
			descent := metrics.Descent.Ceil()
			baseline := box.Min.Y + (box.Dy()+ascent-descent)/2
			d := &font.Drawer{
				Dst:  dst,
				Src:  image.NewUniform(fg),
				Face: face,
				Dot:  fixed.P(box.Min.X, baseline),
			}
			d.DrawString(text)
			return face.Close()
		}
		_ = face.Close()
		size -= 1
	}
}

func luma(c color.RGBA) float64 {
	return (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
