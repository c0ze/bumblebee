package disk_test

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c0ze/bumblebee/cache"
	"github.com/c0ze/bumblebee/cache/disk"
)

func newStore(t *testing.T, maxBytes int64) *disk.Store {
	t.Helper()
	s, err := disk.New(t.TempDir(), maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func put(t *testing.T, s *disk.Store, key, route, ct string, ttl time.Duration, body string) {
	t.Helper()
	if err := s.Put(cache.PutReq{Key: cache.Key(key), Route: route, ContentType: ct, TTL: ttl, Data: strings.NewReader(body)}); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, s *disk.Store, key string) (string, cache.Meta, bool) {
	t.Helper()
	rc, m, hit := s.Get(cache.Key(key))
	if !hit {
		return "", m, false
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return string(b), m, true
}

func TestDiskHitMissMeta(t *testing.T) {
	s := newStore(t, 1<<20)
	if _, _, hit := s.Get("absent"); hit {
		t.Fatal("expected miss")
	}
	put(t, s, "k1", "/r", "video/mp4", 0, "hello")
	got, m, hit := read(t, s, "k1")
	if !hit || got != "hello" || m.ContentType != "video/mp4" || m.Size != 5 {
		t.Fatalf("got=%q meta=%+v hit=%v", got, m, hit)
	}
}

func TestDiskTTL(t *testing.T) {
	s := newStore(t, 1<<20)
	put(t, s, "k", "/r", "", 20*time.Millisecond, "x")
	if _, _, hit := s.Get("k"); !hit {
		t.Fatal("hit before ttl")
	}
	time.Sleep(40 * time.Millisecond)
	if _, _, hit := s.Get("k"); hit {
		t.Fatal("miss after ttl")
	}
}

func TestDiskLRUEviction(t *testing.T) {
	s := newStore(t, 10)
	put(t, s, "a", "/r", "", 0, "12345")
	put(t, s, "b", "/r", "", 0, "12345")
	_, _, _ = read(t, s, "a") // touch a -> b is LRU
	put(t, s, "c", "/r", "", 0, "12345")
	if _, _, hit := s.Get("b"); hit {
		t.Fatal("b should be evicted")
	}
	if _, _, hit := s.Get("a"); !hit {
		t.Fatal("a should remain")
	}
}

func TestDiskIndexRebuild(t *testing.T) {
	dir := t.TempDir()
	s1, err := disk.New(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Put(cache.PutReq{Key: "k", Route: "/r", ContentType: "text/plain", Data: strings.NewReader("persisted")}); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	s2, err := disk.New(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, m, hit := read(t, s2, "k")
	if !hit || got != "persisted" || m.ContentType != "text/plain" {
		t.Fatalf("rebuild: got=%q meta=%+v hit=%v", got, m, hit)
	}
}

func TestDiskConcurrentNoRace(t *testing.T) {
	s := newStore(t, 1<<16)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				k := cache.Key(string(rune('a' + (j % 8))))
				_ = s.Put(cache.PutReq{Key: k, Route: "/r", Data: bytes.NewReader([]byte("payload"))})
				if rc, _, hit := s.Get(k); hit {
					io.Copy(io.Discard, rc)
					rc.Close()
				}
				_ = s.Snapshot()
				if j%200 == 0 {
					s.Purge("")
				}
			}
		}()
	}
	wg.Wait()
}
