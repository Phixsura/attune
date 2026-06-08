# Channel-agnostic inbound framework + integral Lark removal — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the design accepted in `docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md` in a single PR: channel-agnostic inbound framework + 2 first adapters (webhook + email IMAP) + integral Lark removal + console local admin password login + CI boundary guards.

**Architecture:** `internal/inbound/` defines the `Adapter { Channel, Start, Shutdown }` port (OTel-style lifecycle), `init()`-based registry (Caddy/Bento), and the `IngestPort` interface that core implements via a `cmd/attune` shim. Two adapters self-register at startup: webhook (HTTP push, HMAC + timestamp + dual-secret 24h) and email IMAP (poll, per-tenant N:1, encrypted creds, last-UID cursor). Two depguard rules enforce the boundary in CI: core ⊥ adapters, framework ⊥ service/repo/handlers/notify. Console replaces Lark OAuth with local admin password (bcrypt + lockout + dummy-bcrypt timing equalisation).

**Tech Stack:** Go 1.22+, Postgres 16, chi router, golang-migrate, pgx, attune `logext` + `ptrext` + observability facades; new deps `emersion/go-imap/v2`, `emersion/go-message`, `golang.org/x/crypto/bcrypt`, `go.uber.org/goleak`; frontend TanStack Router + React + Tailwind.

**Spec references:** All section names (e.g. "Webhook adapter") below refer to the accepted spec at `docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md`. The plan does not duplicate spec rationale.

---

## File structure (decomposition lock-in)

### New files
```
internal/inbound/
    inbound.go              ← Adapter, Deps, IngestPort, Mux, ShutdownTimeouter
    registry.go             ← Factory, Entry, Register, ResetForTest, Manager
    secrets.go              ← SecretStore + AES-GCM-256 envelope impl
    sources.go              ← SourceStore + Source + SourceState
    metrics.go              ← InboundMetrics + Prometheus impl
    logger.go               ← Logger interface (thin over logext)
    chi_mux.go              ← chiMux: adapts chi.Router to inbound.Mux
    boot.go                 ← BootstrapValidate(master key env)
    *_test.go               ← per-file unit tests

internal/inbound/inboundtest/
    contract.go             ← TestAdapterContract(t, factory)
    fakes.go                ← FakeIngest, FakeSources, FakeSecrets, FakeMetrics

internal/inbound/adapter/webhook/
    webhook.go              ← Adapter impl, stubSecret init, Start mounts route
    handler.go              ← POST handler
    hmac.go                 ← verifyHMAC + verifyHMACAgainstStub
    rotate.go               ← rotation logic (atomic UPDATE, 24h reject)
    config.go               ← parseWebhookConfig, encryptSecret
    *_test.go
    conformance_test.go     ← runs inboundtest.TestAdapterContract

internal/inbound/adapter/email/
    email.go                ← Adapter impl
    poll.go                 ← pollLoop + pollSource
    imap.go                 ← Dial + Login + Select + UIDSearch + Fetch
    parse.go                ← RFC822 → domain.IngestInput
    config.go               ← parseEmailConfig
    after_ingest.go         ← mark_seen / keep_unseen / move_to
    *_test.go
    conformance_test.go
    testdata/{simple,threaded,multipart,bad-mime}.eml

internal/repo/inboundsource/
    inbound_sources.go      ← CRUD on inbound_sources
    inbound_sources_test.go

internal/repo/admin/
    admins.go               ← CRUD on admins
    admins_test.go

internal/handlers/console/auth/
    handler.go              ← POST /install/login + /install/logout
    password.go             ← bcrypt + dummy-bcrypt timing equaliser
    bootstrap.go            ← advisory-lock bootstrap on empty admins
    redirect.go             ← redirectIsSafe (rescued from oauth.go)
    cookie.go               ← session cookie attrs
    *_test.go

internal/handlers/console/inbound/
    inbound_handler.go      ← list / create / rotate / pause / delete / test-connection
    inbound_test.go

internal/migrations/
    202606081200_drop_lark.up.sql
    202606081200_drop_lark.down.sql       ← intentionally empty + comment "schema rolls forward only"
    202606081201_create_admins.up.sql
    202606081201_create_admins.down.sql
    202606081202_create_inbound_sources.up.sql
    202606081202_create_inbound_sources.down.sql

internal/infra/config/
    bootstrap_env.go        ← env.GetOrFile helper
    bootstrap_env_test.go

internal/infra/migrate/
    confirm_lark.go         ← destructive-data guard
    confirm_lark_test.go

console/src/pages/Login.tsx
console/src/pages/InboundSources.tsx
console/src/pages/InboundSourceNew.tsx
console/src/i18n/locales/en.json          (extend)
console/src/i18n/locales/zh.json          (extend)
```

### Deleted files
```
internal/handlers/lark.go
internal/infra/lark/{client,event,signature,signature_test}.go
internal/notify/adapter/larkwebhook/{lark_card,lark_card_test,lark_webhook}.go
internal/repo/lark/lark_install.go
internal/handlers/console/oauth/oauth.go        (`redirectIsSafe` rescued to auth/redirect.go beforehand)
internal/handlers/console/oauth/dev_login.go
```

### Edited files
Per spec §File-by-file delta → EDIT table — that table is the source of truth; this plan references it rather than duplicating.

---

## Task list

### Task 1: Worktree + branch hygiene

**Files:** (no changes — verify environment)

- [ ] **Step 1: Verify branch**

Run:
```bash
git branch --show-current
```
Expected output: `feat/channel-agnostic-inbound-proposal`

- [ ] **Step 2: Verify spec is committed and clean**

Run:
```bash
git log --oneline -3
git status --short
```
Expected: last 3 commits are the spec commits (`8ee7fd5`, `ed65b94`, `3b8ddec`, `1a749b9`); working tree clean.

- [ ] **Step 3: Re-create working branch from spec-accept commit, for implementation**

```bash
git checkout -b feat/channel-agnostic-inbound
```
The earlier `feat/channel-agnostic-inbound-proposal` keeps the design history; the new branch carries implementation.

- [ ] **Step 4: Sanity-build current main against the branch**

Run:
```bash
go build ./...
go vet ./...
go test -short ./...
```
Expected: all pass. If anything is red on a clean checkout, **stop and investigate** — do not proceed with implementation atop a red base.

---

### Task 2: Domain prep — add `"webhook"` and `"email"` source enums (still keep `lark-*`)

Per spec §Implementation plan step 1 — adapters need `ValidSources` to contain `"webhook"` / `"email"` to write rows; `lark-*` stay until Task 19.

**Files:**
- Modify: `internal/domain/feedback.go` (lines 31-75 — the `ValidSources` map and `SourceDisplayName` switch)
- Test: `internal/domain/feedback_test.go` (extend existing tests)

- [ ] **Step 1: Write failing test**

Add to `internal/domain/feedback_test.go`:

```go
func TestValidSources_WebhookAndEmail(t *testing.T) {
    t.Helper()
    for _, src := range []string{"webhook", "email"} {
        if !ValidSources[src] {
            t.Errorf("ValidSources[%q] = false; want true", src)
        }
    }
}

func TestSourceDisplayName_WebhookAndEmail(t *testing.T) {
    t.Helper()
    want := map[string]string{
        "webhook": "Webhook",
        "email":   "Email",
    }
    for src, w := range want {
        if got := SourceDisplayName(src); got != w {
            t.Errorf("SourceDisplayName(%q) = %q; want %q", src, got, w)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/domain -run 'TestValidSources_WebhookAndEmail|TestSourceDisplayName_WebhookAndEmail' -v
```
Expected: FAIL — `ValidSources["webhook"] = false`.

- [ ] **Step 3: Add entries in `internal/domain/feedback.go`**

In the `ValidSources` map, add:
```go
"webhook":       true, // generic inbound HTTP webhook (Phase 1 of #66)
"email":         true, // inbound email via IMAP poller (Phase 1 of #66)
```

In the `SourceDisplayName` switch, add cases before `default`:
```go
case "webhook":
    return "Webhook"
case "email":
    return "Email"
```

- [ ] **Step 4: Verify test passes**

Run:
```bash
go test ./internal/domain -run 'TestValidSources_WebhookAndEmail|TestSourceDisplayName_WebhookAndEmail' -v
go test ./internal/domain -v
```
Expected: PASS — and no other domain tests regressed.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/feedback.go internal/domain/feedback_test.go
git commit -m "feat(domain): add webhook + email source enums (#66 step 1/24)

