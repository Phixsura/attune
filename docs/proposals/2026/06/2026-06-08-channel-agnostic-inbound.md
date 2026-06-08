# Channel-agnostic inbound framework + integral Lark removal

| | |
|---|---|
| **Issue** | #66 |
| **Status** | Implemented |
| **Started** | 2026-06-08 |
| **Related** | #19 (canonical proto contract, CLOSED — provides `pb.IngestRequest`), #34 (outbound notify adapter SDK — symmetric counterpart), #35 (email-to-feedback gateway — folded in here), #40 (console SSO/OIDC — post-this), #7 (private-deploy docs — needs follow-up), #63 (Grafana panels — consumes new metrics), #48 (logext facade — applies to new code) |

## Problem

Lark is a first-class citizen of attune's core domain:

- `internal/domain/feedback.go:31-41` hard-codes five `lark-*` source enums (`lark-group`, `lark-bitable`, `lark-approval`, `lark-helpdesk`, `lark-form`) inside `ValidSources`, the channel-agnostic core's input validator.
- `internal/handlers/lark.go` owns `POST /v1/lark/event` and is wired in `cmd/attune/router.go:63` (`r.Mount("/lark", larkHandler.Routes())`).
- `internal/infra/lark/` holds signature / event-decode protocol logic that core dispatch needs to know exists.
- The console's only real-user login path is Lark OAuth (`internal/handlers/console/oauth/oauth.go:31`: "_the console is Lark-only today and YAGNI applies_"); `dev_login` is a backdoor explicitly tagged "_must be removed before any real user-facing deployment_".
- Outbound: `internal/notify/adapter/larkwebhook/` ships a Lark notify target; `internal/repo/lark/` holds tenant installs; `tenants.lark_install` + `tenant_users.lark_open_id` columns persist Lark identity.

Two compounding problems flow from this:

