//go:build gui

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/automationd/screenpear/services"
)

type runningApp struct {
	dir  string // result dir for this run
	base string // API base URL
}

// launchApp starts bin/screenpear with isolated config/temp dirs and the
// container WebKit settings, waits for the API port file, and registers
// shutdown. Skips when the binary is not built.
func launchApp(t *testing.T) *runningApp {
	t.Helper()
	binary := filepath.Join(projectRoot(), "bin", "screenpear")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skipf("binary not built (%v); run `task build` first", err)
	}
	dir := resultDir(t)

	// Isolated config/temp dirs so the port file is this run's, not a stale
	// one from a developer session.
	home := filepath.Join(dir, "home")
	tmp := filepath.Join(dir, "tmp")
	for _, d := range []string{home, tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	portFile := filepath.Join(tmp, "com.automationd.screenpear", "api-port")

	logFile, err := os.Create(filepath.Join(dir, "screenpear.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logFile.Close() })

	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(),
		"SCREENPEAR_DEBUG=1",
		"SCREENPEAR_WEBVIEW=1",
		"XDG_CONFIG_HOME="+home,
		"TMPDIR="+tmp,
		"MESA_LOADER_DRIVER_OVERRIDE=swrast",
		"LIBGL_ALWAYS_SOFTWARE=1",
		// Container/CI WebKitGTK: no xdg-dbus-proxy for the bwrap sandbox, no
		// GPU, no a11y bus. Without these the web process aborts on first
		// evaluate_javascript. Harmless on a real desktop.
		"GTK_A11Y=none",
		"WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1",
		"WEBKIT_DISABLE_DMABUF_RENDERER=1",
		"WEBKIT_DISABLE_COMPOSITING_MODE=1",
	)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	})

	port := waitForPort(t, portFile, 30*time.Second)
	app := &runningApp{dir: dir, base: "http://127.0.0.1:" + strconv.Itoa(port)}
	t.Logf("api at %s", app.base)
	return app
}

// windowID finds the main window through xdotool by its title.
func (a *runningApp) windowID(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^Screenpear$").Output()
		if err == nil {
			if ids := strings.Fields(string(out)); len(ids) > 0 {
				return ids[len(ids)-1]
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Screenpear window not found")
	return ""
}

// shot captures the app window into <result dir>/<step>.png. Every GUI test
// calls it at least once so each run leaves a picture of what the app showed.
// Skips quietly when there is no display tooling (CI without X11).
func (a *runningApp) shot(t *testing.T, step string) {
	t.Helper()
	for _, tool := range []string{"xdotool", "import"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Logf("screenshot %s skipped: %s not available", step, tool)
			return
		}
	}
	if os.Getenv("DISPLAY") == "" {
		t.Logf("screenshot %s skipped: no DISPLAY", step)
		return
	}
	id := a.windowID(t)
	out := filepath.Join(a.dir, step+".png")
	if msg, err := exec.Command("import", "-window", id, out).CombinedOutput(); err != nil {
		t.Fatalf("screenshot %s: %v: %s", step, err, msg)
	}
	t.Logf("screenshot -> %s", out)
}

// TestGUI drives the loopback API of the running binary: health, analyze,
// mask and the image preview route the SwiftUI window relies on.
//
//	task build && SCREENPEAR_E2E=1 go test -v -tags gui -run TestGUI ./test/e2e/
func TestGUI(t *testing.T) {
	skipUnlessE2E(t)
	app := launchApp(t)
	base, dir := app.base, app.dir
	time.Sleep(2 * time.Second) // first paint + engine init
	app.shot(t, "01-launched")

	var health map[string]any
	getJSON(t, base+"/api/health", &health)
	if health["ok"] != true {
		t.Fatalf("health: %v", health)
	}

	fixture := testdata("input", "nutcorp-01-input.png")
	var analysis services.Analysis
	postJSON(t, base+"/api/analyze", map[string]string{"path": fixture}, &analysis)
	if len(analysis.Lines) == 0 {
		t.Fatal("analyze returned no lines")
	}
	writeJSON(t, filepath.Join(dir, "analyze.json"), analysis)

	var masked services.MaskResult
	postJSON(t, base+"/api/mask", services.MaskRequest{
		Path:   fixture,
		Output: filepath.Join(dir, "nutcorp-01-output.png"),
		Rules:  services.NewOCRService(services.DefaultOptions(), nil).DefaultRules(),
		Lines:  analysis.Lines,
	}, &masked)
	if len(masked.Replaced) == 0 {
		t.Error("mask replaced nothing")
	}
	writeJSON(t, filepath.Join(dir, "mask.json"), masked)

	res, err := http.Get(base + "/api/image?path=" + fixture)
	if err != nil || res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "image/") {
		t.Fatalf("image route: err=%v status=%v type=%q", err, res.StatusCode, res.Header.Get("Content-Type"))
	}
	app.shot(t, "02-after-api-calls")
}

func waitForPort(t *testing.T, portFile string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(portFile); err == nil {
			if p, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && p > 0 {
				return p
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("port file %s not written within %s", portFile, timeout)
	return 0
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("GET %s: decode: %v", url, err)
	}
}

func postJSON(t *testing.T, url string, body, v any) {
	t.Helper()
	b, _ := json.Marshal(body)
	client := &http.Client{Timeout: 10 * time.Minute}
	res, err := client.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		var e map[string]string
		_ = json.NewDecoder(res.Body).Decode(&e)
		t.Fatalf("POST %s: %d %s", url, res.StatusCode, fmt.Sprint(e))
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("POST %s: decode: %v", url, err)
	}
}

// TestDemo is the scripted Linux demo: launch the app, push a fixture into
// the window through POST /api/open, mask it with the keyboard shortcut, and
// store a screenshot of the window after each step (01-empty, 02-analyzed,
// 03-masked). Needs an X display and xdotool.
func TestDemo(t *testing.T) {
	skipUnlessE2E(t)
	if _, err := exec.LookPath("xdotool"); err != nil || os.Getenv("DISPLAY") == "" {
		t.Skip("needs an X display and xdotool")
	}
	app := launchApp(t)
	fixture := testdata("input", "nutcorp-01-input.png")

	time.Sleep(2 * time.Second) // let the first paint and the engine init settle
	app.shot(t, "01-empty")

	postJSON(t, app.base+"/api/open", map[string]string{"path": fixture}, &map[string]any{})
	time.Sleep(4 * time.Second) // load + OCR + overlay
	app.shot(t, "02-analyzed")

	if out, err := exec.Command("xdotool", "windowactivate", "--sync", app.windowID(t), "key", "ctrl+Return").CombinedOutput(); err != nil {
		t.Fatalf("xdotool: %v: %s", err, out)
	}
	time.Sleep(3 * time.Second)
	app.shot(t, "03-masked")

	// The masked PNG lands beside the input; keep a copy with the run.
	out := filepath.Join(filepath.Dir(fixture), "nutcorp-01-output.png")
	if data, err := os.ReadFile(out); err == nil {
		_ = os.WriteFile(filepath.Join(app.dir, "nutcorp-01-output.png"), data, 0o644)
		_ = os.Remove(out)
	} else {
		t.Errorf("masked output not written: %v", err)
	}
}