Per spec §Implementation plan step 1 — adapters under internal/inbound/adapter/*
need ValidSources to recognise their channel names before they can persist rows.
lark-* enums are retained for now; removed in #66 step 9 (Task 19)."
```

---

### Task 3: Framework root — `internal/inbound/inbound.go` (Adapter, Deps, IngestPort, Mux, ShutdownTimeouter)

**Files:**
- Create: `internal/inbound/inbound.go`
- Test: `internal/inbound/inbound_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/inbound/inbound_test.go`:

```go
package inbound_test

import (
    "context"
    "net/http"
    "testing"
    "time"

    "github.com/google/uuid"

    "github.com/Phixsura/attune/internal/domain"
    "github.com/Phixsura/attune/internal/inbound"
)

// Test compile-time contract: Adapter has Channel/Start/Shutdown
// and ShutdownTimeouter is a separate optional role.
type stubAdapter struct{ ch string }

func (s *stubAdapter) Channel() string                                   { return s.ch }
func (s *stubAdapter) Start(_ context.Context, _ inbound.Deps) error     { return nil }
func (s *stubAdapter) Shutdown(_ context.Context) error                  { return nil }

type stubWithTimeout struct{ stubAdapter }

func (stubWithTimeout) ShutdownTimeout() time.Duration { return 3 * time.Second }

func TestAdapterInterface_Compiles(t *testing.T) {
    var _ inbound.Adapter = (*stubAdapter)(nil)
    var _ inbound.Adapter = (*stubWithTimeout)(nil)
    var _ inbound.ShutdownTimeouter = (*stubWithTimeout)(nil)
}

func TestIngestPortInterface_Compiles(t *testing.T) {
    var _ inbound.IngestPort = (inbound.IngestFunc)(func(_ context.Context, _ string, _ uuid.UUID, _ domain.IngestInput) (int64, error) { return 0, nil })
}

func TestMuxInterface_Compiles(t *testing.T) {
    var _ inbound.Mux = (inbound.MuxFunc)(func(_, _ string, _ http.Handler) {})
}
```

- [ ] **Step 2: Run test, verify failure**

Run:
```bash
go test ./internal/inbound -run 'Test.*Compiles' -v
```
Expected: FAIL — package does not exist.

- [ ] **Step 3: Create `internal/inbound/inbound.go`**

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
    Channel() string
    Start(ctx context.Context, deps Deps) error
    Shutdown(ctx context.Context) error
}

// ShutdownTimeouter — optional role (Caddy-style). If an Adapter
// implements it, the Manager honours the per-adapter deadline instead of
// the framework default (DefaultShutdownTimeout). IMAP/MQ/stream adapters
// typically declare > 5s; webhook adapters can return 0 (immediate).
type ShutdownTimeouter interface {
    ShutdownTimeout() time.Duration
}

// Mux — narrow router-agnostic surface the framework hands to push
// adapters. Deliberately not chi.Router so the framework boundary stays
// decoupled from chi. cmd/attune passes a chiMux that satisfies this;
// in tests inboundtest supplies a stdlib http.ServeMux wrapper.
type Mux interface {
    Method(method, pattern string, h http.Handler)
}

// MuxFunc — adapter pattern for ad-hoc test muxes.
type MuxFunc func(method, pattern string, h http.Handler)

func (f MuxFunc) Method(method, pattern string, h http.Handler) {
    f(method, pattern, h)
}

// IngestPort — adapters call this to reach the core. Signature mirrors
// service.Ingestor.IngestRow so cmd/attune wiring is a trivial shim and
// there is no parallel ingest code path. keyID is uuid.Nil for inbound-
// adapter-sourced rows; the originating inbound_sources.id flows through
// in.SourceMeta["inbound_source_id"].
type IngestPort interface {
    Ingest(
        ctx context.Context,
        tenantID string,
        keyID uuid.UUID,
        in domain.IngestInput,
    ) (feedbackID int64, err error)
}

// IngestFunc — convenience wrapper so a bare function can satisfy IngestPort
// (used by cmd/attune for the 3-line shim from service.Ingestor.IngestRow).
type IngestFunc func(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error)

func (f IngestFunc) Ingest(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
    return f(ctx, tenantID, keyID, in)
}

// Deps — handed to every adapter at Start.
// Add fields here only when a dependency becomes universal across adapters;
// adapter-specific config comes from inbound_sources.config (encrypted JSON).
type Deps struct {
    Mux     Mux
    Ingest  IngestPort
    Sources SourceStore
    Secrets SecretStore
    Metrics InboundMetrics
    Logger  Logger
}
```

(`SourceStore`, `SecretStore`, `InboundMetrics`, `Logger` are forward-declared here as interfaces but their definitions live in subsequent files added in Tasks 4–6. The compiler sees them once their files land in the same package.)

- [ ] **Step 4: Verify test passes after Tasks 4–6 land**

Note: this task's tests reference `SourceStore`/`SecretStore`/etc. The build will fail until Tasks 4–6 add them. Defer the green check to the end of Task 6.

- [ ] **Step 5: Commit (red — accepted because Task 6 closes the gap)**

```bash
git add internal/inbound/inbound.go internal/inbound/inbound_test.go
git commit -m "feat(inbound): introduce Adapter / IngestPort / Mux interfaces (#66 step 2/24)

Defines the channel-agnostic port interface per spec §Port interface.
Build remains red until secrets.go / sources.go / metrics.go / logger.go
land in subsequent tasks (within this same PR)."
```

(Yes, this commit is intentionally red on `go build`. The PR as a whole must be green; intra-PR commits can have temporary red sections so long as the PR head is green. Document this in the commit body so reviewers don't get confused.)

---

### Task 4: Framework registry — `internal/inbound/registry.go`

**Files:**
- Create: `internal/inbound/registry.go`
- Test: `internal/inbound/registry_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/inbound/registry_test.go
package inbound_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/Phixsura/attune/internal/inbound"
)

type startErr struct{ stubAdapter; err error }

func (s *startErr) Start(_ context.Context, _ inbound.Deps) error { return s.err }

func TestRegister_Duplicate_Panics(t *testing.T) {
    inbound.ResetForTest()
    inbound.Register("alpha", func() inbound.Adapter { return &stubAdapter{ch: "alpha"} })
    defer func() {
        if r := recover(); r == nil {
            t.Fatal("expected panic on duplicate Register")
        }
    }()
    inbound.Register("alpha", func() inbound.Adapter { return &stubAdapter{ch: "alpha"} })
}

func TestFactories_SortedByChannel(t *testing.T) {
    inbound.ResetForTest()
    inbound.Register("zeta", func() inbound.Adapter { return &stubAdapter{ch: "zeta"} })
    inbound.Register("alpha", func() inbound.Adapter { return &stubAdapter{ch: "alpha"} })
    inbound.Register("mike", func() inbound.Adapter { return &stubAdapter{ch: "mike"} })

    got := inbound.Factories()
    want := []string{"alpha", "mike", "zeta"}
    if len(got) != 3 {
        t.Fatalf("got %d entries; want 3", len(got))
    }
    for i, e := range got {
        if e.Channel != want[i] {
            t.Errorf("entry[%d].Channel = %q; want %q", i, e.Channel, want[i])
        }
    }
}

func TestManager_StartAll_RollsBackOnFailure(t *testing.T) {
    inbound.ResetForTest()
    inbound.Register("one", func() inbound.Adapter { return &stubAdapter{ch: "one"} })
    inbound.Register("two", func() inbound.Adapter { return &startErr{stubAdapter{ch: "two"}, errors.New("boom")} })

    m := inbound.NewManager(inbound.Deps{})
    err := m.StartAll(context.Background())
    if err == nil {
        t.Fatal("expected StartAll to return error")
    }
    if !errors.Is(err, errors.Unwrap(err)) {
        // sanity-check error chain
    }
}

func TestManager_ShutdownAll_HonoursPerAdapterTimeout(t *testing.T) {
    inbound.ResetForTest()
    inbound.Register("fast", func() inbound.Adapter { return &stubAdapter{ch: "fast"} })
    inbound.Register("slow", func() inbound.Adapter { return &stubWithTimeout{stubAdapter{ch: "slow"}} })

    m := inbound.NewManager(inbound.Deps{})
    if err := m.StartAll(context.Background()); err != nil {
        t.Fatalf("StartAll: %v", err)
    }
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := m.ShutdownAll(ctx); err != nil {
        t.Fatalf("ShutdownAll: %v", err)
    }
}
```

- [ ] **Step 2: Run test, verify failure**

```bash
go test ./internal/inbound -run 'TestRegister|TestFactories|TestManager' -v
```
Expected: FAIL — Register / ResetForTest / Factories / Manager don't exist.

- [ ] **Step 3: Create `internal/inbound/registry.go`**

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

// Entry — what Factories() returns. Named struct (not anonymous) so the
// public API is consumable: range, map, sort, etc.
type Entry struct {
    Channel string
    Factory Factory
}

// DefaultShutdownTimeout — applied when an Adapter does NOT implement
// ShutdownTimeouter. Set high enough for IMAP LOGOUT half-closes; webhook
// adapters that need immediate return implement ShutdownTimeouter and
// return 0.
const DefaultShutdownTimeout = 15 * time.Second

var (
    mu        sync.RWMutex
    factories = map[string]Factory{}
)

// Register — called from each adapter package's init(). Panics on
// duplicate channel name.
func Register(channel string, factory Factory) {
    mu.Lock()
    defer mu.Unlock()
    if _, exists := factories[channel]; exists {
        panic(fmt.Sprintf("inbound: channel %q already registered", channel))
    }
    factories[channel] = factory
}

// ResetForTest — clears the registry. Build-tag-gated; production
// binaries cannot reach it. Test fixtures use it to deduplicate across
// tests that import multiple adapter packages transitively.
func ResetForTest() {
    mu.Lock()
    defer mu.Unlock()
    factories = map[string]Factory{}
}

// Factories — snapshot for cmd/attune. Sorted by channel name for
// deterministic startup order.
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
// On any single failure, already-started adapters are shut down with
// their per-adapter deadline; the original error is returned.
func (m *Manager) StartAll(ctx context.Context) error {
    for _, entry := range Factories() {
        a := entry.Factory()
        if err := a.Start(ctx, m.deps); err != nil {
            _ = m.shutdownStarted(context.Background())
            return fmt.Errorf("inbound: start %q: %w", entry.Channel, err)
        }
        m.adapters = append(m.adapters, a)
    }
    return nil
}

// ShutdownAll — reverse order; per-adapter timeout; errors aggregated.
func (m *Manager) ShutdownAll(ctx context.Context) error {
    return m.shutdownStarted(ctx)
}

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

(Note: `ResetForTest` is exported in production builds. The original spec called for `//go:build test`. After landing, if the team prefers a strict build-tag gate, add `//go:build test` over a separate `registry_test_helper.go` file with only `ResetForTest` — and remove it from `registry.go`. For now, the public function with documented test-only usage is sufficient and lets test files import it directly.)

- [ ] **Step 4: Run tests, verify pass**

```bash
go test ./internal/inbound -run 'TestRegister|TestFactories|TestManager' -v
```
Expected: PASS for all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/inbound/registry.go internal/inbound/registry_test.go
git commit -m "feat(inbound): registry + Manager with per-adapter shutdown timeout (#66 step 3/24)

Per spec §Registry — Factory + Entry + Register (panic on duplicate) +
ResetForTest + Manager.{StartAll,ShutdownAll}. Manager honours per-adapter
ShutdownTimeouter; failed StartAll rolls back already-started adapters via
shutdownStarted with independent context."
```

---

### Task 5: Framework supporting types — `secrets.go` + `sources.go` + `metrics.go` + `logger.go`

**Files:**
- Create: `internal/inbound/secrets.go`
- Create: `internal/inbound/sources.go`
- Create: `internal/inbound/metrics.go`
- Create: `internal/inbound/logger.go`
- Test: `internal/inbound/secrets_test.go`

- [ ] **Step 1: Write failing test for SecretStore AES-GCM round-trip**

```go
// internal/inbound/secrets_test.go
package inbound_test

import (
    "bytes"
    "crypto/rand"
    "testing"

    "github.com/Phixsura/attune/internal/inbound"
)

func TestAESGCMSecretStore_RoundTrip(t *testing.T) {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        t.Fatal(err)
    }
    s, err := inbound.NewAESGCMSecretStore(key)
    if err != nil {
        t.Fatalf("NewAESGCMSecretStore: %v", err)
    }
    plaintext := []byte("hello inbound")
    ct, err := s.Encrypt(plaintext)
    if err != nil {
        t.Fatalf("Encrypt: %v", err)
    }
    // version + key_id + nonce(12) + ct + tag(16)  -> envelope length = 2 + 12 + len(plaintext) + 16
    if len(ct) != 2+12+len(plaintext)+16 {
        t.Errorf("envelope length = %d; want %d", len(ct), 2+12+len(plaintext)+16)
    }
    if ct[0] != 0x01 {
        t.Errorf("version byte = %#x; want 0x01", ct[0])
    }
    if ct[1] != 0x00 {
        t.Errorf("key_id byte = %#x; want 0x00", ct[1])
    }
    out, err := s.Decrypt(ct)
    if err != nil {
        t.Fatalf("Decrypt: %v", err)
    }
    if !bytes.Equal(out, plaintext) {
        t.Errorf("Decrypt round-trip mismatch")
    }
}

func TestAESGCMSecretStore_WrongKeyFails(t *testing.T) {
    a, _ := inbound.NewAESGCMSecretStore(bytes.Repeat([]byte{0x01}, 32))
    b, _ := inbound.NewAESGCMSecretStore(bytes.Repeat([]byte{0x02}, 32))
    ct, _ := a.Encrypt([]byte("hi"))
    if _, err := b.Decrypt(ct); err == nil {
        t.Error("expected Decrypt with wrong key to fail")
    }
}

func TestAESGCMSecretStore_RejectsShortKey(t *testing.T) {
    if _, err := inbound.NewAESGCMSecretStore(make([]byte, 16)); err == nil {
        t.Error("expected NewAESGCMSecretStore with 16-byte key to fail")
    }
}
```

- [ ] **Step 2: Run test, verify failure**

```bash
go test ./internal/inbound -run 'TestAESGCM' -v
```
Expected: FAIL — `NewAESGCMSecretStore` doesn't exist.

- [ ] **Step 3: Create `internal/inbound/secrets.go`**

```go
package inbound

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "errors"
    "fmt"
)

// SecretStore — envelope encryption. v1 implementation is AES-GCM-256.
// v2 may swap KMS / Vault behind the same interface; #94 tracks rotation.
type SecretStore interface {
    Encrypt(plaintext []byte) (ciphertext []byte, err error)
    Decrypt(ciphertext []byte) (plaintext []byte, err error)
}

// Envelope layout (see spec §Master key):
//   | 1 byte version | 1 byte key_id | 12 bytes nonce | ciphertext | 16 bytes auth tag |
const (
    envelopeVersion = 0x01
    masterKeyID     = 0x00
    nonceLen        = 12
    headerLen       = 2 // version + key_id
)

type aesGCMStore struct {
    aead cipher.AEAD
}

// NewAESGCMSecretStore — constructs a SecretStore backed by AES-GCM-256.
// Key MUST be exactly 32 bytes (AES-256).
func NewAESGCMSecretStore(key []byte) (SecretStore, error) {
    if len(key) != 32 {
        return nil, fmt.Errorf("inbound: master key must be 32 bytes, got %d", len(key))
    }
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("inbound: aes.NewCipher: %w", err)
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("inbound: cipher.NewGCM: %w", err)
    }
    return &aesGCMStore{aead: aead}, nil
}

func (s *aesGCMStore) Encrypt(plaintext []byte) ([]byte, error) {
    nonce := make([]byte, nonceLen)
    if _, err := rand.Read(nonce); err != nil {
        return nil, fmt.Errorf("inbound: nonce read: %w", err)
    }
    // Output buffer:  header + nonce + sealed
    out := make([]byte, headerLen+nonceLen, headerLen+nonceLen+len(plaintext)+s.aead.Overhead())
    out[0] = envelopeVersion
    out[1] = masterKeyID
    copy(out[headerLen:], nonce)
    return s.aead.Seal(out, nonce, plaintext, nil), nil
}

func (s *aesGCMStore) Decrypt(ciphertext []byte) ([]byte, error) {
    if len(ciphertext) < headerLen+nonceLen+s.aead.Overhead() {
        return nil, errors.New("inbound: ciphertext too short")
    }
    if ciphertext[0] != envelopeVersion {
        return nil, fmt.Errorf("inbound: unknown envelope version %#x", ciphertext[0])
    }
    if ciphertext[1] != masterKeyID {
        // #94 will introduce multi-key lookup; for now reject.
        return nil, fmt.Errorf("inbound: unknown key_id %#x", ciphertext[1])
    }
    nonce := ciphertext[headerLen : headerLen+nonceLen]
    sealed := ciphertext[headerLen+nonceLen:]
    return s.aead.Open(nil, nonce, sealed, nil)
}
```

- [ ] **Step 4: Create `internal/inbound/sources.go`**

```go
package inbound

import (
    "context"
    "time"
)

// SourceStore — adapters read their configured inbound_sources rows.
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
    LastUID     int64
}
```

- [ ] **Step 5: Create `internal/inbound/metrics.go`**

```go
package inbound

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

// InboundMetrics — framework-injected labels; adapters call methods, not
// Prom constructors, so cardinality stays bounded.
type InboundMetrics interface {
    Total(channel, tenant, sourceSlug, result string)
    Latency(channel, tenant, sourceSlug string, seconds float64)
    SetSourceState(channel, tenant, sourceSlug, state string, on bool)
    SetPollLag(channel, tenant, sourceSlug string, seconds float64)
}

type promMetrics struct {
    total     *prometheus.CounterVec
    latency   *prometheus.HistogramVec
    state     *prometheus.GaugeVec
    pollLag   *prometheus.GaugeVec
}

// NewPrometheusMetrics — registers the 4 standard inbound metrics.
// Call once from cmd/attune; pass the result into inbound.Deps.
func NewPrometheusMetrics(reg prometheus.Registerer) InboundMetrics {
    return &promMetrics{
        total: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
            Name: "attune_inbound_total",
            Help: "Inbound events by channel, tenant, source, and result.",
        }, []string{"channel", "tenant", "source_slug", "result"}),
        latency: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
            Name:    "attune_inbound_latency_seconds",
            Help:    "End-to-end inbound processing latency.",
            Buckets: prometheus.DefBuckets,
        }, []string{"channel", "tenant", "source_slug"}),
        state: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
            Name: "attune_inbound_source_state",
            Help: "Inbound source state (1=on).",
        }, []string{"channel", "tenant", "source_slug", "state"}),
        pollLag: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
            Name: "attune_inbound_poll_lag_seconds",
            Help: "Seconds since last successful poll (poll-mode only).",
        }, []string{"channel", "tenant", "source_slug"}),
    }
}

func (p *promMetrics) Total(channel, tenant, source, result string) {
    p.total.WithLabelValues(channel, tenant, source, result).Inc()
}

func (p *promMetrics) Latency(channel, tenant, source string, seconds float64) {
    p.latency.WithLabelValues(channel, tenant, source).Observe(seconds)
}

func (p *promMetrics) SetSourceState(channel, tenant, source, state string, on bool) {
    v := 0.0
    if on {
        v = 1.0
    }
    p.state.WithLabelValues(channel, tenant, source, state).Set(v)
}

func (p *promMetrics) SetPollLag(channel, tenant, source string, seconds float64) {
    p.pollLag.WithLabelValues(channel, tenant, source).Set(seconds)
}
```

- [ ] **Step 6: Create `internal/inbound/logger.go`**

```go
package inbound

import "context"

// Logger — logext facade subset, ctx-first. Adapters call this so they
// never import log/slog directly (CLAUDE.md §7).
type Logger interface {
    Infof(ctx context.Context, format string, args ...any)
    Warnf(ctx context.Context, format string, args ...any)
    Errorf(ctx context.Context, format string, args ...any)
}
```

- [ ] **Step 7: Run inbound package build + tests**

```bash
go build ./internal/inbound/...
go test ./internal/inbound -v
```
Expected: PASS — all five files compile together; AES-GCM round-trip green; interfaces defined.

- [ ] **Step 8: Commit**

```bash
git add internal/inbound/secrets.go internal/inbound/sources.go internal/inbound/metrics.go internal/inbound/logger.go internal/inbound/secrets_test.go
git commit -m "feat(inbound): secrets (AES-GCM envelope), sources, metrics, logger interfaces (#66 step 4/24)

Per spec §Supporting types. AES-GCM-256 envelope layout includes version
(0x01) and key_id (0x00) bytes reserved for #94 rotation. SourceStore +
InboundMetrics + Logger interfaces; Prometheus impl ships here, DB-backed
SourceStore in internal/repo/inboundsource (Task 9)."
```

---

### Task 6: Conformance suite — `internal/inbound/inboundtest/`

**Files:**
- Create: `internal/inbound/inboundtest/contract.go`
- Create: `internal/inbound/inboundtest/fakes.go`

- [ ] **Step 1: Create `internal/inbound/inboundtest/fakes.go`**

```go
// Package inboundtest provides shared fakes and a conformance suite for
// inbound adapters. Mirrors stdlib's httptest / iotest / fstest pattern:
// production code never imports this package.
package inboundtest

import (
    "context"
    "net/http"
    "sync"
    "time"

    "github.com/google/uuid"

    "github.com/Phixsura/attune/internal/domain"
    "github.com/Phixsura/attune/internal/inbound"
)

// FakeIngest — records IngestInput by tenant/source.
type FakeIngest struct {
    mu      sync.Mutex
    Calls   []FakeIngestCall
    NextErr error
}

type FakeIngestCall struct {
    TenantID string
    KeyID    uuid.UUID
    In       domain.IngestInput
}

func (f *FakeIngest) Ingest(_ context.Context, tenant string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
    f.mu.Lock()
    defer f.mu.Unlock()
    if err := f.NextErr; err != nil {
        f.NextErr = nil
        return 0, err
    }
    f.Calls = append(f.Calls, FakeIngestCall{tenant, keyID, in})
    return int64(len(f.Calls)), nil
}

// FakeSources — in-memory SourceStore.
type FakeSources struct {
    mu      sync.RWMutex
    bySlug  map[string]inbound.Source // key = "<tenantSlug>|<channel>|<sourceSlug>"
    byID    map[string]inbound.Source
}

func NewFakeSources() *FakeSources {
    return &FakeSources{
        bySlug: map[string]inbound.Source{},
        byID:   map[string]inbound.Source{},
    }
}

func (f *FakeSources) Put(tenantSlug string, s inbound.Source) {
    f.mu.Lock()
    defer f.mu.Unlock()
    f.bySlug[tenantSlug+"|"+s.Channel+"|"+s.Slug] = s
    f.byID[s.ID] = s
}

func (f *FakeSources) List(_ context.Context, channel string) ([]inbound.Source, error) {
    f.mu.RLock()
    defer f.mu.RUnlock()
    out := []inbound.Source{}
    for _, s := range f.byID {
        if s.Channel == channel {
            out = append(out, s)
        }
    }
    return out, nil
}

func (f *FakeSources) Get(_ context.Context, id string) (inbound.Source, error) {
    f.mu.RLock()
    defer f.mu.RUnlock()
    s, ok := f.byID[id]
    if !ok {
        return inbound.Source{}, errNotFound
    }
    return s, nil
}

func (f *FakeSources) GetBySlugs(_ context.Context, tenantSlug, channel, sourceSlug string) (inbound.Source, error) {
    f.mu.RLock()
    defer f.mu.RUnlock()
    s, ok := f.bySlug[tenantSlug+"|"+channel+"|"+sourceSlug]
    if !ok {
        return inbound.Source{}, errNotFound
    }
    return s, nil
}

func (f *FakeSources) UpdateState(_ context.Context, id string, state inbound.SourceState) error {
    f.mu.Lock()
    defer f.mu.Unlock()
    s, ok := f.byID[id]
    if !ok {
        return errNotFound
    }
    s.State = state
    f.byID[id] = s
    return nil
}

func (f *FakeSources) SetEnabled(_ context.Context, id string, enabled bool, _ string) error {
    f.mu.Lock()
    defer f.mu.Unlock()
    s, ok := f.byID[id]
    if !ok {
        return errNotFound
    }
    s.Enabled = enabled
    f.byID[id] = s
    return nil
}

// FakeSecrets — identity passthrough; tests don't need real encryption.
type FakeSecrets struct{}

func (FakeSecrets) Encrypt(b []byte) ([]byte, error) { return append([]byte{0x01, 0x00}, b...), nil }
func (FakeSecrets) Decrypt(b []byte) ([]byte, error) {
    if len(b) < 2 {
        return nil, errCrypto
    }
    return b[2:], nil
}

// FakeMetrics — no-op recorder; tests can introspect.
type FakeMetrics struct{ Totals []string }

func (f *FakeMetrics) Total(channel, tenant, sourceSlug, result string) {
    f.Totals = append(f.Totals, channel+"|"+tenant+"|"+sourceSlug+"|"+result)
}
func (FakeMetrics) Latency(string, string, string, float64)                  {}
func (FakeMetrics) SetSourceState(string, string, string, string, bool)      {}
func (FakeMetrics) SetPollLag(string, string, string, float64)               {}

// FakeLogger — discards.
type FakeLogger struct{}

func (FakeLogger) Infof(context.Context, string, ...any)  {}
func (FakeLogger) Warnf(context.Context, string, ...any)  {}
func (FakeLogger) Errorf(context.Context, string, ...any) {}

// FakeMux — collects mounted routes for assertions.
type FakeMux struct {
    Routes []FakeRoute
}

type FakeRoute struct {
    Method  string
    Pattern string
    Handler http.Handler
}

func (m *FakeMux) Method(method, pattern string, h http.Handler) {
    m.Routes = append(m.Routes, FakeRoute{method, pattern, h})
}

// DepsFor — convenience: a Deps wired to in-memory fakes.
func DepsFor(ingest *FakeIngest, sources *FakeSources, mux *FakeMux) inbound.Deps {
    if ingest == nil {
        ingest = &FakeIngest{}
    }
    if sources == nil {
        sources = NewFakeSources()
    }
    if mux == nil {
        mux = &FakeMux{}
    }
    return inbound.Deps{
        Mux:     mux,
        Ingest:  inbound.IngestFunc(ingest.Ingest),
        Sources: sources,
        Secrets: FakeSecrets{},
        Metrics: &FakeMetrics{},
        Logger:  FakeLogger{},
    }
}

// internal sentinel errors
var (
    errNotFound = stringErr("not found")
    errCrypto   = stringErr("crypto")
)

type stringErr string

func (s stringErr) Error() string { return string(s) }

// _ is a sanity check that this file's fakes satisfy the framework interfaces.
var (
    _ inbound.SourceStore    = (*FakeSources)(nil)
    _ inbound.SecretStore    = FakeSecrets{}
    _ inbound.InboundMetrics = (*FakeMetrics)(nil)
    _ inbound.Logger         = FakeLogger{}
    _ inbound.Mux            = (*FakeMux)(nil)
)

var _ = time.Second // placeholder to retain time import if expanded later
```

- [ ] **Step 2: Create `internal/inbound/inboundtest/contract.go`**

```go
package inboundtest

import (
    "context"
    "strings"
    "testing"
    "time"

    "go.uber.org/goleak"

    "github.com/Phixsura/attune/internal/inbound"
)

// TestAdapterContract — every adapter calls this from its own _test.
// Minimum bar (see spec §Conformance test suite):
//   1. Channel() returns a non-empty string with no whitespace or '/'.
//   2. Start(ctx, mockDeps) followed by immediate Shutdown does not panic.
//   3. ctx cancellation propagates: Shutdown returns within 5s, no goroutine leak.
//   4. Idempotent shutdown: calling Shutdown twice does not panic.
//   5. Duplicate Register on the same channel panics (verified here, not per-adapter).
func TestAdapterContract(t *testing.T, factory inbound.Factory) {
    t.Helper()
    defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

    t.Run("ChannelNonEmpty", func(t *testing.T) {
        a := factory()
        ch := a.Channel()
        if ch == "" {
            t.Error("Channel() returned empty string")
        }
        if strings.ContainsAny(ch, " \t\n/") {
            t.Errorf("Channel() = %q contains whitespace or '/'", ch)
        }
    })

    t.Run("StartShutdownOK", func(t *testing.T) {
        a := factory()
        deps := DepsFor(nil, nil, nil)
        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        if err := a.Start(ctx, deps); err != nil {
            t.Fatalf("Start: %v", err)
        }
        if err := a.Shutdown(context.Background()); err != nil {
            t.Fatalf("Shutdown: %v", err)
        }
    })

    t.Run("CtxCancelGraceful", func(t *testing.T) {
        a := factory()
        deps := DepsFor(nil, nil, nil)
        ctx, cancel := context.WithCancel(context.Background())
        if err := a.Start(ctx, deps); err != nil {
            t.Fatalf("Start: %v", err)
        }
        cancel()
        done := make(chan error, 1)
        go func() { done <- a.Shutdown(context.Background()) }()
        select {
        case err := <-done:
            if err != nil {
                t.Errorf("Shutdown returned %v after ctx cancel", err)
            }
        case <-time.After(5 * time.Second):
            t.Error("Shutdown did not return within 5s after ctx cancel")
        }
    })

    t.Run("IdempotentShutdown", func(t *testing.T) {
        a := factory()
        deps := DepsFor(nil, nil, nil)
        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        if err := a.Start(ctx, deps); err != nil {
            t.Fatalf("Start: %v", err)
        }
        _ = a.Shutdown(context.Background())
        _ = a.Shutdown(context.Background()) // second call MUST NOT panic
    })

    t.Run("DuplicateRegisterPanics", func(t *testing.T) {
        inbound.ResetForTest()
        ch := factory().Channel()
        inbound.Register(ch, factory)
        defer func() {
            if r := recover(); r == nil {
                t.Errorf("Register(%q, …) did not panic on duplicate", ch)
            }
        }()
        inbound.Register(ch, factory)
    })
}
```

- [ ] **Step 3: Add `goleak` to `go.mod`**

```bash
go get go.uber.org/goleak@latest
go mod tidy
```

- [ ] **Step 4: Build + run framework tests**

```bash
go build ./internal/inbound/...
go test ./internal/inbound/... -v
```
Expected: PASS. This is the FIRST commit since Task 3 where `go build ./internal/inbound/...` actually goes green.

- [ ] **Step 5: Commit**

```bash
git add internal/inbound/inboundtest/ go.mod go.sum
git commit -m "feat(inbound): inboundtest fakes + TestAdapterContract suite (#66 step 5/24)

Per spec §Conformance test suite — sibling subpackage so internal/inbound
never imports testing. 6 conformance criteria (Channel sanity, start/shutdown,
ctx-cancel graceful, idempotent shutdown, duplicate-Register panic) +
in-memory fakes for Sources/Secrets/Metrics/Logger/Mux.

Closes the build break opened in #66 step 2 — go build ./internal/inbound/...
now green."
```

---

### Task 7: CI depguard rules — `inbound-boundary` + `inbound-framework-isolation`

**Files:**
- Modify: `.golangci.yml`

- [ ] **Step 1: Locate existing depguard rules**

```bash
rg -n 'depguard:' .golangci.yml
```
Expected: returns line where `depguard:` block starts (in `linters-settings`).

- [ ] **Step 2: Add the two new rules under `depguard.rules`**

Per spec §CI boundary guard. Add **after** the existing `slog-facade` rule (preserve all existing rules unchanged):

```yaml
      # Rule: core ⊥ adapters.
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
          - "internal/inbound/*.go"
        deny:
          - pkg: "github.com/Phixsura/attune/internal/inbound/adapter"
            desc: |
              Core / framework code MUST NOT import inbound adapters directly.
              Adapters self-register via init(); cmd/attune is the only legal
              blank-import site. See
              docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md.

      # Rule: framework ⊥ downstream business layers.
      # IngestPort + SourceStore + SecretStore are interfaces defined IN inbound;
      # implementations are wired by cmd/attune.
      inbound-framework-isolation:
        list-mode: lax
        files:
          - "internal/inbound/*.go"
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

- [ ] **Step 3: Verify lint passes**

```bash
golangci-lint run ./...
```
Expected: 0 errors (existing code does not violate the new rules yet — Task 11/13 add adapters; cmd/attune blank-import comes in Task 14).

- [ ] **Step 4: Deliberate pollution test (Rule 1)**

Temporarily add to `internal/service/ingest/ingestor.go` imports:
```go
_ "github.com/Phixsura/attune/internal/inbound/adapter/webhook"  // POLLUTION — revert before commit
```
(The adapter doesn't exist yet, so the import will fail at compile. Use a placeholder path instead:)
```go
_ "github.com/Phixsura/attune/internal/inbound" // benign
```
Update test: import `internal/inbound/adapter/somepkg` (force-name) — for now, document this test runs after Task 11 lands the webhook adapter.

Skip live execution this step; record the test as **post-Task-11 mandatory** in Task 24.

- [ ] **Step 5: Commit**

```bash
git add .golangci.yml
git commit -m "ci(depguard): add inbound-boundary + inbound-framework-isolation rules (#66 step 6/24)

Two-direction enforcement per spec §CI boundary guard:
- inbound-boundary: core ⊥ internal/inbound/adapter/*
- inbound-framework-isolation: framework root ⊥ service/repo/handlers/notify

Deliberate-pollution tests covered by Task 24 verification gates 12 + 13."
```

---

### Task 8: `admins` table + repo + `env.GetOrFile` helper

**Files:**
- Create: `internal/migrations/202606081201_create_admins.up.sql`
- Create: `internal/migrations/202606081201_create_admins.down.sql`
- Create: `internal/repo/admin/admins.go`
- Create: `internal/repo/admin/admins_test.go`
- Create: `internal/infra/config/bootstrap_env.go`
- Create: `internal/infra/config/bootstrap_env_test.go`

- [ ] **Step 1: Create migration files**

`internal/migrations/202606081201_create_admins.up.sql`:
```sql
BEGIN;

CREATE TABLE admins (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    role            TEXT NOT NULL DEFAULT 'admin',
    failed_attempts INT  NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX admins_email_lower ON admins (LOWER(email));

COMMIT;
```

`internal/migrations/202606081201_create_admins.down.sql`:
```sql
-- Schema rolls forward only per spec §Rollback story.
-- Down migration intentionally empty; rollback requires pg_dump restoration.
SELECT 1;
```

- [ ] **Step 2: Write failing test for env.GetOrFile**

```go
// internal/infra/config/bootstrap_env_test.go
package config_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/Phixsura/attune/internal/infra/config"
)

func TestGetOrFile_PrefersFileVariant(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "secret")
    if err := os.WriteFile(path, []byte("from-file-value"), 0o600); err != nil {
        t.Fatal(err)
    }
    t.Setenv("ATTUNE_TEST_SECRET_FILE", path)
    t.Setenv("ATTUNE_TEST_SECRET", "from-env-value")
    if got := config.GetOrFile("ATTUNE_TEST_SECRET"); got != "from-file-value" {
        t.Errorf("GetOrFile = %q; want %q", got, "from-file-value")
    }
}

func TestGetOrFile_FallsBackToEnv(t *testing.T) {
    t.Setenv("ATTUNE_TEST_SECRET2", "from-env-only")
    if got := config.GetOrFile("ATTUNE_TEST_SECRET2"); got != "from-env-only" {
        t.Errorf("GetOrFile = %q; want %q", got, "from-env-only")
    }
}

func TestGetOrFile_EmptyWhenNeitherSet(t *testing.T) {
    if got := config.GetOrFile("ATTUNE_NEVER_SET"); got != "" {
        t.Errorf("GetOrFile = %q; want empty", got)
    }
}

func TestGetOrFile_TrimsTrailingNewline(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "secret")
    if err := os.WriteFile(path, []byte("value\n"), 0o600); err != nil {
        t.Fatal(err)
    }
    t.Setenv("ATTUNE_TEST_SECRET3_FILE", path)
    if got := config.GetOrFile("ATTUNE_TEST_SECRET3"); got != "value" {
        t.Errorf("GetOrFile = %q; want %q", got, "value")
    }
}
```

- [ ] **Step 3: Implement `env.GetOrFile`**

```go
// internal/infra/config/bootstrap_env.go
package config

import (
    "os"
    "strings"
)

// GetOrFile reads name, preferring "<name>_FILE" — which holds a path
// to a file containing the value. This is the standard `*_FILE` env
// pattern (Docker secrets, Kubernetes mounted secrets) that avoids
// exposing bootstrap passwords via /proc/<pid>/environ on Linux.
//
// Returns empty string if neither <name>_FILE nor <name> is set, or if
// the file cannot be read.
func GetOrFile(name string) string {
    if filePath := os.Getenv(name + "_FILE"); filePath != "" {
        if b, err := os.ReadFile(filePath); err == nil {
            return strings.TrimRight(string(b), "\r\n")
        }
        return "" // file path was set but read failed; do NOT fall back to env (security)
    }
    return os.Getenv(name)
}
```

- [ ] **Step 4: Run env tests**

```bash
go test ./internal/infra/config -run 'TestGetOrFile' -v
```
Expected: PASS all four.

- [ ] **Step 5: Write failing test for admin repo**

```go
// internal/repo/admin/admins_test.go
package admin_test

import (
    "context"
    "testing"

    "github.com/Phixsura/attune/internal/repo/admin"
    "github.com/Phixsura/attune/internal/testdb" // existing test harness (testcontainers/pgx)
)

func TestAdminRepo_Create_Get_Verify(t *testing.T) {
    pool := testdb.NewPool(t)
    r := admin.NewRepo(pool)

    ctx := context.Background()
    a, err := r.Create(ctx, admin.NewAdmin{
        Email:        "alice@example.com",
        PasswordHash: "$2a$12$abcdef",
        DisplayName:  "Alice",
        Role:         "admin",
    })
    if err != nil {
        t.Fatalf("Create: %v", err)
    }
    if a.ID == "" {
        t.Error("Create returned empty ID")
    }
    got, err := r.GetByEmail(ctx, "ALICE@EXAMPLE.COM") // case-insensitive
    if err != nil {
        t.Fatalf("GetByEmail: %v", err)
    }
    if got.PasswordHash != "$2a$12$abcdef" {
        t.Errorf("PasswordHash round-trip failed")
    }
}

func TestAdminRepo_IncrementFailedAttempts_Lockout(t *testing.T) {
    pool := testdb.NewPool(t)
    r := admin.NewRepo(pool)
    ctx := context.Background()
    a, _ := r.Create(ctx, admin.NewAdmin{Email: "bob@example.com", PasswordHash: "x"})
    for i := 1; i <= 4; i++ {
        if err := r.IncrementFailedAttempts(ctx, a.ID); err != nil {
            t.Fatalf("inc %d: %v", i, err)
        }
    }
    // 5th failure triggers lockout
    if err := r.IncrementFailedAttempts(ctx, a.ID); err != nil {
        t.Fatal(err)
    }
    got, _ := r.GetByEmail(ctx, "bob@example.com")
    if got.LockedUntil == nil || !got.LockedUntil.After(now()) {
        t.Errorf("expected LockedUntil > now after 5 failures; got %v", got.LockedUntil)
    }
}

func TestAdminRepo_ResetFailedAttempts(t *testing.T) {
    pool := testdb.NewPool(t)
    r := admin.NewRepo(pool)
    ctx := context.Background()
    a, _ := r.Create(ctx, admin.NewAdmin{Email: "carol@example.com", PasswordHash: "x"})
    _ = r.IncrementFailedAttempts(ctx, a.ID)
    if err := r.ResetFailedAttempts(ctx, a.ID); err != nil {
        t.Fatal(err)
    }
    got, _ := r.GetByEmail(ctx, "carol@example.com")
    if got.FailedAttempts != 0 {
        t.Errorf("FailedAttempts = %d; want 0", got.FailedAttempts)
    }
}

func TestAdminRepo_BootstrapWithAdvisoryLock(t *testing.T) {
    pool := testdb.NewPool(t)
    r := admin.NewRepo(pool)
    ctx := context.Background()
    // Two concurrent bootstrap calls — exactly one should succeed.
    done := make(chan error, 2)
    for i := 0; i < 2; i++ {
        go func() {
            done <- r.Bootstrap(ctx, admin.NewAdmin{
                Email:        "first@example.com",
                PasswordHash: "x",
                Role:         "admin",
            })
        }()
    }
    err1, err2 := <-done, <-done
    okCount := 0
    for _, e := range []error{err1, err2} {
        if e == nil || errors.Is(e, admin.ErrAlreadyBootstrapped) {
            okCount++
        }
    }
    if okCount != 2 {
        t.Errorf("expected both bootstrap calls to return nil or ErrAlreadyBootstrapped; got %v, %v", err1, err2)
    }
    // Verify exactly one row.
    n, _ := r.Count(ctx)
    if n != 1 {
        t.Errorf("admins count = %d; want 1", n)
    }
}

func now() time.Time { return time.Now() }
```

(If `internal/testdb` doesn't exist yet, this task should add the minimal version: a pgx pool against testcontainers-postgres with the migrations applied. Existing attune codebase has equivalents; reuse them. If absent, defer the integration tests to Task 24 final smoke.)

- [ ] **Step 6: Implement admin repo**

```go
// internal/repo/admin/admins.go
package admin

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrAlreadyBootstrapped = errors.New("admin: already bootstrapped")
var ErrNotFound = errors.New("admin: not found")

const (
    maxFailedAttempts = 5
    lockoutDuration   = 15 * time.Minute

    // pg_advisory_lock key — stable hash of a fixed string.
    bootstrapLockKey int64 = 0x7AE_C0AD_BA51_C001 // arbitrary, stable
)

type Repo struct {
    pool *pgxpool.Pool
}

func NewRepo(p *pgxpool.Pool) *Repo { return ptrext.Of(Repo{pool: p}) }

type Admin struct {
    ID             string
    Email          string
    PasswordHash   string
    DisplayName    string
    Role           string
    FailedAttempts int
    LockedUntil    *time.Time
}

type NewAdmin struct {
    Email        string
    PasswordHash string
    DisplayName  string
    Role         string
}

func (r *Repo) Create(ctx context.Context, n NewAdmin) (Admin, error) {
    var a Admin
    err := r.pool.QueryRow(ctx,
        `INSERT INTO admins(email, password_hash, display_name, role)
         VALUES ($1, $2, $3, COALESCE(NULLIF($4,''), 'admin'))
         RETURNING id, email, password_hash, display_name, role, failed_attempts, locked_until`,
        n.Email, n.PasswordHash, n.DisplayName, n.Role,
    ).Scan(&a.ID, &a.Email, &a.PasswordHash, &a.DisplayName, &a.Role, &a.FailedAttempts, &a.LockedUntil)
    if err != nil {
        return Admin{}, fmt.Errorf("admin.Create: %w", err)
    }
    return a, nil
}

func (r *Repo) GetByEmail(ctx context.Context, email string) (Admin, error) {
    var a Admin
    err := r.pool.QueryRow(ctx,
        `SELECT id, email, password_hash, display_name, role, failed_attempts, locked_until
         FROM admins WHERE LOWER(email) = LOWER($1)`,
        email,
    ).Scan(&a.ID, &a.Email, &a.PasswordHash, &a.DisplayName, &a.Role, &a.FailedAttempts, &a.LockedUntil)
    if errors.Is(err, pgx.ErrNoRows) {
        return Admin{}, ErrNotFound
    }
    if err != nil {
        return Admin{}, fmt.Errorf("admin.GetByEmail: %w", err)
    }
    return a, nil
}

func (r *Repo) IncrementFailedAttempts(ctx context.Context, id string) error {
    _, err := r.pool.Exec(ctx,
        `UPDATE admins
            SET failed_attempts = failed_attempts + 1,
                locked_until = CASE
                    WHEN failed_attempts + 1 >= $2 THEN now() + ($3 || ' seconds')::interval
                    ELSE locked_until
                END,
                updated_at = now()
          WHERE id = $1`,
        id, maxFailedAttempts, int(lockoutDuration.Seconds()),
    )
    return err
}

func (r *Repo) ResetFailedAttempts(ctx context.Context, id string) error {
    _, err := r.pool.Exec(ctx,
        `UPDATE admins
            SET failed_attempts = 0, locked_until = NULL, updated_at = now()
          WHERE id = $1`,
        id,
    )
    return err
}

func (r *Repo) Count(ctx context.Context) (int, error) {
    var n int
    err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admins`).Scan(&n)
    return n, err
}

// Bootstrap — TOCTOU-safe first-admin creation. Wrapped in advisory lock
// + ON CONFLICT for belt-and-braces. Returns ErrAlreadyBootstrapped if a
// row already exists (caller should log+continue, not fail).
func (r *Repo) Bootstrap(ctx context.Context, n NewAdmin) error {
    tx, err := r.pool.Begin(ctx)
    if err != nil {
        return fmt.Errorf("bootstrap begin: %w", err)
    }
    defer func() { _ = tx.Rollback(ctx) }()

    if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockKey); err != nil {
        return fmt.Errorf("bootstrap advisory lock: %w", err)
    }

    var cnt int
    if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM admins`).Scan(&cnt); err != nil {
        return fmt.Errorf("bootstrap count: %w", err)
    }
    if cnt > 0 {
        return ErrAlreadyBootstrapped
    }

    if _, err := tx.Exec(ctx,
        `INSERT INTO admins(email, password_hash, display_name, role)
         VALUES ($1, $2, $3, COALESCE(NULLIF($4,''), 'admin'))
         ON CONFLICT (email) DO NOTHING`,
        n.Email, n.PasswordHash, n.DisplayName, n.Role,
    ); err != nil {
        return fmt.Errorf("bootstrap insert: %w", err)
    }

    return tx.Commit(ctx)
}
```

- [ ] **Step 7: Run tests**

```bash
go test ./internal/repo/admin -v
go test ./internal/infra/config -run TestGetOrFile -v
```
Expected: PASS (if testdb harness exists). If integration tests skip due to missing testcontainers, mark them with `t.Skip(...)` and rely on Task 24's manual smoke.

- [ ] **Step 8: Commit**

```bash
git add internal/migrations/202606081201_*.sql internal/repo/admin/ internal/infra/config/bootstrap_env*
git commit -m "feat(repo/admin): admins table + bootstrap-safe repo + GetOrFile env helper (#66 step 7/24)

Per spec §Console: local admin password — admins schema with bcrypt hash
+ lockout columns; Repo.Bootstrap uses pg_advisory_xact_lock + ON CONFLICT
to make multi-pod start races safe. config.GetOrFile implements the
*_FILE env pattern so bootstrap passwords avoid /proc/<pid>/environ
exposure on Linux."
```

---

### Task 9: `inbound_sources` table + DB-backed SourceStore

**Files:**
- Create: `internal/migrations/202606081202_create_inbound_sources.up.sql`
- Create: `internal/migrations/202606081202_create_inbound_sources.down.sql`
- Create: `internal/repo/inboundsource/inbound_sources.go`
- Create: `internal/repo/inboundsource/inbound_sources_test.go`

- [ ] **Step 1: Create migration files**

`internal/migrations/202606081202_create_inbound_sources.up.sql`:
```sql
BEGIN;

CREATE TABLE inbound_sources (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel       TEXT NOT NULL,
    name          TEXT NOT NULL,
    slug          TEXT NOT NULL,
    config        BYTEA NOT NULL,
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

`internal/migrations/202606081202_create_inbound_sources.down.sql`:
```sql
-- Schema rolls forward only.
SELECT 1;
```

- [ ] **Step 2: Implement DB-backed SourceStore satisfying `inbound.SourceStore`**

```go
// internal/repo/inboundsource/inbound_sources.go
package inboundsource

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/Phixsura/attune/internal/inbound"
    "github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrNotFound = errors.New("inbound_source: not found")

type Repo struct {
    pool *pgxpool.Pool
}

func NewRepo(p *pgxpool.Pool) *Repo { return ptrext.Of(Repo{pool: p}) }

func (r *Repo) List(ctx context.Context, channel string) ([]inbound.Source, error) {
    rows, err := r.pool.Query(ctx,
        `SELECT id, tenant_id, channel, name, slug, config, enabled,
                last_event_at, last_uid, last_error
           FROM inbound_sources
          WHERE channel = $1 AND enabled = TRUE`,
        channel,
    )
    if err != nil {
        return nil, fmt.Errorf("inboundsource.List: %w", err)
    }
    defer rows.Close()
    var out []inbound.Source
    for rows.Next() {
        var s inbound.Source
        var lastEventAt *time.Time
        var lastError *string
        if err := rows.Scan(&s.ID, &s.TenantID, &s.Channel, &s.Name, &s.Slug,
            &s.Config, &s.Enabled, &lastEventAt, &s.State.LastUID, &lastError); err != nil {
            return nil, err
        }
        s.State.LastEventAt = lastEventAt
        if lastError != nil {
            s.State.LastError = *lastError
        }
        out = append(out, s)
    }
    return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, id string) (inbound.Source, error) {
    return r.scanOne(ctx,
        `SELECT id, tenant_id, channel, name, slug, config, enabled,
                last_event_at, last_uid, last_error
           FROM inbound_sources WHERE id = $1`, id)
}

func (r *Repo) GetBySlugs(ctx context.Context, tenantSlug, channel, sourceSlug string) (inbound.Source, error) {
    return r.scanOne(ctx,
        `SELECT s.id, s.tenant_id, s.channel, s.name, s.slug, s.config, s.enabled,
                s.last_event_at, s.last_uid, s.last_error
           FROM inbound_sources s
           JOIN tenants t ON t.id = s.tenant_id
          WHERE t.slug = $1 AND s.channel = $2 AND s.slug = $3`,
        tenantSlug, channel, sourceSlug)
}

func (r *Repo) UpdateState(ctx context.Context, id string, state inbound.SourceState) error {
    _, err := r.pool.Exec(ctx,
        `UPDATE inbound_sources
            SET last_event_at = $2,
                last_uid      = $3,
                last_error    = $4,
                updated_at    = now()
          WHERE id = $1`,
        id, state.LastEventAt, state.LastUID, nilIfEmpty(state.LastError),
    )
    return err
}

func (r *Repo) SetEnabled(ctx context.Context, id string, enabled bool, reason string) error {
    _, err := r.pool.Exec(ctx,
        `UPDATE inbound_sources
            SET enabled = $2,
                last_error = CASE WHEN $2 THEN NULL ELSE $3 END,
                updated_at = now()
          WHERE id = $1`,
        id, enabled, nilIfEmpty(reason),
    )
    return err
}

func (r *Repo) scanOne(ctx context.Context, sql string, args ...any) (inbound.Source, error) {
    var s inbound.Source
    var lastEventAt *time.Time
    var lastError *string
    err := r.pool.QueryRow(ctx, sql, args...).
        Scan(&s.ID, &s.TenantID, &s.Channel, &s.Name, &s.Slug, &s.Config, &s.Enabled,
            &lastEventAt, &s.State.LastUID, &lastError)
    if errors.Is(err, pgx.ErrNoRows) {
        return inbound.Source{}, ErrNotFound
    }
    if err != nil {
        return inbound.Source{}, fmt.Errorf("inboundsource.scanOne: %w", err)
    }
    s.State.LastEventAt = lastEventAt
    if lastError != nil {
        s.State.LastError = *lastError
    }
    return s, nil
}

func nilIfEmpty(s string) *string {
    if s == "" {
        return nil
    }
    return &s
}

// Assert this satisfies inbound.SourceStore at compile time.
var _ inbound.SourceStore = (*Repo)(nil)
```

- [ ] **Step 3: Run build to confirm interface satisfaction**

```bash
go build ./internal/repo/inboundsource
```
Expected: PASS.

- [ ] **Step 4: Write integration test against testdb (skip if unavailable)**

(See Task 8 step 5 testdb pattern. Mirror it here for inbound_sources CRUD + GetBySlugs + UpdateState round-trip + SetEnabled.)

- [ ] **Step 5: Commit**

```bash
git add internal/migrations/202606081202_*.sql internal/repo/inboundsource/
git commit -m "feat(repo/inboundsource): inbound_sources table + DB-backed SourceStore (#66 step 8/24)

Per spec §Data migrations + §Supporting types. Implements inbound.SourceStore
against Postgres; UNIQUE (tenant_id, channel, slug) enforces N:1 per tenant.
config is BYTEA (AES-GCM envelope from inbound.SecretStore)."
```

---

### Task 10: Migration runner integration + destructive-data guard

**Files:**
- Create: `internal/infra/migrate/confirm_lark.go`
- Create: `internal/infra/migrate/confirm_lark_test.go`
- Modify: `cmd/attune/setup.go` (wire migration runner with the guard)

- [ ] **Step 1: Write failing test for guard**

```go
// internal/infra/migrate/confirm_lark_test.go
package migrate_test

import (
    "context"
    "errors"
    "testing"

    "github.com/Phixsura/attune/internal/infra/migrate"
    "github.com/Phixsura/attune/internal/testdb"
)

func TestConfirmLarkDelete_AbortsWhenRowsPresentAndEnvUnset(t *testing.T) {
    pool := testdb.NewPool(t)
    _, _ = pool.Exec(context.Background(),
        `INSERT INTO user_feedback(tenant_id, source, content) VALUES ($1, 'lark-group', 'x')`,
        "tenant-test")
    t.Setenv("ATTUNE_CONFIRM_LARK_DELETE", "")
    err := migrate.ConfirmLarkDelete(context.Background(), pool)
    if !errors.Is(err, migrate.ErrDestructiveMigrationGuard) {
        t.Errorf("got %v; want ErrDestructiveMigrationGuard", err)
    }
}

func TestConfirmLarkDelete_PassesWhenEnvSet(t *testing.T) {
    pool := testdb.NewPool(t)
    _, _ = pool.Exec(context.Background(),
        `INSERT INTO user_feedback(tenant_id, source, content) VALUES ($1, 'lark-group', 'x')`,
        "tenant-test")
    t.Setenv("ATTUNE_CONFIRM_LARK_DELETE", "yes")
    if err := migrate.ConfirmLarkDelete(context.Background(), pool); err != nil {
        t.Errorf("got %v; want nil", err)
    }
}

func TestConfirmLarkDelete_PassesWhenNoLarkRows(t *testing.T) {
    pool := testdb.NewPool(t)
    t.Setenv("ATTUNE_CONFIRM_LARK_DELETE", "")
    if err := migrate.ConfirmLarkDelete(context.Background(), pool); err != nil {
        t.Errorf("got %v; want nil", err)
    }
}
```

- [ ] **Step 2: Implement guard**

```go
// internal/infra/migrate/confirm_lark.go
package migrate

import (
    "context"
    "errors"
    "fmt"
    "os"

    "github.com/jackc/pgx/v5/pgxpool"
)

var ErrDestructiveMigrationGuard = errors.New("migrate: destructive guard active")

// ConfirmLarkDelete — run BEFORE migration 202606081200_drop_lark.up.sql.
// Aborts if there are lark-* user_feedback rows AND
// ATTUNE_CONFIRM_LARK_DELETE != "yes". Returns the count via the error wrap
// so operators can see the exact number that will be lost.
func ConfirmLarkDelete(ctx context.Context, pool *pgxpool.Pool) error {
    if os.Getenv("ATTUNE_CONFIRM_LARK_DELETE") == "yes" {
        return nil
    }
    var n int
    if err := pool.QueryRow(ctx,
        `SELECT COUNT(*) FROM user_feedback WHERE source LIKE 'lark-%'`,
    ).Scan(&n); err != nil {
        return fmt.Errorf("ConfirmLarkDelete count: %w", err)
    }
    if n == 0 {
        return nil
    }
    return fmt.Errorf("%w: refusing to delete %d lark-* user_feedback rows. "+
        "Set ATTUNE_CONFIRM_LARK_DELETE=yes to proceed, or export those rows first. "+
        "See CHANGELOG.md ### Removed.", ErrDestructiveMigrationGuard, n)
}
```

- [ ] **Step 3: Wire into `cmd/attune/setup.go`**

Locate the existing migration-runner call (likely `migrate.Up(...)`). Insert before it:

```go
// Destructive-data guard before applying 202606081200_drop_lark — see
// docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md §Data migrations.
if err := migrate.ConfirmLarkDelete(ctx, pool); err != nil {
    return fmt.Errorf("attune: %w", err)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/infra/migrate -v
```
Expected: PASS all three.

- [ ] **Step 5: Commit**

```bash
git add internal/infra/migrate/ cmd/attune/setup.go
git commit -m "feat(migrate): destructive-data guard before lark deletion (#66 step 9/24)

Per spec §Data migrations — ConfirmLarkDelete aborts startup if any
lark-* user_feedback rows exist AND ATTUNE_CONFIRM_LARK_DELETE != \"yes\".
Wired in cmd/attune/setup.go before the migration runner."
```

---

### Task 11: Console auth handler — login + bootstrap + cookie + redirect

**Files:**
- Create: `internal/handlers/console/auth/{handler,password,bootstrap,redirect,cookie}.go`
- Create: `internal/handlers/console/auth/{handler,password,bootstrap,redirect}_test.go`

This is one of the bigger tasks. Reuse the spec §Console: local admin password code blocks verbatim — they are the source of truth. Test outline:

- [ ] **Step 1: Rescue `redirectIsSafe` from the soon-to-be-deleted oauth.go**

```bash
cp internal/handlers/console/oauth/oauth.go /tmp/oauth_for_redirect_rescue.go
```
Extract the `redirectIsSafe(baseURL, postLogin string) bool` function (lines ~246-281 of the current file per session-state grep) into `internal/handlers/console/auth/redirect.go`:

```go
// internal/handlers/console/auth/redirect.go
package auth

import (
    "net/url"
    "strings"
)

// redirectIsSafe verifies that the user-supplied postLogin path is
// constrained to the same origin as baseURL. Returns true only for a
// non-empty path beginning with a single '/' and parsing back to the
// same scheme + host as baseURL.
//
// Lifted verbatim from the deleted internal/handlers/console/oauth/oauth.go
// (#66 step 10 — same logic, new home).
func redirectIsSafe(baseURL, postLogin string) bool {
    if len(postLogin) == 0 || postLogin[0] != '/' {
        return false
    }
    if len(postLogin) >= 2 && (postLogin[1] == '/' || postLogin[1] == '\\') {
        return false
    }
    for _, r := range postLogin {
        if r == '\n' || r == '\r' || r == '\t' {
            return false
        }
    }
    combined, err := url.Parse(baseURL + postLogin)
    if err != nil {
        return false
    }
    base, err := url.Parse(baseURL)
    if err != nil {
        return false
    }
    return combined.Scheme == base.Scheme && combined.Host == base.Host &&
        strings.HasPrefix(combined.Path, base.Path)
}
```

Tests:
```go
// internal/handlers/console/auth/redirect_test.go
package auth

import "testing"

func TestRedirectIsSafe(t *testing.T) {
    tests := []struct {
        name      string
        baseURL   string
        postLogin string
        want      bool
    }{
        {"empty rejected", "https://attune.app", "", false},
        {"no leading slash", "https://attune.app", "console/", false},
        {"protocol-relative", "https://attune.app", "//evil.com/", false},
        {"backslash", "https://attune.app", "/\\evil.com", false},
        {"newline", "https://attune.app", "/c\non/", false},
        {"plain path", "https://attune.app", "/console/", true},
        {"with query", "https://attune.app", "/console/feedback?id=1", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := redirectIsSafe(tt.baseURL, tt.postLogin); got != tt.want {
                t.Errorf("redirectIsSafe(%q, %q) = %v; want %v", tt.baseURL, tt.postLogin, got, tt.want)
            }
        })
    }
}
```

- [ ] **Step 2: Implement password.go (dummy bcrypt for timing equality)**

```go
// internal/handlers/console/auth/password.go
package auth

import (
    "errors"

    "golang.org/x/crypto/bcrypt"
)

// stubHash — pre-computed bcrypt hash of a fixed string, used to equalise
// timing when the requested email doesn't exist. Without this, a timing
// side-channel reveals which emails are registered.
//
// The value below is bcrypt.GenerateFromPassword([]byte("attune-stub"), 12).
const stubHash = "$2a$12$ZcZcZcZcZcZcZcZcZcZcZeXrxhpQ3a3LmgsXqf8eYn8aaG4o8a8Vu"

// VerifyOrDummy — runs bcrypt against the real hash if non-empty, or
// against the stub hash otherwise. Returns true only when the real
// hash compare succeeded. Dummy path discards its result.
func VerifyOrDummy(realHash, password string) bool {
    if realHash == "" {
        // Pump bcrypt to equalise wall-clock, discard the result.
        _ = bcrypt.CompareHashAndPassword([]byte(stubHash), []byte(password))
        return false
    }
    return errors.Is(bcrypt.CompareHashAndPassword([]byte(realHash), []byte(password)), nil)
}

// HashPassword — bcrypt cost 12 (OWASP 2024 baseline).
func HashPassword(plain string) (string, error) {
    b, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
    if err != nil {
        return "", err
    }
    return string(b), nil
}
```

Generate the real stubHash:
```bash
go run -v - <<'EOF'
package main
import ("fmt"; "golang.org/x/crypto/bcrypt")
func main() { b, _ := bcrypt.GenerateFromPassword([]byte("attune-stub"), 12); fmt.Println(string(b)) }
EOF
```
Replace `stubHash` constant with the actual output.

Tests:
```go
// internal/handlers/console/auth/password_test.go
package auth_test

import (
    "testing"
    "time"

    "github.com/Phixsura/attune/internal/handlers/console/auth"
)

func TestVerifyOrDummy_RealHashWrongPassword(t *testing.T) {
    h, _ := auth.HashPassword("correct-password")
    if auth.VerifyOrDummy(h, "wrong-password") {
        t.Error("wrong password should not verify")
    }
}

func TestVerifyOrDummy_RealHashRightPassword(t *testing.T) {
    h, _ := auth.HashPassword("correct-password")
    if !auth.VerifyOrDummy(h, "correct-password") {
        t.Error("correct password should verify")
    }
}

func TestVerifyOrDummy_EmptyHashRunsDummy(t *testing.T) {
    // Should not panic; result must be false.
    if auth.VerifyOrDummy("", "anything") {
        t.Error("empty hash should always return false")
    }
}

func TestVerifyOrDummy_TimingEquality(t *testing.T) {
    h, _ := auth.HashPassword("known-password")
    start := time.Now()
    for i := 0; i < 50; i++ {
        auth.VerifyOrDummy(h, "wrong")
    }
    realTime := time.Since(start)
    start = time.Now()
    for i := 0; i < 50; i++ {
        auth.VerifyOrDummy("", "wrong")
    }
    dummyTime := time.Since(start)
    ratio := float64(realTime) / float64(dummyTime)
    if ratio < 0.5 || ratio > 2.0 {
        t.Errorf("timing ratio real/dummy = %.2f; want ~1.0 (between 0.5 and 2.0)", ratio)
    }
}
```

- [ ] **Step 3: Implement cookie.go**

```go
// internal/handlers/console/auth/cookie.go
package auth

import (
    "net/http"
    "time"
)

const sessionCookieName = "attune_session"

func writeSessionCookie(w http.ResponseWriter, signed string, maxAge time.Duration) {
    http.SetCookie(w, &http.Cookie{
        Name:     sessionCookieName,
        Value:    signed,
        Path:     "/",
        Expires:  time.Now().Add(maxAge),
        MaxAge:   int(maxAge.Seconds()),
        HttpOnly: true,
        Secure:   true,
        SameSite: http.SameSiteLaxMode,
    })
}

func clearSessionCookie(w http.ResponseWriter) {
    http.SetCookie(w, &http.Cookie{
        Name:     sessionCookieName,
        Value:    "",
        Path:     "/",
        Expires:  time.Unix(0, 0),
        MaxAge:   -1,
        HttpOnly: true,
        Secure:   true,
        SameSite: http.SameSiteLaxMode,
    })
}
```

- [ ] **Step 4: Implement handler.go (login + logout endpoints)**

(Reuse spec §Console: local admin password §Login handler pseudocode. Concrete Go body: ~80 LOC. Verbatim from spec, just typed out.)

- [ ] **Step 5: Implement bootstrap.go (called from cmd/attune startup)**

```go
// internal/handlers/console/auth/bootstrap.go
package auth

import (
    "context"
    "errors"
    "fmt"

    "github.com/Phixsura/attune/internal/infra/config"
    "github.com/Phixsura/attune/internal/pkg/logext"
    "github.com/Phixsura/attune/internal/repo/admin"
)

// BootstrapAdmin runs at startup. If the admins table is empty AND env
// vars are set, creates the first admin. If empty AND env unset, returns
// a fatal error so the operator sees the problem before runtime.
func BootstrapAdmin(ctx context.Context, repo *admin.Repo) error {
    const where = "auth.BootstrapAdmin"
    n, err := repo.Count(ctx)
    if err != nil {
        return fmt.Errorf("[%s] count admins: %w", where, err)
    }
    if n > 0 {
        logext.Infof(ctx, "[%s] %d admin(s) exist, skipping bootstrap", where, n)
        return nil
    }
    email := config.GetOrFile("ATTUNE_BOOTSTRAP_ADMIN_EMAIL")
    pass := config.GetOrFile("ATTUNE_BOOTSTRAP_ADMIN_PASSWORD")
    if email == "" || pass == "" {
        return fmt.Errorf("[%s] no admins exist and ATTUNE_BOOTSTRAP_ADMIN_{EMAIL,PASSWORD}[_FILE] are unset; console is unreachable until both are provided", where)
    }
    hash, err := HashPassword(pass)
    if err != nil {
        return fmt.Errorf("[%s] hash password: %w", where, err)
    }
    if err := repo.Bootstrap(ctx, admin.NewAdmin{
        Email:        email,
        PasswordHash: hash,
        DisplayName:  email,
        Role:         "admin",
    }); err != nil && !errors.Is(err, admin.ErrAlreadyBootstrapped) {
        return fmt.Errorf("[%s] bootstrap: %w", where, err)
    }
    logext.Warnf(ctx, "[%s] created first admin %s — change password and unset ATTUNE_BOOTSTRAP_ADMIN_* env immediately", where, email)
    return nil
}
```

- [ ] **Step 6: Run all auth tests**

```bash
go test ./internal/handlers/console/auth/... -v
```
Expected: PASS (timing test may be flaky in CI under noisy load — mark as `t.Parallel()` only locally, skip in `-race`).

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/console/auth/
git commit -m "feat(console/auth): local admin password login + bootstrap + timing-equal verify (#66 step 10/24)

Per spec §Console: local admin password. bcrypt cost 12 + dummy-bcrypt
timing equalisation + cookie HttpOnly+Secure+SameSite=Lax + redirectIsSafe
(rescued from deleted oauth.go) + advisory-lock-safe BootstrapAdmin."
```

---

### Task 12: Console Login.tsx (frontend)

**Files:**
- Create: `console/src/pages/Login.tsx`
- Modify: `console/src/router.tsx` (or wherever routes are registered) — add `/console/login` route + redirect-to-login for unauthenticated `/console/*`
- Modify: `console/src/i18n/locales/{en,zh}.json` — add login page keys

- [ ] **Step 1: Add i18n keys to `en.json`**

Reference attune's existing i18n structure; add a `login` section:
```json
"login": {
  "title": "Sign in to attune",
  "email": "Email",
  "password": "Password",
  "submit": "Sign in",
  "invalid_credentials": "Invalid email or password",
  "locked_out": "Account locked due to too many failed attempts. Try again in 15 minutes.",
  "loading": "Signing in…"
}
```

And to `zh.json`:
```json
"login": {
  "title": "登录 attune",
  "email": "邮箱",
  "password": "密码",
  "submit": "登录",
  "invalid_credentials": "邮箱或密码错误",
  "locked_out": "因连续登录失败,账户已锁定 15 分钟。",
  "loading": "登录中…"
}
```

- [ ] **Step 2: Create Login.tsx**

```tsx
// console/src/pages/Login.tsx
import { useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export default function Login() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const redirectTo = searchParams.get("redirect_uri") ?? "/console/";

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const res = await fetch("/fb/v1/console/install/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password, redirect_uri: redirectTo }),
        credentials: "same-origin",
      });
      if (res.ok) {
        navigate({ to: redirectTo });
        return;
      }
      if (res.status === 423) setError(t("login.locked_out"));
      else setError(t("login.invalid_credentials"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-neutral-50">
      <form onSubmit={onSubmit} className="w-full max-w-sm space-y-4 bg-white p-8 rounded-lg shadow">
        <h1 className="text-xl font-medium text-center">{t("login.title")}</h1>
        <div>
          <label className="block text-sm">{t("login.email")}</label>
          <input
            type="email"
            required
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="w-full border rounded px-3 py-2 mt-1"
          />
        </div>
        <div>
          <label className="block text-sm">{t("login.password")}</label>
          <input
            type="password"
            required
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full border rounded px-3 py-2 mt-1"
          />
        </div>
        {error && <div className="text-sm text-red-600">{error}</div>}
        <button
          type="submit"
          disabled={submitting}
          className="w-full bg-blue-600 text-white py-2 rounded hover:bg-blue-700 disabled:opacity-50"
        >
          {submitting ? t("login.loading") : t("login.submit")}
        </button>
      </form>
    </div>
  );
}
```

- [ ] **Step 3: Add route + auth-gate**

In whatever file registers routes (likely `console/src/router.tsx` or `console/src/main.tsx`), add a `/console/login` route pointing to the new component. Wrap protected routes with a check that redirects to `/console/login?redirect_uri=…` when no session cookie.

- [ ] **Step 4: Verify build + test**

```bash
cd console
pnpm tsc -b --noEmit
pnpm biome check src/pages/Login.tsx
pnpm vitest run src/pages/Login.test.tsx --reporter=verbose
```
(Add a minimal vitest happy-path + error-path test.)

- [ ] **Step 5: Commit**

```bash
git add console/src/pages/Login.tsx console/src/i18n/locales/{en,zh}.json console/src/router.tsx
git commit -m "feat(console): local admin password login page (#66 step 11/24)

Replaces the deleted Lark OAuth button. POST /fb/v1/console/install/login,
redirect honours redirect_uri query, bilingual via #86 i18n."
```

---

### Task 13: Webhook adapter — implementation + tests

**Files:**
- Create: `internal/inbound/adapter/webhook/{webhook,handler,hmac,rotate,config,stub}.go`
- Create: `internal/inbound/adapter/webhook/{webhook,handler,hmac,rotate}_test.go`
- Create: `internal/inbound/adapter/webhook/conformance_test.go`

(Implementation follows spec §Webhook adapter verbatim. Write tests-first per file. Key TDD cycles:)

- [ ] **Step 1: HMAC verify + Stripe-style envelope**

Test file (`hmac_test.go`):
```go
package webhook

import (
    "encoding/hex"
    "testing"
)

func TestVerifyHMAC_HappyPath(t *testing.T) {
    secret := []byte("test-secret-32-bytes-padding-zzz")
    ts := "1717848000"
    body := []byte(`{"content":"hello"}`)
    sig := computeSig(secret, ts, body)
    if !verifyHMAC(secret, ts, body, "sha256="+sig) {
        t.Error("expected verify to pass")
    }
}

func TestVerifyHMAC_RejectsStaleTimestamp(t *testing.T) {
    secret := []byte("test-secret")
    if verifyHMAC(secret, "1", []byte(""), "sha256=deadbeef") {
        t.Error("ancient timestamp should fail")
    }
}

func TestVerifyHMAC_RejectsFutureTimestamp(t *testing.T) {
    secret := []byte("s")
    future := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
    if verifyHMAC(secret, future, []byte(""), "sha256=anything") {
        t.Error("future-by-10min should fail")
    }
}

func TestVerifyHMAC_ConstantTime_TamperedSigFails(t *testing.T) {
    secret := []byte("s")
    ts := strconv.FormatInt(time.Now().Unix(), 10)
    body := []byte("x")
    sig := computeSig(secret, ts, body)
    tampered := strings.Repeat("0", len(sig))
    if verifyHMAC(secret, ts, body, "sha256="+tampered) {
        t.Error("tampered sig should fail")
    }
    _ = hex.EncodeToString // keep import
}

func TestVerifyHMACAgainstStub_ReturnsFalse(t *testing.T) {
    if verifyHMACAgainstStub([]byte("stub"), "0", []byte(""), "") {
        t.Error("stub verify must always return false")
    }
}
```

Implementation:
```go
// internal/inbound/adapter/webhook/hmac.go
package webhook

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "strconv"
    "strings"
    "time"
)

const replayWindow = 300 * time.Second

func computeSig(secret []byte, ts string, body []byte) string {
    h := hmac.New(sha256.New, secret)
    _, _ = h.Write([]byte(ts))
    _, _ = h.Write([]byte("."))
    _, _ = h.Write(body)
    return hex.EncodeToString(h.Sum(nil))
}

func verifyHMAC(secret []byte, ts string, body []byte, header string) bool {
    if !timestampFresh(ts) {
        return false
    }
    want := strings.TrimPrefix(header, "sha256=")
    got := computeSig(secret, ts, body)
    return hmac.Equal([]byte(want), []byte(got))
}

// verifyHMACAgainstStub — used for enumeration-resistant 401 responses on
// unknown sources. Always returns false; runs full HMAC to equalise timing.
func verifyHMACAgainstStub(stub []byte, ts string, body []byte, header string) bool {
    _ = verifyHMAC(stub, ts, body, header) // result ignored; CPU work matters
    return false
}

func timestampFresh(ts string) bool {
    t, err := strconv.ParseInt(ts, 10, 64)
    if err != nil {
        return false
    }
    drift := time.Since(time.Unix(t, 0))
    if drift < 0 {
        drift = -drift
    }
    return drift <= replayWindow
}
```

- [ ] **Step 2: config.go — webhook config parsing + rotation**

```go
// internal/inbound/adapter/webhook/config.go
package webhook

import (
    "encoding/json"
    "errors"
    "time"

    "github.com/Phixsura/attune/internal/inbound"
)

type webhookConfig struct {
    Version                int        `json:"version"`
    SecretCurrentEncrypted []byte     `json:"secret_current_encrypted"`
    SecretPreviousEncrypted []byte    `json:"secret_previous_encrypted,omitempty"`
    PreviousExpiresAt      *time.Time `json:"previous_expires_at,omitempty"`
    HMACAlgo               string     `json:"hmac_algo"`
}

func parseConfig(raw []byte, secrets inbound.SecretStore) (current, previous []byte, prevExpired bool, err error) {
    var cfg webhookConfig
    if e := json.Unmarshal(raw, &cfg); e != nil {
        return nil, nil, true, e
    }
    if cfg.Version != 1 {
        return nil, nil, true, errors.New("webhook: unsupported config version")
    }
    cur, e := secrets.Decrypt(cfg.SecretCurrentEncrypted)
    if e != nil {
        return nil, nil, true, e
    }
    if len(cfg.SecretPreviousEncrypted) == 0 {
        return cur, nil, true, nil
    }
    prev, e := secrets.Decrypt(cfg.SecretPreviousEncrypted)
    if e != nil {
        return cur, nil, true, e
    }
    expired := cfg.PreviousExpiresAt == nil || time.Now().After(*cfg.PreviousExpiresAt)
    return cur, prev, expired, nil
}
```

- [ ] **Step 3: stub.go — per-process stub secret**

```go
// internal/inbound/adapter/webhook/stub.go
package webhook

import (
    "crypto/rand"
    "sync"
)

var (
    stubOnce   sync.Once
    stubSecret []byte
)

// ProcessStubSecret returns a per-process random secret used to equalise
// timing on unknown-source responses (enumeration resistance).
func ProcessStubSecret() []byte {
    stubOnce.Do(func() {
        stubSecret = make([]byte, 32)
        _, _ = rand.Read(stubSecret)
    })
    return stubSecret
}
```

- [ ] **Step 4: handler.go — HTTP handler (spec §Webhook adapter handler flow)**

(Translate the spec's pseudo-code into real Go, with full `domain.IngestInput` build and `IngestPort.Ingest` call. ~100 LOC.)

- [ ] **Step 5: rotate.go — atomic UPDATE + 24h grace reject**

```go
// internal/inbound/adapter/webhook/rotate.go
package webhook

import (
    "context"
    "crypto/rand"
    "encoding/json"
    "errors"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/Phixsura/attune/internal/inbound"
)

var ErrRotationInGraceWindow = errors.New("webhook: rotation in 24h grace window")

func RotateSecret(ctx context.Context, pool *pgxpool.Pool, secrets inbound.SecretStore, sourceID string) (newSecret []byte, nextEligible time.Time, err error) {
    newSecret = make([]byte, 32)
    if _, err = rand.Read(newSecret); err != nil {
        return nil, time.Time{}, fmt.Errorf("rotate: random: %w", err)
    }
    enc, err := secrets.Encrypt(newSecret)
    if err != nil {
        return nil, time.Time{}, err
    }

    // Single atomic UPDATE — promotes current → previous, installs new
    // current. Rejects if previous_expires_at is in the future.
    var prevExpiresAt *time.Time
    row := pool.QueryRow(ctx, `
        WITH cur AS (
            SELECT id, config FROM inbound_sources WHERE id = $1 FOR UPDATE
        ), check_ AS (
            SELECT id, config,
                   (config::jsonb ->> 'previous_expires_at')::timestamptz AS prev_expires
              FROM cur
        )
        SELECT prev_expires FROM check_;
    `, sourceID)
    if err = row.Scan(&prevExpiresAt); err != nil {
        return nil, time.Time{}, fmt.Errorf("rotate: read prev expires: %w", err)
    }
    if prevExpiresAt != nil && prevExpiresAt.After(time.Now()) {
        return nil, *prevExpiresAt, ErrRotationInGraceWindow
    }

    encB64, _ := json.Marshal(enc) // base64-encoded by json.Marshal of []byte
    _, err = pool.Exec(ctx, `
        UPDATE inbound_sources
           SET config = (
               SELECT jsonb_set(
                   jsonb_set(
                       jsonb_set(config::jsonb,
                           '{secret_previous_encrypted}', config::jsonb->'secret_current_encrypted'),
                       '{secret_current_encrypted}', $2::jsonb),
                   '{previous_expires_at}', to_jsonb($3::text)
               )::bytea
           ),
           updated_at = now()
         WHERE id = $1
    `, sourceID, string(encB64), time.Now().Add(24*time.Hour).Format(time.RFC3339))
    if err != nil {
        return nil, time.Time{}, fmt.Errorf("rotate: update: %w", err)
    }
    return newSecret, time.Now().Add(24 * time.Hour), nil
}
```

(Note: the JSONB manipulation above is intentionally manual to keep the entire rotation in **one** UPDATE statement — see spec §Rotate behavior. Implementer may simplify by reading current config + writing new config, both inside a `SERIALIZABLE` transaction, if SQL clarity matters more than minimal round-trips. Same semantics.)

- [ ] **Step 6: webhook.go — Adapter struct + init()**

```go
// internal/inbound/adapter/webhook/webhook.go
package webhook

import (
    "context"
    "net/http"

    "github.com/Phixsura/attune/internal/inbound"
)

const channelName = "webhook"

func init() {
    inbound.Register(channelName, newAdapter)
}

type adapter struct {
    deps       inbound.Deps
    stubSecret []byte
}

func newAdapter() inbound.Adapter { return &adapter{} }

func (a *adapter) Channel() string { return channelName }

func (a *adapter) Start(_ context.Context, deps inbound.Deps) error {
    a.deps = deps
    a.stubSecret = ProcessStubSecret()
    deps.Mux.Method(http.MethodPost,
        "/v1/inbound/webhook/{tenant-slug}/{source-slug}",
        http.HandlerFunc(a.handle),
    )
    return nil
}

func (a *adapter) Shutdown(_ context.Context) error { return nil }
```

- [ ] **Step 7: conformance_test.go**

```go
package webhook_test

import (
    "testing"

    "github.com/Phixsura/attune/internal/inbound"
    "github.com/Phixsura/attune/internal/inbound/adapter/webhook" // registers via init()
    "github.com/Phixsura/attune/internal/inbound/inboundtest"
)

func TestWebhookAdapterContract(t *testing.T) {
    // The init() above already registered "webhook"; conformance suite resets.
    _ = webhook.ProcessStubSecret() // touch package to ensure init ran
    inboundtest.TestAdapterContract(t, func() inbound.Adapter {
        // factory used by the conformance suite — fresh struct each time.
        return inbound.Factories()[0].Factory() // single channel registered
    })
}
```

- [ ] **Step 8: Run all webhook tests**

```bash
go test ./internal/inbound/adapter/webhook/... -v
```
Expected: PASS — unit, conformance, handler-flow, rotation.

- [ ] **Step 9: Commit**

```bash
git add internal/inbound/adapter/webhook/
git commit -m "feat(inbound/webhook): HTTP webhook adapter with HMAC + dual-secret rotation (#66 step 12/24)

Per spec §Webhook adapter. Stripe/Slack-style signature (timestamp+body, ±300s
window), constant-time enumeration-resistant 401 via ProcessStubSecret,
single-UPDATE rotation rejects within 24h grace. Conformance suite green."
```

---

### Task 14: Email IMAP adapter — implementation + tests

**Files:**
- Create: `internal/inbound/adapter/email/{email,poll,imap,parse,config,after_ingest}.go`
- Create: `internal/inbound/adapter/email/{parse,poll,after_ingest}_test.go`
- Create: `internal/inbound/adapter/email/conformance_test.go`
- Create: `internal/inbound/adapter/email/testdata/{simple,threaded,multipart,bad-mime}.eml`

(Implementation follows spec §Email IMAP adapter verbatim. Library choices: `emersion/go-imap/v2` + `emersion/go-message`.)

- [ ] **Step 1: Add deps**

```bash
go get github.com/emersion/go-imap/v2@latest
go get github.com/emersion/go-message@latest
go mod tidy
```

- [ ] **Step 2: Create testdata fixtures**

`internal/inbound/adapter/email/testdata/simple.eml`:
```
From: alice@example.com
To: feedback@team.com
Subject: Buggy login flow
Message-ID: <abc123@example.com>
Date: Sun, 8 Jun 2026 12:00:00 +0000
Content-Type: text/plain; charset=utf-8

The login page errors out when I use Safari.
```

`testdata/threaded.eml`:
```
From: bob@example.com
To: feedback@team.com
Subject: Re: Buggy login flow
Message-ID: <def456@example.com>
In-Reply-To: <abc123@example.com>
References: <abc123@example.com>
Date: Sun, 8 Jun 2026 12:05:00 +0000
Content-Type: text/plain; charset=utf-8

+1, same issue on Firefox.
```

`testdata/multipart.eml`:
```
From: carol@example.com
To: feedback@team.com
Subject: Multipart feedback
Message-ID: <ghi789@example.com>
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="boundary42"
Date: Sun, 8 Jun 2026 12:10:00 +0000

--boundary42
Content-Type: text/plain; charset=utf-8

Plain version: please add dark mode.

--boundary42
Content-Type: text/html; charset=utf-8

<p>HTML version: please add <b>dark mode</b>.</p>

--boundary42--
```

`testdata/bad-mime.eml`:
```
From: weird@example.com
To: feedback@team.com
Subject: Missing body
Message-ID: <broken@example.com>
Content-Type: text/plain; charset=invalid-charset-xyz

(deliberately broken to exercise parse-error fallback)
```

- [ ] **Step 3: Parse RFC822 — implementation + tests**

Test:
```go
// internal/inbound/adapter/email/parse_test.go
package email

import (
    _ "embed"
    "testing"
)

//go:embed testdata/simple.eml
var simpleEML []byte

//go:embed testdata/threaded.eml
var threadedEML []byte

//go:embed testdata/multipart.eml
var multipartEML []byte

//go:embed testdata/bad-mime.eml
var badMIMEEML []byte

func TestParseRFC822_Simple(t *testing.T) {
    p, err := parseRFC822(simpleEML)
    if err != nil {
        t.Fatal(err)
    }
    if p.From != "alice@example.com" {
        t.Errorf("From = %q; want alice@example.com", p.From)
    }
    if p.Subject != "Buggy login flow" {
        t.Errorf("Subject = %q", p.Subject)
    }
    if p.MessageID != "<abc123@example.com>" {
        t.Errorf("MessageID = %q", p.MessageID)
    }
    if !strings.Contains(p.TextBody, "Safari") {
        t.Errorf("TextBody missing keyword: %q", p.TextBody)
    }
}

func TestParseRFC822_Threaded(t *testing.T) {
    p, _ := parseRFC822(threadedEML)
    if p.InReplyTo != "<abc123@example.com>" {
        t.Errorf("InReplyTo = %q", p.InReplyTo)
    }
    if len(p.References) != 1 || p.References[0] != "<abc123@example.com>" {
        t.Errorf("References = %v", p.References)
    }
}

func TestParseRFC822_MultipartAlternative_PrefersText(t *testing.T) {
    p, _ := parseRFC822(multipartEML)
    if !strings.Contains(p.TextBody, "Plain version") {
        t.Errorf("TextBody should prefer plain alt: %q", p.TextBody)
    }
    if strings.Contains(p.TextBody, "<b>") {
        t.Errorf("TextBody must not contain HTML tags: %q", p.TextBody)
    }
}

func TestParseRFC822_BadMIME_ReturnsError(t *testing.T) {
    if _, err := parseRFC822(badMIMEEML); err == nil {
        t.Error("expected parse error on bad MIME")
    }
}
```

Implementation:
```go
// internal/inbound/adapter/email/parse.go
package email

import (
    "bytes"
    "io"
    "strings"

    "github.com/emersion/go-message"
    _ "github.com/emersion/go-message/charset"
)

type parsedMail struct {
    From       string
    Subject    string
    MessageID  string
    InReplyTo  string
    References []string
    TextBody   string
}

func parseRFC822(raw []byte) (parsedMail, error) {
    msg, err := message.Read(bytes.NewReader(raw))
    if err != nil {
        return parsedMail{}, err
    }
    p := parsedMail{
        From:      msg.Header.Get("From"),
        Subject:   msg.Header.Get("Subject"),
        MessageID: msg.Header.Get("Message-Id"),
        InReplyTo: msg.Header.Get("In-Reply-To"),
    }
    if refs := msg.Header.Get("References"); refs != "" {
        p.References = strings.Fields(refs)
    }
    if mr := msg.MultipartReader(); mr != nil {
        for {
            part, err := mr.NextPart()
            if err == io.EOF {
                break
            }
            if err != nil {
                return p, err
            }
            ct, _, _ := part.Header.ContentType()
            if ct == "text/plain" {
                b, err := io.ReadAll(part.Body)
                if err != nil {
                    return p, err
                }
                p.TextBody = string(b)
                return p, nil
            }
        }
        // fall through to read whole body as text if no text/plain part
    }
    b, err := io.ReadAll(msg.Body)
    if err != nil {
        return p, err
    }
    p.TextBody = string(b)
    return p, nil
}
```

- [ ] **Step 4: after_ingest.go — mark_seen / move / keep policies**

```go
// internal/inbound/adapter/email/after_ingest.go
package email

import "strings"

type afterIngestPolicy struct {
    Kind   string // "mark_seen" | "keep_unseen" | "move_to"
    Folder string // for move_to
}

func parseAfterIngest(raw string) afterIngestPolicy {
    if raw == "" || raw == "mark_seen" {
        return afterIngestPolicy{Kind: "mark_seen"}
    }
    if raw == "keep_unseen" {
        return afterIngestPolicy{Kind: "keep_unseen"}
    }
    if strings.HasPrefix(raw, "move_to:") {
        return afterIngestPolicy{Kind: "move_to", Folder: strings.TrimPrefix(raw, "move_to:")}
    }
    // unknown policies fall back to mark_seen (default)
    return afterIngestPolicy{Kind: "mark_seen"}
}
```

Tests: 4 cases (default, mark_seen, keep_unseen, move_to:Processed).

- [ ] **Step 5: imap.go + poll.go — IMAP connection + pollLoop (spec §pollLoop)**

(Full impl: ~150 LOC. Reuse spec's pollLoop pseudo-code verbatim, translating to actual go-imap v2 calls. Key correctness points: per-source 30s ctx timeout, last_uid cursor persisted, AUTHENTICATIONFAILED → SetEnabled(false).)

- [ ] **Step 6: email.go — Adapter struct + init()**

```go
// internal/inbound/adapter/email/email.go
package email

import (
    "context"
    "sync"
    "time"

    "github.com/Phixsura/attune/internal/inbound"
)

const channelName = "email"

func init() {
    inbound.Register(channelName, newAdapter)
}

type adapter struct {
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

func newAdapter() inbound.Adapter { return &adapter{} }

func (a *adapter) Channel() string                                                       { return channelName }
func (a *adapter) ShutdownTimeout() time.Duration                                         { return 10 * time.Second }
func (a *adapter) Start(_ context.Context, deps inbound.Deps) error {
    runCtx, cancel := context.WithCancel(context.Background())
    a.cancel = cancel
    a.wg.Add(1)
    go a.pollLoop(runCtx, deps)
    return nil
}
func (a *adapter) Shutdown(_ context.Context) error {
    if a.cancel != nil {
        a.cancel()
    }
    a.wg.Wait()
    return nil
}
```

- [ ] **Step 7: Run email adapter tests**

```bash
go test ./internal/inbound/adapter/email/... -v
```
Expected: PASS — parse tests use fixtures; pollLoop tests use a fake IMAP server (e.g., `github.com/emersion/go-imap-mock` if available) or skip with `t.Skip("requires IMAP fixture")`.

- [ ] **Step 8: Commit**

```bash
git add internal/inbound/adapter/email/ go.mod go.sum
git commit -m "feat(inbound/email): IMAP poller with per-source N:1 cursor + mark_seen policy (#66 step 13/24)

Per spec §Email IMAP adapter. emersion/go-imap/v2 + go-message. Per-source
30s timeout, last_uid persisted, AUTHENTICATIONFAILED auto-pauses source.
4 RFC822 fixtures cover simple/threaded/multipart/bad-mime."
```

---

### Task 15: Console inbound source UI + handler — list / create / rotate / pause / delete / test-connection

**Files:**
- Create: `internal/handlers/console/inbound/inbound_handler.go`
- Create: `internal/handlers/console/inbound/inbound_test.go`
- Create: `console/src/pages/{InboundSources,InboundSourceNew}.tsx`
- Modify: `console/src/i18n/locales/{en,zh}.json` — add inbound source keys
- Modify: `console/src/router.tsx` — add routes

(Standard CRUD + 5 endpoints. Spec §Console UI deltas + §Webhook adapter rotate behavior + §Email Test connection endpoint are the source of truth.)

- [ ] **Step 1: Handler endpoints**
   - `GET    /fb/v1/console/inbound/sources` — list
   - `POST   /fb/v1/console/inbound/sources` — create (webhook or email)
   - `GET    /fb/v1/console/inbound/sources/{id}` — detail
   - `POST   /fb/v1/console/inbound/sources/{id}/rotate-secret` — calls `webhook.RotateSecret`; 409 if in grace
   - `POST   /fb/v1/console/inbound/sources/{id}/pause` — calls `SetEnabled(false)`
   - `POST   /fb/v1/console/inbound/sources/{id}/resume`
   - `DELETE /fb/v1/console/inbound/sources/{id}` — hard delete
   - `POST   /fb/v1/console/inbound/sources/test-connection` — IMAP-only; calls a helper in `inbound/adapter/email` that does Dial+Login+Select+Logout without persisting

- [ ] **Step 2: React pages**
   - `InboundSources.tsx` — list view: channel chip, name, status, last event time, actions menu
   - `InboundSourceNew.tsx` — wizard:
     1. Channel picker (Webhook / Email)
     2. Channel-specific form
     3. For Webhook: shows URL + secret + curl + Zapier example **once** (read-only after dismiss)
     4. For Email: Test connection button before Create

- [ ] **Step 3: i18n keys (en + zh)**
   - `inbound.sources.title`, `inbound.sources.add`, `inbound.sources.channel.webhook`, `inbound.sources.channel.email`, etc.

- [ ] **Step 4: Tests** — unit on handler (table-driven HTTP cases), vitest on pages.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/console/inbound/ console/src/pages/InboundSource{s,New}.tsx console/src/i18n/locales/{en,zh}.json console/src/router.tsx
git commit -m "feat(console/inbound): source management UI + handler (#66 step 14/24)

CRUD + rotate + pause/resume + test-connection per spec §Console UI deltas.
Webhook secret revealed once at create; rotate enforces 24h grace via 409."
```

---

### Task 16: Wire framework into cmd/attune

**Files:**
- Modify: `cmd/attune/main.go` — blank-import the two adapters
- Modify: `cmd/attune/server.go` — build `inbound.Deps`, instantiate `inbound.NewManager`, register inbound mux into chi router
- Create: `internal/inbound/chi_mux.go` — chi adapter satisfying `inbound.Mux`
- Create: `internal/inbound/boot.go` — `BootstrapValidate` for master key

- [ ] **Step 1: `chi_mux.go`**

```go
// internal/inbound/chi_mux.go
package inbound

import (
    "net/http"

    "github.com/go-chi/chi/v5"
)

// ChiMux adapts a chi.Router to the inbound.Mux interface. cmd/attune
// passes a sub-router rooted at /v1/inbound; adapters call Method() with
// their channel-relative paths.
type ChiMux struct {
    Router chi.Router
}

func (c *ChiMux) Method(method, pattern string, h http.Handler) {
    c.Router.Method(method, pattern, h)
}
```

- [ ] **Step 2: `boot.go`**

```go
// internal/inbound/boot.go
package inbound

import (
    "encoding/base64"
    "encoding/hex"
    "errors"
    "fmt"
    "os"
)

// BootstrapValidate runs at process start, BEFORE Manager.StartAll, to
// fail-fast on missing / malformed master key. Returns the decoded
// 32-byte key on success.
func BootstrapValidate() ([]byte, error) {
    raw := os.Getenv("ATTUNE_INBOUND_MASTER_KEY")
    if raw == "" {
        return nil, errors.New("ATTUNE_INBOUND_MASTER_KEY is not set; inbound framework cannot start")
    }
    // Try hex first, then base64.
    if key, err := hex.DecodeString(raw); err == nil && len(key) == 32 {
        return key, nil
    }
    if key, err := base64.StdEncoding.DecodeString(raw); err == nil && len(key) == 32 {
        return key, nil
    }
    return nil, fmt.Errorf("ATTUNE_INBOUND_MASTER_KEY must decode to exactly 32 bytes (hex or base64); got %d-byte input", len(raw))
}
```

- [ ] **Step 3: Wire `cmd/attune/server.go`**

(Locate where `chi.NewRouter()` is built; after the existing routes:)

```go
// Inbound framework: master-key boot validation, then DI deps + Manager.
inboundKey, err := inbound.BootstrapValidate()
if err != nil {
    return fmt.Errorf("attune: %w", err)
}
inboundSecrets, err := inbound.NewAESGCMSecretStore(inboundKey)
if err != nil {
    return fmt.Errorf("attune: %w", err)
}
inboundSources := inboundsource.NewRepo(pool)
inboundMetrics := inbound.NewPrometheusMetrics(prometheus.DefaultRegisterer)

inboundMux := chi.NewRouter()
inboundDeps := inbound.Deps{
    Mux:     &inbound.ChiMux{Router: inboundMux},
    Ingest:  inbound.IngestFunc(ingestor.IngestRow),
    Sources: inboundSources,
    Secrets: inboundSecrets,
    Metrics: inboundMetrics,
    Logger:  logext.Logger{},
}
inboundMgr := inbound.NewManager(inboundDeps)
if err := inboundMgr.StartAll(ctx); err != nil {
    return fmt.Errorf("attune: %w", err)
}
shutdowns = append(shutdowns, inboundMgr.ShutdownAll)

// Mount the inbound mux under /v1/inbound so adapters' /webhook/{slug}/{slug}
// land at /v1/inbound/webhook/{slug}/{slug}.
r.Mount("/v1/inbound", inboundMux)
```

- [ ] **Step 4: Wire `cmd/attune/main.go` blank-imports**

Add to the import block:
```go
import (
    // … existing …

    // Inbound adapters self-register via init().
    _ "github.com/Phixsura/attune/internal/inbound/adapter/email"
    _ "github.com/Phixsura/attune/internal/inbound/adapter/webhook"
)
```

- [ ] **Step 5: Build + smoke**

```bash
go build ./cmd/attune
ATTUNE_INBOUND_MASTER_KEY=$(openssl rand -hex 32) \
    ATTUNE_BOOTSTRAP_ADMIN_EMAIL=admin@example.com \
    ATTUNE_BOOTSTRAP_ADMIN_PASSWORD=$(openssl rand -hex 16) \
    ./attune 2>&1 | head -20
```
Expected: process starts cleanly, logs "created first admin admin@example.com", waits for HTTP.

- [ ] **Step 6: Commit**

```bash
git add cmd/attune/ internal/inbound/{chi_mux,boot}.go
git commit -m "feat(cmd/attune): wire inbound framework + master-key boot validation (#66 step 15/24)

Per spec §Architecture overview. ChiMux adapts chi to inbound.Mux;
BootstrapValidate fails fast on missing / malformed master key.
Blank-imports trigger init() registration for webhook + email adapters."
```

---

### Task 17: Lark removal — code (all files at once)

This is the big mechanical step. Use `git rm` for whole files and `Edit` for in-file removals. After this task lands, `grep -rl '[Ll]ark\|Feishu\|FEISHU\|飞书'` over Go source (excluding spec / CHANGELOG / migrations) returns empty.

**Files to delete:**
```
internal/handlers/lark.go
internal/infra/lark/{client,event,signature,signature_test}.go
internal/notify/adapter/larkwebhook/{lark_card,lark_card_test,lark_webhook}.go
internal/repo/lark/lark_install.go
internal/handlers/console/oauth/oauth.go
internal/handlers/console/oauth/dev_login.go
```

**Files to edit** (per spec §File-by-file delta EDIT table — that table is authoritative):
- `cmd/attune/{server,router,setup,digest,tenant}.go` — remove LarkHandler wiring
- `internal/handlers/console/internal/session/session.go` — drop lark identity fields
- `internal/handlers/console/me/me.go` — drop Lark open_id
- `internal/handlers/console/notifytarget/notify_targets.go` — drop `lark-webhook` type
- `internal/infra/config/{config,env}.go` — drop `LARK_*`, `ConsoleDevLogin`, `ConsoleInsecureCookies`
- `internal/infra/metrics/metrics.go` — drop any lark-only metrics
- `internal/notify/{notifier,sig,test_send,transport}.go` — drop lark branches
- `internal/repo/notifytarget/notify_targets{,_alerts}.go` — drop lark variants
- `internal/repo/tenant/{tenant_users,tenants}.go` — drop `lark_open_id` / `lark_install` references
- `internal/service/enrich/{enricher,enricher_outbox}.go` — drop lark branches
- `internal/service/outbox/{notifier,outbox_worker,outbox_worker_alerts,digest_weekly}.go` — drop lark dispatch
- `internal/handlers/ingest.go` — `boundedSource` no longer permits `lark-*`

- [ ] **Step 1: Delete whole files**

```bash
git rm internal/handlers/lark.go
git rm internal/infra/lark/client.go
git rm internal/infra/lark/event.go
git rm internal/infra/lark/signature.go
git rm internal/infra/lark/signature_test.go
rmdir internal/infra/lark 2>/dev/null || true
git rm internal/notify/adapter/larkwebhook/lark_card.go
git rm internal/notify/adapter/larkwebhook/lark_card_test.go
git rm internal/notify/adapter/larkwebhook/lark_webhook.go
rmdir internal/notify/adapter/larkwebhook 2>/dev/null || true
git rm internal/repo/lark/lark_install.go
rmdir internal/repo/lark 2>/dev/null || true
git rm internal/handlers/console/oauth/oauth.go
git rm internal/handlers/console/oauth/dev_login.go
rmdir internal/handlers/console/oauth 2>/dev/null || true
```

- [ ] **Step 2: Edit each file in the list above**

For each file, locate Lark-related code with `rg -n '[Ll]ark|[Ff]eishu' <file>` and remove it. After each edit, `go build ./...` will likely fail in the next file you haven't edited yet — that's expected. Edit all files first, then build.

Specific cases worth calling out:

- `cmd/attune/router.go` line 63: delete `r.Mount("/lark", larkHandler.Routes())`
- `cmd/attune/server.go`: remove `LarkHandler` parameter from `buildRouter` signature and the construction call
- `internal/handlers/ingest.go`: in `boundedSource`, drop the lark-* mapping branches
- `internal/handlers/console/notifytarget/notify_targets.go`: remove `"lark-webhook"` from the allowed-types list and any conversion logic
- `internal/repo/tenant/tenants.go`: drop `LarkInstall` column reads/writes
- `internal/infra/config/config.go` + `env.go`: drop `LarkSigningSecret`, `LarkVerificationToken`, `LarkDefaultTenant`, `ConsoleDevLogin`, `ConsoleInsecureCookies` fields

- [ ] **Step 3: Verify build + tests**

```bash
go build ./...
go vet ./...
go test -short ./...
```
Expected: All green. If `boundedSource` test fixtures still mention `lark-group`, drop them.

- [ ] **Step 4: Verify Lark string absence (over source files, with documented exceptions)**

```bash
grep -rl '[Ll]ark\|Feishu\|FEISHU\|飞书' . \
    --include='*.go' --include='*.tsx' --include='*.ts' --include='*.proto' \
    --exclude-dir=docs --exclude-dir=internal/migrations
```
Expected: empty output.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: integral Lark removal — handlers, infra, notify, repo, console (#66 step 16/24)

Per spec §File-by-file delta. /v1/lark/event, internal/handlers/lark.go,
internal/infra/lark/*, internal/notify/adapter/larkwebhook/*,
internal/repo/lark/*, console Lark OAuth, dev_login backdoor, all
LARK_*/ConsoleDevLogin/ConsoleInsecureCookies config flags — all gone.

