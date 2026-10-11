package main

import (
	"os"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// mainWindow abstracts the two kinds of main window: the Wails webview
// (Linux, Windows, and macOS when the native helper is not bundled) and the
// out-of-process SwiftUI window on macOS. Tray and menu code only needs
// show/hide/toggle.
type mainWindow interface {
	Show()
	Hide()
	Toggle()
	Close()
}

var (
	mainWindowMu  sync.Mutex
	mainWindowCur mainWindow
)

func currentMainWindow() mainWindow {
	mainWindowMu.Lock()
	defer mainWindowMu.Unlock()
	return mainWindowCur
}

// newMainWindow picks the native window when this build ships one (macOS
// .app bundle with the SwiftUI helper) and the webview otherwise.
// SCREENPEAR_WEBVIEW=1 forces the webview, which is how the macOS webview
// path stays testable.
func newMainWindow(app *application.App, apiPort int) mainWindow {
	var w mainWindow
	if os.Getenv("SCREENPEAR_WEBVIEW") == "" {
		w = newNativeMainWindow(apiPort)
	}
	if w == nil {
		w = newWebviewMainWindow(app)
	}
	mainWindowMu.Lock()
	mainWindowCur = w
	mainWindowMu.Unlock()
	return w
}

// --- webview window ---

type webviewMainWindow struct {
	w application.Window
}

func newWebviewMainWindow(app *application.App) *webviewMainWindow {
	options := application.WebviewWindowOptions{
		Title:            appName,
		Width:            1100,
		Height:           760,
		MinWidth:         820,
		MinHeight:        560,
		BackgroundColour: application.NewRGB(0x0f, 0x10, 0x14),
		URL:              "/",
	}
	applyPlatformWindowOptions(&options)
	w := app.Window.NewWithOptions(options)

	// Tray app: closing hides. Quit lives in the tray and app menus.
	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		w.Hide()
		e.Cancel()
	})
	return &webviewMainWindow{w: w}
}

func (m *webviewMainWindow) Show() { m.w.Show(); m.w.Focus() }
func (m *webviewMainWindow) Hide() { m.w.Hide() }
func (m *webviewMainWindow) Toggle() {
	if m.w.IsVisible() {
		m.Hide()
	} else {
		m.Show()
	}
}
func (m *webviewMainWindow) Close() {}
