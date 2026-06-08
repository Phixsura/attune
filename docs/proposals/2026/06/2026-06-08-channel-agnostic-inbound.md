# Channel-agnostic inbound framework + integral Lark removal

| | |
|---|---|
| **Issue** | #66 |
| **Status** | Proposed |
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
| | Adding a channel = a new package + one blank-import line. Zero edits to core packages. |
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
| MCP server exposing attune history | separate server; not in the inbound framework |
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

```
handlers  →  service  →  repo
                       →  notify
                       →  infra/llmclient
handlers  →  domain
─── added: ───────────────────────────────────────────────────────────────
inbound   →  service          (only via IngestPort interface, DI-injected)
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

    "github.com/go-chi/chi/v5"
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

// Deps — handed to every adapter at Start.
// Add fields here only when a dependency becomes universal across adapters;
// adapter-specific config comes from inbound_sources.config (encrypted JSON).
type Deps struct {
    Mux     chi.Router       // adapter mounts HTTP routes (push adapters only)
    Ingest  IngestPort       // canonical normalize → persist
    Sources SourceStore      // load + update inbound_sources rows
    Secrets SecretStore      // envelope encrypt/decrypt per-source credentials
    Metrics InboundMetrics
    Logger  Logger
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

    "github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Factory — adapter package's init() calls Register(channel, factory).
type Factory func() Adapter

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

// Factories — snapshot for cmd/attune. Returns a sorted slice for
// deterministic startup order.
func Factories() []struct {
    Channel string
    Factory Factory
} {
    mu.RLock()
    defer mu.RUnlock()
    out := make([]struct{ Channel string; Factory Factory }, 0, len(factories))
    for ch, f := range factories {
        out = append(out, struct{ Channel string; Factory Factory }{ch, f})
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
// On any single failure, already-started adapters are shut down (using a
// short independent context, since the caller's ctx may already be cancelled).
func (m *Manager) StartAll(ctx context.Context) error {
    for _, entry := range Factories() {
        a := entry.Factory()
        if err := a.Start(ctx, m.deps); err != nil {
            shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            _ = m.shutdownStarted(shutCtx)
            cancel()
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

func (m *Manager) shutdownStarted(ctx context.Context) error {
    var errs []error
    for i := len(m.adapters) - 1; i >= 0; i-- {
        if err := m.adapters[i].Shutdown(ctx); err != nil {
            errs = append(errs, err)
        }
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
// Minimum bar:
//   1. Channel() returns a non-empty string with no whitespace or '/'.
//   2. Start(ctx, mockDeps) followed by immediate Shutdown does not panic.
//   3. ctx cancellation propagates: Shutdown returns within 5s, no goroutine leak.
//   4. Idempotent shutdown: calling Shutdown twice does not panic.
//   5. Mock IngestPort receives at least one IngestInput via the adapter's
//      fixture-driven path.
//   6. Duplicate Register on the same channel panics (verified at framework
//      level, not per-adapter).
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
  if !constant_time_eq(digest, current_secret_digest) {
      if !constant_time_eq(digest, previous_secret_digest) || previous_expired
          → 401 (result="auth_err")
  }
```

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

1. Generate 32 fresh random bytes → new secret.
2. `secret_previous_encrypted` ← current `secret_current_encrypted`; `previous_expires_at` ← now + 24h.
3. `secret_current_encrypted` ← encrypt(new secret).
4. Return new secret once (response body); never persisted in plaintext.

Adapter HMAC verify tries `current` first, falls back to `previous` if `previous_expires_at` in the future. Standard dual-secret 24h overlap (Hookdeck / Stripe convention).

**Handler flow (abridged)**

```go
func (a *adapter) handle(w http.ResponseWriter, r *http.Request) {
    const where = "inbound.webhook.handle"
    ctx := r.Context()

    tenantSlug := chi.URLParam(r, "tenant-slug")
    sourceSlug := chi.URLParam(r, "source-slug")

    src, err := a.deps.Sources.GetBySlugs(ctx, tenantSlug, "webhook", sourceSlug)
    if err != nil || !src.Enabled {
        a.deps.Metrics.Total("webhook", tenantSlug, sourceSlug, "not_found")
        http.NotFound(w, r); return
    }

    body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
    if err != nil { /* 400 validate_err */ }

    cfg, err := src.parseWebhookConfig(a.deps.Secrets)
    if err != nil { /* 500 internal_err */ }

    ts := r.Header.Get("X-Attune-Timestamp")
    sig := r.Header.Get("X-Attune-Signature")
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

On startup, if `SELECT COUNT(*) FROM admins == 0`:

```
email := env.MustGet("ATTUNE_BOOTSTRAP_ADMIN_EMAIL")
pass  := env.MustGet("ATTUNE_BOOTSTRAP_ADMIN_PASSWORD")
INSERT INTO admins(email, password_hash=bcrypt(pass), role='admin', …)
logext.Warnf(ctx, "[bootstrap] created first admin %s — change password immediately", email)
```

Bootstrap is **idempotent**: on subsequent starts, the row already exists, env is ignored, info-log records the skip.

**Login handler**

```
POST /fb/v1/console/install/login         body: {email, password}
   → lookup admin where LOWER(email)=LOWER($) and (locked_until is null or expired)
   → bcrypt.CompareHashAndPassword
   → on ok: zero failed_attempts; sign session cookie; 302 /console/
   → on fail: failed_attempts++; if >= 5, locked_until = now()+15min; 401 "invalid credentials"