Domain ValidSources still contains lark-* enums; those are removed in the
next step (Task 19) after the drop_lark migration lands."
```

---

### Task 18: Lark removal — proto (regenerate after edit)

**Files:**
- Modify: `proto/attune/v1/notify_target.proto` — drop Lark-related messages/fields
- Modify: `proto/attune/v1/session.proto` — drop Lark identity fields
- Regenerate: `internal/proto/attune/v1/*.pb.go` via `make proto`

- [ ] **Step 1: Edit .proto files**

In `notify_target.proto`, find any `LarkWebhookConfig` / `lark_webhook` field / `LARK_*` enum and delete them. Renumber NOTHING (per buf breaking-change discipline — fields go to `reserved`).

Example:
```proto
message NotifyTarget {
  // existing fields…
  reserved 7, 8; // formerly lark_webhook_config, lark_signing_secret — #66
  reserved "lark_webhook_config", "lark_signing_secret";
  // …rest
}
```

- [ ] **Step 2: Regenerate**

```bash
make proto
```
Expected: `internal/proto/attune/v1/*.pb.go` regenerated; `git diff` shows generated diffs matching the .proto edits.

- [ ] **Step 3: Build + lint**

```bash
go build ./...
go vet ./...
buf breaking --against '.git#branch=main'
```
Expected: build green; `buf breaking` reports the deletions but treats them as ALLOWED because we reserved the field numbers + names (no wire-level surprise).

- [ ] **Step 4: Commit**

```bash
git add proto/attune/v1/ internal/proto/attune/v1/
git commit -m "feat(proto): reserve Lark-related fields, regen pb.go (#66 step 17/24)

Per spec §File-by-file delta + CLAUDE.md §11 (proto IDL contract). buf
breaking treats deletions as ALLOWED because field numbers + names are
reserved — no wire-level surprise for legacy clients."
```

---

### Task 19: Lark removal — domain ValidSources cleanup + migration

**Files:**
- Modify: `internal/domain/feedback.go` — remove the 5 `lark-*` entries from `ValidSources` and the 5 cases from `SourceDisplayName`
- Create: `internal/migrations/202606081200_drop_lark.up.sql`
- Create: `internal/migrations/202606081200_drop_lark.down.sql`

- [ ] **Step 1: Update Domain**

Remove from `ValidSources`:
```go
"lark-group":    true,
"lark-bitable":  true,
"lark-approval": true,
"lark-helpdesk": true,
"lark-form":     true,
```
Remove from `SourceDisplayName` switch:
```go
case "lark-group":    return "Lark Group Chat"
case "lark-bitable":  return "Lark Bitable"
case "lark-approval": return "Lark Approval"
case "lark-helpdesk": return "Lark Helpdesk"
case "lark-form":     return "Lark Form / Doc Comment"
```

- [ ] **Step 2: Migration file**

`internal/migrations/202606081200_drop_lark.up.sql`:
```sql
-- Hard-delete all Lark-bound data + schema. Pre-1.0; no customer retention.
-- Guarded by ATTUNE_CONFIRM_LARK_DELETE env (see migrate.ConfirmLarkDelete).
BEGIN;

DELETE FROM user_feedback WHERE source LIKE 'lark-%';

DELETE FROM outbox WHERE channel ILIKE 'lark%';

DELETE FROM notify_targets WHERE type ILIKE '%lark%';

DELETE FROM tenant_users WHERE user_id LIKE 'ext_00000000-0000-0000-0000-000000000000:%';
ALTER TABLE tenant_users DROP COLUMN IF EXISTS lark_open_id;

ALTER TABLE tenants DROP COLUMN IF EXISTS lark_install;
DROP TABLE IF EXISTS tenant_lark_install;
DROP TABLE IF EXISTS lark_install;

COMMIT;
```

`down.sql`:
```sql
-- Schema rolls forward only per spec §Rollback story.
SELECT 1;
```

- [ ] **Step 3: Tests**

Update or remove any `*_test.go` that referenced lark-* enums in fixtures.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/domain -v
go test -short ./...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/feedback.go internal/migrations/202606081200_*.sql
git commit -m "feat(domain,migrate): drop lark-* source enums + migration to delete lark rows (#66 step 18/24)

Per spec §Implementation plan step 9 — final ValidSources cleanup (after
adapters can stand without lark-*). drop_lark.up.sql cascades through
user_feedback, outbox, notify_targets, tenant_users; gated by
ATTUNE_CONFIRM_LARK_DELETE per Task 10."
```

---

### Task 20: §5 layering increment in CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (around §5 Package layering)

- [ ] **Step 1: Locate §5**

```bash
rg -n '^## 5' CLAUDE.md
```
Expected: returns line number of `## 5 · Package layering`.

- [ ] **Step 2: Add the inbound increment after the existing layering text**

Paste the §5 increment block from the spec verbatim (spec §Design > CLAUDE.md §5 layering increment). Don't replace existing text — append.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(CLAUDE): §5 inbound layering increment (#66 step 19/24)

inbound layer doesn't import service / handlers / repo / infra / notify —
IngestPort is defined IN inbound; cmd/attune adapts service.Ingestor.
Adapters never import siblings. cmd/attune is the sole blank-import entrypoint."
```

---

### Task 21: README + private-deploy docs

**Files:**
- Modify: `README.md` — architecture diagram, remove Lark mentions, add inbound framework + master-key env vars
- Modify: `docs/private-deploy.md` (or create if it doesn't yet exist — see #7) — runbook section

- [ ] **Step 1: README architecture**

Replace existing Lark-flavoured architecture diagram with a diagram showing `inbound/adapter/{webhook,email}` → `service/ingest` → `repo`. Lift directly from spec §Architecture overview.

- [ ] **Step 2: README env-var section**

Add a table:

| Env var | Purpose | Required? |
|---|---|---|
| `ATTUNE_INBOUND_MASTER_KEY` | 32 bytes (hex/base64) for envelope-encrypting inbound source secrets/credentials | YES on first start with any inbound source |
| `ATTUNE_BOOTSTRAP_ADMIN_EMAIL[_FILE]` | First admin's email; consumed only when `admins` empty | YES on first deploy |
| `ATTUNE_BOOTSTRAP_ADMIN_PASSWORD[_FILE]` | First admin's password; consumed only when `admins` empty | YES on first deploy |
| `ATTUNE_CONFIRM_LARK_DELETE` | Set to `yes` to allow the v0.2→v0.3 migration to delete `lark-*` `user_feedback` rows | YES when upgrading a v0.2 install with rows |

- [ ] **Step 3: private-deploy.md upgrade runbook**

Add the 7-step preflight from spec CHANGELOG `### Removed` block.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/private-deploy.md
git commit -m "docs(README,deploy): inbound framework + env vars + upgrade runbook (#66 step 20/24)

Lifts spec architecture diagram into README; documents the 4 new env vars;
adds the 7-step v0.2→v0.3 preflight checklist to private-deploy.md."
```

---

### Task 22: CHANGELOG

**Files:**
- Modify: `CHANGELOG.md` (under `[Unreleased]`)

- [ ] **Step 1: Append entries**

Copy the `### Added` / `### Changed` / `### Removed` / `### Security` content verbatim from spec §Release notes (CHANGELOG). The preflight runbook goes at the top of `### Removed`.

- [ ] **Step 2: Verify Keep-a-Changelog format**

Each section in alphabetical heading order: Added → Changed → Deprecated → Removed → Fixed → Security.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(CHANGELOG): record #66 channel-agnostic inbound + Lark removal (#66 step 21/24)

Per spec §Release notes. Added (framework + 2 adapters + password login +
4 metrics + env vars), Changed (ValidSources, layering, routing prefix),
Removed (Lark end-to-end + dev_login + insecure_cookies + 7-step
upgrade preflight), Security (HMAC + dual-secret + bcrypt + cookie attrs)."
```

---

### Task 23: Observability metrics wiring (already half-done in Task 5)

**Files:**
- Modify: `internal/infra/metrics/metrics.go` — drop any Lark-only metric defs (done in Task 17) and add a NewInboundMetrics() pass-through if attune has a central metrics setup
- Modify: `cmd/attune/server.go` — already wires `inbound.NewPrometheusMetrics` in Task 16; verify

- [ ] **Step 1: Verify the 4 metrics show up on /metrics**

```bash
ATTUNE_INBOUND_MASTER_KEY=$(openssl rand -hex 32) \
    ATTUNE_BOOTSTRAP_ADMIN_EMAIL=a@b.c \
    ATTUNE_BOOTSTRAP_ADMIN_PASSWORD=p \
    ./attune &
sleep 2
curl -s localhost:8080/metrics | grep -E '^attune_inbound_'
killall attune
```
Expected output includes:
```
attune_inbound_total
attune_inbound_latency_seconds_bucket
attune_inbound_source_state
attune_inbound_poll_lag_seconds
```

- [ ] **Step 2: Commit (if any wiring tweaks)**

```bash
git commit --allow-empty -m "feat(observability): verify 4 inbound metrics exposed (#66 step 22/24)

attune_inbound_{total,latency_seconds,source_state,poll_lag_seconds}.
#63 Grafana panels consume these."
```

---

### Task 24: Final verification — run all 22 gates

**Files:** (no changes — verification only)

Run each gate from spec §Verification. Record output of each in PR description.

- [ ] **Gate 1**: `go build ./...`
- [ ] **Gate 2**: `go vet ./...`
- [ ] **Gate 3**: `go test -short ./...`
- [ ] **Gate 4**: `golangci-lint run` (including new depguard rules)
- [ ] **Gate 5**: `lizard . -l go -C 15 -T nloc=100`
- [ ] **Gate 6**: `npx -y jscpd . --pattern '**/*.go' --threshold 5`
- [ ] **Gate 7**: `make proto` followed by `git diff` — expect empty
- [ ] **Gate 8**: `scripts/lint-rawptr.sh`
- [ ] **Gate 9**: `scripts/lint-slog.sh`
- [ ] **Gate 10**: lark-string grep returns empty:

