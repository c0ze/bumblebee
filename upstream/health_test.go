package upstream_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/c0ze/bumblebee/upstream"
)

func TestPassiveHealthMarksDownAndRecovers(t *testing.T) {
	dead := "http://127.0.0.1:1" // always connection-refused

	// fail_threshold=2, short cooldown. retries=0 so each Do hits one host.
	p := upstream.New("t", []string{dead}, time.Second, 0, 0, 2, 50*time.Millisecond)
	defer p.Close()

	mkJob := func() *upstream.Job {
		return &upstream.Job{BuildReq: func(host string) (*http.Request, error) {
			return http.NewRequestWithContext(context.Background(), "GET", host, nil)
		}}
	}

	// Two transport failures on the only host -> it goes DOWN.
	for i := 0; i < 2; i++ {
		if res := p.Do(mkJob()); res.Err == nil {
			t.Fatalf("attempt %d: expected transport error", i)
		}
	}
	// All hosts down -> Do returns 503 immediately (no host picked).
	if res := p.Do(mkJob()); res.Status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when all hosts down, got %d", res.Status)
	}
	if st := p.Snapshot(); len(st) != 1 || st[0].State != "DOWN" {
		t.Fatalf("host state: %+v", st)
	}
	// After cooldown it is eligible again.
	time.Sleep(70 * time.Millisecond)
	if st := p.Snapshot(); len(st) != 1 || st[0].State != "UP" {
		t.Fatalf("host should recover to UP after cooldown, got %s", st[0].State)
	}
}
