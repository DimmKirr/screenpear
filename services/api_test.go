package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These run without the OCR runtime: they only touch routes that never reach
// the engine, so they are safe in `go test ./...` on a toolchain-only CI box.
func TestAPIHealthAndRules(t *testing.T) {
	api := NewAPI(NewOCRService(DefaultOptions(), nil), "com.test.screenpear", "test")
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]any
	_ = json.NewDecoder(res.Body).Decode(&health)
	if health["ok"] != true || health["version"] != "test" {
		t.Fatalf("health = %v", health)
	}

	res, err = http.Get(srv.URL + "/api/rules")
	if err != nil {
		t.Fatal(err)
	}
	var rules []map[string]string
	_ = json.NewDecoder(res.Body).Decode(&rules)
	if len(rules) == 0 || rules[0]["from"] != "NutCorp" {
		t.Fatalf("rules = %v", rules)
	}
}

func TestAPIRejectsBadInput(t *testing.T) {
	api := NewAPI(NewOCRService(DefaultOptions(), nil), "com.test.screenpear", "test")
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	res, _ := http.Get(srv.URL + "/api/image?path=relative.png")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("relative image path: status %d", res.StatusCode)
	}
	res, _ = http.Post(srv.URL+"/api/mask", "application/json", strings.NewReader(`{}`))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty mask request: status %d", res.StatusCode)
	}
	res, _ = http.Post(srv.URL+"/api/analyze", "application/json", strings.NewReader(`{"path":"/nonexistent/x.png"}`))
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file: status %d", res.StatusCode)
	}
}

func TestDefaultOutputPath(t *testing.T) {
	cases := map[string]string{
		"/x/nutcorp-01-input.png": "/x/nutcorp-01-output.png",
		"/x/shot.jpg":             "/x/shot-masked.png",
	}
	for in, want := range cases {
		if got := DefaultOutputPath(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}