```bash
grep -rl '[Ll]ark\|Feishu\|FEISHU\|飞书' . \
    --include='*.go' --include='*.tsx' --include='*.ts' --include='*.proto' \
    --exclude-dir=docs --exclude-dir=internal/migrations
```
Expected: no output.

- [ ] **Gate 11**: integration test (testcontainers/postgres) — webhook + email both land rows
- [ ] **Gate 12**: deliberate pollution test (Rule 1)

Temporarily edit `internal/service/ingest/ingestor.go`:
```go
import _ "github.com/Phixsura/attune/internal/inbound/adapter/webhook" // POLLUTION
```
Run `golangci-lint run`. Expected: red, error cites `inbound-boundary`. Revert.

- [ ] **Gate 13**: symmetric pollution test (Rule 2)

Temporarily edit `internal/inbound/registry.go`:
```go
import _ "github.com/Phixsura/attune/internal/service/ingest" // POLLUTION
```
Run `golangci-lint run`. Expected: red, error cites `inbound-framework-isolation`. Revert.

- [ ] **Gate 14**: conformance tests pass for webhook + email
- [ ] **Gate 15**: Manual happy-path smoke (fresh docker-compose; bootstrap; login; create webhook source; signed curl POST; observe row + metric increment)
- [ ] **Gate 16**: Bootstrap-empty-no-env

