// Package services holds the Go side of the app: the OCR service bound into
// the webview via Wails bindings, and the loopback HTTP API the native
// SwiftUI window and the e2e tests call. Both front doors share one
// OCRService so every platform gets identical results.
package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/devcell-sh/go-textshot"

	"github.com/automationd/screenpear/internal/mask"
)

// Analysis is what the frontends render: every recognized line with its box
// in source-image pixels, plus the image size so boxes can be scaled.
type Analysis struct {
	Path      string      `json:"path"`
	Width     int         `json:"width"`
	Height    int         `json:"height"`
	Lines     []mask.Line `json:"lines"`
	ElapsedMS int64       `json:"elapsedMs"`
}

// MaskRequest asks for Path to be rewritten with Rules and written to Output
// (default: <stem>-masked.png beside the input). Lines are the analysis the
// user reviewed; passing them back rather than re-running OCR keeps what the
// user saw and what gets masked identical. LineIDs, when set, limits masking
// to those lines.
type MaskRequest struct {
	Path    string      `json:"path"`
	Output  string      `json:"output,omitempty"`
	Rules   []mask.Rule `json:"rules"`
	Lines   []mask.Line `json:"lines"`
	LineIDs []int       `json:"lineIds,omitempty"`
	// SubstituteAll replaces every selected line no rule matches with
	// generated same-shape text, so a screenshot with no dictionary hits
	// still comes out masked.
	SubstituteAll bool `json:"substituteAll,omitempty"`
	// FlatBackground skips content-aware inpainting (MI-GAN, 28 MB model
	// fetched on first use) and fills boxes with the border colour. Off by
	// default: inpainting keeps gradients and noise under the new text.
	FlatBackground bool `json:"flatBackground,omitempty"`
}

// MaskResult names the written file and what changed in it.
type MaskResult struct {
	Output    string             `json:"output"`
	Replaced  []mask.Replacement `json:"replaced"`
	ElapsedMS int64              `json:"elapsedMs"`
}

// OCRService runs text detection and masking. The textshot engine is created
// on first use because New downloads ONNX Runtime and the models (~40 MB)
// into the user cache on a fresh machine, which must not block app start.
type OCRService struct {
	opts   textshot.Options
	logger *slog.Logger

	once sync.Once
	eng  *textshot.Engine
	err  error
}

// DefaultOptions keeps textshot's own defaults: cache under the user cache
// dir, GOMAXPROCS threads, 1920 px detector side.
func DefaultOptions() textshot.Options { return textshot.Options{} }

// NewOCRService returns a service that will lazily initialise textshot with
// opts. A nil logger discards engine diagnostics.
func NewOCRService(opts textshot.Options, logger *slog.Logger) *OCRService {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	opts.Logger = logger
	return &OCRService{opts: opts, logger: logger}
}

// engine returns the shared textshot engine, initialising it on first call.
// Unexported on purpose: Wails binds every exported method of a service into
// the frontend, and the engine handle must not cross that boundary.
func (s *OCRService) engine(ctx context.Context) (*textshot.Engine, error) {
	s.once.Do(func() {
		start := time.Now()
		s.eng, s.err = textshot.New(ctx, s.opts)
		if s.err != nil {
			s.logger.Error("ocr: engine init failed", "err", s.err)
			return
		}
		s.logger.Info("ocr: engine ready", "ms", time.Since(start).Milliseconds())
	})
	return s.eng, s.err
}

// Ready reports whether the engine initialised, forcing initialisation. The
// frontends call it right after launch so the download happens while the
// user is still picking a file.
func (s *OCRService) Ready() (bool, error) {
	_, err := s.engine(context.Background())
	if err != nil {
		return false, err
	}
	return true, nil
}

// DefaultRules is the built-in replacement dictionary.
func (s *OCRService) DefaultRules() []mask.Rule {
	return mask.DefaultRules()
}

// Analyze runs text detection on the image at path.
func (s *OCRService) Analyze(path string) (*Analysis, error) {
	return s.analyze(context.Background(), path)
}

// analyze is Analyze with a caller-supplied context (the HTTP API's).
func (s *OCRService) analyze(ctx context.Context, path string) (*Analysis, error) {
	img, err := loadImage(path)
	if err != nil {
		return nil, err
	}
	eng, err := s.engine(ctx)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	results, err := eng.DetectText(ctx, img)
	if err != nil {
		return nil, fmt.Errorf("ocr: detect %s: %w", filepath.Base(path), err)
	}
	lines := make([]mask.Line, len(results))
	for i, r := range results {
		lines[i] = mask.FromRect(i, r.Text, r.Score, r.Box)
		lines[i].Classify()
	}
	// Orientation sweep: the recogniser reads left-to-right only, so a
	// vertical label (rotated 90 either way) is invisible to it. Run the
	// image turned upright for each direction and keep the hits that do not
	// overlap an upright line, tagged with their reading angle.
	b := img.Bounds()
	for _, turn := range []struct {
		rotate func(image.Image) *image.RGBA
		angle  float64
	}{{rotateCCW, 90}, {rotateCW, -90}} {
		turned := turn.rotate(img)
		rs, err := eng.DetectText(ctx, turned)
		if err != nil {
			return nil, fmt.Errorf("ocr: detect %s (rotated): %w", filepath.Base(path), err)
		}
		for _, r := range rs {
			box := unrotateBox(r.Box, b.Dx(), b.Dy(), turn.angle)
			if overlapsAny(box, lines) {
				continue
			}
			l := mask.FromRect(len(lines), r.Text, r.Score, box)
			l.Angle = turn.angle
			l.Classify()
			lines = append(lines, l)
		}
	}
	return &Analysis{
		Path:      path,
		Width:     b.Dx(),
		Height:    b.Dy(),
		Lines:     lines,
		ElapsedMS: time.Since(start).Milliseconds(),
	}, nil
}

