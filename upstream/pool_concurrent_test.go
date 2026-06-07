package upstream_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/c0ze/bumblebee/upstream"
)

// TestConcurrentDoNoRace exercises the pool under genuinely concurrent load:
// many goroutines call Do (and Snapshot) at once, so dispatch, worker outcomes,
// retry re-dispatch, and stats all interleave in the owner goroutine. The
// sequential tests in pool_test.go don't prove the race-free claim; this does.
func TestConcurrentDoNoRace(t *testing.T) {
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "a") }))
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "b") }))
	defer a.Close()
	defer b.Close()

	// retries=1 so the retry-next path (which re-dispatches inside the owner) is
	// also exercised concurrently when a request happens to fail.
	p := upstream.New("t", []string{a.URL, b.URL}, 2*time.Second, 1, 0)
	defer p.Close()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				res := p.Do(&upstream.Job{BuildReq: buildFor("GET", "")})
				if res.Resp != nil {
					res.Resp.Body.Close()
				}
				_ = p.Snapshot()
			}
		}()
	}
	wg.Wait()
}
