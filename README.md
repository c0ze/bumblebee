# 🐝 Bumblebee

Bumblebee is a small, fast **stream manipulator router**. Put it in front of any
upstream service and it **fetches**, **caches**, **transforms**, and **serves**
the result. Transformations are configurable per route, so the same binary can
sit in front of a TTS service (transcode PCM→MP3), an image origin
(resize/recompress), a video source (transcode), or anything else.

> **Status: early.** The engine core — config-driven routing, in-memory
> caching, upstream pooling, and a transform pipeline — is the first milestone.
> The audio (`lame`), image, and video (`ffmpeg`) transformers and a disk cache
> backend are in progress.

## How it works

```
client ─▶ bumblebee ─▶ cache hit? ──serve
                          │ miss
                          ▼
                    upstream pool ─▶ transform pipeline ─▶ cache + serve
```

Each route in the config declares:

- an **upstream** — a templated URL plus a pool of hosts (round-robin with
  retry-next on transport failure);
- a **cache** policy — a byte-bounded LRU with TTL; the key is derived from the
  route, selected headers/query params, the effective transform params, and the
  request body;
- a **transform pipeline** — an ordered list of steps (`passthrough` today;
  `lame`, `image`, and `video` next), each tunable per request.

### Race-free by construction

State shared across requests — the cache, each upstream pool, and the stats
snapshot — is **owned by a single goroutine and reached only over channels**, so
there are no shared-memory locks and no data races by design. The whole suite
runs under `go test -race`.

## Configuration

```yaml
server:
  addr: ":8080"
  # auth_token: ${BUMBLE_AUTH_TOKEN}   # optional; guards /stats and /cache/purge
cache:
  default_backend: memory
  memory: { max_bytes: 512MB }
routes:
  - path: /proxy/*
    upstream:
      method: GET
      url: "https://origin.example.com/{path}"
      pool: ["origin.example.com"]
      timeout: 30s
      retries: 2
    cache:
      backend: memory
      ttl: 1h
      key_query: [w, h]
    pipeline:
      - type: passthrough
```

URL templates support `{host}` (chosen from the pool), `{path}`, and `{query}`.

## Run

```sh
go run ./cmd/bumblebee -config examples/passthrough.yaml
# or
docker build -t bumblebee . && docker run -p 8080:8080 bumblebee
```

## Endpoints

- `GET /health` — liveness
- `GET /stats` — JSON cache + per-host stats (bearer-token protected when
  `server.auth_token` is set)
- `POST /cache/purge` — clear the cache (`?route=/proxy/*` to scope)

Every proxied response carries `X-Cache: HIT` or `X-Cache: MISS`.

## Development

The toolchain is pinned with [mise](https://mise.jdx.dev) (`mise.toml`):

```sh
mise install                    # install the pinned Go toolchain
mise exec -- go test -race ./...
```

## License

MIT © 2026 Arda Karaduman