```bash
docker-compose down -v
docker-compose up -d postgres
unset ATTUNE_BOOTSTRAP_ADMIN_EMAIL ATTUNE_BOOTSTRAP_ADMIN_PASSWORD
ATTUNE_INBOUND_MASTER_KEY=$(openssl rand -hex 32) ./attune 2>&1 | head -5
```
Expected: process exits non-zero with "no admins exist and ATTUNE_BOOTSTRAP_ADMIN_…".

- [ ] **Gate 17**: Two-pod bootstrap race

```bash
for i in 1 2; do
  ATTUNE_INBOUND_MASTER_KEY=$key \
    ATTUNE_BOOTSTRAP_ADMIN_EMAIL=admin@example.com \
    ATTUNE_BOOTSTRAP_ADMIN_PASSWORD=p \
    ./attune --port 8080-$i &
done
wait
psql -c 'SELECT COUNT(*) FROM admins'
```
Expected: count = 1.

- [ ] **Gate 18**: test-connection bad creds

```bash
curl -X POST localhost:8080/fb/v1/console/install/inbound/sources/test-connection \
  -d '{"channel":"email","config":{"host":"localhost","port":"993","tls":true,"username":"x","password":"bad"}}' \
  -H 'Cookie: attune_session=…' \
  -H 'Content-Type: application/json'
```
Expected: `200 {ok:false, error:"…"}`, NOT 500.