POST /fb/v1/console/install/logout        clears cookie; 302 /console/login
```

Login responses never distinguish "unknown email" vs "wrong password" (timing-safe lookup, generic 401 text). The `dev_login` backdoor is removed entirely; `ConsoleDevLogin` and `ConsoleInsecureCookies` config flags are removed.

### Data migrations

Three migrations, applied in order:

#### `202606081200_drop_lark.sql`

```sql
-- Hard-delete all Lark-bound data + schema. Pre-1.0; no customer retention.
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

### CI boundary guard — `.golangci.yml` depguard rule

```yaml
linters-settings:
  depguard:
    rules:
      # Existing rules retained.
      slog-facade: { ... }

      # New rule: inbound-boundary.
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
| Post-deploy bug | `helm rollback` / docker-compose to v0.2 image |
| Schema rollback | **Not supported** — Lark tables/columns are gone; `inbound_sources` and `admins` are live. Code rolls back; schema rolls forward only. |
| Per-source misconfiguration / secret leak | Console "Pause source" (sets `enabled=false`); equivalently `UPDATE inbound_sources SET enabled=FALSE WHERE id=…` |

## Implementation plan

**Single PR, all-in-one delivery.** Comparable in scope to #19 (single PR, +10812/-3779 across 164 files, ~1.8 days clock).

**Work ordering inside the PR** (each step compiles + passes vet/lint):

1. Framework skeleton: `internal/inbound/{inbound,registry,secrets,sources,metrics,logger,mux}.go` + `inboundtest/`.
2. depguard rule + verification (deliberate-pollution test, reverted before commit).
3. `admins` + `inbound_sources` migrations; repos.
4. Console auth: `handlers/console/auth/` + `Login.tsx`.
5. Webhook adapter: full implementation + tests.
6. Email adapter: full implementation + tests.
7. Console inbound UI: `InboundSources.tsx` + `InboundSourceNew.tsx`.
8. Lark removal: file deletes + all EDIT entries; migration `202606081200_drop_lark.sql`.
9. Proto re-gen (`make proto`); CHANGELOG; README updates.
10. CLAUDE.md §5 layering increment.

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
- **Lark integration end-to-end** — one-way upgrade — `/v1/lark/event`, `internal/handlers/lark.go`, `internal/infra/lark/*`, `internal/notify/adapter/larkwebhook/*`, `internal/repo/lark/*`, console Lark OAuth, all `lark-*` source enums, `tenants.lark_install` column, `tenant_users.lark_open_id` column, `lark-webhook` notify target, `dev_login` backdoor, `ConsoleDevLogin` / `ConsoleInsecureCookies` config flags (#66).
- Existing `user_feedback` rows with `source LIKE 'lark-%'` are DELETED.

`### Security`:
- `dev_login` backdoor removed; production deployments require the password login.
- Inbound webhook signatures include timestamp (±300s replay window).

### Versioning

Pre-1.0, MINOR bump allowed for breaking changes (CLAUDE.md §3). Target tag: **`v0.3.0`** on merge.

## Verification

PR cannot merge until all 14 gates are green:

1. `go build ./...` — zero errors
2. `go vet ./...` — zero warnings
3. `go test -short ./...` — all pass
4. `golangci-lint run` — including the new `inbound-boundary` rule
5. `lizard . -l go -C 15 -T nloc=100` — within limits
6. `npx -y jscpd . --pattern '**/*.go' --threshold 5` — duplication < 4%
7. `make proto` — committed output matches generation
8. `scripts/lint-rawptr.sh` — zero raw pointer ops
9. `scripts/lint-slog.sh` — zero direct slog calls
10. **`grep -rl '[Ll]ark\|Feishu\|FEISHU\|飞书' --include='*.go' --include='*.tsx' --include='*.ts' --include='*.sql' --include='*.proto' . | grep -v CHANGELOG.md`** returns empty
11. Integration test (testcontainers/postgres) green — webhook and email both land rows in `user_feedback` with correct `source_meta.inbound_source_id`
12. **Deliberate pollution test**: temporarily add `import _ "…/inbound/adapter/webhook"` to `internal/service/ingest/ingestor.go`, confirm CI fails citing `inbound-boundary`, revert (not committed)
13. Conformance test passes for both webhook and email adapters
14. Manual smoke: fresh docker-compose, bootstrap first admin, console login, create webhook source, signed curl POST, observe `user_feedback` row + `attune_inbound_total{result="ok"}` increment

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
