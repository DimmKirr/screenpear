package mask

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
	"unicode"
)

func TestRewriteLongestFirst(t *testing.T) {
	rules := []Rule{{"mm-", "ib-"}, {"mm-core", "ib-core"}}
	got, changed := Rewrite("mm-core and mm-web", rules)
	if !changed || got != "ib-core and ib-web" {
		t.Fatalf("got %q changed=%v", got, changed)
	}
	if _, changed := Rewrite("untouched", rules); changed {
		t.Fatal("expected no change")
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	in := "# comment\nNutCorp=SquirrelCorp\n\nbroken line\n mm- = ib- \n"
	rules := ParseRules(in)
	if len(rules) != 2 || rules[1] != (Rule{"mm-", "ib-"}) {
		t.Fatalf("parsed %+v", rules)
	}
	if FormatRules(rules) != "NutCorp=SquirrelCorp\nmm-=ib-\n" {
		t.Fatalf("formatted %q", FormatRules(rules))
	}
}

func TestApplyPaintsOnlyMatchingLines(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 60))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	// Fake "ink" inside the first box so contrastColor has something to find.
	draw.Draw(src, image.Rect(12, 14, 90, 30), image.NewUniform(color.RGBA{20, 20, 20, 255}), image.Point{}, draw.Src)

	lines := []Line{
		FromRect(0, "NutCorp Login", 0.9, image.Rect(10, 10, 120, 32)),
		FromRect(1, "Password", 0.9, image.Rect(10, 36, 90, 56)),
	}
	out, replaced, err := Apply(src, lines, DefaultRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(replaced) != 1 || replaced[0].To != "SquirrelCorp Login" {
		t.Fatalf("replaced %+v", replaced)
	}
	// Second box untouched: pixel identical to source.
	if out.RGBAAt(20, 40) != src.RGBAAt(20, 40) {
		t.Fatal("unmatched line was repainted")
	}
	// First box repainted: the fake ink block is gone at a spot glyphs are
	// unlikely to cover (right edge of the box).
	if out.RGBAAt(118, 15) != (color.RGBA{250, 250, 250, 255}) {
		t.Fatalf("background not restored: %v", out.RGBAAt(118, 15))
	}
	// Source is untouched.
	if src.RGBAAt(20, 20) != (color.RGBA{20, 20, 20, 255}) {
		t.Fatal("source image was modified")
	}

	_, replaced, err = Apply(src, lines, DefaultRules(), []int{1})
	if err != nil || len(replaced) != 0 {
		t.Fatalf("only=[1] should replace nothing, got %+v err=%v", replaced, err)
	}
}

func TestSubstituteKeepsShape(t *testing.T) {
	for _, in := range []string{"nutcorp-prod", "https://nutcorp.net/dashboard", "10.2.50.31", "DevOps", "Hello World 42!"} {
		a, b := Substitute(in, 3), Substitute(in, 3)
		if a != b {
			t.Fatalf("%q: not deterministic", in)
		}
		if a == in {
			t.Fatalf("%q: unchanged", in)
		}
		ra, ri := []rune(a), []rune(in)
		if len(ra) != len(ri) {
			t.Fatalf("%q -> %q: length changed", in, a)
		}
		for i := range ri {
			switch {
			case unicode.IsLetter(ri[i]):
				if !unicode.IsLetter(ra[i]) || unicode.IsUpper(ri[i]) != unicode.IsUpper(ra[i]) {
					t.Fatalf("%q -> %q: letter/case shape lost at %d", in, a, i)
				}
			case unicode.IsDigit(ri[i]):
				if !unicode.IsDigit(ra[i]) {
					t.Fatalf("%q -> %q: digit shape lost at %d", in, a, i)
				}
			default:
				if ra[i] != ri[i] {
					t.Fatalf("%q -> %q: punctuation changed at %d", in, a, i)
				}
			}
		}
		t.Logf("%s -> %s", in, a)
	}
}

func TestApplySubstitutesUnmatched(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 40))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	lines := []Line{FromRect(0, "Password", 0.9, image.Rect(10, 8, 120, 30))}
	_, replaced, err := ApplyWith(src, lines, nil, nil, Options{SubstituteUnmatched: true})
	if err != nil || len(replaced) != 1 || replaced[0].To == "Password" || len(replaced[0].To) != len("Password") {
		t.Fatalf("replaced=%+v err=%v", replaced, err)
	}
}

func TestRotateRoundTrip(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})
	src.Set(3, 1, color.RGBA{0, 0, 255, 255})
	ccw := rotateCCW(src)
	if ccw.Bounds().Dx() != 2 || ccw.Bounds().Dy() != 4 {
		t.Fatalf("ccw size %v", ccw.Bounds())
	}
	// Top-left of the source ends up bottom-left after a CCW turn.
	if ccw.RGBAAt(0, 3) != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("red misplaced: %v", ccw.RGBAAt(0, 3))
	}
	back := rotateCW(ccw)
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			if back.RGBAAt(x, y) != src.RGBAAt(x, y) {
				t.Fatalf("round trip differs at %d,%d", x, y)
			}
		}
	}
}

func TestVerticalLineDetection(t *testing.T) {
	if !FromRect(0, "Settings", 1, image.Rect(0, 0, 20, 120)).Vertical() {
		t.Fatal("tall box with a word must be vertical")
	}
	if FromRect(0, "OK", 1, image.Rect(0, 0, 20, 120)).Vertical() {
		t.Fatal("two characters never count as vertical")
	}
	if FromRect(0, "Settings", 1, image.Rect(0, 0, 120, 20)).Vertical() {
		t.Fatal("wide box is horizontal")
	}
}

