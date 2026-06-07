package router

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/c0ze/bumblebee/cache"
	"github.com/c0ze/bumblebee/cache/memory"
	"github.com/c0ze/bumblebee/internal/config"
	"github.com/c0ze/bumblebee/transform"
	"github.com/c0ze/bumblebee/upstream"
)

type route struct {
	path        string
	method      string
	upstreamURL string
	forwardBody bool
	fwdHeaders  []string
	fwdQuery    []string
	keyHeaders  []string
	keyQuery    []string
	ttl         time.Duration
	pool        *upstream.Pool
	pipeline    *transform.Pipeline
	store       cache.Store
}

type server struct {
	version string
	token   string
	store   *memory.Store
	routes  []*route
}

// New builds an http.Handler from config and returns a cleanup func.
func New(cfg *config.Config, version string) (http.Handler, func(), error) {
	store := memory.New(int64(cfg.Cache.Memory.MaxBytes))
	s := &server{version: version, token: cfg.Server.AuthToken, store: store}

	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/stats", s.auth(s.handleStats))
	r.Post("/cache/purge", s.auth(s.handlePurge))

	for i := range cfg.Routes {
		rc := cfg.Routes[i]
		pipe, err := transform.Build(toStages(rc.Pipeline))
		if err != nil {
			for _, rt := range s.routes {
				rt.pool.Close()
			}
			store.Close()
			return nil, nil, err
		}
		rt := &route{
			path:        rc.Path,
			method:      rc.Upstream.Method,
			upstreamURL: rc.Upstream.URL,
			forwardBody: rc.Upstream.ForwardBody,
			fwdHeaders:  rc.Upstream.ForwardHeaders,
			fwdQuery:    rc.Upstream.ForwardQuery,
			keyHeaders:  rc.Cache.KeyHeaders,
			keyQuery:    rc.Cache.KeyQuery,
			ttl:         time.Duration(rc.Cache.TTL),
			pool:        upstream.New(rc.Path, rc.Upstream.Pool, time.Duration(rc.Upstream.Timeout), rc.Upstream.Retries, rc.Upstream.MaxInflight),
			pipeline:    pipe,
			store:       store,
		}
		s.routes = append(s.routes, rt)
		r.Method(rc.Upstream.Method, rc.Path, rt.handler(s))
	}

	cleanup := func() {
		for _, rt := range s.routes {
			rt.pool.Close()
		}
		store.Close()
	}
	return r, cleanup, nil
}

func (rt *route) handler(s *server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var body []byte
		if rt.forwardBody {
			var err error
			body, err = io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		get := func(key string) (string, bool) {
			if v := r.Header.Get(key); v != "" {
				return v, true
			}
			if v := r.URL.Query().Get(key); v != "" {
				return v, true
			}
			return "", false
		}
		eff := rt.pipeline.Resolve(get)
		key := deriveKey(rt, r, body, eff)

		if rc, meta, hit := rt.store.Get(key); hit {
			defer rc.Close()
			w.Header().Set("X-Cache", "HIT")
			if meta.ContentType != "" {
				w.Header().Set("Content-Type", meta.ContentType)
			}
			io.Copy(w, rc)
			return
		}

		job := &upstream.Job{BuildReq: func(host string) (*http.Request, error) {
			u := buildURL(rt.upstreamURL, host, r)
			var rdr io.Reader
			if rt.forwardBody {
				rdr = bytes.NewReader(body)
			}
			req, err := http.NewRequestWithContext(ctx, rt.method, u, rdr)
			if err != nil {
				return nil, err
			}
			for _, hk := range rt.fwdHeaders {
				if v := r.Header.Get(hk); v != "" {
					req.Header.Set(hk, v)
				}
			}
			if len(rt.fwdQuery) > 0 {
				q := req.URL.Query()
				for _, qk := range rt.fwdQuery {
					if v := r.URL.Query().Get(qk); v != "" {
						q.Set(qk, v)
					}
				}
				req.URL.RawQuery = q.Encode()
			}
			return req, nil
		}}

		res := rt.pool.Do(job)
		if res.Err != nil {
			w.WriteHeader(res.Status)
			return
		}
		defer res.Resp.Body.Close()
		if res.Resp.StatusCode/100 != 2 {
			w.WriteHeader(res.Resp.StatusCode)
			io.Copy(w, res.Resp.Body)
			return
		}

		var out bytes.Buffer
		ct, err := rt.pipeline.Run(ctx, res.Resp.Body, &out, eff)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if ct == "" {
			ct = res.Resp.Header.Get("Content-Type")
		}
		data := out.Bytes()
		_ = rt.store.Put(cache.PutReq{
			Key:         key,
			Route:       rt.path,
			ContentType: ct,
			TTL:         rt.ttl,
			Data:        bytes.NewReader(append([]byte(nil), data...)),
		})
		w.Header().Set("X-Cache", "MISS")
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Write(data)
	}
}

// buildURL substitutes {host}, {path}, and {query} in the upstream URL template.
func buildURL(tmpl, host string, r *http.Request) string {
	u := strings.ReplaceAll(tmpl, "{host}", host)
	u = strings.ReplaceAll(u, "{path}", strings.TrimPrefix(r.URL.Path, "/"))
	u = strings.ReplaceAll(u, "{query}", r.URL.RawQuery)
	return u
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

type statsResp struct {
	BuildVersion string                         `json:"build_version"`
	Cache        cache.Stats                    `json:"cache"`
	Routes       map[string][]upstream.HostStat `json:"routes"`
}

func (s *server) handleStats(w http.ResponseWriter, _ *http.Request) {
	resp := statsResp{
		BuildVersion: s.version,
		Cache:        s.store.Snapshot(),
		Routes:       map[string][]upstream.HostStat{},
	}
	for _, rt := range s.routes {
		resp.Routes[rt.path] = rt.pool.Snapshot()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *server) handlePurge(w http.ResponseWriter, r *http.Request) {
	s.store.Purge(r.URL.Query().Get("route"))
	w.WriteHeader(http.StatusOK)
}

func toStages(in []config.StageConfig) []transform.Stage {
	out := make([]transform.Stage, len(in))
	for i, s := range in {
		out[i] = transform.Stage{Type: s.Type, Params: transform.Params(s.Params), Overrides: s.Overrides}
	}
	return out
}
