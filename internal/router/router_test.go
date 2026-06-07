package router_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c0ze/bumblebee/internal/config"
	"github.com/c0ze/bumblebee/internal/router"
	_ "github.com/c0ze/bumblebee/transform/passthrough"
)

func newServer(t *testing.T, upstreamURL, authToken string) (*httptest.Server, func()) {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{Addr: ":0", AuthToken: authToken},
		Cache:  config.CacheConfig{DefaultBackend: "memory"},
		Routes: []config.RouteConfig{{
			Path: "/echo/*",
			Upstream: config.UpstreamConfig{
				Method: "GET",
				URL:    upstreamURL + "/{path}",
				Pool:   []string{"ignored"},
			},
			Cache:    config.RouteCache{Backend: "memory", KeyQuery: []string{"v"}},
			Pipeline: []config.StageConfig{{Type: "passthrough"}},
		}},
	}
	cfg.Cache.Memory.MaxBytes = 1 << 20
	h, cleanup, err := router.New(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	return ts, func() { ts.Close(); cleanup() }
}

func TestCacheHitMiss(t *testing.T) {
	var calls int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "payload")
	}))
	defer origin.Close()

	ts, cleanup := newServer(t, origin.URL, "")
	defer cleanup()

	// First request: MISS
	resp, _ := http.Get(ts.URL + "/echo/a?v=1")
	if resp.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("want MISS, got %q", resp.Header.Get("X-Cache"))
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "payload" || resp.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("body=%q ct=%q", b, resp.Header.Get("Content-Type"))
	}

	// Second identical request: HIT (origin not called again)
	resp2, _ := http.Get(ts.URL + "/echo/a?v=1")
	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("want HIT, got %q", resp2.Header.Get("X-Cache"))
	}
	resp2.Body.Close()

	// Different key_query value: MISS again
	resp3, _ := http.Get(ts.URL + "/echo/a?v=2")
	if resp3.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("want MISS for v=2, got %q", resp3.Header.Get("X-Cache"))
	}
	resp3.Body.Close()

	if calls != 2 {
		t.Fatalf("origin called %d times, want 2", calls)
	}
}

func TestNon2xxNotCached(t *testing.T) {
	var calls int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer origin.Close()

	ts, cleanup := newServer(t, origin.URL, "")
	defer cleanup()

	resp, _ := http.Get(ts.URL + "/echo/a?v=1")
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Cache") == "HIT" {
		t.Fatal("503 response should not be cached (got HIT)")
	}

	// Second request must hit origin again (not served from cache).
	resp2, _ := http.Get(ts.URL + "/echo/a?v=1")
	resp2.Body.Close()
	if calls != 2 {
		t.Fatalf("origin called %d times, want 2 (non-2xx must not be cached)", calls)
	}
}

func TestPurge(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "hello")
	}))
	defer origin.Close()

	ts, cleanup := newServer(t, origin.URL, "")
	defer cleanup()

	// Populate cache.
	resp, _ := http.Get(ts.URL + "/echo/a?v=1")
	resp.Body.Close()
	if resp.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("first request: want MISS, got %q", resp.Header.Get("X-Cache"))
	}

	// Confirm it is cached.
	resp2, _ := http.Get(ts.URL + "/echo/a?v=1")
	resp2.Body.Close()
	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("second request: want HIT, got %q", resp2.Header.Get("X-Cache"))
	}

	// Purge the cache (no auth token on this server).
	presp, _ := http.Post(ts.URL+"/cache/purge", "", nil)
	presp.Body.Close()
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("purge: want 200, got %d", presp.StatusCode)
	}

	// After purge the same request must be a MISS again.
	resp3, _ := http.Get(ts.URL + "/echo/a?v=1")
	resp3.Body.Close()
	if resp3.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("after purge: want MISS, got %q", resp3.Header.Get("X-Cache"))
	}
}

func TestStatsAuth(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer origin.Close()
	ts, cleanup := newServer(t, origin.URL, "sek")
	defer cleanup()

	resp, _ := http.Get(ts.URL + "/stats")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: want 401, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/stats", nil)
	req.Header.Set("Authorization", "Bearer sek")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("with token: want 200, got %d", resp2.StatusCode)
	}
	body, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body), "\"build_version\"") {
		t.Fatalf("stats body missing build_version: %s", body)
	}
}
