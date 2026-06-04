# Proposal — standalone, multi-arch Dockerfile + `docker-build` CI job

| | |
|---|---|
| **Issue** | #14 |
| **Status** | Proposed |
| **Started** | 2026-06-04 22:32 CST |
| **Related** | #1 (CI), #2 (release → reuses this image), #5 (docker-compose) |

## Problem

The current `Dockerfile` assumes the **old monorepo layout**: it `COPY`s `gateway/`
and `listen/` subdirectories and relies on a `replace github.com/.../llmgateway`
directive that no longer exists in `go.mod`. On the standalone repo those
directories don't exist, so `docker build .` fails. Consequently the
`docker-build` smoke job was deferred from #1, and **#2 (release → `ghcr.io`) is
blocked** — you can't publish an image that won't build.

## Goals

- `docker build .` succeeds on the standalone repo (context = repo root).
- **Multi-arch** images: `linux/amd64` + `linux/arm64`.
- Small, secure, reproducible runtime.
- A CI **smoke** job that catches Dockerfile regressions on every relevant PR.

## Non-goals

- Pushing to a registry / tagging / releases — that's **#2**.
- Changing the service's runtime config contract (`FEEDBACK_API_CONFIG`, port 8090).

## Proposal

### Dockerfile (rewrite)

```dockerfile
# syntax=docker/dockerfile:1

# ── Stage 1: build (cross-compiled, static) ──
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
ARG GOPROXY=https://proxy.golang.org,direct   # local CN builds can --build-arg override
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /listen ./cmd/listen

# ── Stage 2: runtime (distroless static, nonroot, no shell) ──
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /listen /app/listen
WORKDIR /app
EXPOSE 8090
ENV FEEDBACK_API_CONFIG=/app/config.yaml
ENTRYPOINT ["/app/listen"]
CMD ["server"]
```

Key decisions:

- **`--platform=$BUILDPLATFORM` + `GOOS/GOARCH` cross-compile** — Go builds the
  target arch on the native (amd64) builder, so `arm64` builds with **no qemu
  emulation**. Both platforms are cheap.
- **Runtime = `distroless/static-debian12:nonroot`** — `ca-certificates` + `tzdata`
  baked in, runs as uid 65532, no shell, ~2 MB. The runtime stage has **no `RUN`**,
  which keeps multi-arch builds qemu-free.
- **`GOPROXY` build-arg** — CI uses the default proxy; local builds behind the GFW
  pass `--build-arg GOPROXY=https://goproxy.cn,direct`.
- **Drop the Aliyun `apk` mirror** — the rewrite uses no `apk` at all.

### CI — `docker-build` job (in `ci.yml`)

```yaml
  docker-build:
    needs: changes
    if: needs.changes.outputs.go == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@<sha> # v6.0.3
        with:
          persist-credentials: false
      - uses: docker/setup-buildx-action@<sha> # v4.1.0
      - uses: docker/build-push-action@<sha> # v7.2.0
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: false
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

Plus: add `Dockerfile` to the `changes` job's `go` paths-filter, and add
`docker-build` to the `ci-gate` aggregator's `needs`.

## Alternatives considered

- **Runtime = alpine** (status quo): has a shell for `docker exec` debugging, but
  the runtime stage's `RUN apk add` forces **qemu** for `arm64` (slow), and the
  image is larger. *Rejected* in favour of distroless (chosen 2026-06-04).
- **`amd64` only**: faster, but drops Apple-Silicon / ARM-server support. *Rejected*
  — multi-arch is nearly free here because the build cross-compiles.
- **One-platform CI smoke**: faster per run, but wouldn't catch `arm64`-specific
  Dockerfile breakage. *Rejected* — cross-compile makes the second platform cheap.

## Risks / tradeoffs

- **No shell in distroless** → can't `docker exec … sh`. Mitigation: use the
  `:debug` distroless variant ad-hoc when debugging.
- **nonroot (uid 65532)** → the service must not need to write outside mounted
  volumes. It reads config + listens; no local writes expected.
- **GHA cache** is per-branch scoped; cold builds on a new branch are slower.

## Implementation plan

1. Rewrite `Dockerfile` (above) **+ add `.dockerignore`** (exclude
   `console/node_modules`, `.git`, `docs`, … — surfaced by the e2e).
2. `ci.yml`: add `Dockerfile` to the `go` paths-filter; add the `docker-build` job;
   add it to `ci-gate.needs`.
3. **Verify locally**: `docker buildx build --platform linux/amd64,linux/arm64 -t listen:ci .`
   (no push) + `actionlint`.
4. Open PR titled `build: …`, body contains **`Closes #14`**; CI runs the
   multi-arch `docker-build` smoke.

## Verification

### Local e2e — run 2026-06-04, **✅ image works end-to-end**

| Check | Result |
|---|---|
| Multi-arch build `linux/amd64 + linux/arm64` (cross-compiled, no qemu) | ✅ |
| Image size | **32.7 MB** (distroless static + binary) |
| Container starts; embedded migrations apply in the distroless runtime | ✅ 7 migrations (001→007) |
| Upstream: real Postgres connect | ✅ `postgres connected` |
| `GET /health` | ✅ `ok` |
| `GET /metrics` (Prometheus exposition) | ✅ `listen_*` metrics |
| Admin CLI in the container (`listen tenant create`, `keys issue`) | ✅ |
| Downstream: `POST /v1/feedback/ingest` (`X-API-Key`) → row stored | ✅ `{"id":1,"enrichmentStatus":"pending"}` |

The e2e surfaced a real gap — **no `.dockerignore`** (so `COPY . .` dragged in
`console/node_modules`, 310 MB). Added one.

### Dependency map (upstream / downstream)

**Upstream** (listen depends on):

| Dep | Use | Connects at startup? | e2e |
|---|---|---|---|
| PostgreSQL | primary store + migrations | **yes** (Ping + RunMigrations) | ✅ |
| LLM (OpenAI-compatible) | enricher labels feedback | no (lazy; only with data) | ⬜ needs mock LLM |
| Lark/Feishu client | inbound events + outbound bot | no (disabled w/o secret) | ⏭️ optional |
| Customer HTTPS webhooks | outbound delivery (via outbox) | no (async worker) | ⏭️ optional |
| OTel collector | tracing | no (noop if endpoint empty) | n/a |

**Downstream** (consumes listen):

| Consumer | Endpoint | Auth | e2e |
|---|---|---|---|
| API clients | `POST /v1/feedback/ingest` | `X-API-Key` | ✅ |
| Prometheus | `GET /metrics` | none | ✅ |
| liveness | `GET /health` | none | ✅ |
| Lark senders | `POST /v1/lark/event` | signature | ⏭️ optional |
| Console SPA | `/fb/v1/console/*` | session cookie | ⏭️ optional |

### Not yet verified (deeper / optional)

- [ ] **Enrich pipeline** (ingest → enricher → LLM → labeled row): needs a mock
      OpenAI-compatible `/v1/chat/completions` server.
- [ ] Lark inbound + outbound bot (need signing secret).
- [ ] Console API (`/fb/v1/console`, needs `CONSOLE_SESSION_KEY`).
- [ ] Customer webhook delivery (outbox → HTTPS POST).

### CI (post-PR)
- [ ] `docker-build` job green on both platforms, wired into `ci-gate`.

## References

- #1 (CI; docker-build deferred), #2 (release → `ghcr.io`), #5 (docker-compose).
- The original monorepo `Dockerfile` (pre-rewrite).
