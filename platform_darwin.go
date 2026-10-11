//go:build darwin && !ios

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// macOS: nothing to tune at app level (WKWebView ships with the OS). The
// webview window, used only when the SwiftUI helper is not bundled, gets a
// hidden-inset title bar and translucent backdrop to sit close to the native
// look.
func applyPlatformAppOptions(_ *application.Options) {}

func applyPlatformWindowOptions(options *application.WebviewWindowOptions) {
	options.Mac = application.MacWindow{
		TitleBar:                application.MacTitleBarHiddenInset,
		InvisibleTitleBarHeight: 44,
		Backdrop:                application.MacBackdropTranslucent,
	}
}

// nativeMainWindow is the SwiftUI window (build/macos-swiftui), shipped as
// Contents/Helpers/ScreenpearUI.app inside the main bundle. A bare SwiftUI
// binary gets SIGTRAP without a bundle context, so it is always a .app.
//
// It is a separate process that reads the API port from --args; the Go side
// only launches, focuses and kills it.
type nativeMainWindow struct {
	bundle  string
	apiPort int

	mu   sync.Mutex
	proc *os.Process
}

const helperName = "ScreenpearUI"

// resolveHelperBundle returns Contents/Helpers/ScreenpearUI.app when this
// executable runs from a bundle that ships it, "" otherwise.
func resolveHelperBundle() string {
	if p := os.Getenv("SCREENPEAR_UI_BUNDLE"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// exe is Contents/MacOS/screenpear; the helper lives at Contents/Helpers/.
	contents := filepath.Dir(filepath.Dir(exe))
	candidate := filepath.Join(contents, "Helpers", helperName+".app")
	if _, err := os.Stat(filepath.Join(candidate, "Contents", "MacOS", helperName)); err == nil {
		return candidate
	}
	return ""
}

func newNativeMainWindow(apiPort int) mainWindow {
	bundle := resolveHelperBundle()
	if bundle == "" {
		log.Printf("window: %s.app not bundled; using webview", helperName)
		return nil
	}
	n := &nativeMainWindow{bundle: bundle, apiPort: apiPort}
	n.Show()
	return n
}

func (n *nativeMainWindow) running() bool {
	if n.proc == nil {
		return false
	}
	if err := n.proc.Signal(os.Signal(nil)); err != nil {
		n.proc = nil
		return false
	}
	return true
}

// Show launches the helper, or brings the running instance to front.
func (n *nativeMainWindow) Show() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.running() {
		_ = exec.Command("open", "-a", n.bundle).Run()
		return
	}
	// -n forces a new instance so stale registrations don't block the launch,
	// -W makes `open` wait so we learn when the window process exits.
	cmd := exec.Command("open", "-n", "-W", "-a", n.bundle, "--args", "--port", strconv.Itoa(n.apiPort))
	if err := cmd.Start(); err != nil {
		log.Printf("window: launch %s: %v", n.bundle, err)
		return
	}
	go func() {
		for i := 0; i < 30; i++ {
			if pid := findHelperPid(); pid > 0 {
				n.mu.Lock()
				n.proc, _ = os.FindProcess(pid)
				n.mu.Unlock()
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = cmd.Wait()
		n.mu.Lock()
		n.proc = nil
		n.mu.Unlock()
	}()
}

func (n *nativeMainWindow) Hide() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.running() {
		_ = exec.Command("osascript", "-e", fmt.Sprintf(`tell application "System Events" to set visible of process "%s" to false`, helperName)).Run()
	}
}

func (n *nativeMainWindow) Toggle() {
	n.mu.Lock()
	running := n.running()
	n.mu.Unlock()
	if running {
		n.Hide()
	} else {
		n.Show()
	}
}

func (n *nativeMainWindow) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.proc != nil {
		_ = n.proc.Kill()
		n.proc = nil
	}
}

func findHelperPid() int {
	out, err := exec.Command("pgrep", "-xn", helperName).Output()
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return pid
}
