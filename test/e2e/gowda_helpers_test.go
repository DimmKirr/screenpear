//go:build gowda

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimmkirr/gowda"
)

const (
	defaultWDAURL      = "http://localhost:8100"
	screenpearBundleID = "com.automationd.screenpear.ui"
)

func wdaURL() string {
	if u := os.Getenv("WDA_URL"); u != "" {
		return u
	}
	return defaultWDAURL
}

func screenpearBundle() string {
	if b := os.Getenv("BUNDLE_ID"); b != "" {
		return b
	}
	return screenpearBundleID
}

func screenpearAPIBase() string {
	if u := os.Getenv("SCREENPEAR_API"); u != "" {
		return u
	}
	port := resolveAPIPort()
	if port == "" {
		return ""
	}
	return "http://127.0.0.1:" + port
}

func resolveAPIPort() string {
	if p := os.Getenv("SCREENPEAR_API_PORT"); p != "" {
		return p
	}
	id := "com.automationd.screenpear"
	candidates := []string{}
	if home, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, id, "api-port"))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), id, "api-port"))
	for _, path := range candidates {
		if b, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

type gowdaSession struct {
	*gowda.Client
	t    *testing.T
	dir  string
	seq  int
	base string // API base URL
}

func setupGowdaSession(t *testing.T) *gowdaSession {
	t.Helper()
	skipUnlessE2E(t)

	ctx := context.Background()
	client := gowda.NewClient(wdaURL())
	if !client.WaitReady(ctx, 10*time.Second) {
		t.Skip("WDA not running; start WebDriverAgent on your Mac first")
	}

	session, err := client.Session(ctx,
		gowda.WithBundleID(screenpearBundle()),
		gowda.WithShouldWaitForQuiescence(false),
	)
	if err != nil {
		t.Fatalf("gowda session: %v", err)
	}

	dir := filepath.Join("results", time.Now().Format("2006-01-02T15-04-05")+"-"+t.Name())
	os.MkdirAll(dir, 0o755)

	base := screenpearAPIBase()
	if base == "" {
		t.Fatal("cannot resolve Screenpear API port; is the app running?")
	}

	gs := &gowdaSession{Client: session, t: t, dir: dir, base: base}

	t.Cleanup(func() {
		if t.Failed() {
			gs.capture("failure")
		}
		session.Close(ctx)
	})

	return gs
}

func (gs *gowdaSession) capture(name string) {
	gs.seq++
	data, err := gs.Screenshot(context.Background())
	if err != nil {
		gs.t.Logf("screenshot %s failed: %v", name, err)
		return
	}
	filename := fmt.Sprintf("%02d-%s.png", gs.seq, name)
	path := filepath.Join(gs.dir, filename)
	os.WriteFile(path, data, 0o644)
	gs.t.Logf("screenshot: %s", path)
}

func (gs *gowdaSession) assertVisible(ctx context.Context, name string) {
	gs.t.Helper()
	_, err := gs.Select(gowda.ByName(name), gowda.WithTimeout(5*time.Second)).Get(ctx)
	if err != nil {
		gs.t.Errorf("expected %q to be visible", name)
	}
}

func (gs *gowdaSession) apiPost(path string, body any) error {
	b, _ := json.Marshal(body)
	client := &http.Client{Timeout: 10 * time.Minute}
	res, err := client.Post(gs.base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		var e map[string]string
		_ = json.NewDecoder(res.Body).Decode(&e)
		return fmt.Errorf("%s: %d %v", path, res.StatusCode, e)
	}
	return nil
}

func (gs *gowdaSession) apiGet(path string) error {
	res, err := http.Get(gs.base + path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("%s: status %d", path, res.StatusCode)
	}
	return nil
}
