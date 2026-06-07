package upstream_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c0ze/bumblebee/upstream"
)

func buildFor(method, base string) func(host string) (*http.Request, error) {
	return func(host string) (*http.Request, error) {
		// host is the pool entry; we use it directly as the URL in these tests.
		return http.NewRequestWithContext(context.Background(), method, host, nil)
	}
}

func TestRoundRobin(t *testing.T) {
	var a, b int32
	sa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { atomic.AddInt32(&a, 1) }))
	sb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { atomic.AddInt32(&b, 1) }))
	defer sa.Close()
	defer sb.Close()

	p := upstream.New("t", []string{sa.URL, sb.URL}, 2*time.Second, 0, 0)
	defer p.Close()
	for i := 0; i < 4; i++ {
		res := p.Do(&upstream.Job{BuildReq: buildFor("GET", "")})
		if res.Err != nil {
			t.Fatalf("req %d: %v", i, res.Err)
		}
		res.Resp.Body.Close()
	}
	if atomic.LoadInt32(&a) != 2 || atomic.LoadInt32(&b) != 2 {
		t.Fatalf("round-robin uneven: a=%d b=%d", a, b)
	}
}

func TestRetryNextOnTransportError(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") }))
	defer good.Close()
	dead := "http://127.0.0.1:1" // connection refused

	// dead first, good second, retries=1 -> should succeed on good.
	p := upstream.New("t", []string{dead, good.URL}, 2*time.Second, 1, 0)
	defer p.Close()
	res := p.Do(&upstream.Job{BuildReq: buildFor("GET", "")})
	if res.Err != nil {
		t.Fatalf("expected success after retry, got %v (status %d)", res.Err, res.Status)
	}
	res.Resp.Body.Close()
}

func TestTimeoutClassified504(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { time.Sleep(200 * time.Millisecond) }))
	defer slow.Close()
	p := upstream.New("t", []string{slow.URL}, 50*time.Millisecond, 0, 0)
	defer p.Close()
	res := p.Do(&upstream.Job{BuildReq: buildFor("GET", "")})
	if res.Err == nil || res.Status != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got status=%d err=%v", res.Status, res.Err)
	}
}
