# Go client SDK for the ingest API

| | |
|---|---|
| **Issue** | #36 |
| **Status** | Accepted |
| **Started** | 2026-06-19 19:41 +0800 |
| **Related** | #37 (Node/TS SDK — landed; defines the shared retry/error/idempotency contract this proposal adopts verbatim), #19 (proto IDL contract — landed, prerequisite), #66 (channel-agnostic ingest — `source`/`page_url` fields), v0.6 milestone (Multi-channel) |

---

## Problem

OSS users who want to push feedback into attune from a Go service today have to
read the OpenAPI doc, hand-roll an `http.Client`, get the `X-API-Key` header
right, remember that `id` comes back as a JSON string, and re-implement retry,
backoff, and idempotency themselves. Every consumer re-derives the same wire
details and re-makes the same mistakes (following redirects that leak the API
key, blind retries that double-insert rows, switching on `message` instead of
`code`).

`@phixsura/attune` (#37) already solved this for Node/TypeScript and was
deliberately built as **one half of a dual SDK launch** — its proposal locks the
retry/error/idempotency contract and states that #36 (the Go half) "adopts [it]
verbatim, so both SDKs are behaviorally identical." This proposal is that Go
half. It must reproduce the same behavior, not invent a second one.

## Goals

- Publish a Go module installable with `go get`, wrapping the ingest endpoint:
  `c := attune.New(baseURL, apiKey)` → `res, err := c.Ingest(ctx, attune.IngestInput{Content: "..."})`.
- **Behavioral parity with the Node SDK** on the wire contract: same retryable
  status set, same bounded backoff, same idempotency-key semantics, same
  `code`-based error model, same security posture (no redirect-follow, CRLF
  guard, bounded response read, versioned `User-Agent`).
- **Wire types generated from the #19 contract** (`proto/attune/v1/ingest.proto`)
  and marshaled with `protojson` — the same codec the server binds with — so the
  SDK cannot drift from the wire format by construction (matches the Node SDK,
  which also generates its types).
- `ctx` is always the first argument; cancellation and deadlines propagate.
- A real `examples/ingest-cli/` built on the SDK, compiled in CI as docs + smoke
  test.
- `go.mod` floor of **Go 1.25** — forced by the generated types'
  `google.golang.org/genproto` dependency (see Decision 2; the issue's original
  "Go 1.22" criterion is consciously superseded — owner-accepted tradeoff to keep
  type generation).
- godoc renders cleanly; unit + e2e tests pass; `CHANGELOG.md` gets an
  `### Added` entry.

## Non-goals

- **No workflow / tag / search CRUD** in this release. The issue scopes those as
  "Future … gated, separate releases." We ship `Ingest` only, behind a client
  surface that can grow later without breaking callers (functional options +
  per-call options structs leave room).
- **No new server behavior.** The contract is frozen by #19 and #37; this is a
  pure client. If we find a contract gap, it is a separate issue, not smuggled
  in here.
- **No CLI distribution.** `examples/ingest-cli` is a buildable example and
  smoke test, not a published binary.

## Proposal

### Decision 1 — Module topology: a nested module at `sdk/go/`

Ship the SDK as its **own Go module** rooted at `sdk/go/`, module path
`github.com/Phixsura/attune/sdk/go`, with its own `go.mod` declaring `go 1.25`.
It lives in this monorepo next to `sdk/node/`, mirroring the established
`sdk/<lang>/` layout.

This is forced by three independent facts, each of which alone rules out a
subpackage of the root module:

1. **`internal/` is unimportable externally.** The generated server types live
   at `github.com/Phixsura/attune/internal/proto/...`. Go's `internal/` rule
   forbids any external consumer from importing them. A subpackage under the
   root module that re-exported them would not help — the consumer still can't
   reach `internal`. The SDK needs its *own* generated copy.
2. **Dependency hygiene.** The root module pulls in `pgx`, `tink`, `chi`, the
   OTel stack, IMAP, the Anthropic SDK, etc. A subpackage would drag that entire
   tree onto every SDK consumer's `go.sum`. A separate module gives the SDK its
   own minimal `go.mod` (just the protobuf/proto-annotation deps its generated
   types need — see Decision 2).
3. **Its own generated proto copy.** The SDK generates its wire types under its
   own module path (Decision 2), which a subpackage sharing the root module's
   `internal/proto` could not do.

**Nested module, not a separate `attune-go` repo.** A nested module
(`sdk/go/go.mod`) keeps the Go and Node SDKs symmetric and in one repo, so a
single PR regenerates proto for both, the e2e harness can build the server from
`../..`, and there is no cross-repo sync lag. This is exactly how
`cloud.google.com/go` ships dozens of independently-versioned SDKs as nested
submodules in one repo. A separate repo would buy a shorter import path at the
cost of split-repo proto sync and a second CI surface — not worth it at this
scale.

**No `go.work`.** Adding a workspace file would fold `sdk/go` into the root
module's `go build ./...` / `go vet ./...` / lizard / coverage gates, which are
tuned for the server and would now also police the SDK (and vice versa — the SDK
would inherit server lint config). We keep the modules independent: the root
gates ignore `sdk/go`, and a dedicated `sdk-go` CI job owns the SDK's gates,
exactly as `sdk-node` owns the Node SDK's. (Local cross-module dev, if ever
needed, uses a developer-local, git-ignored `go.work` — not committed.)

