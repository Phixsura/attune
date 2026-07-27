<!-- markdownlint-disable MD013 -->

# Amplitude & Mixpanel Cohort Sync

| Field | Value |
|---|---|
| Issue | [#233](https://github.com/Phixsura/attune/issues/233) |
| Status | Proposed |
| Started | 2026-07-27T14:00:00+08:00 |
| Related | [#202](https://github.com/Phixsura/attune/issues/202), [external sync framework](./2026-07-08-external-sync-framework.md), [customer requests](./2026-07-07-customer-requests.md) |

## Problem

Product analytics platforms (Amplitude, Mixpanel, PostHog, Pendo) know *who*
your users are — power users, churning accounts, enterprise cohorts — but that
intelligence never reaches the feedback layer. When a PM triages incoming
feedback in Attune, they cannot answer "how many of these requests came from
our power-user cohort?" or "which requests matter most to our enterprise
segment?" without leaving the tool and cross-referencing spreadsheets.

Issue #233 asks Attune to import named cohorts from Amplitude and Mixpanel so
operators can filter feedback and customer requests by cohort membership,
incorporate cohort signals into priority scoring, and monitor sync health in
Console.

## Goals

1. Operators can configure Amplitude and Mixpanel as cohort sources in Console.
2. Named cohorts sync into Attune with incremental membership updates.
3. Feedback and customer requests can be filtered by cohort membership.
4. Stale cohort membership (users who leave a cohort) is handled predictably
   via soft-delete with configurable TTL.
5. Sync health (last sync, error state, member counts) is visible in Console.
6. The design is provider-extensible — adding a third analytics provider (e.g.
   PostHog, Pendo) should require only a new adapter package + webhook handler.

## Non-goals

- Building Attune's own cohort definition engine (behavioral segmentation
  belongs in the analytics tool).
- Pushing Attune data *back* to Amplitude/Mixpanel (reverse sync).
- Real-time sub-minute membership freshness (cohort membership is inherently
  eventual — minutes-to-hours is the industry norm).
- Priority score *computation* based on cohort membership (this proposal
  delivers the data model and filters; score formulas are a follow-up).
- Embedding cohort metadata in the outbound notification envelope (cohort
  membership is a filter dimension, not a feedback attribute).

## World-class benchmark

Eight platforms were studied to understand how best-in-class products handle
cohort sync. The findings drove every major design decision.

### Sync topology: Attune as destination (push), not poller (pull)

| Product | Topology | How |
|---|---|---|
| **Productboard** | Push (Amplitude destination) | Amplitude pushes cohort membership via REST; Productboard exposes a webhook endpoint |
| **Braze** | Push (both providers) | Amplitude list-based sync + Mixpanel cohort export to Braze |
| **LaunchDarkly** | Push (Amplitude destination) | Amplitude syncs to LD segments; hourly auto-refresh recommended |
| **Canny** | Push (Segment identify) | User traits including segment membership pushed via Segment |
| **Pendo** | Push (metadata/identify) | Segment identify pushes visitor metadata; Tray.ai pushes audience membership |

**Every world-class product positions itself as a push destination, not a
poller.** The reasons are structural:

- Amplitude's pull API (Behavioral Cohorts Download) is capped at 500
  requests/month on Growth/Enterprise plans, making scheduled polling
  expensive and fragile.
- Push is inherently incremental (add/remove diffs), eliminating full-snapshot
  diff computation on the Attune side.
- Mixpanel's custom webhook push sends `add_members` / `remove_members` diffs
  every ~15 minutes — real-time enough for cohort membership.

**Design decision:** Attune exposes webhook endpoints as an Amplitude
destination and a Mixpanel custom webhook target. Manual refresh (on-demand
pull) is a fallback for first-time import, error recovery, and consistency
verification.

### Cohort-to-feedback association: runtime JOIN, not materialized tags

| Product | Association |
|---|---|
| **Productboard** | Filter insights board by Segment → runtime join |
| **Braze** | Cohort membership as segment filter in campaign builder |
| **LaunchDarkly** | Cohort → segment, used as flag targeting rule |
| **Canny** | User segmentation filters feedback by attribute-based groups |

**Design decision:** Cohort membership is a first-class filter dimension
joined at query time via `user_feedback.subject_key` ↔
`cohort_memberships.external_user_id`, not a pre-materialized tag on each
feedback row. This avoids double-write consistency issues and keeps cohort
membership the single source of truth.

### Stale membership: soft-delete with TTL (Hightouch mirror pattern)

Hightouch's three sync modes (upsert / mirror / append) inform the stale
handling design. The **mirror** pattern — add new, update existing, remove
departed — maps directly to push-based cohort sync. Departed members get a
`left_at` timestamp; a configurable TTL (default 30 days) controls when
expired memberships are cleaned up.

## Proposal

### Architecture overview

```
                                     ┌──────────────────────────────────────────────┐
Amplitude ──list-based push──▶       │  POST /v1/cohort-sync/amplitude/{tid}/{sid}  │
  (create / add / remove)            │                                              │
                                     │          cohortsync webhook handler           │
Mixpanel  ──webhook push────▶        │  POST /v1/cohort-sync/mixpanel/{tid}/{sid}   │
  (members / add / remove)           │  (auth verify + body read + adapter parse)   │
                                     └─────────────────────┬────────────────────────┘
                                                           │
                                     ┌─────────────────────▼────────────────────┐
                                     │         cohortsync service               │
                                     │  • upsert cohort definition              │
                                     │  • apply membership delta                │
                                     │  • apply full-snapshot reconciliation     │
                                     │  • record sync run                       │
                                     │  • stale TTL enforcement                 │
                                     │  • GDPR erasure cascade                  │
                                     └─────────────────────┬────────────────────┘
                                                           │
                                     ┌─────────────────────▼────────────────────┐
                                     │         cohortsync repo                  │
                                     │  cohort_sources                          │
                                     │  cohorts                                 │
                                     │  cohort_memberships                      │
                                     │  cohort_sync_runs                        │
                                     └─────────────────────┬────────────────────┘
                                                           │
                          ┌────────────────────────────────▼─────────────────────────────────┐
                          │                      Query-time JOIN                              │
                          │  user_feedback.subject_key ↔ cohort_memberships.external_user_id │
                          │  customer_request via feedback_links aggregate                    │
                          └──────────────────────────────────────────────────────────────────┘
```

Console provides:
- Cohort source configuration (credentials, provider selection)
- Cohort list with member counts and sync health
- Manual "Sync Now" button (triggers on-demand pull fallback)
- Cohort filter in feedback list and customer request list

### Package layout

```
internal/
  cohortsync/                          # provider adapter contract + registry
    adapter/amplitude/                 # Amplitude list-based REST receiver
    adapter/mixpanel/                  # Mixpanel webhook receiver
  service/cohortsync/                  # orchestration: membership delta, TTL, runs
  repo/cohortsync/                     # SQL: cohort_sources, cohorts, memberships, runs
  handlers/console/cohortsync/         # Console CRUD API
  handlers/cohortsyncwebhook/          # public webhook endpoints (no console auth)
```

Depguard rules follow the established pattern:
- `cohortsync` (framework root) does not import `service` / `repo` / `handlers`.
- `cohortsync/adapter/<provider>` imports only `cohortsync` (the framework root).
- Webhook handlers are in `handlers/cohortsyncwebhook/`, separate from
  Console handlers (different auth: provider-specific token vs. session).

### Provider adapter contract

```go
// Package cohortsync owns the provider adapter contract for cohort sync.
package cohortsync

// Connection is the decrypted provider-facing connection shape.
type Connection struct {
    ID             string
    TenantID       string
    Provider       string
    Name           string
    AuthType       string
    BaseURL        string
    ProviderConfig []byte
    Credential     []byte
}

// MemberDelta is one user entering or leaving a cohort.
type MemberDelta struct {
    ExternalUserID string
    Email          string
    DisplayName    string
    Properties     map[string]any
    Action         string // "add" or "remove"
}

// SyncPayload is the normalized input from any provider webhook.
type SyncPayload struct {
    Provider        string
    ExternalCohortID string
    CohortName      string
    IsFullSnapshot  bool   // true = replace entire membership (Mixpanel "members" action)
    Deltas          []MemberDelta
}

// Provider is the adapter interface for a cohort analytics provider.
type Provider interface {
    // Provider returns the stable provider token (e.g. "amplitude").
    Provider() string

    // ParseWebhook normalizes a raw HTTP request body into a SyncPayload.
    // The handler reads the body and passes it as bytes (not *http.Request)
    // so the framework root avoids a net/http dependency — matching the
    // externalsync Provider contract which also receives pre-read data.
    // Returns the payload and an error if the request is malformed.
    ParseWebhook(body []byte, headers map[string]string, secret []byte) (SyncPayload, error)

    // PullCohort fetches the current full membership for on-demand refresh.
    // Called when the operator clicks "Sync Now" in Console.
    PullCohort(ctx context.Context, conn Connection, externalCohortID string) (SyncPayload, error)
}
```

The registry follows the `externalsync.Register()` pattern: adapters
self-register via `init()`, `cmd/attune` blank-imports, lookup is by
provider token.

**Design note:** Unlike `externalsync.Provider` (which initiates outbound
HTTP calls via `Pull`/`Push`), `cohortsync.Provider` primarily *receives*
inbound webhooks. `ParseWebhook` takes pre-read `[]byte` body + headers
(not `*http.Request`) so the framework root stays free of `net/http` —
the handler reads the body, the adapter only parses.

### Amplitude adapter (`cohortsync/adapter/amplitude`)

Amplitude's cohort destination protocol is list-based with three operations:

Amplitude's partner integration requires the partner to define three
separate endpoint URLs (one per operation). Attune uses path suffixes to
distinguish them:

| Amplitude operation | Attune endpoint | Behavior |
|---|---|---|
| Create list | `POST /v1/cohort-sync/amplitude/{tid}/{sid}/create` | Upsert `cohorts` row; process initial member batch as adds |
| Add users | `POST /v1/cohort-sync/amplitude/{tid}/{sid}/add` | Apply `add` deltas to `cohort_memberships` |
| Remove users | `POST /v1/cohort-sync/amplitude/{tid}/{sid}/remove` | Apply `remove` deltas (set `left_at`, compute `expires_at`) |

The URL includes `{tenant_id}` and `{source_id}` for explicit routing,
matching the `externalsyncwebhook` pattern (`/github/{tenant_id}/{connection_id}`).
The three path suffixes map 1:1 to Amplitude's partner protocol endpoints.

Authentication: Attune generates an API key per cohort source; the operator
pastes it into Amplitude's destination configuration as the basic-auth
username (empty password). The handler validates via constant-time comparison
against the decrypted credential stored in `cohort_sources`.

Amplitude uses 4 concurrent threads and retries 8 times with exponential
backoff (1s → ~2min). The handler must be idempotent: repeated add/remove
of the same user is a no-op (upsert semantics on the unique constraint).

**PullCohort fallback:** Uses the Behavioral Cohorts Download API (async
three-step: request → poll → download CSV). Rate-limited at 500/month, so
this is an operator-initiated action only, never scheduled.

**Egress SSRF protection:** `PullCohort` makes outbound HTTP calls to the
provider API. The `base_url` field on `cohort_sources` is user-supplied
and therefore an SSRF surface. The cohortsync framework mirrors the
`externalsync/egress.go` pattern: a `cohortsync.NewHTTPClient(timeout)`
wraps `nethardening.Policy.NewHTTPTransport()` with OTel instrumentation.
`ValidateProviderURL(base_url)` is called at source creation/update time,
rejecting private IPs, cloud metadata, and DNS rebinding domains.
`nethardening.BlockedError` is classified as a non-retryable validation
error. This prevents SSRF via malicious `base_url` values targeting
internal infrastructure (e.g., `169.254.169.254`).

### Mixpanel adapter (`cohortsync/adapter/mixpanel`)

Mixpanel's custom webhook protocol:

| Mixpanel action | Behavior |
|---|---|
| `members` | Full snapshot — mark all current members not in the list as `left_at = now`, upsert all listed members |
| `add_members` | Apply `add` deltas |
| `remove_members` | Apply `remove` deltas (set `left_at`, compute `expires_at`) |

Authentication: optional basic auth. Attune generates credentials; the
operator pastes them into the Mixpanel webhook configuration.

**Cohort auto-creation:** Mixpanel's webhook payload includes the cohort
name (configured in Mixpanel's sync setup). On first push, the handler
upserts a `cohorts` row keyed by `(tenant_id, cohort_source_id,
external_cohort_id)`. Operators can rename cohorts in Console after
auto-creation.

Mixpanel sends `email`, `mixpanel_distinct_id`, `first_name`, `last_name`
per member, plus optional custom properties. Large cohorts may arrive in
multiple messages identified by `mixpanel_session_id`. Each chunk is
processed independently via idempotent upsert (`INSERT ON CONFLICT`) —
no cross-request buffering required.

**Full-snapshot reconciliation (`members` action):** Each chunk upserts
its members and sets `last_seen_at = now()`. Reconciliation (marking
absent members as departed) is **not triggered per-chunk**. Instead, the
service uses a time-window approach: a `cohort_sync_runs` row is created
when the first `members` chunk arrives (keyed by `mixpanel_session_id`).
Subsequent chunks for the same session update the same run. After a
configurable quiet period (default 5 minutes with no new chunk for the
same session), the reconciliation runs: members whose
`last_seen_at < run.started_at` are marked departed. This quiet-period
approach is robust because Mixpanel does not document a session-complete
signal. The operator can also trigger reconciliation explicitly via
"Sync Now".

**Concurrent sync guard:** If a `cohort_sync_runs` row with
`status = 'running'` exists for a cohort, a manual "Sync Now" request
returns 409 Conflict. Webhook pushes during a running manual pull are
accepted and processed (they are additive deltas, not competing
snapshots). This prevents membership churn from overlapping full
snapshots without blocking incremental updates.

**Disabled cohort behavior:** When a webhook push arrives for a cohort
where `enabled = false`, the handler returns 200 (to avoid
provider-side retry storms or sync pauses) but skips processing and
logs a warning. The sync run is recorded with `status = 'skipped'`
(added to the CHECK constraint).

Mixpanel pauses syncs on non-transient errors (400/401/403/404). The
handler returns 200 on success, 400 on malformed input, 401 on auth
failure.

**PullCohort fallback:** Uses the Engage Query API filtered by cohort.
No explicit rate cap, but throttled to operator-initiated only.

### Data model

#### Migration `117_cohort_sync.sql`

```sql
-- Cohort source: one per provider connection (Amplitude project, Mixpanel project)
CREATE TABLE IF NOT EXISTS cohort_sources (
  id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id                  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  provider                   TEXT NOT NULL,
  name                       TEXT NOT NULL,
  auth_type                  TEXT NOT NULL DEFAULT 'api_key',
  credential_key_id          TEXT NOT NULL DEFAULT '',
  credential_ciphertext      BYTEA NOT NULL DEFAULT '',
  base_url                   TEXT NOT NULL DEFAULT '',
  provider_config            JSONB NOT NULL DEFAULT '{}'::jsonb,
  webhook_secret_key_id      TEXT NOT NULL DEFAULT '',
  webhook_secret_ciphertext  BYTEA NOT NULL DEFAULT '',
  enabled                    BOOLEAN NOT NULL DEFAULT TRUE,
  status                     TEXT NOT NULL DEFAULT 'active',
  last_sync_at               TIMESTAMPTZ,
  last_error                 TEXT NOT NULL DEFAULT '',
  created_by                 TEXT NOT NULL DEFAULT '',
  updated_by                 TEXT NOT NULL DEFAULT '',
  created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_cohort_sources_provider CHECK (provider IN ('amplitude', 'mixpanel')),
  CONSTRAINT chk_cohort_sources_status CHECK (status IN ('active', 'disabled', 'error')),
  CONSTRAINT chk_cohort_sources_name_nonempty CHECK (name <> ''),
  CONSTRAINT chk_cohort_sources_config_object CHECK (jsonb_typeof(provider_config) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_cohort_sources_tenant
  ON cohort_sources (tenant_id);

-- Cohort definition: one per synced cohort
CREATE TABLE IF NOT EXISTS cohorts (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  cohort_source_id    UUID NOT NULL REFERENCES cohort_sources(id) ON DELETE CASCADE,
  external_cohort_id  TEXT NOT NULL,
  name                TEXT NOT NULL,
  description         TEXT NOT NULL DEFAULT '',
  stale_ttl_days      INT NOT NULL DEFAULT 30,
  member_count        INT NOT NULL DEFAULT 0,
  enabled             BOOLEAN NOT NULL DEFAULT TRUE,
  last_synced_at      TIMESTAMPTZ,
  last_error          TEXT NOT NULL DEFAULT '',
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_cohorts_name_nonempty CHECK (name <> ''),
  CONSTRAINT chk_cohorts_external_id_nonempty CHECK (external_cohort_id <> ''),
  CONSTRAINT chk_cohorts_stale_ttl CHECK (stale_ttl_days BETWEEN 1 AND 365),
  CONSTRAINT uq_cohorts_source_external UNIQUE (tenant_id, cohort_source_id, external_cohort_id)
);

CREATE INDEX IF NOT EXISTS idx_cohorts_tenant
  ON cohorts (tenant_id);

-- Cohort membership: one row per user per cohort
CREATE TABLE IF NOT EXISTS cohort_memberships (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id          TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  cohort_id          UUID NOT NULL REFERENCES cohorts(id) ON DELETE CASCADE,
  external_user_id   TEXT NOT NULL,
  email              TEXT NOT NULL DEFAULT '',
  display_name       TEXT NOT NULL DEFAULT '',
  user_properties    JSONB NOT NULL DEFAULT '{}'::jsonb,
  joined_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  left_at            TIMESTAMPTZ,
  expires_at         TIMESTAMPTZ,
  last_seen_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_cohort_memberships_ext_id_nonempty CHECK (external_user_id <> ''),
  CONSTRAINT chk_cohort_memberships_properties_object CHECK (jsonb_typeof(user_properties) = 'object'),
  CONSTRAINT uq_cohort_memberships_user UNIQUE (tenant_id, cohort_id, external_user_id)
);

-- Active members for a cohort (filter queries)
CREATE INDEX IF NOT EXISTS idx_cohort_memberships_active
  ON cohort_memberships (tenant_id, cohort_id)
  WHERE left_at IS NULL;

-- All cohorts a user belongs to (feedback JOIN path)
CREATE INDEX IF NOT EXISTS idx_cohort_memberships_by_user
  ON cohort_memberships (tenant_id, external_user_id)
  WHERE left_at IS NULL;

-- Expired membership cleanup
CREATE INDEX IF NOT EXISTS idx_cohort_memberships_expired
  ON cohort_memberships (expires_at)
  WHERE expires_at IS NOT NULL AND left_at IS NOT NULL;

-- Sync run log
CREATE TABLE IF NOT EXISTS cohort_sync_runs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  cohort_id       UUID NOT NULL REFERENCES cohorts(id) ON DELETE CASCADE,
  trigger         TEXT NOT NULL DEFAULT 'webhook',
  status          TEXT NOT NULL DEFAULT 'running',
  members_added   INT NOT NULL DEFAULT 0,
  members_removed INT NOT NULL DEFAULT 0,
  members_total   INT NOT NULL DEFAULT 0,
  error_message   TEXT NOT NULL DEFAULT '',
  started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at     TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_cohort_sync_runs_trigger CHECK (trigger IN ('webhook', 'manual', 'system')),
  CONSTRAINT chk_cohort_sync_runs_status CHECK (status IN ('running', 'succeeded', 'failed', 'skipped'))
);

CREATE INDEX IF NOT EXISTS idx_cohort_sync_runs_cohort
  ON cohort_sync_runs (tenant_id, cohort_id, created_at DESC);
```

#### User identity matching

The feedback→cohort JOIN key requires mapping between:
- `user_feedback.subject_key` = the normalized raw upstream user identifier
  (populated by `subjectkey.Normalize(sourceUser, legacyUserID)` at ingest;
  migration 038 backfilled existing rows)
- `cohort_memberships.external_user_id` = Amplitude user_id or Mixpanel
  distinct_id

The `subject_key` column is exactly the `source_user` value (or its
legacy equivalent extracted from the composed `user_id`). An existing
partial index `idx_user_feedback_tenant_subject_key` already covers the
JOIN path — **no new functional index is needed**.

```sql
SELECT f.*
FROM user_feedback f
JOIN cohort_memberships cm
  ON cm.tenant_id = f.tenant_id
  AND cm.external_user_id = f.subject_key
  AND cm.cohort_id = $cohort_id
  AND cm.left_at IS NULL
WHERE f.tenant_id = $tenant_id
  AND f.subject_key <> ''
```

In the existing `ConsoleListOpts` query builder, this becomes an
`EXISTS` subquery (matching the `TagID` filter pattern):

```go
if opts.CohortID != nil {
    qb.and("EXISTS (SELECT 1 FROM cohort_memberships cm" +
        " WHERE cm.tenant_id = $1" +
        " AND cm.cohort_id = " + qb.addArg(*opts.CohortID) + "::uuid" +
        " AND cm.external_user_id = subject_key" +
        " AND cm.left_at IS NULL)")
}
```

**Email fallback:** If `subject_key` is an email address and the cohort
membership has an `email` field, they match naturally (both are the same
string). For cases where the operator uses different ID schemes across
systems, Console's cohort source setup wizard documents the mapping
requirement — matching how Productboard instructs operators to ensure
their Amplitude user_id maps to the Productboard user identifier.

#### Customer request filtering

Customer requests link to feedback via `customer_request_feedback_links`.
Filtering requests by cohort is a two-hop JOIN:

```sql
SELECT DISTINCT cr.*
FROM customer_requests cr
WHERE cr.tenant_id = $tenant_id
  AND EXISTS (
    SELECT 1
    FROM customer_request_feedback_links fl
    JOIN user_feedback f ON f.tenant_id = fl.tenant_id AND f.id = fl.feedback_id
    JOIN cohort_memberships cm
      ON cm.tenant_id = f.tenant_id
      AND cm.external_user_id = f.subject_key
      AND cm.cohort_id = $cohort_id
      AND cm.left_at IS NULL
    WHERE fl.tenant_id = cr.tenant_id
      AND fl.request_id = cr.id
      AND f.subject_key <> ''
  )
```

### Stale membership lifecycle

```
User in cohort        → joined_at = now, left_at = NULL, expires_at = NULL
User leaves cohort    → left_at = now, expires_at = now + stale_ttl_days
User re-joins cohort  → left_at = NULL, expires_at = NULL, last_seen_at = now
TTL expires           → background cleanup deletes the row
```

The cleanup worker runs as a periodic task (daily), deleting rows where
`expires_at < now()`. It is safe to delete because:
- Active queries filter `WHERE left_at IS NULL`, so expired rows are invisible.
- Historical "was once a member" queries can check `left_at IS NOT NULL`
  within the TTL window.

**`member_count` maintenance:** The `cohorts.member_count` column is a
cached display metric, not a transactional invariant. It is updated once
at the end of each sync run via
`SELECT count(*) FROM cohort_memberships WHERE cohort_id = $1 AND left_at IS NULL`.
This avoids consistency issues from concurrent add/remove operations and
keeps the update cost bounded to one COUNT per run rather than per delta.

### Console UI

The cohort sync Console pages live under `/console/cohort-sync/`:

1. **Sources list** — configured Amplitude/Mixpanel connections with status
   badges (active/disabled/error).
2. **Source setup wizard** — provider selection, credential entry, user ID
   mapping instructions, test connection.
3. **Cohorts list** — per-source cohorts with member count, last sync time,
   enabled toggle. Cohorts auto-populate as the provider pushes them.
4. **Cohort detail** — member count, sync run history, error log, "Sync Now"
   button for manual pull fallback.
5. **Feedback list filter** — cohort dropdown in the existing filter bar.
   Selecting a cohort adds the membership JOIN to the query.
6. **Customer request list filter** — same cohort dropdown, two-hop JOIN.

### Proto contract

New proto file `proto/attune/v1/cohort_sync.proto`:

```protobuf
// Console-facing cohort sync management API.
service CohortSyncService {
  // Sources
  rpc ListCohortSources(ListCohortSourcesRequest) returns (ListCohortSourcesResponse);
  rpc CreateCohortSource(CreateCohortSourceRequest) returns (CohortSource);
  rpc UpdateCohortSource(UpdateCohortSourceRequest) returns (CohortSource);
  rpc DeleteCohortSource(DeleteCohortSourceRequest) returns (DeleteCohortSourceResponse);
  rpc TestCohortSource(TestCohortSourceRequest) returns (TestCohortSourceResponse);

  // Cohorts
  rpc ListCohorts(ListCohortsRequest) returns (ListCohortsResponse);
  rpc UpdateCohort(UpdateCohortRequest) returns (Cohort);
  rpc SyncCohort(SyncCohortRequest) returns (SyncCohortResponse);

  // Sync runs
  rpc ListCohortSyncRuns(ListCohortSyncRunsRequest) returns (ListCohortSyncRunsResponse);

  // Health
  rpc GetCohortSyncHealth(GetCohortSyncHealthRequest) returns (CohortSyncHealth);
}
```

Webhook endpoints are not proto-managed — they implement provider-specific
wire formats (Amplitude list-based JSON, Mixpanel webhook JSON) and live
in `handlers/cohortsyncwebhook/` with direct chi route mounting.

`TestCohortSource` verifies the PullCohort fallback path: it decrypts
credentials, calls the provider's list-cohorts API (Amplitude Behavioral
Cohorts list / Mixpanel Engage cohort list), and returns OK + cohort
count if reachable. This confirms the credentials work before the
operator configures the push destination in Amplitude/Mixpanel.

### Audit actions

New audit actions for the `audit_log` table:

- `cohort_source.create` / `cohort_source.update` / `cohort_source.delete`
- `cohort.update` (enable/disable, TTL change)
- `cohort.sync` (manual sync triggered)

These must be added to both the Go `validActions` set and the DB
`chk_audit_action_value` CHECK constraint (migration).

### Metrics

New Prometheus metrics in `internal/infra/metrics`:

| Metric | Type | Labels |
|---|---|---|
| `attune_cohort_sync_webhook_requests_total` | Counter | `provider`, `status` |
| `attune_cohort_sync_members_changed_total` | Counter | `provider`, `action` (add/remove) |
| `attune_cohort_sync_runs_total` | Counter | `provider`, `trigger`, `status` |
| `attune_cohort_sync_active_members` | Gauge | `provider` |
| `attune_cohort_sync_stale_members_cleaned_total` | Counter | |

Each metric must be registered in the metrics catalog, added to the
Grafana dashboard, and documented in the metrics README.

### GDPR compliance

`cohort_memberships` stores PII (`email`, `display_name`,
`user_properties`). The GDPR erasure service (`internal/service/gdpr/`)
must be extended to delete `cohort_memberships` rows where
`external_user_id` matches the erasure subject's `subject_key`. This
aligns with the existing pattern: GDPR erasure already cascades through
`user_feedback` via `subject_key`; cohort memberships are an additional
table in the same cascade.

### Webhook body size limit

Amplitude supports cohorts up to 2M users; Mixpanel up to 10M. A full
`members` snapshot for a large cohort can be tens of megabytes. The
existing `externalsyncwebhook` handler uses `maxWebhookBodyBytes = 1MB`.

Cohort sync webhooks use a higher limit: **32MB** (`maxCohortWebhookBodyBytes
= 32 << 20`). This accommodates the largest cohort snapshots while still
preventing unbounded allocation. Payloads exceeding this limit return
413 Request Entity Too Large.

### Depguard rules

Two new depguard rules in `.golangci.yml`:

```yaml
cohortsync-boundary:
  list-mode: lax
  files:
    - "**/internal/service/**"
    - "**/internal/handlers/**"
    - "**/internal/repo/**"
  deny:
    - pkg: "github.com/Phixsura/attune/internal/cohortsync/adapter/*"
      desc: "cohortsync adapters self-register; only cmd/attune may blank-import"

cohortsync-framework-isolation:
  list-mode: lax
  files:
    - "**/internal/cohortsync/**"
  deny:
    - pkg: "github.com/Phixsura/attune/internal/service/**"
      desc: "cohortsync framework must not import service layer"
    - pkg: "github.com/Phixsura/attune/internal/repo/**"
      desc: "cohortsync framework must not import repo layer"
    - pkg: "github.com/Phixsura/attune/internal/handlers/**"
      desc: "cohortsync framework must not import handlers layer"
```

### Provider CHECK constraint evolution

The `chk_cohort_sources_provider` CHECK constraint is intentionally
restrictive for v1 (`amplitude`, `mixpanel`). Adding a third provider
(e.g. PostHog) requires a migration to extend the CHECK — this is a
deliberate correctness gate for the initial release, not an oversight.

Once three or more providers exist, the CHECK should migrate to
registry-only validation (matching `externalsync`'s approach where
provider tokens are validated by `externalsync.ValidateProviderToken()`
without a DB constraint). This avoids DDL-level friction on every new
provider.

## Alternatives considered

### A. Reuse the externalsync framework directly

Register Amplitude/Mixpanel as externalsync Providers. Cohort members would
be PullRecords mapped via externalsync Mappings.

**Rejected:** The externalsync framework is designed for 1:1 record mapping
(one feedback/request ↔ one external issue) with field mapping, conflict
resolution, and bidirectional push/pull. Cohort sync is a fundamentally
different shape — it is set-based membership, not record-based mapping.
Forcing cohorts through externalsync would leave Mapping, FieldMapping,
StatusMapping, ConflictPolicy, and TombstonePolicy as meaningless N/A
fields, adding complexity without value.

### B. Attune polls (pull-only) both providers on a cron schedule

Run a scheduled worker that calls Amplitude's Behavioral Cohorts Download
API and Mixpanel's Engage API periodically.

**Rejected:** Amplitude's Download API is capped at 500 requests/month on
Growth/Enterprise plans. With hourly polling of 10 cohorts, you exhaust
the quota in ~3 weeks. Every world-class product (Productboard, Braze,
LaunchDarkly) positions itself as a push destination instead. Push is also
inherently incremental, eliminating full-snapshot diff computation.

### C. Build a generic "audience" abstraction above externalsync

Create an `audience` domain concept that wraps both externalsync and cohort
sync behind a unified filter primitive.

**Rejected for now:** Over-abstraction at this stage. Cohort sync and
record sync have different lifecycles, different data shapes, and different
query patterns. A unifying abstraction may emerge later, but it should be
derived from two concrete implementations, not designed speculatively.

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| **User ID mismatch** between analytics tool and Attune `source_user` | Console setup wizard explicitly documents the mapping requirement; test-connection verifies at least one matching user |
| **Large cohorts** (Amplitude supports up to 2M users) could strain the membership table | Partial index on active members limits query cost; bulk upsert uses batch INSERT ON CONFLICT; stale TTL prevents unbounded growth |
| **Amplitude destination setup requires partner listing** (Integration Portal) | Attune implements the partner REST API spec but can be used without portal listing via Amplitude's custom webhook destination or REST API |
| **Mixpanel webhook reliability** — Mixpanel pauses on 4xx | Handler returns 200 eagerly after payload validation; membership application is async-safe via idempotent upserts |
| **SSRF via `base_url`** — PullCohort makes outbound HTTP to a user-supplied URL | `nethardening.Policy` applied at connect time (post-DNS); `ValidateProviderURL` at config time; `BlockedError` is non-retryable |
| **Provider CHECK constraint** on `cohort_sources.provider` | The CHECK is intentionally restrictive (`amplitude`, `mixpanel`) for v1; adding a provider requires a migration to extend the CHECK — this is a deliberate gate, not an oversight |

## Implementation plan

### Phase 1: Data model + service layer

1. Migration `117_cohort_sync.sql` with all four tables + indexes.
2. Audit action migration extending `chk_audit_action_value`.
3. `internal/cohortsync/` — provider contract, registry, `MemberDelta` /
   `SyncPayload` types.
4. `internal/repo/cohortsync/` — CRUD for sources, cohorts, memberships,
   runs. Bulk upsert membership with `INSERT ON CONFLICT`.
5. `internal/service/cohortsync/` — `ApplyDelta`, `ApplyFullSnapshot`,
   `RecordRun`, `CleanExpired`, credential encrypt/decrypt via shared
   `secretstore.TinkStore`.
6. Unit tests for service + repo with fixture data.

### Phase 2: Provider adapters + webhook handlers

7. `internal/cohortsync/adapter/amplitude/` — `ParseWebhook` (list-based
   create/add/remove), `PullCohort` (Download API async three-step).
8. `internal/cohortsync/adapter/mixpanel/` — `ParseWebhook` (members /
   add_members / remove_members, stateless chunk processing),
   `PullCohort` (Engage API).
9. `internal/handlers/cohortsyncwebhook/` — HTTP handlers for
   `/v1/cohort-sync/amplitude` and `/v1/cohort-sync/mixpanel`. Auth
   middleware validates provider-specific credentials.
10. Fixture tests with recorded API response payloads.

### Phase 3: Console API + feedback/request filter integration

11. Proto `cohort_sync.proto` + `make proto`.
12. `internal/handlers/console/cohortsync/` — Console CRUD handlers.
13. Extend feedback list query with optional cohort filter JOIN
    (via existing `subject_key` column and index).
14. Extend customer request list query with cohort filter JOIN.
15. Extend GDPR erasure to cascade into `cohort_memberships`.
16. Metrics registration + Grafana dashboard panel.

### Phase 4: Console frontend

17. `console/src/features/cohort-sync/` — source list, setup wizard,
    cohort list, cohort detail, sync history.
18. Cohort filter dropdown in feedback list and customer request list.
19. Health badge in control tower dashboard.

### Phase 5: Stale cleanup + observability + depguard

20. Background cleanup worker for expired memberships.
21. Add `cohortsync-boundary` and `cohortsync-framework-isolation`
    depguard rules to `.golangci.yml`.
22. Alerting rules for sync failures and stale sources.
23. Documentation in `docs/private-deploy.md` for Amplitude/Mixpanel setup.

## Verification

- [ ] Unit tests for all repo CRUD operations with fixture data.
- [ ] Unit tests for service layer: delta application, full snapshot
      replacement, TTL computation, cleanup.
- [ ] Fixture tests for Amplitude adapter: parse list-based create/add/remove
      payloads from recorded responses.
- [ ] Fixture tests for Mixpanel adapter: parse members/add_members/
      remove_members payloads from recorded responses.
- [ ] Integration test: webhook handler end-to-end with real HTTP requests
      and database verification.
- [ ] Integration test: feedback list filtered by cohort returns correct
      results after membership sync.
- [ ] Integration test: customer request list filtered by cohort returns
      correct results via two-hop JOIN.
- [ ] Stale cleanup: membership with expired TTL is removed; re-joined
      members are not affected.
- [ ] GDPR erasure: erasing a subject_key cascades to cohort_memberships
      rows matching the external_user_id.
- [ ] Webhook body size: payloads exceeding 32MB are rejected with 413.
- [ ] Depguard: `cohortsync-boundary` and `cohortsync-framework-isolation`
      rules pass with zero violations.
- [ ] Console smoke test: source creation, cohort list population, manual
      sync, filter application.
- [ ] `make ci-check` passes with all quality gates green.

## References

- [Amplitude Behavioral Cohorts API](https://amplitude.com/docs/apis/analytics/behavioral-cohorts)
- [Amplitude Cohort Destination Partner Guide](https://amplitude.com/docs/partners/create-a-cohort-sync-integration)
- [Amplitude Receiving Behavioral Cohorts](https://amplitude.com/docs/partners/receiving-behavioral-cohorts)
- [Mixpanel Cohort Sync Custom Webhooks](https://docs.mixpanel.com/docs/cohort-sync/webhooks)
- [Productboard Amplitude Integration](https://support.productboard.com/hc/en-us/articles/4415882801299)
- [Productboard Mixpanel Integration](https://support.productboard.com/hc/en-us/articles/4424743639443)
- [Braze Cohort Sync from Amplitude](https://www.braze.com/docs/partners/data_and_analytics/customer_data_platform/amplitude/amplitude_cohort_import)
- [Braze Cohort Sync from Mixpanel](https://www.braze.com/docs/partners/data_and_analytics/analytics/mixpanel/mixpanel_cohort_import)
- [LaunchDarkly Amplitude Cohort Sync](https://launchdarkly.com/docs/home/flags/amplitude)
- [Hightouch Reverse ETL Sync Modes](https://hightouch.com/platform/reverse-etl)
- [Segment Engage Audiences](https://segment.com/docs/engage/audiences/)
- [Canny User Segmentation](https://help.canny.io/en/articles/3176557-user-segmentation)
- [Pendo Segments](https://support.pendo.io/hc/en-us/articles/360031862532-Segments)
