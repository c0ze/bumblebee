package memory_test

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c0ze/bumblebee/cache"
	"github.com/c0ze/bumblebee/cache/memory"
)

func put(t *testing.T, s *memory.Store, key, route, ct string, ttl time.Duration, body string) {
	t.Helper()
	if err := s.Put(cache.PutReq{Key: cache.Key(key), Route: route, ContentType: ct, TTL: ttl, Data: strings.NewReader(body)}); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, s *memory.Store, key string) (string, cache.Meta, bool) {
	t.Helper()
	rc, m, hit := s.Get(cache.Key(key))
	if !hit {
		return "", m, false
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return string(b), m, true
}

func TestHitMissAndMeta(t *testing.T) {
	s := memory.New(1 << 20)
	defer s.Close()
	if _, _, hit := s.Get("absent"); hit {
		t.Fatal("expected miss")
	}
	put(t, s, "k1", "/r", "audio/mpeg", 0, "hello")
	got, m, hit := read(t, s, "k1")
	if !hit || got != "hello" || m.ContentType != "audio/mpeg" || m.Size != 5 {
		t.Fatalf("got=%q meta=%+v hit=%v", got, m, hit)
	}
}

func TestTTLExpiry(t *testing.T) {
	s := memory.New(1 << 20)
	defer s.Close()
	put(t, s, "k", "/r", "", 20*time.Millisecond, "x")
	if _, _, hit := s.Get("k"); !hit {
		t.Fatal("should hit before ttl")
	}
	time.Sleep(40 * time.Millisecond)
	if _, _, hit := s.Get("k"); hit {
		t.Fatal("should miss after ttl")
	}
}

func TestLRUByteEviction(t *testing.T) {
	s := memory.New(10) // 10 bytes
	defer s.Close()
	put(t, s, "a", "/r", "", 0, "12345") // 5
	put(t, s, "b", "/r", "", 0, "12345") // 5  -> total 10
	_, _, _ = read(t, s, "a")            // touch a so b is LRU
	put(t, s, "c", "/r", "", 0, "12345") // evicts b
	if _, _, hit := s.Get("b"); hit {
		t.Fatal("b should have been evicted")
	}
	if _, _, hit := s.Get("a"); !hit {
		t.Fatal("a should remain")
	}
}

func TestScopedPurge(t *testing.T) {
	s := memory.New(1 << 20)
	defer s.Close()
	put(t, s, "k1", "/a", "", 0, "x")
	put(t, s, "k2", "/b", "", 0, "y")
	s.Purge("/a")
	if _, _, hit := s.Get("k1"); hit {
		t.Fatal("k1 (route /a) should be purged")
	}
	if _, _, hit := s.Get("k2"); !hit {
		t.Fatal("k2 (route /b) should remain")
	}
}

func TestConcurrentAccessNoRace(t *testing.T) {
	s := memory.New(1 << 16)
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				k := cache.Key(string(rune('a' + (j % 16))))
				_ = s.Put(cache.PutReq{Key: k, Route: "/r", Data: bytes.NewReader([]byte("payload"))})
				if rc, _, hit := s.Get(k); hit {
					io.Copy(io.Discard, rc)
					rc.Close()
				}
				_ = s.Snapshot()
				if j%500 == 0 {
					s.Purge("")
				}
			}
		}(i)
	}
	wg.Wait()
}