**Release tagging.** Go's nested-module convention versions this module with
tags of the form **`sdk/go/vX.Y.Z`** (the module subdirectory is the tag
prefix). `go get github.com/Phixsura/attune/sdk/go@v0.1.0` resolves the
`sdk/go/v0.1.0` tag. This is a hard Go tooling requirement, and it cleanly
avoids colliding with the Node SDK's `sdk-v*` tags.

### Decision 2 — Wire types generated from proto, idiomatic facade on top

Generate the Go message types from `proto/attune/v1` via a second `buf.gen`
template (`buf.gen.sdk-go.yaml`, prefix `…/sdk/go`) into the **public, importable**
package `sdk/go/attune/v1` (`github.com/Phixsura/attune/sdk/go/attune/v1`), scoped
to `ingest.proto`'s import closure (ingest → tag, workflow → common; 4 files). The
wire layer marshals/unmarshals these with **`protojson`** — the *same* codec
`internal/handlers/ingest.go` binds with — so `int64 id`⇄JSON-string,
`google.protobuf.Struct`⇄JSON-object, and lowerCamelCase field names match the
server by construction.

This mirrors the Node SDK on **both** counts: types generated from proto, **and**
those generated types are part of the public surface. The root `attune` package
re-exports `IngestRequest` / `IngestResponse` / `ErrorResponse` / `ErrorCode` as
aliases (`wire.go`), exactly as the Node SDK's `index.ts` re-exports them, so a
caller can `attune.IngestRequest{…}` or import the `attune/v1` package for the
full `ErrorCode` enum. The retry-policy helpers (`IsRetryable`, `BackoffDelay`,
`ParseRetryAfter`) and `TransportErrorCode` are exported too, matching Node's
public surface.

For ergonomics, `Ingest` also accepts a hand-written facade (the generated
`*structpb.Struct` for `source_meta` is not user-friendly):

```go
type IngestInput struct {
    Content    string         // required
    Source     string         // server defaults to "api"
    SourceUser string
    SourceMeta map[string]any  // mapped to *structpb.Struct internally
    PageURL    string
}

type IngestResult struct {
    ID               string // generated int64 id, rendered as a string
    EnrichmentStatus string // "pending" at ingest time
}

func (c *Client) Ingest(ctx context.Context, in IngestInput, opts ...RequestOption) (IngestResult, error)
```

`Ingest` maps `IngestInput` → generated `IngestRequest` (`structpb.NewStruct`
for `SourceMeta`), `protojson`-marshals, POSTs, and on 2xx unmarshals into the
generated `IngestResponse`. Errors parse the generated `ErrorResponse` (whose
`code` is a `string` on the wire, so unknown future codes degrade gracefully).

