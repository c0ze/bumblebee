package upstream

import (
	"errors"
	"net"
	"net/http"
	"time"
)

// Result is the outcome of a Job. On error, Status is the mapped HTTP status.
type Result struct {
	Resp   *http.Response
	Status int
	Err    error
}

// Job builds the upstream request for a chosen host name and receives the Result.
type Job struct {
	BuildReq func(host string) (*http.Request, error)
	result   chan Result
}

// HostStat is a per-host snapshot for /stats.
type HostStat struct {
	Name       string    `json:"host"`
	State      string    `json:"state"`
	LastStatus int       `json:"last_status"`
	OK         int64     `json:"ok"`
	Errors     int64     `json:"errors"`
	LastAccess time.Time `json:"last_access"`
}

type host struct {
	name       string
	inflight   int
	last       time.Time
	lastStatus int
	ok         int64
	errs       int64
	failStreak int
	downUntil  time.Time
}

type attempt struct {
	job   *Job
	tries int
	h     *host
}

type outcome struct {
	at   *attempt
	resp *http.Response
	err  error
}

// Pool is a round-robin upstream host pool owned by a single goroutine.
type Pool struct {
	name          string
	hosts         []*host
	next          int
	retries       int
	maxInflight   int
	failThreshold int
	cooldown      time.Duration
	client        *http.Client

	jobs chan *Job
	outs chan outcome
	snap chan chan []HostStat
	quit chan struct{}
}

var errNoHost = errors.New("no upstream host available")

// New starts a pool. maxInflight <= 0 means unbounded. failThreshold <= 0 disables passive health.
func New(name string, hostNames []string, timeout time.Duration, retries, maxInflight, failThreshold int, cooldown time.Duration) *Pool {
	if maxInflight <= 0 {
		maxInflight = 1 << 30
	}
	p := &Pool{
		name:          name,
		retries:       retries,
		maxInflight:   maxInflight,
		failThreshold: failThreshold,
		cooldown:      cooldown,
		client:        &http.Client{Timeout: timeout},
		jobs:          make(chan *Job),
		outs:          make(chan outcome),
		snap:          make(chan chan []HostStat),
		quit:          make(chan struct{}),
	}
	for _, n := range hostNames {
		p.hosts = append(p.hosts, &host{name: n})
	}
	go p.loop()
	return p
}

func (p *Pool) loop() {
	for {
		select {
		case j := <-p.jobs:
			p.dispatch(&attempt{job: j, tries: p.retries})
		case o := <-p.outs:
			p.handle(o)
		case ch := <-p.snap:
			ch <- p.stats()
		case <-p.quit:
			return
		}
	}
}

func (p *Pool) pick() *host {
	n := len(p.hosts)
	for i := 0; i < n; i++ {
		h := p.hosts[p.next%n]
		p.next++
		if h.inflight < p.maxInflight && time.Now().After(h.downUntil) {
			return h
		}
	}
	return nil
}

func (p *Pool) dispatch(at *attempt) {
	h := p.pick()
	if h == nil {
		at.job.result <- Result{Status: http.StatusServiceUnavailable, Err: errNoHost}
		return
	}
	req, err := at.job.BuildReq(h.name)
	if err != nil {
		at.job.result <- Result{Status: http.StatusBadGateway, Err: err}
		return
	}
	at.h = h
	h.inflight++
	go func() {
		resp, err := p.client.Do(req)
		select {
		case p.outs <- outcome{at: at, resp: resp, err: err}:
		case <-p.quit:
			if resp != nil {
				resp.Body.Close()
			}
		}
	}()
}

func (p *Pool) handle(o outcome) {
	h := o.at.h
	h.inflight--
	h.last = time.Now()
	if o.err != nil {
		status := classify(o.err)
		h.errs++
		h.lastStatus = status
		h.failStreak++
		if p.failThreshold > 0 && h.failStreak >= p.failThreshold {
			h.downUntil = time.Now().Add(p.cooldown)
		}
		if o.at.tries > 0 {
			o.at.tries--
			p.dispatch(o.at)
			return
		}
		o.at.job.result <- Result{Status: status, Err: o.err}
		return
	}
	h.ok++
	h.lastStatus = o.resp.StatusCode
	h.failStreak = 0
	h.downUntil = time.Time{}
	o.at.job.result <- Result{Resp: o.resp}
}

func (p *Pool) stats() []HostStat {
	now := time.Now()
	out := make([]HostStat, 0, len(p.hosts))
	for _, h := range p.hosts {
		state := "UP"
		if now.Before(h.downUntil) {
			state = "DOWN"
		}
		out = append(out, HostStat{Name: h.name, State: state, LastStatus: h.lastStatus, OK: h.ok, Errors: h.errs, LastAccess: h.last})
	}
	return out
}

func classify(err error) int {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return http.StatusGatewayTimeout
	}
	return http.StatusServiceUnavailable
}

// Do submits a job and blocks for its Result.
func (p *Pool) Do(j *Job) Result {
	j.result = make(chan Result, 1)
	p.jobs <- j
	return <-j.result
}

// Snapshot returns per-host stats computed inside the owner goroutine.
func (p *Pool) Snapshot() []HostStat { ch := make(chan []HostStat); p.snap <- ch; return <-ch }

// Close stops the pool goroutine.
func (p *Pool) Close() { close(p.quit) }