1. **Engineering**: every new channel — generic webhook, email (#35), Slack, RSS — would leak its specifics into the core, validators, domain, and contract; the surface re-couples with each addition.
2. **Product**: Lark-as-default cements attune into one IM ecosystem. attune's stated direction is the opposite — a **bidirectional product intelligence hub** that ingests both feedback (what users say to you) and market signals (what the world says about your product, competitors, niche).

A prior proposal — `2026-06-06-inbound-adapter-framework.md` — sketched a "de-root + first-adapter" path that kept Lark as the inaugural adapter. It is **superseded by this one** because:

- It assumed `Ingestor.Ingest(ctx, *pb.IngestRequest)`, but post-#19 the real signature is `IngestRow(ctx, tenantID, keyID, domain.IngestInput)` — three params, `domain.IngestInput` not `pb.IngestRequest`.
- It split work into Phase 1/2/3 incrementally; we ship all phases in one PR.
- It preserved Lark via a legacy-alias map; product direction is to **remove Lark entirely** (no customer data preserved; pre-1.0; no production users).
- It under-scoped Console — silently inheriting the Lark-OAuth-only login.

That file is deleted in this PR.

## Vision (long-term)

**attune is a Go + AI-native + truly OSS bidirectional product intelligence hub, with bilingual / cross-platform / cross-IM first-class support.**

One normalized event stream consumes both *passive feedback* (webhook / email / in-app widget / Slack helpdesk) and *active intelligence* (RSS / web scraper / competitor monitor / trends / news / social media / Reddit / HN / GitHub Trending / Product Hunt / App Store reviews / Google Alerts). One LLM enrichment pipeline classifies, dedupes, and produces insight.

**Differentiation, mapped on the OSS landscape:**

- Sentry / Linear / Statuspage → feedback only, no active monitoring
- Huginn / n8n / Activepieces → active only; Ruby/TS; not AI-native
- Mention / Brand24 / Visualping → active strong, but closed-source and not self-hosted
- Miniflux → Go + self-hosted + Apache 2.0, but read-only RSS consumption only
- TRENDRADAR → validates Chinese-market demand at 59k stars, but Python monolith, no abstraction, single maintainer

No OSS today combines: **Go single-binary self-deploy + true OSI license + AI-native enrichment + multi-tenant + 4-mode inbound + Chinese-platform/IM first-class**. That intersection is attune's defensible position.

**This PR's role:** the **infrastructure PR** for that vision. It cuts the Lark-bound legacy out, lands a four-mode-capable Adapter framework, and ships two first-batch adapters (webhook + email IMAP — the minimum complete passive set). RSS / scraper / Chinese platforms / MQ / social / MCP server all become *additive* PRs that touch zero core code.

### Long-term source taxonomy (framework's ceiling)

The Adapter port (defined in §Design) absorbs all four execution modes — push / poll / schedule / stream — with no signature change:

```
Push                              Poll                              Schedule                       Stream
────                              ────                              ────────                       ──────
✓ webhook  (this PR)              ✓ email-IMAP  (this PR)           ─ web-scraper                 ─ mq-mqtt
─ slack-events                    ─ rss / atom / jsonfeed           ─ producthunt-daily           ─ mq-kafka
─ discord-events                  ─ github-trending                 ─ hackernews-top              ─ mq-amqp / nats
─ telegram-bot                    ─ reddit-search                   ─ appstore-reviews            ─ mq-redis-streams
─ webform (in-app widget)         ─ youtube-comments                ─ playstore-reviews           ─ social-firehose
─ wework-events                   ─ stackoverflow-tag               ─ sec-edgar-filing            ─ slack-rtm
─ dingtalk-events                 ─ weibo-hot / zhihu-hot           ─ google-alerts-feed
─ feishu-events                   ─ toutiao-hot / douyin-hot        ─ generic-cron-crawler
                                  ─ bilibili-search
```

`✓` = delivered in this PR; `─` = framework already absorbs, future PR.

## Goals

| Category | Goal |
|---|---|
| **Architecture** | Core de-channelized: `internal/service/ingest` does not know which channel produced an event. |
| | `internal/inbound/` framework absorbs push / poll / schedule / stream with zero interface change. |
| | Adding a channel = a new package + one blank-import line + one `ValidSources` entry. (The `ValidSources` step folds away once #95 makes the enum registry-driven.) |
| **Code cleanup** | The string `lark` / `Lark` / `LARK` / `Feishu` / `FEISHU` / `飞书` no longer appears anywhere in the repo except `CHANGELOG.md` "Removed". |
| | Console login does not depend on Lark; local admin password is added. |
| **Delivery** | First-batch adapters: `webhook` (push) + `email` IMAP (poll). |
| | `inbound_sources` table with N:1 per tenant, encrypted secret/credential storage, Console management UI. |
| | CI depguard rule blocks `service|handlers|repo|infra|notify|domain` → `internal/inbound/adapter/*`. |
| **Observability / Testing** | Unified `attune_inbound_total{channel, tenant, source_slug, result}` metric across adapters. |
| | Conformance test suite every adapter must pass. |

## Non-goals

(Framework already designed to absorb these — they ship in follow-up PRs.)

| Not in this PR | Path when added later |
|---|---|
| RSS / Atom / JSON Feed inbound | poll mode + fork/borrow Miniflux parsers |
| Weibo / Zhihu / Toutiao / Bilibili / Douyin inbound | poll mode + upstream API or self-built crawler (Chinese-market priority) |
| Web scraper / page-diff (Visualping-style) | schedule mode + optional CSS/XPath/Readability module |
| Social / X / Reddit / HN / Product Hunt / App Store reviews | poll or stream mode |
| MQ subscription (MQTT / Kafka / AMQP / NATS / Redis Streams) | stream mode |
| Chinese IM outbound (Feishu / DingTalk / WeWork / Bark) | outbound notify adapter SDK (#34) + a channel pack |
| MCP server exposing attune as an agent-accessible tool surface (search / triage / classify / record signals as a first-class principal, with per-agent attribution and auditable actions) | separate server / package; not in the inbound framework; warrants its own design proposal + tracking issue |
| Inbound master key rotation (HKDF + per-source DEK + envelope versioning) | #94 — this PR ships a `key_id` byte in the envelope ciphertext for forward-compat; full rotation lands separately |
| Audit log entries for `inbound_sources` lifecycle (create / rotate / pause / delete) | folds into #39 — audit log for sensitive console actions |
| Making `domain.ValidSources` registry-driven (adapter-declared source names) | #95 — this PR still edits `ValidSources` for `"webhook"`, `"email"`; refactor follows once the pattern is settled |
| Console OIDC / SSO | reserved for #40 |
| Outbound notify adapter SDK itself | #34 |
| Webhook transform DSL (JS sandbox / JSONPath mapping) | clients adapt their own payload; later, a webhook-adapter sub-config |
| Per-source inbound rate limit | existing process-level rate limit remains |
| Any Lark data retention / migration | hard delete; no preservation |

## Design

### Architecture overview

```
                       ┌─────────────────────────────────────────────┐
                       │            cmd/attune (main)                │
                       │   blank-import inbound/adapter/{webhook,email} │
                       │   m := inbound.NewManager(deps); m.StartAll(ctx) │
                       └────────────────────┬────────────────────────┘
                                            │ Deps{ Mux, Ingest, Secrets, Sources, Metrics, Logger }
            ┌───────────────────────────────┴───────────────────────────────┐
            ▼                                                                ▼
  ┌─────────────────────┐                                       ┌──────────────────────┐
  │ internal/inbound/   │ ← framework: port + registry + Deps    │ internal/inbound/    │
  │   inbound.go        │                                        │   adapter/webhook/   │
  │   registry.go       │                                        │   adapter/email/     │
  │   secrets.go        │                                        │                      │
  │   sources.go        │                                        │ init() ⇒ Register    │
  │   metrics.go        │                                        │                      │
  │   mux.go            │ ── chi sub-router ────────────────▶   │                      │
  └─────────┬───────────┘                                        └──────────────────────┘
            │ IngestPort.Ingest(ctx, tenantID, keyID, in)
            ▼
  ┌──────────────────────────────────────────────────────────────────────────────────┐
  │ internal/service/ingest/ingestor.go   ← CORE · channel-blind                      │
  │   IngestRow → validate → repo.Insert(user_feedback) → fireEnrich (async)         │
  └──────────────────────────────────────────────────────────────────────────────────┘

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  depguard:  service/+handlers/+repo/+infra/+notify/+domain ⊥ internal/inbound/adapter/*
  depguard:  internal/inbound/(framework)                   ⊥ internal/inbound/adapter/*
             (cmd/attune is the only legal blank-import site)
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### CLAUDE.md §5 layering increment

`internal/inbound` itself never imports `internal/service` — the `IngestPort` interface is defined inside `internal/inbound` and `cmd/attune` provides the adapter (a 3-line shim from `service.Ingestor.IngestRow` to `inbound.IngestPort.Ingest`). The arrow below points to where the contract is satisfied at wire-up time, not where source-level imports go.

```
handlers  →  service  →  repo
                       →  notify
                       →  infra/llmclient
handlers  →  domain
─── added: ───────────────────────────────────────────────────────────────
inbound   ↛  service / handlers / repo / infra / notify   (NO direct imports)
              IngestPort is defined IN inbound; cmd/attune adapts service.Ingestor → inbound.IngestPort
inbound/adapter/<channel>  →  inbound (framework)
                          ↛  inbound/adapter/<other>   (adapters never import siblings)
                          ↛  service / handlers / repo / infra / notify   (no downward reach)
cmd/attune  →  inbound, inbound/adapter/*               (sole blank-import entrypoint)
```

### Port interface — `internal/inbound/inbound.go`

```go
// Package inbound is attune's channel-agnostic ingest framework.
//
// "Inbound" covers ANY event landing in attune's normalized ingest path —
// regardless of who initiates the TCP connection: webhooks are remote-
// initiated (push), IMAP/RSS pollers are attune-initiated (pull), MQ
// subscribers stream, schedulers crawl on a cron. All four modes implement
// the same Adapter interface.
//
// Hard rule: no package under internal/service|handlers|repo|infra|notify
// may import internal/inbound/adapter/*. cmd/attune is the only legal
// blank-import site. Enforced by golangci-lint depguard.
package inbound

import (
    "context"
    "net/http"
    "time"

    "github.com/google/uuid"

    "github.com/Phixsura/attune/internal/domain"
)

// Adapter — every channel implements this.
//
// Mode mapping:
//   push:     Start mounts deps.Mux routes; Shutdown is a no-op.
//   poll:     Start spawns a ticker goroutine; Shutdown cancels & waits.
//   schedule: Same as poll with a cron expression instead of a fixed tick.
//   stream:   Start opens a subscription + spawns a reader goroutine;
//             Shutdown closes the subscription & waits.
//
// Start MUST return quickly (OTel Component contract). Long-running work
// uses context.WithCancel(context.Background()) stored on the receiver;
// Shutdown cancels it and waits on a sync.WaitGroup.
type Adapter interface {
    Channel() string                              // "webhook" | "email" — unique within registry
    Start(ctx context.Context, deps Deps) error
    Shutdown(ctx context.Context) error
}

// Mux — narrow router-agnostic surface the framework hands to push adapters.
// We deliberately do NOT type this as `chi.Router` so the framework boundary
// stays decoupled from chi. cmd/attune passes a chi sub-router that satisfies
// this interface; in tests inboundtest supplies a stdlib http.ServeMux wrapper.
type Mux interface {
    Method(method, pattern string, h http.Handler)
}

// Deps — handed to every adapter at Start.
// Add fields here only when a dependency becomes universal across adapters;
// adapter-specific config comes from inbound_sources.config (encrypted JSON).
type Deps struct {
    Mux     Mux              // adapter mounts HTTP routes (push adapters only)
    Ingest  IngestPort       // canonical normalize → persist
    Sources SourceStore      // load + update inbound_sources rows
    Secrets SecretStore      // envelope encrypt/decrypt per-source credentials
    Metrics InboundMetrics
    Logger  Logger
}

// ShutdownTimeouter — optional role (Caddy-style). If an Adapter implements it,
// the Manager honours the per-adapter deadline instead of the framework default
// (15s). IMAP/MQ/stream adapters typically declare > 5s; webhook adapters can
// return 0 (immediate).
type ShutdownTimeouter interface {
    ShutdownTimeout() time.Duration
}

// IngestPort — adapters call this to reach the core. Signature mirrors the
// existing service.Ingestor.IngestRow so cmd/attune wiring is a trivial shim
// (no parallel ingest code path).
//
// keyID is uuid.Nil for inbound-adapter-sourced rows; the originating
// inbound_sources.id flows through SourceMeta["inbound_source_id"].
type IngestPort interface {
    Ingest(
        ctx context.Context,
        tenantID string,
        keyID uuid.UUID,
        in domain.IngestInput,
    ) (feedbackID int64, err error)
}
```

### Registry — `internal/inbound/registry.go`

```go
package inbound

import (
    "context"
    "errors"
    "fmt"
    "sort"
    "sync"
    "time"

    "github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Factory — adapter package's init() calls Register(channel, factory).
type Factory func() Adapter

// Entry — what Factories() returns. Named struct (not anonymous) so the public
// API is consumable: range, map, sort, etc.
type Entry struct {
    Channel string
    Factory Factory
}

// DefaultShutdownTimeout — applied when an Adapter does NOT implement
// ShutdownTimeouter. Set high enough for IMAP LOGOUT half-closes; webhook
// adapters that need immediate return implement ShutdownTimeouter and return 0.
const DefaultShutdownTimeout = 15 * time.Second

var (
    mu        sync.RWMutex
    factories = map[string]Factory{}
)

// Register — called from each adapter package's init(). Panics on duplicate
// channel name (compile-time-equivalent guarantee).
func Register(channel string, factory Factory) {
    mu.Lock()
    defer mu.Unlock()
    if _, exists := factories[channel]; exists {
        panic(fmt.Sprintf("inbound: channel %q already registered", channel))
    }
    factories[channel] = factory
}

// ResetForTest — clears the registry. Build-tag-gated so production binaries
// cannot call it. Allows tests that import multiple adapter packages
// transitively to avoid the "panic on package load" race.
//
//go:build test
func ResetForTest() {
    mu.Lock()
    defer mu.Unlock()
    factories = map[string]Factory{}
}

// Factories — snapshot for cmd/attune. Returns a sorted slice for deterministic
// startup order.
func Factories() []Entry {
    mu.RLock()
    defer mu.RUnlock()
    out := make([]Entry, 0, len(factories))
    for ch, f := range factories {
        out = append(out, Entry{Channel: ch, Factory: f})
    }
    sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
    return out
}

// Manager — orchestrates Start/Shutdown across all registered adapters.
type Manager struct {
    deps     Deps
    adapters []Adapter
}

func NewManager(deps Deps) *Manager { return ptrext.Of(Manager{deps: deps}) }

// StartAll — starts every registered adapter in deterministic order.
// On any single failure, already-started adapters are shut down with their
// per-adapter deadline (see shutdownStarted).
func (m *Manager) StartAll(ctx context.Context) error {
    for _, entry := range Factories() {
        a := entry.Factory()
        if err := a.Start(ctx, m.deps); err != nil {
            _ = m.shutdownStarted(context.Background()) // ctx may be cancelled; honour per-adapter timeouts
            return fmt.Errorf("inbound: start %q: %w", entry.Channel, err)
        }
        m.adapters = append(m.adapters, a)
    }
    return nil
}

// ShutdownAll — reverse order (OTel pattern: last started, first stopped).
// Aggregates errors via errors.Join.
func (m *Manager) ShutdownAll(ctx context.Context) error {
    return m.shutdownStarted(ctx)
}

// shutdownStarted — each adapter gets its own deadline (DefaultShutdownTimeout
// or what ShutdownTimeouter declares). A wedged adapter does not steal the
// budget from the next one.
func (m *Manager) shutdownStarted(parent context.Context) error {
    var errs []error
    for i := len(m.adapters) - 1; i >= 0; i-- {
        a := m.adapters[i]
        budget := DefaultShutdownTimeout
        if t, ok := a.(ShutdownTimeouter); ok {
            budget = t.ShutdownTimeout()
        }
        ctx, cancel := context.WithTimeout(parent, budget)
        if err := a.Shutdown(ctx); err != nil {
            errs = append(errs, fmt.Errorf("inbound: shutdown adapter %d: %w", i, err))
        }
        cancel()
    }
    return errors.Join(errs...)
}
```

### Supporting types — `internal/inbound/{secrets,sources,metrics,logger}.go`

```go
// SecretStore — envelope encryption. v1: AES-GCM with ATTUNE_INBOUND_MASTER_KEY
// env var. v2 may swap KMS / Vault behind the same interface.
type SecretStore interface {
    Encrypt(plaintext []byte) (ciphertext []byte, err error)
    Decrypt(ciphertext []byte) (plaintext []byte, err error)
}

// SourceStore — adapter reads its configured rows from inbound_sources.
type SourceStore interface {
    List(ctx context.Context, channel string) ([]Source, error)
    Get(ctx context.Context, id string) (Source, error)
    GetBySlugs(ctx context.Context, tenantSlug, channel, sourceSlug string) (Source, error)
    UpdateState(ctx context.Context, id string, state SourceState) error
    SetEnabled(ctx context.Context, id string, enabled bool, reason string) error
}

type Source struct {
    ID       string
    TenantID string
    Channel  string
    Name     string
    Slug     string
    Config   []byte // encrypted JSON; channel-specific schema (each adapter unmarshals)
    Enabled  bool
    State    SourceState
}

type SourceState struct {
    LastEventAt *time.Time
    LastError   string
    LastUID     int64 // email IMAP / RSS cursor
}

// InboundMetrics — framework-injected labels; adapters call methods, not
// constructors, so cardinality stays bounded.
type InboundMetrics interface {
    Total(channel, tenant, sourceSlug, result string)                  // attune_inbound_total
    Latency(channel, tenant, sourceSlug string, seconds float64)       // attune_inbound_latency_seconds
    SetSourceState(channel, tenant, sourceSlug, state string, on bool) // attune_inbound_source_state
    SetPollLag(channel, tenant, sourceSlug string, seconds float64)    // attune_inbound_poll_lag_seconds
}

// Logger — logext facade subset, ctx-first. Adapters call this so they never
// import log/slog directly (CLAUDE.md §7).
type Logger interface {
    Infof(ctx context.Context, format string, args ...any)
    Warnf(ctx context.Context, format string, args ...any)
    Errorf(ctx context.Context, format string, args ...any)
}
```

### Conformance test suite — `internal/inbound/inboundtest/`

A standalone subpackage (mirrors `httptest`, `iotest`, `fstest`) so the main `internal/inbound` package never imports `testing`:

```go
package inboundtest

import (
    "context"
    "testing"
    "time"

    "go.uber.org/goleak"

    "github.com/Phixsura/attune/internal/inbound"
)

// TestAdapterContract — every adapter calls this from its own _test.
// Minimum bar (5 lifecycle gates):
//   1. Channel() returns a non-empty string with no whitespace or '/'.
//   2. Start(ctx, mockDeps) followed by immediate Shutdown does not panic.
//   3. ctx cancellation propagates: Shutdown returns within 5s, no goroutine leak.
//   4. Idempotent shutdown: calling Shutdown twice does not panic.
//   5. Duplicate Register on the same channel panics.
//
// IngestPort end-to-end coverage is delegated to each adapter's own
// adapter-level test where the fixture (httptest for webhook, fake
// imapclient for email) is naturally available; the conformance suite
// stays focused on lifecycle so it can run without channel-specific
// scaffolding.
func TestAdapterContract(t *testing.T, factory inbound.Factory) { … }
```

### Webhook adapter — `internal/inbound/adapter/webhook/`

**URL contract**

| Field | Value |
|---|---|
| Path | `POST /v1/inbound/webhook/{tenant-slug}/{source-slug}` |
| Content-Type | `application/json` |
| Body | canonical `attunev1.IngestRequest` JSON, size cap 64 KiB (matches `/v1/feedback/ingest`) |
| Tenant resolution | `{tenant-slug}` → `tenants.slug` → `tenants.id` |
| Source resolution | `(tenants.id, channel='webhook', slug={source-slug})` → unique row |

**Signature contract (Stripe / Slack-style)**

```
Header:   X-Attune-Timestamp:  <unix-seconds>
Header:   X-Attune-Signature:  sha256=<hex-digest>

Digest =  HMAC-SHA256( secret_raw_bytes, "<timestamp>.<request-body-bytes>" )

Server:
  if |now - timestamp| > 300s    → 401 (result="auth_err")
  if !subtle.ConstantTimeCompare(digest, current_secret_digest) {
      if !subtle.ConstantTimeCompare(digest, previous_secret_digest) || previous_expired
          → 401 (result="auth_err")
  }
```

**Enumeration resistance.** Unknown `{tenant-slug}/{source-slug}` MUST return the
same 401 response as a signature mismatch — never 404. The handler runs HMAC
verify against a per-process stub secret (random at boot) for unknown sources so
the response time and body match a real auth failure. Metric is recorded as
`result="auth_err"` regardless of whether the source existed. This blocks
tenant/source enumeration via timing or status-code differential.

**Webhook source row's `config` (decrypted)**

```json
{
  "version": 1,
  "secret_current_encrypted": "<base64 envelope ciphertext>",
  "secret_previous_encrypted": null,
  "previous_expires_at": null,
  "hmac_algo": "sha256",
  "created_at": "2026-06-08T12:34:56Z",
  "rotated_at": null
}
```

**Rotate behavior**

`POST /fb/v1/console/inbound/sources/{id}/rotate-secret`:

1. If `previous_expires_at IS NOT NULL AND previous_expires_at > now()` → **reject with 409 Conflict** + body `{ "error": "rotation_in_grace_window", "next_eligible_at": <iso8601> }`. This prevents a double-rotate from overwriting the still-valid `previous` and stranding clients mid-roll.
2. Otherwise: generate 32 fresh random bytes → new secret.
3. Single atomic SQL: `UPDATE inbound_sources SET config = jsonb_build_object(... 'secret_current_encrypted', encrypt(new_secret), 'secret_previous_encrypted', config->>'secret_current_encrypted', 'previous_expires_at', now() + interval '24 hours', ...) WHERE id = $1`. One round-trip, one commit; no intermediate states observable to concurrent requests.
4. Return new secret once (response body); never persisted in plaintext.

Adapter HMAC verify tries `current` first, falls back to `previous` if `previous_expires_at` in the future. Standard dual-secret 24h overlap (Hookdeck / Stripe convention).

### Master key — envelope encryption + forward-compat for rotation

All `secret_*_encrypted` values use AES-GCM-256 envelope encryption with
`ATTUNE_INBOUND_MASTER_KEY` (exactly 32 bytes, hex- or base64-decoded). The
ciphertext layout is:

```
| 1 byte version | 1 byte key_id | 12 bytes nonce | ciphertext | 16 bytes auth tag |
```

- **`version=0x01`** identifies this envelope format.
- **`key_id=0x00`** identifies the master key — this PR ships a single key, but
  the byte is reserved so #94 (master key rotation) can introduce `key_id=0x01,
  0x02 …` without a one-shot re-encrypt of the table.

**Boot validation.** At startup, before `Manager.StartAll`, attune asserts:

1. `ATTUNE_INBOUND_MASTER_KEY` is set;
2. After decoding (hex preferred, base64 fallback) it is exactly 32 bytes.

Either check failing means the process **refuses to start** with a fatal log
line naming the env var. Lazy validation (first decrypt) is explicitly rejected:
operators must learn about misconfig at boot, not from a 500 later.

**Handler flow (abridged)**

```go
func (a *adapter) handle(w http.ResponseWriter, r *http.Request) {
    const where = "inbound.webhook.handle"
    ctx := r.Context()

    tenantSlug := chi.URLParam(r, "tenant-slug")
    sourceSlug := chi.URLParam(r, "source-slug")

    src, srcErr := a.deps.Sources.GetBySlugs(ctx, tenantSlug, "webhook", sourceSlug)

    // Read body BEFORE branching on srcErr so timing does not leak source
    // existence. MaxBytesReader caps unauthenticated work at 64 KiB.
    body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
    if err != nil { /* 400 validate_err */ }

    ts := r.Header.Get("X-Attune-Timestamp")
    sig := r.Header.Get("X-Attune-Signature")

    if srcErr != nil || !src.Enabled {
        // Constant-time verify against a per-process stub secret so unknown
        // sources are indistinguishable from auth failures (enumeration
        // resistance). The result is always 401.
        _ = verifyHMACAgainstStub(a.stubSecret, ts, body, sig)
        a.deps.Metrics.Total("webhook", tenantSlug, sourceSlug, "auth_err")
        writeError(ctx, w, 401, "unauthorized", "signature/timestamp invalid")
        return
    }

    cfg, err := src.parseWebhookConfig(a.deps.Secrets)
    if err != nil { /* 500 internal_err */ }

    if !verifyHMAC(cfg, ts, body, sig) {
        a.deps.Metrics.Total("webhook", src.TenantID, sourceSlug, "auth_err")
        writeError(ctx, w, 401, "unauthorized", "signature/timestamp invalid")
        return
    }

    var req attunev1.IngestRequest
    if err := protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(body, &req); err != nil {
        /* 400 validate_err */
    }

    in := domain.IngestInput{
        Source:     "webhook",
        Content:    req.GetContent(),
        SourceUser: req.GetSourceUser(),
        PageURL:    req.GetPageUrl(),
        SourceMeta: mergeMeta(req.GetSourceMeta(), map[string]any{
            "inbound_source_id":   src.ID,
            "inbound_source_name": src.Name,
        }),
    }
    id, err := a.deps.Ingest.Ingest(ctx, src.TenantID, uuid.Nil, in)
    if err != nil { /* 400 or 500 by errors.Is */ }

    a.deps.Metrics.Total("webhook", src.TenantID, sourceSlug, "ok")
    _ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
        LastEventAt: ptrext.Of(time.Now()),
    })
    writeJSONProto(w, 200, &attunev1.IngestResponse{Id: id, EnrichmentStatus: "pending"})
}
```

### Email IMAP adapter — `internal/inbound/adapter/email/`

**Email source row's `config` (decrypted)**

```json
{
  "version": 1,
  "host": "imap.gmail.com",
  "port": 993,
  "tls": true,
  "username": "feedback@team.com",
  "password_encrypted": "<base64 envelope ciphertext>",
  "folder": "INBOX",
  "poll_interval_seconds": 60,
  "start_from": "now",
  "after_ingest": "mark_seen"
}
```

`start_from` ∈ `{"now", "all_unseen"}` (default `now` — skip backlog).
`after_ingest` ∈ `{"mark_seen", "keep_unseen", "move_to:<folder>"}` (default `mark_seen` — matches helpdesk convention; `keep_unseen` available for users who want their mailbox UI to keep new-message indicators).

**Library choices**

- IMAP client: `github.com/emersion/go-imap/v2` — pure Go, ~3k stars, active. Adds to `go.mod`.
- MIME parsing: `github.com/emersion/go-message` — same author, handles multipart / charset / encoded headers. Adds to `go.mod`.

Depguard ensures these imports stay inside `internal/inbound/adapter/email/` only.

**pollLoop**

```go
func (a *adapter) pollLoop(ctx context.Context) {
    defer a.wg.Done()
    for {
        sources, _ := a.deps.Sources.List(ctx, "email")
        for _, src := range sources {
            if !src.Enabled { continue }
            srcCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
            a.pollSource(srcCtx, src)
            cancel()
            if ctx.Err() != nil { return }
        }
        select {
        case <-time.After(60 * time.Second):
        case <-ctx.Done():
            return
        }
    }
}
```

`pollSource`:

1. Decrypt creds via `deps.Secrets`.
2. `imap.DialTLS` → `Login` → `Select(folder)`.
3. `UIDSearch(UID > state.LastUID)`.
4. For each UID (in order, up to a per-tick cap of 100): Fetch RFC822 → parse → build `domain.IngestInput` → `deps.Ingest.Ingest(...)` → on success update `LastUID` cursor → apply `after_ingest` policy (`mark_seen` / `move_to:<folder>` / no-op).
5. Update `LastEventAt`.

**Error fallback**

| Error class | Behaviour |
|---|---|
| Transient (network blip, 5xx) | Record `LastError`; next tick retries; **do not** disable source. |
| Auth failure (`AUTHENTICATIONFAILED`) | Record `LastError`; **`enabled=false`** automatically; admin must reconnect via Console. |
| Parse failure (one malformed message) | Record `result=validate_err` in metrics; advance `LastUID` (otherwise loop wedges); continue. |
| Context cancellation (shutdown) | Finish in-flight Fetch; return; goroutine exits cleanly. |

**Test connection endpoint (Console new-source wizard)**

```
POST /fb/v1/console/inbound/sources/test-connection
body: {channel:"email", config:{host,port,tls,user,pass,folder}}
→ Dial + Login + Select(folder) + Logout (no message fetch).
→ 200 {ok:true} | 200 {ok:false, error:"…"}
```

### Console: local admin password — `internal/handlers/console/auth/`

Naming: package `auth` (not `login`) leaves room for follow-on auth flows (OIDC / SSO via #40).

**`admins` schema**

```sql
CREATE TABLE admins (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,           -- bcrypt cost 12
    display_name    TEXT NOT NULL DEFAULT '',
    role            TEXT NOT NULL DEFAULT 'admin',  -- broadens with #38 RBAC
    failed_attempts INT  NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX admins_email_lower ON admins (LOWER(email));
```

**Bootstrap**

On startup, behind a Postgres advisory lock to make multi-pod start races safe:

```
SELECT pg_advisory_lock(hashtext('attune_bootstrap_admin'));

SELECT COUNT(*) FROM admins;
IF count > 0:
    pg_advisory_unlock(...); logext.Infof(ctx, "[bootstrap] %d admin(s) exist, skip", count); RETURN

email := env.GetOrFile("ATTUNE_BOOTSTRAP_ADMIN_EMAIL")     // *_FILE variant preferred
pass  := env.GetOrFile("ATTUNE_BOOTSTRAP_ADMIN_PASSWORD")  // *_FILE variant preferred
IF email == "" OR pass == "":
    pg_advisory_unlock(...); RETURN fatal "no admins exist and ATTUNE_BOOTSTRAP_ADMIN_{EMAIL,PASSWORD}[_FILE] are unset"

INSERT INTO admins(email, password_hash=bcrypt(pass, cost=12), role='admin', ...)
    ON CONFLICT (email) DO NOTHING;       -- belt-and-braces alongside the advisory lock

pg_advisory_unlock(...);
logext.Warnf(ctx, "[bootstrap] created first admin %s — change password and unset env immediately", email)
```

The `*_FILE` variants are the recommended path on Linux: `ATTUNE_BOOTSTRAP_ADMIN_PASSWORD_FILE=/run/secrets/admin_password` avoids password exposure via `/proc/<pid>/environ` to same-UID processes. Implementation uses `env.GetOrFile(name)` which prefers `<name>_FILE` if set and falls back to `<name>`.

**Fail-fast contract**:

1. `admins` empty + neither env nor `*_FILE` set → process exits with non-zero status.
2. Advisory lock held by another pod → second pod waits, then sees row exists, logs skip.
3. After first successful bootstrap, deployer doc instructs `unset ATTUNE_BOOTSTRAP_ADMIN_*` to remove the lingering env.

**Login handler**

```
POST /fb/v1/console/install/login         body: {email, password, redirect_uri?}
   → lookup admin where LOWER(email)=LOWER($) and (locked_until is null or expired)
   → if row exists:    bcrypt.CompareHashAndPassword(row.hash, password)
     if row NOT exist: bcrypt.CompareHashAndPassword(stubHash, password)   ← dummy verify
   → on ok: zero failed_attempts; sign session cookie with attrs below; 302 to safe redirect
   → on fail: if row exists, failed_attempts++; if >= 5, locked_until = now()+15min;
              return 401 "invalid credentials" (same body + same timing as user-not-found)
POST /fb/v1/console/install/logout        clears cookie; 302 /console/login
```

**Timing-safe lookup.** The dummy bcrypt run on user-not-found equalises wall-clock between "unknown email" and "wrong password"; reviewers can otherwise enumerate registered admins via response time.

**Session cookie attributes** (the implementation MUST set all four):

```
Name:     attune_session
HttpOnly: true                    ← blocks JS access
Secure:   true                    ← TLS only; CLAUDE.md §8 already requires HTTPS for non-loopback
SameSite: Lax                     ← blocks cross-site form POST CSRF
Path:     /                       ← console SPA + /install/* under same path
Max-Age:  symmetric with session.Signer expiry
```

**Post-login redirect** uses the existing `redirectIsSafe(baseURL, redirect_uri)` helper retained from the deleted `internal/handlers/console/oauth/oauth.go` (moved to `internal/handlers/console/auth/redirect.go`). It enforces:
- non-empty, starts with single `/`, not `//` (protocol-relative)
- no backslashes / control chars
- combined `baseURL + redirect_uri` parses to the same scheme + host as `baseURL`

Login responses never distinguish "unknown email" vs "wrong password" (dummy bcrypt + generic 401 text + identical headers). The `dev_login` backdoor is removed entirely; `ConsoleDevLogin` and `ConsoleInsecureCookies` config flags are removed.

### Data migrations

**Migration runner.** attune uses `golang-migrate/migrate` (already in `go.mod`).
Each `.sql` file is one Postgres transaction. The runner records the highest
applied version in `schema_migrations`; on failure mid-batch, the version
column reflects the **last successful** file and an operator can re-invoke
the runner to resume from the next file — there is no "poisoned half-applied"
state because each file is atomic.

**Destructive-data guard.** Migration `202606081200_drop_lark.sql` deletes
arbitrary numbers of `user_feedback` rows. Before applying it, the runner
checks:

```
IF env ATTUNE_CONFIRM_LARK_DELETE != "yes":
    n := SELECT COUNT(*) FROM user_feedback WHERE source LIKE 'lark-%'
    IF n > 0:
        ABORT migration with message:
            "Refusing to delete %d lark-* user_feedback rows.
             Set ATTUNE_CONFIRM_LARK_DELETE=yes to proceed, or export those
             rows first. See CHANGELOG.md ### Removed."
```

The check runs as a separate `SELECT` before the destructive migration's
transaction starts, so an aborted run leaves the schema unchanged. On a fresh
install (no rows) or with the env explicitly set, the migration proceeds
normally.

Three migrations, applied in order:

#### `202606081200_drop_lark.sql`

```sql
-- Hard-delete all Lark-bound data + schema. Pre-1.0; no customer retention.
-- Guarded by ATTUNE_CONFIRM_LARK_DELETE env (see "Destructive-data guard" above).
BEGIN;

-- 1. Delete user_feedback rows with lark-* source.
DELETE FROM user_feedback WHERE source LIKE 'lark-%';

-- 2. Delete outbox rows targeting lark channels.
DELETE FROM outbox WHERE channel ILIKE 'lark%';

-- 3. Delete lark-typed notify targets.
DELETE FROM notify_targets WHERE type ILIKE '%lark%';

-- 4. Delete tenant_users with Lark-originated identities.
DELETE FROM tenant_users WHERE user_id LIKE 'ext_00000000-0000-0000-0000-000000000000:%';
ALTER TABLE tenant_users DROP COLUMN IF EXISTS lark_open_id;

-- 5. Drop Lark install columns / tables.
ALTER TABLE tenants DROP COLUMN IF EXISTS lark_install;
DROP TABLE IF EXISTS tenant_lark_install;
DROP TABLE IF EXISTS lark_install;

COMMIT;
```

(Column / table names verified against the live schema during PR implementation; `IF EXISTS` clauses keep the migration safe across minor schema drift.)

#### `202606081201_create_admins.sql`

(See `admins` DDL above.)

#### `202606081202_create_inbound_sources.sql`

```sql
BEGIN;

CREATE TABLE inbound_sources (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel       TEXT NOT NULL,
    name          TEXT NOT NULL,
    slug          TEXT NOT NULL,
    config        BYTEA NOT NULL,              -- AES-GCM(JSON)
    enabled       BOOL NOT NULL DEFAULT TRUE,
    last_event_at TIMESTAMPTZ,
    last_uid      BIGINT NOT NULL DEFAULT 0,
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, channel, slug)
);

CREATE INDEX inbound_sources_channel_enabled
    ON inbound_sources (channel, enabled) WHERE enabled = TRUE;
CREATE INDEX inbound_sources_tenant
    ON inbound_sources (tenant_id, channel);

COMMIT;
```

### New dependencies (`go.mod`)

Per CLAUDE.md §8 every new external dep needs a PR-described justification.
This PR adds:

| Package | Why | Alternatives considered |
|---|---|---|
| `github.com/emersion/go-imap/v2` | Pure-Go IMAP client; only realistic option for the email adapter | `mxk/go-imap` (unmaintained), `BrianLeishman/go-imap` (wraps net/textproto; less complete UID support) |
| `github.com/emersion/go-message` | Pure-Go RFC822 / multipart parser by the same author; handles charset + quoted-printable + base64 | `net/mail` stdlib (parses headers only — no MIME bodies); `jhillyerd/enmime` (CGO, larger surface) |
| `golang.org/x/crypto/bcrypt` | Console password hashing | `argon2id` (better security, but requires tuning per-deploy memory cost; bcrypt cost 12 is well-understood and matches the existing OWASP 2024 baseline) |
| `go.uber.org/goleak` | Goroutine-leak detection in conformance tests | manual `runtime.NumGoroutine` snapshots (brittle) |

`golang.org/x/crypto/bcrypt` and `go.uber.org/goleak` are de-facto standard
extensions of the Go stdlib and carry low supply-chain risk. The two emersion
packages are scoped to `internal/inbound/adapter/email/` only; depguard's
`inbound-framework-isolation` rule keeps them from leaking elsewhere.

### CI boundary guard — `.golangci.yml` depguard rule

```yaml
linters-settings:
  depguard:
    rules:
      # Existing rules retained.
      slog-facade: { ... }

      # Rule 1: core ⊥ adapters.
      # Core / framework code MUST NOT reach into inbound adapters.
      # Adapters self-register via init(); cmd/attune is the only legal
      # blank-import site.
      inbound-boundary:
        list-mode: lax
        files:
          - "$gostd"
          - "internal/service/**"
          - "internal/handlers/**"
          - "internal/repo/**"
          - "internal/infra/**"
          - "internal/notify/**"
          - "internal/domain/**"
          - "internal/inbound/*.go"        # framework files themselves
        deny:
          - pkg: "github.com/Phixsura/attune/internal/inbound/adapter"
            desc: |
              Core / framework code MUST NOT import inbound adapters directly.
              Adapters self-register via init(); cmd/attune is the only legal
              blank-import site. See
              docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md.

      # Rule 2: framework ⊥ downstream business layers.
      # internal/inbound is the channel-agnostic framework. It defines
      # IngestPort (and SourceStore/SecretStore/Metrics) as interfaces; the
      # implementations live in cmd/attune (Ingestor shim) and internal/repo
      # (DB-backed Sources). The framework MUST NOT shortcut and import them.
      inbound-framework-isolation:
        list-mode: lax
        files:
          - "internal/inbound/*.go"        # framework root only — not adapters
        deny:
          - pkg: "github.com/Phixsura/attune/internal/service"
            desc: "inbound framework defines IngestPort; impl is wired by cmd/attune"
          - pkg: "github.com/Phixsura/attune/internal/repo"
            desc: "inbound framework defines SourceStore; impl wired by cmd/attune"
          - pkg: "github.com/Phixsura/attune/internal/handlers"
            desc: "framework is below handlers"
          - pkg: "github.com/Phixsura/attune/internal/notify"
            desc: "inbound and notify are sibling subsystems"
```

`cmd/attune/**` is exempt (depguard `files` allow-list excludes it implicitly in lax mode; the lint passes for cmd/attune's blank imports). The exact YAML attributes are cross-checked against the live `.golangci.yml` during implementation; the design intent here is the contract.

### Observability

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `attune_inbound_total` | Counter | `channel, tenant, source_slug, result` | Inbound events. `result ∈ {ok, validate_err, auth_err, internal_err, not_found, transient_err}` |
| `attune_inbound_latency_seconds` | Histogram | `channel, tenant, source_slug` | adapter receive → `IngestPort.Ingest` return |
| `attune_inbound_source_state` | Gauge | `channel, tenant, source_slug, state` | `state ∈ {enabled, paused, error}`, 1/0 encoded |
| `attune_inbound_poll_lag_seconds` | Gauge | `channel, tenant, source_slug` | seconds since last successful poll (poll mode only) |

Cardinality: `tenant × source_slug` is bounded by the `inbound_sources` table size; no free-form values reach the label space. Error text lives in `inbound_sources.last_error`, never in a metric label.

**Tracing**: each inbound event produces a root span with attributes `attune.inbound.{channel,tenant_id,source_id,source_slug,result}` + `attune.feedback.id` (on success). The span is parent of the existing enrichment span (chain unchanged).

**Logging**: `logext.Infof / Warnf / Errorf` with `ctx` first (CLAUDE.md §7). Format example:

```
[inbound.webhook.handle] OK,inbound_trace_id:{tid},channel:webhook,tenant_id:{t},source_id:{s},feedback_id:{f}
```

Direct `log/slog` imports stay banned for inbound code; the depguard `slog-facade` rule already covers this.

### File-by-file delta

#### CREATE

```
internal/inbound/
    inbound.go              ← Adapter, Deps, IngestPort
    registry.go             ← Factory, Register, Factories, Manager
    secrets.go              ← SecretStore + AES-GCM impl
    sources.go              ← SourceStore + Source + SourceState
    metrics.go              ← InboundMetrics + Prometheus impl
    logger.go               ← Logger interface (logext-backed)
    mux.go                  ← chi sub-router helper
    inbound_test.go         ← unit: registry duplicate-panic, manager start/stop order
internal/inbound/inboundtest/
    contract.go             ← TestAdapterContract(t, factory) helper
    mocks.go                ← mock IngestPort / SecretStore / SourceStore
internal/inbound/adapter/webhook/
    webhook.go              ← Adapter impl
    hmac.go                 ← HMAC verify + constant-time compare
    handler.go              ← POST handler
    webhook_test.go
    conformance_test.go     ← inboundtest.TestAdapterContract(t, …)
internal/inbound/adapter/email/
    email.go
    poll.go
    imap.go
    parse.go
    email_test.go
    conformance_test.go
    testdata/{simple,threaded,multipart}.eml
internal/repo/inboundsource/
    inbound_sources.go
internal/handlers/console/inbound/
    inbound_handler.go      ← list / create / rotate / pause / delete
    inbound_test.go
internal/handlers/console/auth/
    handler.go              ← POST /install/login + /logout
    password.go             ← bcrypt
    bootstrap.go            ← startup first-admin creation
    auth_test.go
internal/repo/admin/
    admins.go
internal/migrations/
    202606081200_drop_lark.sql
    202606081201_create_admins.sql
    202606081202_create_inbound_sources.sql
console/src/pages/Login.tsx
console/src/pages/InboundSources.tsx
console/src/pages/InboundSourceNew.tsx
console/src/i18n/locales/{en,zh}.json     ← new keys for login + inbound source pages (per #86 i18n)
docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md   (this file)
```

#### DELETE

```
internal/handlers/lark.go
internal/infra/lark/{client,event,signature,signature_test}.go
internal/notify/adapter/larkwebhook/{lark_card,lark_card_test,lark_webhook}.go
internal/repo/lark/lark_install.go
internal/handlers/console/oauth/oauth.go
internal/handlers/console/oauth/dev_login.go
docs/proposals/2026/06/2026-06-06-inbound-adapter-framework.md   (superseded)
```

#### EDIT (Lark-removal + new wiring)

| File | Change |
|---|---|
| `internal/domain/feedback.go` | Drop 5 `lark-*` entries from `ValidSources`; drop 5 cases from `SourceDisplayName`; add `"webhook"`, `"email"` with display names |
| `internal/handlers/ingest.go` | Tighten `boundedSource`; no `lark-*` permitted |
| `cmd/attune/server.go` | Remove `LarkHandler` wiring; add `inbound.NewManager + StartAll`; add console auth handler |
| `cmd/attune/router.go` | Remove `r.Mount("/lark", …)`; add `r.Route("/v1/inbound", inboundMux)` |
| `cmd/attune/{setup,digest,tenant}.go` | Remove Lark default-tenant resolution and config |
| `cmd/attune/main.go` | Blank-import `internal/inbound/adapter/{webhook,email}` |
| `internal/handlers/console/internal/session/session.go` | Remove Lark identity fields |
| `internal/handlers/console/me/me.go` | Drop `lark_open_id` exposure |
| `internal/handlers/console/notifytarget/notify_targets.go` | Drop `lark-webhook` type |
| `internal/infra/config/{config,env}.go` | Drop `LARK_*` env vars, `ConsoleDevLogin`, `ConsoleInsecureCookies` |
| `internal/infra/metrics/metrics.go` | Add 4 `attune_inbound_*` metrics; drop any Lark-only metrics |
| `internal/notify/{notifier,sig,test_send,transport}.go` | Drop Lark notify-type branches |
| `internal/repo/notifytarget/notify_targets{,_alerts}.go` | Drop Lark variants |
| `internal/repo/tenant/{tenant_users,tenants}.go` | Drop `lark_open_id` / `lark_install` references |
| `internal/service/enrich/{enricher,enricher_outbox}.go` | Drop Lark branches |
| `internal/service/outbox/{notifier,outbox_worker,outbox_worker_alerts,digest_weekly}.go` | Drop Lark dispatch |
| `proto/attune/v1/{notify_target,session}.proto` | Drop Lark fields; `make proto` |
| `.golangci.yml` | Add `inbound-boundary` rule (above) |
| `CLAUDE.md` §5 | Inbound layering increment (Design section above) |
| `README.md` | Update architecture diagram; remove Lark integration prose; add inbound framework section |
| `CHANGELOG.md` | Entries listed under §Implementation plan / Release notes |

## Alternatives considered

| Alternative | Why rejected |
|---|---|
| **Keep Lark as the first adapter (old 2026-06-06 proposal)** | User has explicitly chosen total Lark removal. Keeping it as an adapter still leaks Lark identity into proto / docs / file paths, contradicting the product direction. |
| **Telegraf-style split: `Input` (poll) + `ServiceInput` (push) parallel interfaces** | Forces every adapter to choose a side at definition time. Future mixed adapters (e.g., webhook + secondary poll) require dual registration. OTel's unified `Component { Start, Shutdown }` is simpler and equally expressive. |
| **Caddy-style scattered role interfaces (`Module` + `Provisioner` + `Validator` + `CleanerUpper` + `ServeHTTP`)** | Excellent for 100+ modules; over-segmented for attune's 2 first adapters + 6 second-wave adapters. We adopt Caddy's init-time `Register` pattern but keep the role surface as one tight interface. |
| **Hookdeck / Svix "per-source opaque URL + JS transform DSL"** | Right model long-term; out of scope short-term — requires a JS sandbox, transform versioning, console UI for editing, all of which compound the PR. Clients adapt their payload to the canonical schema until that feature is justified. |
| **Better Stack "opaque URL + UI-mapped extraction"** | Same scope problem (Console UI for declarative mapping). |
| **PagerDuty "single URL + routing_key in body"** | Conflicts with per-source independence (N:1). Would require body-level dispatch per adapter — invents a new abstraction layer over HTTP routing. |
| **Bento `Connect + ReadBatch + Close` pull interface** | Designed for pull-only metrics pipelines; doesn't fit push (webhook) cleanly. OTel's `Start/Shutdown` covers both. |
| **Defer console password login; require Lark only** | The user explicitly chose to remove Lark; that leaves only `dev_login`, which is gated and tagged as removal-required. Console would be inaccessible after this PR. |
| **Keep `dev_login` indefinitely** | Backdoor with no production-safe defaults; violates CLAUDE.md §8 security baseline. |
| **Explicit-wire adapter registration (match the existing `internal/notify/adapter/*` pattern, where `cmd/attune/setup.go` directly imports `larkwebhook` etc.)** | Considered. Rejected for inbound because: (1) `init()+blank-import` is the convergent pattern across 4 surveyed Go OSS projects (Bento, Caddy, Telegraf, n8n) — explicit wire is the minority pattern; (2) inbound is expected to grow many more channels (RSS, scraper, social, MQ, Chinese platforms) where additive registration is the harder constraint; (3) the asymmetry with outbound `notify/adapter` is intentional and explicitly delegated to #34, which is expected to converge notify onto the same pattern, not the other way. Outbound today has 3 channels; inbound is being designed for ~10+. |

## Risks / tradeoffs

| Risk | Impact | Mitigation |
|---|---|---|
| Operator upgrades a stale v0.2 install with non-empty Lark data | Data deleted unrecoverably | `CHANGELOG.md` `### Removed` opens with a one-way-upgrade warning; README upgrade section links here |
| Inbound framework start-up failure blocks attune entirely | Service won't boot | `Manager.StartAll` logs each adapter's failure individually; per-source auth failure auto-pauses one source rather than crashing the process; email IMAP transient errors don't disable the source |
| HMAC secret leak | Forged webhooks | `### Security` section instructs `rotate-secret`; dual-secret 24h overlap window means rotation is zero-downtime |
| Email pollLoop wedges on one source's hung network call | Other sources stall | Per-source `pollSource(ctx)` uses an independent 30s timeout; failure records `LastError` and moves on |
| `ATTUNE_BOOTSTRAP_ADMIN_*` env lingering on a re-deployed pod | Could be misread as an attempted re-bootstrap | Bootstrap only runs when `admins` is empty; subsequent starts info-log the skip; operator deploy doc instructs unsetting the env after first start |
| `ATTUNE_INBOUND_MASTER_KEY` lost | Every encrypted source config becomes unreadable; all webhook/email adapters fail | Listed prominently in private-deploy.md (#7 follow-up) as a top-priority backup item alongside DB credentials |

### Rollback story

| Scenario | Procedure |
|---|---|
| Post-deploy bug (v0.3 code-only issue) | `helm rollback` / docker-compose to a previous v0.3.x image — NOT to v0.2 (see next row) |
| **Reverse rollback to v0.2 after schema migration** | **Will runtime-panic** — v0.2 code reads `tenants.lark_install` and `tenant_users.lark_open_id` columns that this PR drops. The v0.3 release notes explicitly mark the upgrade as one-way; operators who want a safety net must `pg_dump` before upgrading. **No automatic shim is shipped** — adding a stub Lark handler would just delay the failure to the first DB read in `LarkHandler.Event`. The path forward is "fix in v0.3.x and roll forward". |
| Schema rollback | **Not supported** — Lark tables/columns are gone; `inbound_sources` and `admins` are live. Code rolls forward only. |
| Per-source misconfiguration / secret leak | Console "Pause source" (sets `enabled=false`); equivalently `UPDATE inbound_sources SET enabled=FALSE WHERE id=…` |
| Lost `ATTUNE_INBOUND_MASTER_KEY` | All encrypted source configs unreadable. No recovery — recreate every source from scratch. Mitigation: back up the key alongside DB credentials before deploy. |

## Implementation plan

**Single PR, all-in-one delivery.** Comparable in scope to #19 (single PR, +10812/-3779 across 164 files, ~1.8 days clock).

**Work ordering inside the PR** (each step compiles + passes vet/lint, **and tests stay green at each step**):

1. **Domain prep** — add `"webhook"` and `"email"` to `domain.ValidSources` + `SourceDisplayName` (and keep `lark-*` entries alive for now — they go away in step 9). This unblocks adapter tests in step 5/6.
2. Framework skeleton: `internal/inbound/{inbound,registry,secrets,sources,metrics,logger,mux}.go` + `internal/inbound/inboundtest/`.
3. depguard rules (both `inbound-boundary` and `inbound-framework-isolation`) + verification (deliberate-pollution test, reverted before commit).
4. `admins` + `inbound_sources` migrations; repos; the destructive-data guard for the lark migration is wired but the migration itself stays unapplied until step 9.
5. Console auth: `handlers/console/auth/` + `Login.tsx`. Console still has Lark OAuth pages at this point.
6. Webhook adapter: full implementation + tests + conformance.
7. Email adapter: full implementation + tests + conformance.
8. Console inbound UI: `InboundSources.tsx` + `InboundSourceNew.tsx`.
9. **Lark removal**: file deletes + all EDIT entries; migration `202606081200_drop_lark.sql` runs (with the destructive guard); remove the `lark-*` entries from `ValidSources` left behind in step 1.
10. Proto re-gen (`make proto`); CHANGELOG; README updates.
11. CLAUDE.md §5 layering increment.

The two-stage `ValidSources` edit (add in step 1, remove the `lark-*` entries in step 9) is the only way to keep both old Lark code and new webhook/email adapters working in the same compile unit while the PR is mid-flight.

### Release notes (CHANGELOG)

`### Added`:
- Inbound adapter framework (`internal/inbound`) with channel-agnostic `Adapter` port, init()-based registration, depguard boundary guard, conformance test suite (`internal/inbound/inboundtest`).
- Webhook inbound adapter: `POST /v1/inbound/webhook/{tenant-slug}/{source-slug}` with HMAC-SHA256 (timestamp + body) and 24h dual-secret rotation (#66).
- Email IMAP inbound adapter: per-tenant N:1 mailboxes, encrypted credentials, last-UID cursor (#66, folds in #35).
- `inbound_sources` table for per-channel per-tenant source configuration.
- Console local admin password login: bcrypt cost 12, 5-strikes/15-min lockout, env-bootstrap on empty `admins` table.
- `ATTUNE_BOOTSTRAP_ADMIN_EMAIL` / `ATTUNE_BOOTSTRAP_ADMIN_PASSWORD` env vars.
- `attune_inbound_total`, `attune_inbound_latency_seconds`, `attune_inbound_source_state`, `attune_inbound_poll_lag_seconds` Prometheus metrics.

`### Changed`:
- `domain.ValidSources`: `"webhook"`, `"email"` added.
- CLAUDE.md §5 layering: `inbound` layer documented.
- HTTP routing: `/v1/inbound` prefix introduced.

`### Removed`:

> ⚠️ **Upgrade preflight — read before deploying v0.3.0.** This release is a **one-way upgrade**. v0.2 schema features (Lark tables, columns, OAuth, source enums) are removed and v0.2 code cannot run against the v0.3 schema.
>
> 1. Stop attune on v0.2.x.
> 2. `pg_dump` your database (mandatory if you have any `lark-*` `user_feedback` rows you wish to preserve).
> 3. If you have non-zero `lark-*` rows you want kept, export them manually: `SELECT * FROM user_feedback WHERE source LIKE 'lark-%'` — no export tooling is shipped.
> 4. Unset any `LARK_*`, `CONSOLE_DEV_LOGIN`, `CONSOLE_INSECURE_COOKIES` env vars.
> 5. Set `ATTUNE_INBOUND_MASTER_KEY` (32 bytes), `ATTUNE_BOOTSTRAP_ADMIN_EMAIL`, `ATTUNE_BOOTSTRAP_ADMIN_PASSWORD` (or `_FILE` variants).
> 6. If your DB contains lark rows you accept losing, also set `ATTUNE_CONFIRM_LARK_DELETE=yes`.
> 7. Deploy v0.3.0. After first start, unset `ATTUNE_BOOTSTRAP_ADMIN_*` and rotate the admin password in Console.

- **Lark integration end-to-end** — one-way upgrade — `/v1/lark/event`, `internal/handlers/lark.go`, `internal/infra/lark/*`, `internal/notify/adapter/larkwebhook/*`, `internal/repo/lark/*`, console Lark OAuth, all `lark-*` source enums, `tenants.lark_install` column, `tenant_users.lark_open_id` column, `lark-webhook` notify target, `dev_login` backdoor, `ConsoleDevLogin` / `ConsoleInsecureCookies` config flags (#66).
- Existing `user_feedback` rows with `source LIKE 'lark-%'` are DELETED (gated by `ATTUNE_CONFIRM_LARK_DELETE=yes`).

`### Security`:
- `dev_login` backdoor removed; production deployments require the password login.
- Inbound webhook signatures include timestamp (±300s replay window) and constant-time enumeration-resistant 401 for unknown sources.
- Webhook secret rotation is dual-secret with a 24h grace window; second rotation inside the window is rejected (409 Conflict).
- Inbound source secrets / IMAP credentials are stored AES-GCM envelope-encrypted (32-byte master key in `ATTUNE_INBOUND_MASTER_KEY`); envelope includes a reserved `key_id` byte for forward-compatible rotation (#94).
- Console password: bcrypt cost 12; 5-strikes / 15-min lockout; dummy bcrypt on user-not-found (timing equalisation); session cookie `HttpOnly + Secure + SameSite=Lax`; post-login redirect validated via `redirectIsSafe`.
- Bootstrap admin env variables support `*_FILE` variants to avoid `/proc/<pid>/environ` exposure on Linux. Operators are instructed to unset the env after the first successful start.
- Lark identity data (`tenant_users.lark_open_id`, `tenants.lark_install`) is hard-deleted.

### Versioning

Pre-1.0, MINOR bump allowed for breaking changes (CLAUDE.md §3). Target tag: **`v0.3.0`** on merge.

## Verification

PR cannot merge until all 14 gates are green:

1. `go build ./...` — zero errors
2. `go vet ./...` — zero warnings
3. `go test -short ./...` — all pass
4. `golangci-lint run` — including the new `inbound-boundary` AND `inbound-framework-isolation` rules
5. `lizard . -l go -C 15 -T nloc=100` — within limits
6. `npx -y jscpd . --pattern '**/*.go' --threshold 5` — duplication < 4%
7. `make proto` — committed output matches generation
8. `scripts/lint-rawptr.sh` — zero raw pointer ops
9. `scripts/lint-slog.sh` — zero direct slog calls
10. **`grep -rl '[Ll]ark\|Feishu\|FEISHU\|飞书' . --include='*.go' --include='*.tsx' --include='*.ts' --include='*.proto' --exclude-dir=docs --exclude-dir=internal/migrations`** returns empty. (Excludes apply: CHANGELOG.md / docs/proposals/** / internal/migrations/** / `.golangci.yml` / this spec file are allowed to mention Lark because they document the removal.)
11. Integration test (testcontainers/postgres) green — webhook and email both land rows in `user_feedback` with correct `source_meta.inbound_source_id`
12. **Deliberate pollution test**: temporarily add `import _ "…/inbound/adapter/webhook"` to `internal/service/ingest/ingestor.go`, confirm CI fails citing `inbound-boundary`, revert (not committed)
13. **Symmetric pollution test**: temporarily add `import "…/internal/service/ingest"` to `internal/inbound/registry.go`, confirm CI fails citing `inbound-framework-isolation`, revert (not committed)
14. Conformance test passes for both webhook and email adapters
15. Manual smoke: fresh docker-compose, bootstrap first admin via env, console login, create webhook source, signed curl POST, observe `user_feedback` row + `attune_inbound_total{result="ok"}` increment
16. **Bootstrap-empty-no-env**: start attune against an empty `admins` table with `ATTUNE_BOOTSTRAP_ADMIN_*` unset; process MUST refuse to start with a clear fatal error
17. **Two-pod bootstrap race**: start two attune processes simultaneously against an empty `admins` table with the same env; exactly one admin row is created (advisory lock + `ON CONFLICT DO NOTHING` both verified)
18. **Email test-connection bad creds**: POST `/install/inbound/sources/test-connection` with a deliberately wrong password returns `200 {ok:false, error:"…"}` — never a 500
19. **Rotate-secret 24h overlap**: rotate a webhook source, immediately POST signed with the OLD secret — must still succeed (previous still valid); attempt to rotate again within 24h — must return 409 Conflict with `next_eligible_at`
20. **Migration with existing lark rows**: seed a test DB with N=1000 `lark-*` `user_feedback` rows; run migrations WITHOUT `ATTUNE_CONFIRM_LARK_DELETE=yes` → migration aborts; set the env and re-run → migration succeeds, rows deleted, lock duration logged
21. **Boot validation**: start attune with `ATTUNE_INBOUND_MASTER_KEY` unset (or wrong length) → process MUST refuse to start before `Manager.StartAll`, fatal error names the env var
22. **Login enumeration timing**: POST `/install/login` 1000× with non-existent email AND 1000× with existing-email-wrong-password; measure response-time distribution — medians must be within 10% of each other (dummy bcrypt active)

## References

### Industry precedents
- OpenTelemetry Collector — `Component { Start, Shutdown }` interface, factory functional-options, deliberate "Start must return quickly" contract.
- Caddy modules — tiny base interface + optional role interfaces; `init() + RegisterModule` self-registration; ID-keyed registry.
- Bento / Benthos (Go) — `service.RegisterBatchInput(name, spec, ctor)` from each `init()`; blank-import for plugins.
- Telegraf — `Input` / `ServiceInput` split with embedding; informs our decision to reject parallel-interfaces in favour of OTel's unified lifecycle.
- n8n — `trigger / poll / webhook / execute` four-mode taxonomy validates that one Adapter port can absorb all four runtime modes.
- Stripe / Slack webhook signing — timestamp + body HMAC, ±300s replay window.
- Hookdeck / Svix — per-source unique URL; dual-secret rotation overlap.
- Miniflux — closest Go-shaped reference for future RSS adapter (Apache 2.0, fork-friendly).
- Huginn / Activepieces — agent-based pull architectures validate the long-term taxonomy.

### attune internal context
- CLAUDE.md §1 (quality gates) · §3 (versioning) · §5 (layering) · §7 (observability / logext) · §7b (ptrext) · §8 (security baseline) · §10 (proposal process) · §11 (proto IDL).
- Existing notify-adapter pattern (`internal/notify/adapter/*`) — mirror of the inbound side this proposal lands.
- `internal/service/ingest/ingestor.go` — the IngestPort contract is sourced from `IngestRow`'s real signature.

### Related issues
- #19 — canonical proto contract (CLOSED) — provides `pb.IngestRequest`
- #34 — outbound notify adapter SDK — symmetric counterpart to this work; not blocked by it
- #35 — email-to-feedback gateway — folded in
- #40 — Console SSO/OIDC — composable with the local password landed here
- #7 — private-deploy.md — needs a follow-up section on `ATTUNE_INBOUND_MASTER_KEY` and bootstrap-admin
- #63 — Grafana panels — consumes the new `attune_inbound_*` metrics
- #48 — logext facade — applies to all new code in this PR
