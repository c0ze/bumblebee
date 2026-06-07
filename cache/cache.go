package cache

import (
	"io"
	"time"
)

// Key is the content-addressed cache key (hex sha256).
type Key string

// Meta describes a cached object.
type Meta struct {
	ContentType string
	Size        int64
}

// PutReq is a request to store an object.
type PutReq struct {
	Key         Key
	Route       string // for scoped purge / per-route stats
	ContentType string
	TTL         time.Duration // 0 = no expiry
	Data        io.Reader
}

// Stats is a snapshot of a store's state.
type Stats struct {
	Entries int   `json:"entries"`
	Bytes   int64 `json:"bytes"`
	Hits    int64 `json:"hits"`
	Misses  int64 `json:"misses"`
}

// Store is a content-addressed object cache. Implementations must be safe for
// concurrent use; the returned reader streams immutable bytes.
type Store interface {
	Get(k Key) (io.ReadCloser, Meta, bool)
	Put(r PutReq) error
	Purge(route string) // "" purges everything
	Snapshot() Stats
	Close()
}