**Cost — owner-accepted (supersedes the issue's "Go 1.22" line).** The generated
`*.pb.go` blank-imports `google.golang.org/genproto/googleapis/api` (for
`google.api` annotations on `ingest.proto`), and that module's `go.mod` requires
**Go 1.25** — so the SDK floor is 1.25, not the 1.22 the issue listed. Generating
also pulls three deps (`protobuf` + `genproto` + `gnostic-models`), giving up the
stdlib-only posture. Both costs were weighed against the alternative
(hand-written `encoding/json` types, zero deps, Go 1.22) and the owner chose
generation for true parity with the Node SDK's "types derived from proto, not
hand-written" — accepting that Go 1.22–1.24 users cannot consume the SDK.
Determinism of the generated output is guarded by the `proto-sync` CI gate (now
running both `buf.gen` templates), and the wire contract is further verified by
the real-server e2e.

### Decision 3 — Client construction and request options

```go
func New(baseURL, apiKey string, opts ...Option) (*Client, error)

// Construction-time options (functional options — google-cloud-go / aws style):
WithHTTPClient(*http.Client) // bring your own transport (otelhttp, proxies)
WithMaxRetries(int)          // default 2
WithTimeout(time.Duration)   // per-attempt; default 30s
WithUserAgentSuffix(string)  // appended after the SDK's own UA token

// Per-call options:
WithIdempotencyKey(string)   // override the auto-generated key
```

`New` validates `baseURL` (parseable, http/https) and rejects an `apiKey`
containing CR/LF up front (header-injection guard, mirrors the Node SDK).
Functional options keep the constructor stable as future capabilities
(workflow/tag CRUD) are added.

### Decision 4 — Retry, backoff, idempotency, security (verbatim parity)

These are copied from the Node SDK's locked contract, re-expressed in Go:

- **Retryable:** status `408`, `429`, or `>= 500`; plus transport errors
  (network failure, per-attempt timeout). `4xx` other than 408/429 are
  permanent — including `409 IDEMPOTENCY_CONFLICT`. A caller-cancelled `ctx` is
  never retried.
- **Backoff:** `min(2000ms, 200ms * 2^attempt)` with ±25% jitter; honor a
  `Retry-After` header (delta-seconds or HTTP-date), capped at 60s. `maxRetries`
  default 2. Jitter source is injectable for deterministic tests.
- **Idempotency:** generate a UUIDv4 per `Ingest` call from `crypto/rand` (no
  `google/uuid` dependency — same manual v4 construction the Node SDK falls back
  to), held stable across that call's retries so a blind retry is deduped
  server-side. Caller can override via `WithIdempotencyKey`. Reject keys with
  CR/LF before sending; the server enforces the `[A-Za-z0-9_-]{8,64}` shape.
- **Security posture:**
  - `http.Client.CheckRedirect` returns `http.ErrUseLastResponse` — never follow
    a 3xx, so a compromised endpoint can't redirect the request and have Go
    re-send `X-API-Key` to an attacker host. A 3xx surfaces as an `AttuneError`.
  - Response body read under a 1 MiB `io.LimitReader` cap (hostile-server OOM
    guard).
  - `User-Agent: attune-go/<version> go/<goversion>` on every request, so the
    server can attribute SDK traffic and stage per-SDK-version rollouts.
  - `X-API-Key`, `Idempotency-Key`, `Content-Type`, `User-Agent` are reserved;
    `WithUserAgentSuffix` appends rather than replaces.

### Decision 5 — Error model

One exported error type, switched on a stable `code`:

```go
type AttuneError struct {
    Code      string // server ErrorCode enum name, or a transport code
    Message   string // human-facing English; never switch on this
    Status    int    // HTTP status; 0 for transport errors
    RequestID string // from the error envelope / X-Request-ID
}
func (e *AttuneError) Error() string
```

Transport codes mirror Node: `NETWORK`, `TIMEOUT`, `ABORTED`. Server errors are
parsed from the `{code, message, requestId}` envelope; if the body is missing,
`Code` falls back from the HTTP status (the same mapping the Node SDK uses).
Typed `ErrorCode` string constants (`CodeUnauthorized`, `CodeValidation`,
`CodeIdempotencyConflict`, `CodeRateLimited`, …) are exported so callers
`errors.As` + switch on `Code` without magic strings.

### Layout

```
sdk/go/
  go.mod                       # module github.com/Phixsura/attune/sdk/go; go 1.25
  go.sum
  client.go                    # New, Client, Ingest, request loop (protojson)
  options.go                   # Option / RequestOption functional options
  retry.go                     # isRetryable, backoffDelay, parseRetryAfter
  errors.go                    # AttuneError, ErrorCode constants
  types.go                     # IngestInput/IngestResult facade ↔ generated proto
  idempotency.go               # crypto/rand UUIDv4
  version.go                   # const Version = "0.1.0"
  wire.go                      # re-exports of the generated wire types
  doc.go                       # package doc for godoc
  attune/v1/                   # generated (buf.gen.sdk-go.yaml), PUBLIC — DO NOT EDIT
  *_test.go                    # unit tests (httptest, injected clock/RoundTripper)
  examples/ingest-cli/         # real CLI built on the SDK
  scripts/e2e.sh               # boots pg + server, live ingest, dedup checks
  README.md
  LICENSE
```

## Alternatives considered

- **Subpackage of the root module (`github.com/Phixsura/attune/sdk/go`, no own
  `go.mod`).** Rejected: can't import `internal/proto` and drags the server's
  whole dependency tree onto consumers. (Decision 1.)
- **Separate `github.com/Phixsura/attune-go` repo.** Rejected: shorter import
  path, but costs cross-repo proto sync, a second CI/release surface, and breaks
  the in-repo symmetry with `sdk/node`. Revisit only if the SDK's release
  cadence diverges hard from the server's. (Decision 1.)
- **Hand-written `encoding/json` wire types (zero deps, Go 1.22).** A fully
  working alternative, and it held the issue's 1.22 floor with no dependencies —
  an earlier revision of this proposal shipped it. Rejected by the owner in
  favor of generation for true Node parity ("types derived from proto, not
  hand-written"); the cost is the Go 1.25 floor and three deps (Decision 2). The
  public API is identical either way, so this remains a clean fallback if the
  1.25 floor later proves too restrictive. (Decision 2.)
- **`google/uuid` for idempotency keys.** Rejected: a whole dependency for one
  UUIDv4; `crypto/rand` + manual v4 bit-twiddling is ~10 lines. (Decision 4.)
- **`go.work` committed at repo root.** Rejected: couples the two modules'
  gates. (Decision 1.)

## Risks / tradeoffs

- **Go 1.25 floor + three deps.** Generating from proto raises the SDK's minimum
  Go from 1.22 to 1.25 (via `genproto`) and adds `protobuf` + `genproto` +
  `gnostic-models`. Owner-accepted for Node parity; the hand-written fallback
  (zero deps, Go 1.22) stays documented should the floor need to drop.
- **Two generated proto copies** (`internal/proto` + `sdk/go/attune/v1`).
  Mitigation: both come from one `make proto` run and the `proto-sync` gate runs
  both `buf.gen` templates + `git diff --exit-code`, so a stale SDK copy fails CI
  exactly like a stale server copy. Generation is deterministic (verified: no
  drift on re-run).
- **`*structpb.Struct` ergonomics.** The generated `source_meta` type is
  unfriendly, so the public `IngestInput.SourceMeta` stays `map[string]any` and
  the SDK converts internally — the one place a thin facade sits over the
  generated types.
- **`sdk/go/vX.Y.Z` tag shape is non-obvious.** Mitigation: documented in the
  SDK README and the release workflow, with the `go get …@vX.Y.Z` ⇄
  `sdk/go/vX.Y.Z` mapping spelled out.
- **Behavioral parity is asserted, not enforced by a shared spec.** The Node and
  Go SDKs share a contract but not code. Mitigation: the retry/backoff constants
  and the retryable-status set get table-driven unit tests with the same cases
  as the Node suite, and both SDKs run the same e2e shape (ingest → dedup →
  concurrent-dedup) against a real server.

## Implementation plan

1. **Proto target + module skeleton.** `buf.gen.sdk-go.yaml` → generate
   `sdk/go/attune/v1` (public); wire `make proto` + the `proto-sync` gate to run
   it. `sdk/go/go.mod` (`go 1.25`), `version.go`, `doc.go`, `LICENSE`, `README.md`.
2. **Core client (TDD).** `errors.go` → `retry.go` → `idempotency.go` →
   `types.go` (facade ↔ generated proto + protojson) → `options.go` →
   `client.go`, each with unit tests written first (httptest server + injected
   clock and `http.RoundTripper`).
3. **Example CLI.** `examples/ingest-cli/` reading flags/env, calling `Ingest`.
4. **e2e harness.** `scripts/e2e.sh` mirroring `sdk/node/scripts/e2e.sh`: boot
   `pgvector:pg17`, build + run the server from `../..`, provision tenant +
   `ingest:write` key, run live ingest, assert DB rows, idempotency dedup, and
   concurrent same-key dedup.
5. **CI.** Add a `sdk-go` job to `.github/workflows/ci.yml` (path filter
   `sdk/go/**`): `go vet`, `go build ./...`, `go test -race ./...`, `gofmt -l`,
   build `examples/ingest-cli`. Extend the `changes` filter so SDK-only PRs
   trigger it.
6. **Release workflow.** `.github/workflows/sdk-go-release.yml` on tag
   `sdk/go/v*`: verify the tag matches `version.go`, run the gates, create a
   GitHub Release with notes from CHANGELOG. (No registry push — the Go module
   proxy fetches the tag on demand; optionally warm `proxy.golang.org`.)
7. **Docs + changelog.** SDK README (install, quickstart, the `@vX.Y.Z` tag
   note), root README v0.6 line update, `CHANGELOG.md` `### Added`.

## Verification

- **Unit:** happy path; error paths `401/403/409/413/429/500`; transport
  network + timeout + ctx-cancel; retry count + backoff sequence (deterministic
  via injected jitter/clock); `Retry-After` (seconds and HTTP-date); idempotency
  key auto-gen, stability across retries, and override; CRLF rejection on
  apiKey/idempotencyKey; redirect (3xx) surfaced as error, not followed; 1 MiB
  body cap; `code`-not-`message` error mapping. Table-driven, sharing the Node
  suite's cases.
- **Examples build** in CI (`sdk-go` job).
- **e2e** (`scripts/e2e.sh`) against a real server + Postgres — this is the
  acceptance gate, not just unit green: live ingest persists, replay with the
  same key yields one row, 8 concurrent same-key calls yield one row.
- **godoc** renders (`go doc ./sdk/go/...` spot-check + doc.go package comment).
- Cite `make ci-check` (or the `sdk-go` subset) output in the PR per CLAUDE.md
  §9 — no asserting green without evidence.

## References

- Issue #36; paired issue #37 (Node SDK, landed); #19 (proto IDL, landed).
- `docs/proposals/2026/06/2026-06-19-node-typescript-sdk.md` — the shared
  retry/error/idempotency contract (sections "Retry / error contract" and the
  Go-SDK callouts).
- `proto/attune/v1/ingest.proto`, `proto/attune/v1/common.proto` — wire types.
- `internal/handlers/ingest.go` — server-side `protojson` binding the SDK
  mirrors.
- `sdk/node/` — reference implementation (`src/client.ts`, `src/retry.ts`,
  `src/errors.ts`, `scripts/e2e.sh`).
- `buf.gen.yaml`, `Makefile` (`proto` target) — codegen pipeline.
- `.github/workflows/ci.yml` (`sdk-node` job), `.github/workflows/sdk-release.yml`
  — CI/release pattern.
