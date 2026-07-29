# Amplitude & Mixpanel Cohort Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Import named cohorts from Amplitude and Mixpanel so operators can filter feedback and customer requests by cohort membership.

**Architecture:** Attune exposes webhook endpoints as a push destination for both providers (Amplitude list-based REST, Mixpanel custom webhook). A new `cohortsync` subsystem (`internal/cohortsync/` framework root + `service/cohortsync/` + `repo/cohortsync/` + `handlers/cohortsyncwebhook/`) handles membership delta application, sync runs, stale TTL, and credential encryption via the shared `secretstore.TinkStore`. Feedback/request filtering uses runtime JOIN on the existing `user_feedback.subject_key` column. Console frontend extends `console/src/features/cohort-sync/`.

**Tech Stack:** Go 1.26.5, PostgreSQL 14+, chi router, pgxpool, Prometheus, protobuf (buf), React + Vite + TanStack Router

**Proposal:** `docs/proposals/2026/07/2026-07-27-amplitude-mixpanel-cohort-sync.md`

## Global Constraints

- Go `vet` + `build` + `test -race`: 0 warnings / 0 errors / all pass.
- `golangci-lint`: 0 findings (includes new `cohortsync-boundary` + `cohortsync-framework-isolation` rules).
- Function CCN ≤ 15, NLOC ≤ 100 (`lizard`).
- No raw `*p` dereference or `&x` — use `ptrext.Of` / `ptrext.Indirect` / `ptrext.IndirectOr`.
- Logging via `logext.Infof` / `logext.Warnf` / `logext.Errorf` with `ctx` first arg. No `log/slog` in business code.
- Credentials never logged in clear text.
- Outbound HTTP via `nethardening.Policy`-wrapped transport (SSRF guard).
- `CHANGELOG.md` updated in the final commit under `[Unreleased] → ### Added`.
- Conventional Commits: `feat(cohortsync): ...`.
- Every new audit action added to BOTH Go `validActions` map AND DB `chk_audit_action_value` migration.
- Every new metric added to `allMetrics` slice, `RegisteredMetricNames`, and `observability/README.md`.
- Proto changes: edit `.proto`, run `make proto`, commit both.

---

### Task 1: Database Migration + Audit Actions

**Files:**
- Create: `internal/infra/database/migrations/117_cohort_sync.sql`
- Create: `internal/infra/database/migrations/118_cohort_sync_audit_actions.sql`
- Modify: `internal/service/auditlog/actions.go` — add cohort actions to `validActions`

**Interfaces:**
- Produces: 4 tables (`cohort_sources`, `cohorts`, `cohort_memberships`, `cohort_sync_runs`) with all CHECK constraints and indexes; 6 new audit action strings.

- [ ] **Step 1: Create migration 117_cohort_sync.sql**

Copy the exact DDL from the proposal's "Migration `117_cohort_sync.sql`" section. This creates four tables: `cohort_sources`, `cohorts`, `cohort_memberships`, `cohort_sync_runs` with all constraints and indexes.

The SQL is fully specified in the proposal. Key points:
- `cohort_sources.provider CHECK (provider IN ('amplitude', 'mixpanel'))`
- `cohort_sync_runs.status CHECK (status IN ('running', 'succeeded', 'failed', 'skipped'))`
- `uq_cohort_memberships_user UNIQUE (tenant_id, cohort_id, external_user_id)`
- Partial indexes: `idx_cohort_memberships_active` (WHERE left_at IS NULL), `idx_cohort_memberships_by_user` (WHERE left_at IS NULL), `idx_cohort_memberships_expired` (WHERE expires_at IS NOT NULL AND left_at IS NOT NULL)

- [ ] **Step 2: Create migration 118_cohort_sync_audit_actions.sql**

Pattern from `116_inbound_source_update_audit.sql`: DROP + re-ADD the `chk_audit_action_value` constraint with the full action list plus the new cohort actions:

```sql
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS chk_audit_action_value;
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_action_value
  CHECK (action IN (
    -- ... all existing actions from migration 116 ...
    'cohort_source.create',
    'cohort_source.update',
    'cohort_source.delete',
    'cohort.update',
    'cohort.sync'
  ));
```

- [ ] **Step 3: Add audit actions to Go validActions map**

In `internal/service/auditlog/actions.go`, add to `validActions`:

```go
"cohort_source.create":  {},
"cohort_source.update":  {},
"cohort_source.delete":  {},
"cohort.update":         {},
"cohort.sync":           {},
```

- [ ] **Step 4: Run migration and tests**

Run: `go build ./... && go vet ./...`
Run: `go test ./internal/service/auditlog/... -v -count=1`

- [ ] **Step 5: Commit**

```
feat(cohortsync): add cohort sync schema and audit actions

Migration 117 creates cohort_sources, cohorts, cohort_memberships,
and cohort_sync_runs tables. Migration 118 extends the audit action
CHECK constraint with cohort_source.* and cohort.* actions.

Closes: part of #233
```

---

### Task 2: Cohortsync Framework Root (Registry + Types)

**Files:**
- Create: `internal/cohortsync/registry.go`
- Create: `internal/cohortsync/registry_test.go`
- Create: `internal/cohortsync/egress.go`

