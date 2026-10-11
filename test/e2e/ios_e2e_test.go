package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIOS builds the native iOS app, installs it in the booted simulator,
// launches it with a fixture through the -ScreenpearDemoImage launch
// argument, and screenshots the simulator after the analyze and mask steps
// into test/results/<ts>-TestIOS/. Relaunching per step is deliberate:
// simctl can launch and capture but has no tap primitive, and XCUITest is a
// whole extra target for two screenshots.
//
//	task test:ios          # macOS with Xcode and a booted simulator
//
// Skips rather than fails when it cannot run (Linux CI, no Xcode, no
// simulator), so it is safe in `go test ./...`.
func TestIOS(t *testing.T) {
	skipUnlessE2E(t)
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("xcrun not available (needs macOS with Xcode)")
	}
	if out, err := exec.Command("xcrun", "simctl", "list", "devices", "booted").Output(); err != nil || !strings.Contains(string(out), "Booted") {
		t.Skip("no booted iOS simulator; run `task ios:dev` first")
	}
	dir := resultDir(t)
	root := projectRoot()

	build := exec.Command("task", "ios:bundle")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("task ios:bundle: %v\n%s", err, out)
	}
	app := filepath.Join(root, "bin", "Screenpear-iOS.app")
	const bundleID = "com.automationd.screenpear"
	if out, err := exec.Command("xcrun", "simctl", "install", "booted", app).CombinedOutput(); err != nil {
		t.Fatalf("simctl install: %v\n%s", err, out)
	}

	// The simulator shares the host filesystem, so the fixture path works
	// as-is; the app copies nothing.
	fixture := testdata("input", "nutcorp-01-input.png")
	for _, step := range []string{"analyzed", "masked"} {
		_ = exec.Command("xcrun", "simctl", "terminate", "booted", bundleID).Run()
		if out, err := exec.Command("xcrun", "simctl", "launch", "booted", bundleID,
			"-ScreenpearDemoImage", fixture, "-ScreenpearDemoStep", step).CombinedOutput(); err != nil {
			t.Fatalf("simctl launch (%s): %v\n%s", step, err, out)
		}
		time.Sleep(4 * time.Second) // Vision + mask + render
		shot := filepath.Join(dir, step+".png")
		if out, err := exec.Command("xcrun", "simctl", "io", "booted", "screenshot", shot).CombinedOutput(); err != nil {
			t.Fatalf("screenshot (%s): %v\n%s", step, err, out)
		}
		if info, err := os.Stat(shot); err != nil || info.Size() == 0 {
			t.Fatalf("screenshot %s not written", shot)
		}
		t.Logf("screenshot -> %s", shot)
	}
	_ = exec.Command("xcrun", "simctl", "terminate", "booted", bundleID).Run()
}