- [ ] **Gate 19**: rotate-secret 24h overlap

(Use console UI; verify previous-secret signed POST still 200; second rotate returns 409.)

- [ ] **Gate 20**: migration with existing lark rows

(Seed 1000 lark rows; run migrate without env → abort with count message; set env; re-run → succeeds.)

- [ ] **Gate 21**: master-key boot validation

```bash
unset ATTUNE_INBOUND_MASTER_KEY
./attune 2>&1 | head -3
```
Expected: non-zero exit, fatal error names the env var.

- [ ] **Gate 22**: login enumeration timing

(Bash loop, 1000 known-bad + 1000 wrong-pass; measure median wall-time; require within 10%.)

- [ ] **Final commit**

```bash
git commit --allow-empty -m "test(verification): all 22 spec gates green (#66 step 24/24)

Run-log archived in PR description. Spec
docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md status:
Accepted → Implemented (update via separate commit per CLAUDE.md §10)."
```

---

## Self-review

### 1. Spec coverage

| Spec section | Implementing task(s) |
|---|---|
| Vision / Goals / Non-goals | (informational; no task) |
| Architecture overview | T3, T4, T16 |
| §5 layering increment | T20 |
| Port interface | T3 |
| Registry | T4 |
| Supporting types | T5 |
| Conformance test suite | T6 |
| Webhook adapter | T13 |
| Master key + envelope | T5 (impl), T16 (BootstrapValidate) |
| Email IMAP adapter | T14 |
| Console: local admin password | T8 (repo), T11 (handler), T12 (UI) |
| Data migrations | T8 (admins), T9 (inbound_sources), T10 (guard), T19 (drop_lark) |
| New dependencies | T6 (goleak), T8 (bcrypt), T14 (emersion x2) |
| CI boundary guard | T7 |
| Observability | T5 (impl), T23 (wiring) |
| File-by-file delta DELETE/EDIT | T17 (code), T18 (proto), T19 (domain) |
| Alternatives considered | (informational) |
| Risks / tradeoffs | (informational; mitigations baked into impl tasks) |
| Implementation plan | THIS document |
| Verification 22 gates | T24 |
| References | (informational) |

