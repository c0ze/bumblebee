package memory

import (
	"strings"
	"testing"
	"time"

	"github.com/c0ze/bumblebee/cache"
)

func TestSweepRemovesExpired(t *testing.T) {
	s := newForTest(1<<20, 10*time.Millisecond)
	defer s.Close()
	if err := s.Put(cache.PutReq{Key: "k", Route: "/r", TTL: 5 * time.Millisecond, Data: strings.NewReader("x")}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	time.Sleep(40 * time.Millisecond) // let the ticker fire after expiry
	if st := s.Snapshot(); st.Entries != 0 {
		t.Fatalf("sweep should have removed the expired entry, entries=%d", st.Entries)
	}
}
