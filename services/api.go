package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// API is the loopback HTTP face of OCRService.
//
// The macOS main window is a separate SwiftUI process (see
// build/macos-swiftui), so it cannot use Wails bindings; it talks to the Go
// process over this API instead, discovering the port through a port file
// the same way a companion app would. The e2e tests use the same surface, and
// the webview gets it mounted under /api/ by the asset middleware so image
// previews are plain <img src="/api/image?path=..."> tags.
type API struct {
	ocr        *OCRService
	identifier string
	version    string
	mux        *http.ServeMux
	listener   net.Listener

	// OnOpen is called by POST /api/open with an absolute image path. main
	// wires it to a Wails event the window listens to; nil means the route
	// answers 503. services/ stays free of the wails import this way.
	OnOpen func(path string)
}

// NewAPI wires the handlers. identifier is the app bundle id, used for the
// port file directory; version is reported by /api/health.
func NewAPI(ocr *OCRService, identifier, version string) *API {
	a := &API{ocr: ocr, identifier: identifier, version: version, mux: http.NewServeMux()}
	a.mux.HandleFunc("GET /api/health", a.health)
	a.mux.HandleFunc("GET /api/rules", a.rules)
	a.mux.HandleFunc("GET /api/image", a.image)
	a.mux.HandleFunc("POST /api/analyze", a.analyze)
	a.mux.HandleFunc("POST /api/mask", a.mask)
	a.mux.HandleFunc("POST /api/open", a.open)
	return a
}

// Handler exposes the routes for mounting inside another server (the Wails
// asset middleware) and for httptest.
func (a *API) Handler() http.Handler { return a.mux }

// Start listens on an ephemeral loopback port and serves in the background.
// It returns the base URL, e.g. http://127.0.0.1:49152.
func (a *API) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("api: listen: %w", err)
	}
	a.listener = ln
	go func() {
		if err := http.Serve(ln, a.mux); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("api: serve: %v", err)
		}
	}()
	return "http://" + ln.Addr().String(), nil
}

// Port is the bound port, or 0 before Start.
func (a *API) Port() int {
	if a.listener == nil {
		return 0
	}
	return a.listener.Addr().(*net.TCPAddr).Port
}

// Close stops the listener and removes the port files.
func (a *API) Close() error {
	for _, dir := range a.portDirs() {
		_ = os.Remove(filepath.Join(dir, "api-port"))
	}
	if a.listener != nil {
		return a.listener.Close()
	}
	return nil
}

// WritePortFile persists the port so out-of-process companions (the SwiftUI
// window) can find the API without arguments. Written to both the user
// config dir and the temp dir: a helper launched via `open` may resolve
// Application Support differently from the Go process.
func (a *API) WritePortFile() {
	port := strconv.Itoa(a.Port())
	for _, dir := range a.portDirs() {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Printf("api: port file: mkdir %s: %v", dir, err)
			continue
		}
		p := filepath.Join(dir, "api-port")
		if err := os.WriteFile(p, []byte(port), 0o644); err != nil {
			log.Printf("api: port file: write %s: %v", p, err)
		}
	}
}

func (a *API) portDirs() []string {
	dirs := []string{filepath.Join(os.TempDir(), a.identifier)}
	if base, err := os.UserConfigDir(); err == nil {
		dirs = append([]string{filepath.Join(base, a.identifier)}, dirs...)
	}
	return dirs
}

// --- handlers ---

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": a.version})
}

func (a *API) rules(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.ocr.DefaultRules())
}

// image serves a local file for preview. The API only listens on loopback
// and runs as the user, so this exposes nothing the user cannot already
// open; it still refuses relative paths so a URL can never resolve against
// the process working directory.
func (a *API) image(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" || !filepath.IsAbs(p) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}
	http.ServeFile(w, r, p)
}

func (a *API) analyze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res, err := a.ocr.analyze(r.Context(), req.Path)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) mask(w http.ResponseWriter, r *http.Request) {
	var req MaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res, err := a.ocr.Mask(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// open pushes an image into the main window (same contract as a drop).
func (a *API) open(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Path == "" || !filepath.IsAbs(req.Path) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}
	if a.OnOpen == nil {
		http.Error(w, "no window attached", http.StatusServiceUnavailable)
		return
	}
	a.OnOpen(req.Path)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, os.ErrNotExist):
		status = http.StatusNotFound
	case strings.Contains(err.Error(), "is required"):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