// Mask rewrites the image per req and writes a PNG.
func (s *OCRService) Mask(req MaskRequest) (*MaskResult, error) {
	if req.Path == "" {
		return nil, errors.New("mask: path is required")
	}
	img, err := loadImage(req.Path)
	if err != nil {
		return nil, err
	}
	lines := req.Lines
	if len(lines) == 0 {
		// No reviewed analysis supplied (CLI or API caller): detect now.
		a, err := s.Analyze(req.Path)
		if err != nil {
			return nil, err
		}
		lines = a.Lines
	}
	start := time.Now()
	opts := mask.Options{SubstituteUnmatched: req.SubstituteAll}
	if !req.FlatBackground {
		opts.Background = s.inpaintBoxes
	}
	out, replaced, err := mask.ApplyWith(img, lines, req.Rules, req.LineIDs, opts)
	if err != nil {
		return nil, err
	}
	output := req.Output
	if output == "" {
		output = DefaultOutputPath(req.Path)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return nil, fmt.Errorf("mask: output dir: %w", err)
	}
	f, err := os.Create(output)
	if err != nil {
		return nil, fmt.Errorf("mask: create output: %w", err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		return nil, fmt.Errorf("mask: encode: %w", err)
	}
	if replaced == nil {
		replaced = []mask.Replacement{}
	}
	return &MaskResult{Output: output, Replaced: replaced, ElapsedMS: time.Since(start).Milliseconds()}, nil
}

// rotateCCW turns an image 90 degrees counter-clockwise, rooted at (0,0).
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

// rotateCW turns an image 90 degrees clockwise, rooted at (0,0).
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

// unrotateBox maps a box found on the turned image (angle 90: image was
// turned CCW; -90: CW) back to the original w x h image.
func unrotateBox(r image.Rectangle, w, h int, angle float64) image.Rectangle {
	if angle > 0 { // turned CCW: x' = y, y' = w-1-x
		return image.Rect(w-r.Max.Y, r.Min.X, w-r.Min.Y, r.Max.X)
	}
	// turned CW: x' = h-1-y, y' = x
	return image.Rect(r.Min.Y, h-r.Max.X, r.Max.Y, h-r.Min.X)
}

func overlapsAny(box image.Rectangle, lines []mask.Line) bool {
	for _, l := range lines {
		in := box.Intersect(l.Rect())
		if !in.Empty() && in.Dx()*in.Dy()*2 > min(box.Dx()*box.Dy(), l.Rect().Dx()*l.Rect().Dy()) {
			return true
		}
	}
	return false
}

// inpaintRing is how many pixels around a text box the inpainting model
// is shown. Tuned by TestInpaintRingTuning (test/e2e): 3 px keeps the
// anti-aliased fringe and the background's local gradient while keeping
// neighbouring widgets out.
const inpaintRing = 3

// inpaintBoxes is the mask.Options.Background hook: MI-GAN through the
// shared engine. Errors are logged and returned so the masker falls back
// to the flat fill rather than failing the whole run.
func (s *OCRService) inpaintBoxes(img image.Image, boxes []image.Rectangle) (image.Image, error) {
	ctx := context.Background()
	eng, err := s.engine(ctx)
	if err != nil {
		return nil, err
	}
	out, err := eng.InpaintBoxes(ctx, img, boxes, inpaintRing)
	if err != nil {
		s.logger.Warn("mask: inpaint unavailable, using flat fill", "err", err)
		return nil, err
	}
	return out, nil
}

// DefaultOutputPath is <dir>/<stem>-masked.png, with a trailing "-input"
// turned into "-output" so test fixtures keep their naming convention.
func DefaultOutputPath(input string) string {
	dir := filepath.Dir(input)
	stem := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	if strings.HasSuffix(stem, "-input") {
		return filepath.Join(dir, strings.TrimSuffix(stem, "-input")+"-output.png")
	}
	return filepath.Join(dir, stem+"-masked.png")
}

// ServiceShutdown releases the engine if it was created. Wails calls it on
// app shutdown for every registered service; tests call it from Cleanup.
func (s *OCRService) ServiceShutdown(_ context.Context) error {
	if s.eng != nil {
		return s.eng.Close()
	}
	return nil
}

func loadImage(path string) (image.Image, error) {
	if path == "" {
		return nil, errors.New("ocr: path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ocr: open: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("ocr: decode %s: %w", filepath.Base(path), err)
	}
	// Upright like every viewer shows it, so boxes and the preview agree.
	return applyOrientation(img, exifOrientation(data)), nil
}