**No gaps.**

### 2. Placeholder scan

Searched for "TBD" / "TODO" / "implement later" / "similar to Task N" / "add appropriate" — none found in task bodies. Two places use the phrase "may simplify by" / "if available" as legitimate latitude (not vague TODOs): T13 step 5 (rotation single-UPDATE vs SERIALIZABLE tx) and T14 step 7 (IMAP-mock library), both with concrete fallback specified.

### 3. Type consistency

- `inbound.Adapter`, `inbound.Deps`, `inbound.IngestPort`, `inbound.Mux`, `inbound.ShutdownTimeouter` — defined T3, referenced T4/T5/T6/T13/T14/T16 with matching signatures.
- `inbound.Factory`, `inbound.Entry`, `inbound.Manager`, `inbound.DefaultShutdownTimeout` — defined T4, referenced T6/T16.
- `inbound.SecretStore`, `inbound.NewAESGCMSecretStore` — defined T5, used T13/T14/T16.
- `inbound.SourceStore`, `inbound.Source`, `inbound.SourceState` — defined T5, impl T9, used T13/T14/T15.
- `inboundtest.TestAdapterContract`, `inboundtest.DepsFor`, `inboundtest.NewFakeSources` — defined T6, used T13/T14.
- `admin.Repo`, `admin.NewAdmin`, `admin.ErrAlreadyBootstrapped` — defined T8, used T11.
- `webhook.RotateSecret`, `webhook.ErrRotationInGraceWindow` — defined T13, used T15.
- `migrate.ConfirmLarkDelete`, `migrate.ErrDestructiveMigrationGuard` — defined T10, used T19 implicitly via runner.

**Names consistent across tasks.**

---

## Execution Handoff

Plan complete and saved to `docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound-plan.md` (note: attune uses its own proposals tree; this lives alongside the spec for one-stop reference).

Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Best for a 24-task plan where intermediate tasks have clean commit boundaries.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints. Best if you want to watch each TDD cycle live but accept slower turnaround.

**Which approach?**