**Interfaces:**
- Produces: `cohortsync.Connection`, `cohortsync.MemberDelta`, `cohortsync.SyncPayload`, `cohortsync.Provider` interface, `cohortsync.Register(provider, display string, factory Factory)`, `cohortsync.Lookup(provider string) (Provider, bool)`, `cohortsync.Providers() []Entry`, `cohortsync.ResetForTest()`, `cohortsync.NewHTTPClient(timeout time.Duration) *http.Client`, `cohortsync.ValidateProviderURL(raw string) error`.

- [ ] **Step 1: Create `internal/cohortsync/registry.go`**

Mirror `internal/externalsync/registry.go` exactly, with the cohortsync-specific types from the proposal:

```go
// Package cohortsync owns the provider adapter contract for cohort sync.
package cohortsync

// Connection, MemberDelta, SyncPayload types from proposal
// Provider interface with Provider(), ParseWebhook(), PullCohort()
// Factory, Entry, registration types
// Register, Lookup, Providers, ResetForTest, ValidateProviderToken functions
// providerShapeRE same as externalsync: ^[a-z0-9_-]+$
```

The `Provider` interface uses `ParseWebhook(body []byte, headers map[string]string, secret []byte) (SyncPayload, error)` — NOT `*http.Request`. And `PullCohort(ctx context.Context, conn Connection, externalCohortID string) (SyncPayload, error)`.

Do NOT register a noop provider here — the adapters self-register in Task 4/5.

- [ ] **Step 2: Create `internal/cohortsync/egress.go`**

Mirror `internal/externalsync/egress.go` exactly:

```go
package cohortsync

import (
    "net/http"
    "time"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
    "github.com/Phixsura/attune/internal/pkg/nethardening"
    "github.com/Phixsura/attune/internal/pkg/ptrext"
)

var egressPolicy = nethardening.Policy{}

func SetEgressPolicy(p nethardening.Policy) { egressPolicy = p }

func NewHTTPClient(timeout time.Duration) *http.Client {
    if timeout <= 0 {
        timeout = 10 * time.Second
    }
    return ptrext.Of(http.Client{
        Transport: otelhttp.NewTransport(egressPolicy.NewHTTPTransport()),
        Timeout:   timeout,
    })
}

func ValidateProviderURL(raw string) error {
    return egressPolicy.ValidateURL(raw)
}
```

- [ ] **Step 3: Create `internal/cohortsync/registry_test.go`**

Test `Register`/`Lookup`/`Providers`/`ResetForTest`/`ValidateProviderToken`, same patterns as `internal/externalsync/registry_test.go`:
- Register + Lookup succeeds
- Register rejects nil factory, bad name, empty display, duplicate
- Providers returns sorted entries
- ValidateProviderToken rejects bad shapes

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cohortsync/... -v -count=1 -race`

- [ ] **Step 5: Commit**

```
feat(cohortsync): add framework root with registry and egress

Provider adapter contract (Connection, MemberDelta, SyncPayload,
Provider interface), thread-safe registry (Register/Lookup/Providers),
and SSRF-hardened egress (NewHTTPClient, ValidateProviderURL).
```

---

### Task 3: Repo Layer (CRUD + Membership Upsert)

**Files:**
- Create: `internal/repo/cohortsync/types.go`
- Create: `internal/repo/cohortsync/repo.go`
- Create: `internal/repo/cohortsync/repo_test.go`

**Interfaces:**
- Consumes: Tables from Task 1.
- Produces: `Repo` struct with `New(pool *pgxpool.Pool) *Repo`, and methods: `CreateSource`, `GetSource`, `ListSources`, `UpdateSource`, `DeleteSource`, `UpsertCohort`, `GetCohort`, `ListCohorts`, `UpdateCohort`, `UpsertMemberships`, `MarkDeparted`, `CleanExpired`, `CountActiveMembers`, `InsertRun`, `UpdateRun`, `ListRuns`.

- [ ] **Step 1: Create `internal/repo/cohortsync/types.go`**

Define the repo-layer row types mirroring the DB schema. Pattern from `internal/repo/externalsync/types.go`:

```go
package cohortsync

import (
    "errors"
    "time"
    "github.com/google/uuid"
)

var (
    ErrSourceNotFound  = errors.New("cohort source not found")
    ErrCohortNotFound  = errors.New("cohort not found")
    ErrRunNotFound     = errors.New("cohort sync run not found")
    ErrConflict        = errors.New("cohort sync conflict")
)

type Source struct {
    ID                      uuid.UUID
    TenantID                string
    Provider                string
    Name                    string
    AuthType                string
    CredentialKeyID         string
    CredentialCiphertext    []byte
    BaseURL                 string
    ProviderConfig          []byte
    WebhookSecretKeyID      string
    WebhookSecretCiphertext []byte
    Enabled                 bool
    Status                  string
    LastSyncAt              *time.Time
    LastError               string
    CreatedBy               string
    UpdatedBy               string
    CreatedAt               time.Time
    UpdatedAt               time.Time
}

