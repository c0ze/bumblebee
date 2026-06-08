package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c0ze/bumblebee/internal/config"
)

const sample = `
server:
  addr: ":9090"
  auth_token: ${TEST_TOKEN}
cache:
  default_backend: memory
  memory: { max_bytes: 1MB }
routes:
  - path: /tts
    upstream:
      method: POST
      url: "http://{host}:5002/api/tts"
      pool: [a, b]
      forward_body: true
      forward_headers: [X-Voice]
      timeout: 30s
      retries: 2
    cache:
      backend: memory
      ttl: 24h
      key_headers: [X-Voice]
    pipeline:
      - type: passthrough
`

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	os.Setenv("TEST_TOKEN", "secret")
	cfg, err := config.Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":9090" || cfg.Server.AuthToken != "secret" {
		t.Fatalf("server: %+v", cfg.Server)
	}
	if int64(cfg.Cache.Memory.MaxBytes) != 1<<20 {
		t.Fatalf("max_bytes: %d (want %d)", cfg.Cache.Memory.MaxBytes, int64(1<<20))
	}
	r := cfg.Routes[0]
	if r.Path != "/tts" || len(r.Upstream.Pool) != 2 || time.Duration(r.Upstream.Timeout) != 30*time.Second {
		t.Fatalf("route: %+v", r)
	}
	if time.Duration(r.Cache.TTL) != 24*time.Hour {
		t.Fatalf("ttl: %v", r.Cache.TTL)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []string{
		"routes: [{path: '', upstream: {url: x, pool: [a]}, pipeline: [{type: passthrough}]}]",
		"routes: [{path: /x, upstream: {url: '', pool: [a]}, pipeline: [{type: passthrough}]}]",
		"routes: [{path: /x, upstream: {url: y, pool: []}, pipeline: [{type: passthrough}]}]",
		"routes: [{path: /x, upstream: {url: y, pool: [a]}, pipeline: []}]",
	}
	for i, c := range cases {
		if _, err := config.Load(write(t, c)); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestDefaultTimeout(t *testing.T) {
	// A route with no explicit timeout should get the 30s default.
	body := `
routes:
  - path: /x
    upstream:
      url: "http://example.com"
      pool: [a]
    pipeline:
      - type: passthrough
`
	cfg, err := config.Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.Routes[0].Upstream.Timeout) != 30*time.Second {
		t.Fatalf("want 30s default timeout, got %v", time.Duration(cfg.Routes[0].Upstream.Timeout))
	}
}

func TestDiskBackendConfig(t *testing.T) {
	body := `
cache:
  default_backend: memory
  memory: { max_bytes: 1MB }
  disk: { dir: /tmp/bb-cache, max_bytes: 2GB }
routes:
  - path: /v/*
    upstream: { url: "http://o/{path}", pool: [a] }
    cache: { backend: disk, ttl: 1h }
    pipeline: [{ type: passthrough }]
`
	cfg, err := config.Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cache.Disk.Dir != "/tmp/bb-cache" || int64(cfg.Cache.Disk.MaxBytes) != 2<<30 {
		t.Fatalf("disk cfg: %+v", cfg.Cache.Disk)
	}
	if cfg.Routes[0].Cache.Backend != "disk" {
		t.Fatalf("backend: %s", cfg.Routes[0].Cache.Backend)
	}
}

func TestDiskBackendRequiresDir(t *testing.T) {
	body := `
cache: { default_backend: memory, memory: { max_bytes: 1MB } }
routes:
  - path: /v/*
    upstream: { url: "http://o/{path}", pool: [a] }
    cache: { backend: disk, ttl: 1h }
    pipeline: [{ type: passthrough }]
`
	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatal("expected error: disk backend without cache.disk.dir")
	}
}
