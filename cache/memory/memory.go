package memory

import (
	"bytes"
	"container/list"
	"io"
	"time"

	"github.com/c0ze/bumblebee/cache"
)

type entry struct {
	key     cache.Key
	route   string
	data    []byte // immutable after put
	ct      string
	size    int64
	expires time.Time // zero = no expiry
	el      *list.Element
}

type getReq struct {
	key  cache.Key
	resp chan getResp
}
type getResp struct {
	data []byte
	ct   string
	hit  bool
}

type putResp struct {
	err error
}
type putReq struct {
	req  cache.PutReq
	resp chan putResp
}

// Store is a byte-bounded LRU cache owned by a single goroutine.
type Store struct {
	maxBytes int64
	curBytes int64
	items    map[cache.Key]*entry
	lru      *list.List // front = most recent
	hits     int64
	misses   int64

	getCh   chan getReq
	putCh   chan putReq
	purgeCh chan string
	snapCh  chan chan cache.Stats
	quit    chan struct{}
}

// New starts a memory store bounded to maxBytes total payload.
func New(maxBytes int64) *Store {
	s := &Store{
		maxBytes: maxBytes,
		items:    map[cache.Key]*entry{},
		lru:      list.New(),
		getCh:    make(chan getReq),
		putCh:    make(chan putReq),
		purgeCh:  make(chan string),
		snapCh:   make(chan chan cache.Stats),
		quit:     make(chan struct{}),
	}
	go s.loop()
	return s
}

func (s *Store) loop() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case r := <-s.getCh:
			r.resp <- s.get(r.key)
		case p := <-s.putCh:
			p.resp <- putResp{err: s.put(p.req)}
		case route := <-s.purgeCh:
			s.purge(route)
		case ch := <-s.snapCh:
			ch <- cache.Stats{Entries: len(s.items), Bytes: s.curBytes, Hits: s.hits, Misses: s.misses}
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
	if !e.expires.IsZero() && time.Now().After(e.expires) {
		s.remove(e)
		s.misses++
		return getResp{}
	}
	s.lru.MoveToFront(e.el)
	s.hits++
	return getResp{data: e.data, ct: e.ct, hit: true}
}

func (s *Store) put(p cache.PutReq) error {
	data, err := io.ReadAll(p.Data)
	if err != nil {
		return err
	}
	if old, ok := s.items[p.Key]; ok {
		s.remove(old)
	}
	e := &entry{key: p.Key, route: p.Route, data: data, ct: p.ContentType, size: int64(len(data))}
	if p.TTL > 0 {
		e.expires = time.Now().Add(p.TTL)
	}
	e.el = s.lru.PushFront(e)
	s.items[p.Key] = e
	s.curBytes += e.size
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
	s.curBytes -= e.size
}

func (s *Store) purge(route string) {
	if route == "" {
		s.items = map[cache.Key]*entry{}
		s.lru.Init()
		s.curBytes = 0
		return
	}
	var toRemove []*entry
	for _, e := range s.items {
		if e.route == route {
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
		if !e.expires.IsZero() && now.After(e.expires) {
			toRemove = append(toRemove, e)
		}
	}
	for _, e := range toRemove {
		s.remove(e)
	}
}

// Get implements cache.Store. The returned reader streams immutable bytes.
func (s *Store) Get(k cache.Key) (io.ReadCloser, cache.Meta, bool) {
	resp := make(chan getResp)
	s.getCh <- getReq{key: k, resp: resp}
	r := <-resp
	if !r.hit {
		return nil, cache.Meta{}, false
	}
	return io.NopCloser(bytes.NewReader(r.data)), cache.Meta{ContentType: r.ct, Size: int64(len(r.data))}, true
}

func (s *Store) Put(r cache.PutReq) error {
	resp := make(chan putResp)
	s.putCh <- putReq{req: r, resp: resp}
	return (<-resp).err
}
func (s *Store) Purge(route string)    { s.purgeCh <- route }
func (s *Store) Snapshot() cache.Stats { ch := make(chan cache.Stats); s.snapCh <- ch; return <-ch }
func (s *Store) Close()                { close(s.quit) }

// Compile-time interface check.
var _ cache.Store = (*Store)(nil)