// A vertical label is repainted along its own axis: the patch must show
// ink distributed along y, not a horizontal word squeezed into the box.
func TestApplyVerticalRepaint(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 60, 200))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	// Fake vertical ink: a dark column band inside the box.
	draw.Draw(src, image.Rect(24, 20, 36, 180), image.NewUniform(color.RGBA{20, 20, 20, 255}), image.Point{}, draw.Src)
	lines := []Line{FromRect(0, "Settings", 0.9, image.Rect(18, 14, 42, 186))}
	out, replaced, err := ApplyWith(src, lines, nil, nil, Options{SubstituteUnmatched: true})
	if err != nil || len(replaced) != 1 {
		t.Fatalf("replaced=%+v err=%v", replaced, err)
	}
	var top, bottom int
	for y := 14; y < 100; y++ {
		for x := 18; x < 42; x++ {
			if luma(out.RGBAAt(x, y)) < 0.5 {
				top++
			}
		}
	}
	for y := 100; y < 186; y++ {
		for x := 18; x < 42; x++ {
			if luma(out.RGBAAt(x, y)) < 0.5 {
				bottom++
			}
		}
	}
	if top == 0 || bottom == 0 {
		t.Fatalf("ink not spread along the vertical axis: top=%d bottom=%d", top, bottom)
	}
}

func TestEraseOrder(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 300, 100))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{240, 240, 240, 255}), image.Point{}, draw.Src)
	// A solid block on a flat panel: not text-like, flat ring -> ring fill,
	// never the model.
	block := FromRect(0, "Label", 1, image.Rect(20, 40, 120, 60))
	draw.Draw(src, block.Rect(), image.NewUniform(color.RGBA{0, 0, 0, 255}), image.Point{}, draw.Src)
	asked := 0
	bg := func(img image.Image, boxes []image.Rectangle) (image.Image, error) {
		asked += len(boxes)
		o := image.NewRGBA(img.Bounds())
		draw.Draw(o, o.Bounds(), image.NewUniform(color.RGBA{1, 2, 3, 255}), image.Point{}, draw.Src)
		return o, nil
	}
	out := Erase(src, []Line{block}, bg)
	if asked != 0 || out.RGBAAt(60, 50) != (color.RGBA{240, 240, 240, 255}) {
		t.Fatalf("flat block: asked=%d px=%v, want ring fill and no model", asked, out.RGBAAt(60, 50))
	}

	// The same block on noise: not text-like, textured ring -> the model.
	// Large per-pixel steps so the ring roughness is well above the
	// texture threshold (a gradient would not be).
	noisy := image.NewRGBA(src.Rect)
	for y := 0; y < 100; y++ {
		for x := 0; x < 300; x++ {
			v := uint8((x*97 + y*57) % 256)
			noisy.SetRGBA(x, y, color.RGBA{v, 255 - v, v / 2, 255})
		}
	}
	draw.Draw(noisy, block.Rect(), image.NewUniform(color.RGBA{0, 0, 0, 255}), image.Point{}, draw.Src)
	out = Erase(noisy, []Line{block}, bg)
	if asked != 1 || out.RGBAAt(60, 50) != (color.RGBA{1, 2, 3, 255}) {
		t.Fatalf("textured block: asked=%d px=%v, want the model", asked, out.RGBAAt(60, 50))
	}
	if out.RGBAAt(10, 50) != noisy.RGBAAt(10, 50) {
		t.Fatal("pixel outside the box changed")
	}
	// Without a model the textured block goes to PatchMatch and still gets filled.
	out = Erase(noisy, []Line{block}, nil)
	if out.RGBAAt(60, 50) == (color.RGBA{0, 0, 0, 255}) {
		t.Fatal("textured block not filled without a model")
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]struct {
		text  string
		score float32
		want  string
	}{
		"chevron":    {"V", 0.96, "symbol"},
		"star":       {"☆", 0.97, "symbol"},
		"brackets":   {"<>", 0.95, "symbol"},
		"lowconf2":   {"d9", 0.50, "symbol"},
		"word":       {"Settings", 0.9, "text"},
		"short word": {"go", 1.0, "text"},
		"number":     {"2024", 0.9, "text"},
		"two chars":  {"ab", 0.9, "text"},
	}
	for name, c := range cases {
		l := Line{Text: c.text, Score: c.score}
		l.Classify()
		if l.Kind != c.want {
			t.Errorf("%s: %q -> %s, want %s", name, c.text, l.Kind, c.want)
		}
	}
	src := image.NewRGBA(image.Rect(0, 0, 100, 40))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	sym := FromRect(0, "V", 0.96, image.Rect(10, 10, 24, 24))
	sym.Classify()
	_, replaced, err := ApplyWith(src, []Line{sym}, nil, nil, Options{SubstituteUnmatched: true})
	if err != nil || len(replaced) != 0 {
		t.Fatalf("symbol was rewritten: %+v err=%v", replaced, err)
	}
	_, replaced, err = ApplyWith(src, []Line{sym}, nil, []int{0}, Options{SubstituteUnmatched: true})
	if err != nil || len(replaced) != 1 {
		t.Fatalf("explicitly selected symbol not rewritten: %+v err=%v", replaced, err)
	}
}
