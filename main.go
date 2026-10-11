// Screenpear: mask sensitive text in screenshots.
//
// Wails 3 shell. The OCR and masking live in services/ and are reached two
// ways: the webview (Linux, Windows) calls the generated bindings, and the
// native SwiftUI window (macOS, build/macos-swiftui) calls the loopback HTTP
// API. Both talk to the same OCRService, so results are identical.
//
// Dev:     task dev
// Build:   task build            -> bin/screenpear
// Package: task package          -> platform bundle/installer
package main

import (
	"embed"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/automationd/screenpear/services"
)

// The built web app is embedded into the binary. `task common:build:frontend`
// populates frontend/dist from the Vite build.
//
//go:embed all:frontend/dist
var assets embed.FS

const (
	appName       = "Screenpear"
	appIdentifier = "com.automationd.screenpear"
	// eventOpen carries an absolute image path to the webview.
	eventOpen = "open"
)

func init() {
	// Registered so the binding generator types the event for the frontend.
	application.RegisterEvent[string](eventOpen)
}

// Injected at link time by the Taskfile: -X main.version=<RFC3339>-<sha>.
// The default keeps `go build ./...` and `go test` working without flags.
var version = "dev"

func main() {
	logger := debugLogger()

	ocr := services.NewOCRService(services.DefaultOptions(), logger)

	api := services.NewAPI(ocr, appIdentifier, version)
	apiBase, err := api.Start()
	if err != nil {
		log.Fatalf("api: %v", err)
	}
	defer api.Close()
	api.WritePortFile()
	log.Printf("api: %s (port file written for companions)", apiBase)

	shell := NewShellService()

	options := application.Options{
		Name:        appName,
		Description: "Mask sensitive text in screenshots",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(ocr),
			application.NewService(shell),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: apiMiddleware(api.Handler()),
		},
		SingleInstance: singleInstanceOptions(),
		Mac: application.MacOptions{
			// Tray app: closing the window keeps the process alive.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
	}
	if logger != nil {
		options.Logger = logger
		options.LogLevel = slog.LevelDebug
	}
	applyPlatformAppOptions(&options)

	app := application.New(options)

	installAppMenu(app)
	window := newMainWindow(app, api.Port())
	installSystemTray(app, window)

	// POST /api/open -> the webview loads the file. The SwiftUI window polls
	// nothing: on macOS the helper owns its own open flow.
	api.OnOpen = func(path string) {
		app.Event.Emit(eventOpen, path)
		window.Show()
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// apiMiddleware mounts the loopback API inside the webview's asset server so
// the page can use "/api/image?path=..." for previews without knowing the
// port. Everything else falls through to the embedded frontend.
func apiMiddleware(api http.Handler) application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				api.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// debugLogger returns Wails' system logger at debug level when
// SCREENPEAR_DEBUG is set, and nil otherwise (Wails' default: info to stdout).
func debugLogger() *slog.Logger {
	if os.Getenv("SCREENPEAR_DEBUG") == "" {
		return nil
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// singleInstanceOptions returns the lock config when D-Bus is available
// (normal desktop), and nil in containers / headless CI where the session
// bus is absent and the lock would crash the app on startup.
func singleInstanceOptions() *application.SingleInstanceOptions {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		return nil
	}
	return &application.SingleInstanceOptions{
		UniqueID: appIdentifier,
		OnSecondInstanceLaunch: func(_ application.SecondInstanceData) {
			if w := currentMainWindow(); w != nil {
				w.Show()
			}
		},
	}
}
