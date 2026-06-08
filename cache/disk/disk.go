// Package disk is a file-backed, byte-bounded LRU cache owned by a single
// goroutine (the same channel-owned model as cache/memory). Objects are stored
// as files named by their key with a JSON ".meta" sidecar; the index is rebuilt
// from sidecars on startup. The large data copy happens in Put (caller
// goroutine); the owner goroutine only does the fast rename+sidecar+index.
package disk

import (
	"container/list"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c0ze/bumblebee/cache"
)

type meta struct {
	Route       string    `json:"route"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Expires     time.Time `json:"expires"` // zero = no expiry
}

type entry struct {
	key  cache.Key
	meta meta
	el   *list.Element
}

type getReq struct {
	key  cache.Key
	resp chan getResp
}
type getResp struct {
	f   *os.File
	ct  string
	sz  int64
	hit bool
}

type putReq struct {
	key     cache.Key
	tmpPath string
	m       meta
	resp    chan error
}

// Store is a disk-backed LRU cache. One goroutine owns all index state.
type Store struct {
	dir      string
	maxBytes int64
	curBytes int64
	items    map[cache.Key]*entry
	lru      *list.List
	hits     int64
	misses   int64

	getCh   chan getReq
	putCh   chan putReq
	purgeCh chan string
	snapCh  chan chan cache.Stats
	quit    chan struct{}
}

// New opens (creating if needed) a disk cache under dir, bounded to maxBytes.
func New(dir string, maxBytes int64) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		dir: dir, maxBytes: maxBytes,
		items: map[cache.Key]*entry{}, lru: list.New(),
		getCh: make(chan getReq), putCh: make(chan putReq),
		purgeCh: make(chan string), snapCh: make(chan chan cache.Stats),
		quit: make(chan struct{}),
	}
	if err := s.rebuild(); err != nil {
		return nil, err
	}
	go s.loop()
	return s, nil
}

func (s *Store) path(k cache.Key) string     { return filepath.Join(s.dir, string(k)) }
func (s *Store) metaPath(k cache.Key) string { return filepath.Join(s.dir, string(k)+".meta") }

func (s *Store) rebuild() error {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, de := range ents {
		name := de.Name()
		if strings.HasPrefix(name, "tmp-") {
			os.Remove(filepath.Join(s.dir, name)) // stale temp from a crash
			continue
		}
		if !strings.HasSuffix(name, ".meta") {
			continue
		}
		key := cache.Key(strings.TrimSuffix(name, ".meta"))
		raw, err := os.ReadFile(s.metaPath(key))
		if err != nil {
			continue
		}
		var m meta
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if _, err := os.Stat(s.path(key)); err != nil {
			continue // object file missing; skip stale sidecar
		}
		e := &entry{key: key, meta: m}
		e.el = s.lru.PushFront(e)
		s.items[key] = e
		s.curBytes += m.Size
	}
	s.evict()
	return nil
}

func (s *Store) loop() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case r := <-s.getCh:
			r.resp <- s.get(r.key)
		case p := <-s.putCh:
			p.resp <- s.finalize(p)
		case route := <-s.purgeCh:
			s.purge(route)
		case ch := <-s.snapCh:
			ch <- s.statsSnapshot("disk")
		case <-tick.C:
			s.sweep()
		case <-s.quit:
			return
		}
	}
}

func (s *Store) get(k cache.Key) getResp {
	e, ok := s.items[k]
	if !ok {
		s.misses++
		return getResp{}
	}
	if !e.meta.Expires.IsZero() && time.Now().After(e.meta.Expires) {
		s.remove(e)
		s.misses++
		return getResp{}
	}
	f, err := os.Open(s.path(k))
	if err != nil {
		s.remove(e)
		s.misses++
		return getResp{}
	}
	s.lru.MoveToFront(e.el)
	s.hits++
	return getResp{f: f, ct: e.meta.ContentType, sz: e.meta.Size, hit: true}
}

// finalize runs in the owner goroutine: rename the caller's temp file into place,
// write the sidecar, update the index. The large copy already happened in Put.
func (s *Store) finalize(p putReq) error {
	raw, _ := json.Marshal(p.m)
	if old, ok := s.items[p.key]; ok {
		s.remove(old)
	}
	if err := os.Rename(p.tmpPath, s.path(p.key)); err != nil {
		os.Remove(p.tmpPath)
		return err
	}
	if err := os.WriteFile(s.metaPath(p.key), raw, 0o644); err != nil {
		os.Remove(s.path(p.key))
		return err
	}
	e := &entry{key: p.key, meta: p.m}
	e.el = s.lru.PushFront(e)
	s.items[p.key] = e
	s.curBytes += p.m.Size
	s.evict()
	return nil
}

func (s *Store) evict() {
	for s.curBytes > s.maxBytes {
		back := s.lru.Back()
		if back == nil {
			return
		}
		s.remove(back.Value.(*entry))
	}
}

func (s *Store) remove(e *entry) {
	s.lru.Remove(e.el)
	delete(s.items, e.key)
	s.curBytes -= e.meta.Size
	os.Remove(s.path(e.key)) // open fds keep serving on Unix
	os.Remove(s.metaPath(e.key))
}

func (s *Store) purge(route string) {
	var toRemove []*entry
	for _, e := range s.items {
		if route == "" || e.meta.Route == route {
			toRemove = append(toRemove, e)
		}
	}
	for _, e := range toRemove {
		s.remove(e)
	}
}

func (s *Store) sweep() {
	now := time.Now()
	var toRemove []*entry
	for _, e := range s.items {
		if !e.meta.Expires.IsZero() && now.After(e.meta.Expires) {
			toRemove = append(toRemove, e)
		}
	}
	for _, e := range toRemove {
		s.remove(e)
	}
}

func (s *Store) statsSnapshot(backend string) cache.Stats {
	byRoute := map[string]cache.RouteStat{}
	for _, e := range s.items {
		r := byRoute[e.meta.Route]
		r.Entries++
		r.Bytes += e.meta.Size
		byRoute[e.meta.Route] = r
	}
	return cache.Stats{Backend: backend, Entries: len(s.items), Bytes: s.curBytes, Hits: s.hits, Misses: s.misses, ByRoute: byRoute}
}

// Get implements cache.Store.
func (s *Store) Get(k cache.Key) (io.ReadCloser, cache.Meta, bool) {
	resp := make(chan getResp)
	s.getCh <- getReq{key: k, resp: resp}
	r := <-resp
	if !r.hit {
		return nil, cache.Meta{}, false
	}
	return r.f, cache.Meta{ContentType: r.ct, Size: r.sz}, true
}

// Put copies Data to a temp file in the caller's goroutine (so large writes run
// concurrently), then hands the finalize step to the owner.
func (s *Store) Put(r cache.PutReq) error {
	tmp, err := os.CreateTemp(s.dir, "tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	n, err := io.Copy(tmp, r.Data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	m := meta{Route: r.Route, ContentType: r.ContentType, Size: n}
	if r.TTL > 0 {
		m.Expires = time.Now().Add(r.TTL)
	}
	resp := make(chan error)
	s.putCh <- putReq{key: r.Key, tmpPath: tmpPath, m: m, resp: resp}
	return <-resp
}

func (s *Store) Purge(route string)    { s.purgeCh <- route }
func (s *Store) Snapshot() cache.Stats { ch := make(chan cache.Stats); s.snapCh <- ch; return <-ch }

// Close stops the owner goroutine. The store must not be used afterward.
func (s *Store) Close() { close(s.quit) }

var _ cache.Store = (*Store)(nil)
