//go:build gowda

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/dimmkirr/gowda"
)

// TestGowdaMaskFlow drives the macOS SwiftUI window through WDA:
//
//  1. Load a fixture image via the loopback API (NSOpenPanel can't be driven).
//  2. Wait for analysis to complete (detected text lines appear in sidebar).
//  3. Edit the replacement rules in the TextEditor to add custom mappings.
//  4. Click Mask.
//  5. Screenshot after each step.
//
// Prerequisites:
//   - macOS with WebDriverAgent running (task ios:wda or Xcode)
//   - Screenpear app running (task run or the .app bundle)
//   - SCREENPEAR_E2E=1
//
// Run:
//
//	SCREENPEAR_E2E=1 go test -v -tags gowda -run TestGowdaMaskFlow ./test/e2e/
func TestGowdaMaskFlow(t *testing.T) {
	ctx := context.Background()
	gs := setupGowdaSession(t)

	// Wait for the app to settle.
	time.Sleep(2 * time.Second)
	gs.capture("01-launched")

	// Step 1: Load image via API.
	fixture := testdata("input", "nutcorp-01-input.png")
	if err := gs.apiPost("/api/open", map[string]string{"path": fixture}); err != nil {
		t.Fatalf("POST /api/open: %v", err)
	}
	t.Logf("loaded fixture: %s", fixture)

	// Wait for analysis to finish (lines appear in sidebar).
	time.Sleep(5 * time.Second)
	gs.capture("02-analyzed")

	// Step 2: Edit replacement rules in the TextEditor.
	// The rules card has a TextEditor; find it and replace its content.
	rulesEditor, err := gs.Select(
		gowda.ByClassName("TextEditor"),
		gowda.WithTimeout(5*time.Second),
	).Get(ctx)
	if err != nil {
		// Fall back to looking for a TextView.
		rulesEditor, err = gs.Select(
			gowda.ByClassName("TextView"),
			gowda.WithTimeout(3*time.Second),
		).Get(ctx)
		if err != nil {
			t.Fatalf("rules editor not found: %v", err)
		}
	}
	rulesEditor.Tap(ctx)
	time.Sleep(300 * time.Millisecond)

	rulesEditor.ClearText(ctx)
	time.Sleep(200 * time.Millisecond)

	rules := "NutCorp=SquirrelCorp\n" +
		"Nice Things=Expensive things\n" +
		"DevOps=SysAdmin\n" +
		"github=screenpear\n" +
		"Upgrade=update"
	rulesEditor.SetText(ctx, rules)
	time.Sleep(500 * time.Millisecond)
	gs.capture("03-rules-edited")

	// Step 3: Click Mask button.
	if err := gs.Select(
		gowda.ByName("Mask"),
		gowda.WithTimeout(3*time.Second),
	).Click(ctx); err != nil {
		t.Fatalf("Mask button not found: %v", err)
	}

	// Masking takes a few seconds (inpainting + redraw).
	time.Sleep(8 * time.Second)
	gs.capture("04-masked")

	// Verify the result card appeared.
	gs.assertVisible(ctx, "Reveal")
	gs.capture("05-result")
}

// TestGowdaOpenAndAnalyze verifies the basic open-analyze flow: load an image
// and confirm that detected text lines appear.
func TestGowdaOpenAndAnalyze(t *testing.T) {
	ctx := context.Background()
	gs := setupGowdaSession(t)

	time.Sleep(2 * time.Second)

	// The empty state shows "Drop a screenshot here".
	gs.assertVisible(ctx, "Drop a screenshot here")
	gs.capture("01-empty")

	fixture := testdata("input", "nutcorp-01-input.png")
	if err := gs.apiPost("/api/open", map[string]string{"path": fixture}); err != nil {
		t.Fatalf("POST /api/open: %v", err)
	}

	time.Sleep(5 * time.Second)
	gs.capture("02-analyzed")

	// The sidebar should show "Detected text" header and line count.
	gs.assertVisible(ctx, "DETECTED TEXT")
}

// TestGowdaSubstituteAll tests the substitute-all flow where every detected
// line gets randomized replacement text, even without rule matches.
func TestGowdaSubstituteAll(t *testing.T) {
	ctx := context.Background()
	gs := setupGowdaSession(t)

	time.Sleep(2 * time.Second)

	fixture := testdata("input", "nutcorp-01-input.png")
	if err := gs.apiPost("/api/open", map[string]string{"path": fixture}); err != nil {
		t.Fatalf("POST /api/open: %v", err)
	}
	time.Sleep(5 * time.Second)
	gs.capture("01-analyzed")

	// Clear rules so nothing matches by dictionary.
	rulesEditor, err := gs.Select(
		gowda.ByClassName("TextEditor"),
		gowda.WithTimeout(5*time.Second),
	).Get(ctx)
	if err != nil {
		rulesEditor, err = gs.Select(
			gowda.ByClassName("TextView"),
			gowda.WithTimeout(3*time.Second),
		).Get(ctx)
		if err != nil {
			t.Fatalf("rules editor not found: %v", err)
		}
	}
	rulesEditor.Tap(ctx)
	time.Sleep(200 * time.Millisecond)
	rulesEditor.ClearText(ctx)
	time.Sleep(200 * time.Millisecond)
	gs.capture("02-rules-cleared")

	if err := gs.Select(
		gowda.ByName("Mask"),
		gowda.WithTimeout(3*time.Second),
	).Click(ctx); err != nil {
		t.Fatalf("Mask button not found: %v", err)
	}

	time.Sleep(8 * time.Second)
	gs.capture("03-masked")
}