type Cohort struct {
    ID               uuid.UUID
    TenantID         string
    CohortSourceID   uuid.UUID
    ExternalCohortID string
    Name             string
    Description      string
    StaleTTLDays     int
    MemberCount      int
    Enabled          bool
    LastSyncedAt     *time.Time
    LastError        string
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type Membership struct {
    ID             uuid.UUID
    TenantID       string
    CohortID       uuid.UUID
    ExternalUserID string
    Email          string
    DisplayName    string
    UserProperties []byte
    JoinedAt       time.Time
    LeftAt         *time.Time
    ExpiresAt      *time.Time
    LastSeenAt     time.Time
}

type SyncRun struct {
    ID             uuid.UUID
    TenantID       string
    CohortID       uuid.UUID
    Trigger        string
    Status         string
    MembersAdded   int
    MembersRemoved int
    MembersTotal   int
    ErrorMessage   string
    StartedAt      time.Time
    FinishedAt     *time.Time
    CreatedAt      time.Time
}
```

- [ ] **Step 2: Create `internal/repo/cohortsync/repo.go`**

Implement the Repo struct and all SQL methods. Key patterns:
- `New(pool *pgxpool.Pool) *Repo` — same as externalsync
- `UpsertMemberships(ctx, tenantID string, cohortID uuid.UUID, members []MembershipUpsert) (added, updated int, err error)` — batch `INSERT ... ON CONFLICT (tenant_id, cohort_id, external_user_id) DO UPDATE SET left_at = NULL, expires_at = NULL, last_seen_at = NOW(), email = EXCLUDED.email, ...`
- `MarkDeparted(ctx, tenantID string, cohortID uuid.UUID, staleTTLDays int, olderThan time.Time) (int, error)` — `UPDATE cohort_memberships SET left_at = NOW(), expires_at = NOW() + interval '1 day' * $staleTTL WHERE tenant_id = $1 AND cohort_id = $2 AND left_at IS NULL AND last_seen_at < $olderThan`
- `CleanExpired(ctx context.Context) (int64, error)` — `DELETE FROM cohort_memberships WHERE expires_at IS NOT NULL AND expires_at < NOW()`
- `CountActiveMembers(ctx, tenantID string, cohortID uuid.UUID) (int, error)` — `SELECT count(*) WHERE left_at IS NULL`

Use `pgx.ErrNoRows` → domain error mapping, `ptrext.Of` for pointer returns.

- [ ] **Step 3: Create `internal/repo/cohortsync/repo_test.go`**

Integration tests (build-tagged `//go:build integration`) for each repo method:
- Create/Get/List/Update/Delete source
- UpsertCohort (insert + update on duplicate)
- UpsertMemberships (insert new + re-join departed)
- MarkDeparted (only marks members with last_seen_at < threshold)
- CleanExpired (only deletes expired rows)
- CountActiveMembers
- InsertRun / UpdateRun / ListRuns

For unit tests without DB, test the row scanning helpers and validation logic.

- [ ] **Step 4: Run tests**

Run: `go build ./internal/repo/cohortsync/... && go vet ./internal/repo/cohortsync/...`
Run: `go test ./internal/repo/cohortsync/... -v -count=1 -race` (unit tests)

- [ ] **Step 5: Commit**

```
feat(cohortsync): add repo layer with CRUD and membership upsert

Source, Cohort, Membership, SyncRun CRUD operations. Bulk membership
upsert via INSERT ON CONFLICT with re-join semantics. Stale cleanup
via MarkDeparted + CleanExpired.
```

---

### Task 4: Service Layer (Delta Application + Credential Encryption)

**Files:**
- Create: `internal/service/cohortsync/service.go`
- Create: `internal/service/cohortsync/service_test.go`

**Interfaces:**
- Consumes: `repo/cohortsync.Repo` (Task 3), `secretstore.TinkStore` (existing), `auditlog.Event` (existing), `cohortsync.SyncPayload` / `MemberDelta` (Task 2).
- Produces: `Service` struct with `New(repo Repo, store Store) *Service`, `SetAuditLogger(audit auditRecorder)`, `CreateSource`, `UpdateSource`, `DeleteSource`, `ListSources`, `ApplyDelta(ctx, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload) (*SyncRunResult, error)`, `ApplyFullSnapshot(ctx, tenantID string, sourceID uuid.UUID, cohortID uuid.UUID, payload cohortsync.SyncPayload) (*SyncRunResult, error)`, `SyncNow(ctx, tenantID string, cohortID uuid.UUID, actor Actor) (*SyncRunResult, error)`, `CleanExpired(ctx) (int64, error)`, `Health(ctx, tenantID string) (HealthSummary, error)`.

- [ ] **Step 1: Create `internal/service/cohortsync/service.go`**

Key patterns mirroring `internal/service/externalsync/service.go`:

```go
package cohortsync

type Store interface {
    EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error)
    DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error)
}

type Repo interface {
    // List all repo methods this service needs (consumer-defined interface)
}

type Service struct {
    repo  Repo
    store Store
    audit auditRecorder
}

func New(repo Repo, store Store) *Service {
    return ptrext.Of(Service{repo: repo, store: store})
}
```

`ApplyDelta` is the core: receives a normalized `SyncPayload`, applies add/remove deltas via repo `UpsertMemberships` / `MarkDeparted`, records a `SyncRun`, updates `cohorts.member_count` at the end.

`CreateSource` encrypts credentials using `s.store.EncryptValue([]byte(credential), sourceAAD(tenantID, id, provider))`, validates `base_url` via `cohortsync.ValidateProviderURL()`.

- [ ] **Step 2: Create `internal/service/cohortsync/service_test.go`**

Unit tests with mock repo + mock store:
- `TestCreateSource_EncryptsCredential`
- `TestApplyDelta_AddMembers`
- `TestApplyDelta_RemoveMembers_SetsLeftAtAndExpiresAt`
- `TestApplyFullSnapshot_MarksAbsentAsDeparted`
- `TestApplyDelta_SkipsDisabledCohort`
- `TestSyncNow_RejectsWhenRunning` (409 guard)
- `TestCleanExpired`
- `TestCreateSource_RejectsSSRFUrl`

- [ ] **Step 3: Run tests**

Run: `go test ./internal/service/cohortsync/... -v -count=1 -race`

- [ ] **Step 4: Commit**

```
feat(cohortsync): add service layer with delta application

ApplyDelta, ApplyFullSnapshot, SyncNow with concurrency guard,
credential encryption via TinkStore, SSRF validation, stale cleanup,
and audit recording.
```

---

### Task 5: Amplitude Adapter

**Files:**
- Create: `internal/cohortsync/adapter/amplitude/adapter.go`
- Create: `internal/cohortsync/adapter/amplitude/adapter_test.go`
- Create: `internal/cohortsync/adapter/amplitude/testdata/create.json`
- Create: `internal/cohortsync/adapter/amplitude/testdata/add.json`
- Create: `internal/cohortsync/adapter/amplitude/testdata/remove.json`

**Interfaces:**
- Consumes: `cohortsync.Provider` interface (Task 2), `cohortsync.NewHTTPClient` (Task 2).
- Produces: Amplitude adapter registered as `"amplitude"` / `"Amplitude"` via `init()`.

- [ ] **Step 1: Create fixture files in testdata/**

Create realistic Amplitude list-based cohort sync payloads based on the partner API documentation:

`testdata/create.json`:
```json
{
  "cohort_id": "abc123",
  "cohort_name": "Power Users",
  "operation": "create",
  "user_ids": ["user-1", "user-2", "user-3"],
  "user_id_type": "BY_ID"
}
```

`testdata/add.json`:
```json
{
  "cohort_id": "abc123",
  "cohort_name": "Power Users",
  "operation": "add",
  "user_ids": ["user-4", "user-5"],
  "user_id_type": "BY_ID"
}
```

`testdata/remove.json`:
```json
{
  "cohort_id": "abc123",
  "cohort_name": "Power Users",
  "operation": "remove",
  "user_ids": ["user-1"],
  "user_id_type": "BY_ID"
}
```

- [ ] **Step 2: Create `internal/cohortsync/adapter/amplitude/adapter.go`**

```go
package amplitude

import (
    core "github.com/Phixsura/attune/internal/cohortsync"
    "github.com/Phixsura/attune/internal/pkg/ptrext"
)

const providerID = "amplitude"

type Adapter struct {
    client *http.Client
}

func init() {
    core.Register(providerID, "Amplitude", func() core.Provider {
        return ptrext.Of(Adapter{client: core.NewHTTPClient(30 * time.Second)})
    })
}

func (a *Adapter) Provider() string { return providerID }

func (a *Adapter) ParseWebhook(body []byte, headers map[string]string, secret []byte) (core.SyncPayload, error) {
    // 1. Parse JSON body
    // 2. Determine operation from "operation" field or URL path suffix
    // 3. Map user_ids to MemberDelta with Action "add" or "remove"
    // 4. For "create" operation, Action = "add" (initial population)
    // Return SyncPayload with IsFullSnapshot = false (Amplitude is incremental)
}

func (a *Adapter) PullCohort(ctx context.Context, conn core.Connection, externalCohortID string) (core.SyncPayload, error) {
    // Amplitude Behavioral Cohorts Download API (async 3-step):
    // 1. POST /api/5/cohorts/request/{cohort_id}
    // 2. GET  /api/5/cohorts/request/{request_id}/status (poll until "COMPLETE")
    // 3. GET  /api/5/cohorts/request/{request_id}/file (download CSV)
    // Parse CSV, convert to MemberDelta list with Action = "add"
    // Return SyncPayload with IsFullSnapshot = true
}
```

- [ ] **Step 3: Create `internal/cohortsync/adapter/amplitude/adapter_test.go`**

Fixture-based tests:
- `TestParseWebhook_Create` — load `testdata/create.json`, verify SyncPayload
- `TestParseWebhook_Add` — load `testdata/add.json`, verify deltas have Action="add"
- `TestParseWebhook_Remove` — load `testdata/remove.json`, verify deltas have Action="remove"
- `TestParseWebhook_MalformedJSON` — verify error
- `TestParseWebhook_EmptyUserIds` — verify empty deltas, no error

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cohortsync/adapter/amplitude/... -v -count=1 -race`

- [ ] **Step 5: Commit**

```
feat(cohortsync): add Amplitude adapter with fixture tests

ParseWebhook handles list-based create/add/remove operations.
PullCohort implements the Behavioral Cohorts Download API fallback.
```

---

### Task 6: Mixpanel Adapter

**Files:**
- Create: `internal/cohortsync/adapter/mixpanel/adapter.go`
- Create: `internal/cohortsync/adapter/mixpanel/adapter_test.go`
- Create: `internal/cohortsync/adapter/mixpanel/testdata/members.json`
- Create: `internal/cohortsync/adapter/mixpanel/testdata/add_members.json`
- Create: `internal/cohortsync/adapter/mixpanel/testdata/remove_members.json`

**Interfaces:**
- Consumes: `cohortsync.Provider` interface (Task 2), `cohortsync.NewHTTPClient` (Task 2).
- Produces: Mixpanel adapter registered as `"mixpanel"` / `"Mixpanel"` via `init()`.

- [ ] **Step 1: Create fixture files in testdata/**

`testdata/members.json`:
```json
{
  "action": "members",
  "cohort_name": "Enterprise Accounts",
  "cohort_id": "cohort-xyz",
  "mixpanel_session_id": "session-001",
  "members": [
    {"email": "alice@example.com", "mixpanel_distinct_id": "uid-1", "first_name": "Alice", "last_name": "Smith"},
    {"email": "bob@example.com", "mixpanel_distinct_id": "uid-2", "first_name": "Bob", "last_name": "Jones"}
  ]
}
```

`testdata/add_members.json` and `testdata/remove_members.json` with `action: "add_members"` / `"remove_members"` respectively.

- [ ] **Step 2: Create `internal/cohortsync/adapter/mixpanel/adapter.go`**

```go
package mixpanel

const providerID = "mixpanel"

func init() {
    core.Register(providerID, "Mixpanel", func() core.Provider {
        return ptrext.Of(Adapter{client: core.NewHTTPClient(30 * time.Second)})
    })
}

func (a *Adapter) ParseWebhook(body []byte, headers map[string]string, secret []byte) (core.SyncPayload, error) {
    // 1. Parse JSON body
    // 2. Switch on "action" field: "members", "add_members", "remove_members"
    // 3. For "members": IsFullSnapshot = true, all members as "add" deltas
    // 4. For "add_members": IsFullSnapshot = false, Action = "add"
    // 5. For "remove_members": IsFullSnapshot = false, Action = "remove"
    // 6. Map mixpanel_distinct_id → ExternalUserID, email → Email,
    //    first_name + " " + last_name → DisplayName
}

func (a *Adapter) PullCohort(ctx context.Context, conn core.Connection, externalCohortID string) (core.SyncPayload, error) {
    // Mixpanel Engage API: GET /api/2.0/engage?filter_by_cohort={"id": cohortID}
    // Paginate through results, convert to MemberDelta with Action = "add"
    // Return with IsFullSnapshot = true
}
```

- [ ] **Step 3: Create fixture tests**

Same pattern as Task 5: test all three actions + malformed input + basic auth verification.

- [ ] **Step 4: Run tests and commit**

```
feat(cohortsync): add Mixpanel adapter with fixture tests

ParseWebhook handles members/add_members/remove_members actions.
PullCohort implements the Engage API fallback.
```

---

### Task 7: Webhook Handlers + Route Mounting

**Files:**
- Create: `internal/handlers/cohortsyncwebhook/handler.go`
- Create: `internal/handlers/cohortsyncwebhook/handler_test.go`
- Modify: `cmd/attune/router_v1.go` — add cohort sync webhook mount
- Modify: `cmd/attune/main.go` — add blank imports for adapters

**Interfaces:**
- Consumes: `service/cohortsync.Service` (Task 4), `cohortsync.Lookup` (Task 2), `repo/cohortsync.Source` (Task 3).
- Produces: HTTP handlers at `/v1/cohort-sync/amplitude/{tenant_id}/{source_id}/{operation}` and `/v1/cohort-sync/mixpanel/{tenant_id}/{source_id}`.

- [ ] **Step 1: Create `internal/handlers/cohortsyncwebhook/handler.go`**

Pattern from `internal/handlers/externalsyncwebhook/handler.go`:

```go
package cohortsyncwebhook

const maxCohortWebhookBodyBytes = 32 << 20 // 32 MB

type service interface {
    GetSourceForWebhook(ctx context.Context, tenantID string, sourceID uuid.UUID) (*repo.Source, error)
    DecryptCredential(source repo.Source) ([]byte, error)
    ApplyDelta(ctx context.Context, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload) (*svc.SyncRunResult, error)
    ApplyFullSnapshot(ctx context.Context, tenantID string, sourceID uuid.UUID, cohortID uuid.UUID, payload cohortsync.SyncPayload) (*svc.SyncRunResult, error)
}

type Handler struct {
    service service
}

func (h *Handler) Routes() chi.Router {
    r := chi.NewRouter()
    r.Post("/amplitude/{tenant_id}/{source_id}/create", h.Amplitude)
    r.Post("/amplitude/{tenant_id}/{source_id}/add", h.Amplitude)
    r.Post("/amplitude/{tenant_id}/{source_id}/remove", h.Amplitude)
    r.Post("/mixpanel/{tenant_id}/{source_id}", h.Mixpanel)
    return r
}

func (h *Handler) Amplitude(w http.ResponseWriter, r *http.Request) {
    // 1. Parse tenant_id, source_id from URL
    // 2. Read body (with 32MB limit)
    // 3. Get source, verify enabled
    // 4. Decrypt credential, verify basic auth
    // 5. Lookup "amplitude" adapter
    // 6. adapter.ParseWebhook(body, headers, secret)
    // 7. service.ApplyDelta(ctx, tenantID, sourceID, payload)
    // 8. Return 200 or 202
}

func (h *Handler) Mixpanel(w http.ResponseWriter, r *http.Request) {
    // Same pattern, but for Mixpanel
    // If payload.IsFullSnapshot → service.ApplyFullSnapshot
    // Else → service.ApplyDelta
}
```

- [ ] **Step 2: Modify `cmd/attune/router_v1.go`**

In `mountV1AdapterRoutes`, add:

```go
// Cohort sync webhook receivers
cohortWebhooks := cohortsyncwebhook.NewHandler(cohortsyncService)
r.Mount("/cohort-sync", cohortWebhooks.Routes())
```

- [ ] **Step 3: Add blank imports to `cmd/attune/main.go`**

```go
// #233 cohortsync adapters self-register via init(); cmd/attune owns
// the production registry population point.
_ "github.com/Phixsura/attune/internal/cohortsync/adapter/amplitude"
_ "github.com/Phixsura/attune/internal/cohortsync/adapter/mixpanel"
```

- [ ] **Step 4: Create handler tests**

`internal/handlers/cohortsyncwebhook/handler_test.go`:
- `TestAmplitude_ValidPayload_Returns200`
- `TestAmplitude_InvalidAuth_Returns401`
- `TestAmplitude_BodyTooLarge_Returns413`
- `TestAmplitude_DisabledSource_Returns200_Skips`
- `TestMixpanel_MembersAction_Returns200`
- `TestMixpanel_MalformedBody_Returns400`

Use `httptest.NewRecorder` + `httptest.NewRequest` with mock service.

- [ ] **Step 5: Run tests**

Run: `go build ./... && go test ./internal/handlers/cohortsyncwebhook/... -v -count=1 -race`

- [ ] **Step 6: Commit**

```
feat(cohortsync): add webhook handlers and route mounting

Amplitude webhook at /v1/cohort-sync/amplitude/{tid}/{sid}/{op}.
Mixpanel webhook at /v1/cohort-sync/mixpanel/{tid}/{sid}.
32MB body limit. Basic auth verification. Disabled-source skip.
```

---

### Task 8: Metrics Registration

**Files:**
- Modify: `internal/infra/metrics/metrics.go` — add cohort sync metrics + register in allMetrics + RegisteredMetricNames
- Modify: `observability/README.md` — document new metrics

**Interfaces:**
- Produces: `metrics.CohortSyncWebhookRequestsTotal`, `metrics.CohortSyncMembersChangedTotal`, `metrics.CohortSyncRunsTotal`, `metrics.CohortSyncActiveMembers`, `metrics.CohortSyncStaleCleanedTotal`.

- [ ] **Step 1: Add metric declarations**

In `internal/infra/metrics/metrics.go`, add after the ExternalSync metrics block:

```go
// CohortSyncWebhookRequestsTotal counts cohort sync webhook requests.
var CohortSyncWebhookRequestsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "attune_cohort_sync_webhook_requests_total",
        Help: "Cohort sync webhook requests by provider and status.",
    },
    []string{"provider", "status"},
)

// CohortSyncMembersChangedTotal counts cohort membership changes.
var CohortSyncMembersChangedTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "attune_cohort_sync_members_changed_total",
        Help: "Cohort membership changes by provider and action.",
    },
    []string{"provider", "action"},
)

// CohortSyncRunsTotal counts cohort sync runs.
var CohortSyncRunsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "attune_cohort_sync_runs_total",
        Help: "Cohort sync runs by provider, trigger, and status.",
    },
    []string{"provider", "trigger", "status"},
)

// CohortSyncActiveMembers gauges active cohort members.
var CohortSyncActiveMembers = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "attune_cohort_sync_active_members",
        Help: "Active cohort members by provider.",
    },
    []string{"provider"},
)

// CohortSyncStaleCleanedTotal counts cleaned stale memberships.
var CohortSyncStaleCleanedTotal = prometheus.NewCounter(
    prometheus.CounterOpts{
        Name: "attune_cohort_sync_stale_members_cleaned_total",
        Help: "Total stale cohort memberships cleaned up.",
    },
)
```

- [ ] **Step 2: Register in allMetrics + RegisteredMetricNames**

Add all 5 to the `allMetrics` slice and the metric name strings to `registeredMetricNamesRuntime()`.

- [ ] **Step 3: Update observability/README.md**

Add metric documentation entries.

- [ ] **Step 4: Run metric drift test**

Run: `go test ./internal/infra/metrics/... -v -count=1 -race`

- [ ] **Step 5: Commit**

```
feat(cohortsync): register Prometheus metrics

webhook_requests_total, members_changed_total, runs_total,
active_members gauge, stale_members_cleaned_total counter.
```

---

### Task 9: Proto Contract + Console API Handlers

**Files:**
- Create: `proto/attune/v1/cohort_sync.proto`
- Modify: Run `make proto` to generate Go + TS + OpenAPI
- Create: `internal/handlers/console/cohortsync/handler.go`
- Create: `internal/handlers/console/cohortsync/handler_test.go`
- Modify: `cmd/attune/router.go` — mount console cohort sync routes

**Interfaces:**
- Consumes: `service/cohortsync.Service` (Task 4), proto types (generated).
- Produces: Console API endpoints for source CRUD, cohort list, sync, health.

- [ ] **Step 1: Create `proto/attune/v1/cohort_sync.proto`**

Follow the `external_sync.proto` pattern with google.api.http annotations:

```protobuf
syntax = "proto3";
package attune.v1;

import "google/api/annotations.proto";

service CohortSyncService {
  rpc ListCohortSources(ListCohortSourcesRequest) returns (ListCohortSourcesResponse) {
    option (google.api.http) = {get: "/fb/v1/console/cohort-sync/sources"};
  }
  rpc CreateCohortSource(CreateCohortSourceRequest) returns (CohortSource) {
    option (google.api.http) = {
      post: "/fb/v1/console/cohort-sync/sources"
      body: "*"
    };
  }
  // ... UpdateCohortSource, DeleteCohortSource, TestCohortSource
  // ... ListCohorts, UpdateCohort, SyncCohort
  // ... ListCohortSyncRuns, GetCohortSyncHealth
}

// Message definitions for all request/response types
```

- [ ] **Step 2: Run `make proto` and commit generated files**

- [ ] **Step 3: Create console handler + mount routes**

Pattern from `internal/handlers/console/externalsync/handler.go` with `dispatcher.Bind`.

- [ ] **Step 4: Tests and commit**

```
feat(cohortsync): add proto contract and Console API handlers

ListCohortSources, CreateCohortSource, UpdateCohortSource,
DeleteCohortSource, TestCohortSource, ListCohorts, UpdateCohort,
SyncCohort, ListCohortSyncRuns, GetCohortSyncHealth.
```

---

### Task 10: Feedback + Customer Request Filter Integration

**Files:**
- Modify: `proto/attune/v1/ingest.proto` — add `optional string cohort_id = 20` to `ListFeedbackRequest`
- Modify: `proto/attune/v1/customer_request.proto` — add cohort_id filter
- Modify: `internal/repo/feedback/feedback_console.go` — add `CohortID *string` to `ConsoleListOpts`, add EXISTS subquery in `applyStateFilters`
- Modify: `internal/repo/customerrequestview/customerrequestview.go` — add cohort filter
- Modify: `internal/handlers/console/feedback/` — wire cohort_id from proto to opts
- Run: `make proto`

**Interfaces:**
- Consumes: `cohort_memberships` table (Task 1), `user_feedback.subject_key` (existing).
- Produces: Feedback and customer request list endpoints accept `cohort_id` filter.

- [ ] **Step 1: Add cohort_id to ListFeedbackRequest proto**

In `proto/attune/v1/ingest.proto`, add to `ListFeedbackRequest`:
```protobuf
optional string cohort_id = 20;
```

- [ ] **Step 2: Add cohort filter to ConsoleListOpts**

In `internal/repo/feedback/feedback_console.go`, add `CohortID *string` to `ConsoleListOpts` and in `applyStateFilters`:

```go
if opts.CohortID != nil {
    qb.and("EXISTS (SELECT 1 FROM cohort_memberships cm WHERE cm.tenant_id = $1 AND cm.cohort_id = " + qb.addArg(ptrext.Indirect(opts.CohortID)) + "::uuid AND cm.external_user_id = subject_key AND cm.left_at IS NULL)")
}
```

- [ ] **Step 3: Wire in handlers + add customer request filter**

- [ ] **Step 4: Run `make proto` + tests**

Run: `go test ./internal/repo/feedback/... -v -count=1 -race`
Run: `go test ./internal/handlers/console/... -v -count=1 -race`

- [ ] **Step 5: Commit**

```
feat(cohortsync): add cohort filter to feedback and request lists

Runtime JOIN via subject_key ↔ cohort_memberships.external_user_id.
Uses existing idx_user_feedback_tenant_subject_key index.
```

---

### Task 11: GDPR Erasure Extension

**Files:**
- Modify: `internal/repo/gdpr/gdpr.go` — add `DELETE FROM cohort_memberships` step

**Interfaces:**
- Consumes: `cohort_memberships` table (Task 1), GDPR `subject_key` erasure cascade (existing).

- [ ] **Step 1: Add cohort membership deletion to GDPR cascade**

In `internal/repo/gdpr/gdpr.go`, in the `deleteLockedSubject` function, add before the `user_feedback` delete:

```go
// Cohort memberships: delete by external_user_id matching the subject key.
if _, err := tx.Exec(ctx, `DELETE FROM cohort_memberships WHERE tenant_id = $1 AND external_user_id = $2`, tenantID, subjectKey); err != nil {
    return fmt.Errorf("delete cohort memberships: %w", err)
}
```

Also add a count field to the `Counts` struct and the count query.

- [ ] **Step 2: Test**

Run: `go test ./internal/repo/gdpr/... -v -count=1 -race`

- [ ] **Step 3: Commit**

```
fix(cohortsync): extend GDPR erasure to cohort_memberships

Cohort memberships containing PII (email, display_name) are deleted
when the subject_key is erased.
```

---

### Task 12: Depguard Rules + Stale Cleanup Worker

**Files:**
- Modify: `.golangci.yml` — add `cohortsync-boundary` and `cohortsync-framework-isolation`
- Create: `internal/service/cohortsync/worker.go`
- Create: `internal/service/cohortsync/worker_test.go`

**Interfaces:**
- Consumes: `Service.CleanExpired` (Task 4), `metrics.CohortSyncStaleCleanedTotal` (Task 8).

- [ ] **Step 1: Add depguard rules to `.golangci.yml`**

Under `depguard.rules`, add the two rules from the proposal's "Depguard rules" section.

- [ ] **Step 2: Create stale cleanup worker**

A simple periodic function called from `cmd/attune/server.go`:

```go
func (s *Service) RunCleanupLoop(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            cleaned, err := s.CleanExpired(ctx)
            if err != nil {
                logext.Errorf(ctx, "[cohortsync.cleanup] failed,err:%s", err.Error())
                continue
            }
            if cleaned > 0 {
                logext.Infof(ctx, "[cohortsync.cleanup] cleaned %d expired memberships", cleaned)
                metrics.CohortSyncStaleCleanedTotal.Add(float64(cleaned))
            }
        }
    }
}
```

- [ ] **Step 3: Run lint + tests**

Run: `golangci-lint run ./internal/cohortsync/...`
Run: `go test ./internal/service/cohortsync/... -v -count=1 -race`

- [ ] **Step 4: Commit**

```
feat(cohortsync): add depguard rules and stale cleanup worker

cohortsync-boundary and cohortsync-framework-isolation enforce
package layering. Daily cleanup worker removes expired memberships.
```

---

### Task 13: Console Frontend

**Files:**
- Create: `console/src/features/cohort-sync/api/cohort-sync.ts`
- Create: `console/src/features/cohort-sync/api/cohort-sync.test.ts`
- Create: `console/src/features/cohort-sync/components/cohort-sync-page.tsx`
- Create: `console/src/features/cohort-sync/components/cohort-sync-page.test.tsx`
- Create: `console/src/features/cohort-sync/components/cohort-sync-ui.tsx`
- Modify: `console/src/features/feedback/` — add cohort filter dropdown
- Modify: `console/src/features/customer-requests/` — add cohort filter dropdown

**Interfaces:**
- Consumes: Proto-generated TS types from Task 9, API endpoints.

- [ ] **Step 1: Create API client**

`console/src/features/cohort-sync/api/cohort-sync.ts` with functions for all Console API endpoints (list sources, create source, list cohorts, sync, health).

- [ ] **Step 2: Create page + UI components**

Following the `external-sync` pattern: a page component that fetches data, a UI component that renders.

- [ ] **Step 3: Add cohort filter to feedback + customer request lists**

Add a cohort dropdown to the existing filter bar in both lists.

- [ ] **Step 4: Tests**

Run: `cd console && pnpm vitest run --coverage`
Run: `cd console && pnpm tsc -b --noEmit`
Run: `cd console && pnpm biome check`

- [ ] **Step 5: Commit**

```
feat(cohortsync): add Console frontend for cohort sync

Source configuration, cohort list, sync history, health status.
Cohort filter dropdown in feedback and customer request lists.
```

---

### Task 14: CHANGELOG + Final CI Check

**Files:**
- Modify: `CHANGELOG.md` — add entry under `[Unreleased] → ### Added`
- Modify: `docs/private-deploy.md` — add Amplitude/Mixpanel setup section

- [ ] **Step 1: Update CHANGELOG.md**

```markdown
### Added
- Amplitude and Mixpanel cohort sync — import named cohorts as push
  destinations; filter feedback and customer requests by cohort membership;
  sync health visible in Console (#233).
```

- [ ] **Step 2: Add deployment documentation**

Brief section in `docs/private-deploy.md` explaining how to configure Amplitude/Mixpanel as cohort sources.

- [ ] **Step 3: Run full CI check**

Run: `make ci-check`

This validates all quality gates: go vet, go build, go test -race, golangci-lint, lizard CCN/NLOC, jscpd duplication, lint-slog, lint-rawptr, lint-errorcode, lint-integration-layout, lint-artifacts, pnpm tsc, pnpm biome, pnpm vitest, pnpm arch, buf lint, buf breaking.

- [ ] **Step 4: Commit**

```
feat(cohortsync): changelog and deployment docs for #233

Closes #233
```

---

## File Map

| Layer | New files | Modified files |
|---|---|---|
| Migrations | `117_cohort_sync.sql`, `118_cohort_sync_audit_actions.sql` | |
| Framework | `internal/cohortsync/registry.go`, `egress.go`, `registry_test.go` | |
| Adapters | `internal/cohortsync/adapter/amplitude/adapter.go`, `adapter_test.go`, `testdata/` | |
| | `internal/cohortsync/adapter/mixpanel/adapter.go`, `adapter_test.go`, `testdata/` | |
| Repo | `internal/repo/cohortsync/types.go`, `repo.go`, `repo_test.go` | |
| Service | `internal/service/cohortsync/service.go`, `service_test.go`, `worker.go` | |
| Handlers | `internal/handlers/cohortsyncwebhook/handler.go`, `handler_test.go` | |
| | `internal/handlers/console/cohortsync/handler.go`, `handler_test.go` | |
| Proto | `proto/attune/v1/cohort_sync.proto` | `proto/attune/v1/ingest.proto`, `customer_request.proto` |
| Console | `console/src/features/cohort-sync/` (5 files) | feedback + customer-request filter components |
| Config | | `.golangci.yml`, `cmd/attune/main.go`, `cmd/attune/router_v1.go`, `cmd/attune/router.go` |
| Metrics | | `internal/infra/metrics/metrics.go`, `observability/README.md` |
| Audit | | `internal/service/auditlog/actions.go` |
| GDPR | | `internal/repo/gdpr/gdpr.go` |
| Docs | | `CHANGELOG.md`, `docs/private-deploy.md` |
