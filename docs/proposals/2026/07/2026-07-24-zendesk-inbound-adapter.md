# Zendesk inbound adapter — extract product signals from support tickets

| | |
|---|---|
| **Issue** | [#229](https://github.com/Phixsura/attune/issues/229) |
| **Status** | Implemented |
| **Started** | 2026-07-24T15:30:00+08:00 |
| **Related** | [#66](../06/2026-06-08-channel-agnostic-inbound.md) (inbound framework), [#202](https://github.com/Phixsura/attune/issues/202) (industry gap closure), [#12](https://github.com/Phixsura/attune/issues/12) (support ticket auto-extraction), [#31](https://github.com/Phixsura/attune/issues/31) (Zendesk bidirectional — future) |

---

## Problem

Zendesk is among the highest-volume sources of raw product signal in B2B SaaS.
Support tickets contain feature requests, bug reports, and pain-point
descriptions that never reach the product backlog because no one manually copies
them. Industry VoC platforms (Productboard, Canny, Medallia) treat Zendesk
extraction as a table-stakes integration.

attune's inbound framework (#66) already handles three channels — webhook (push),
email (poll), Slack (poll) — behind the same `Adapter` port. Adding Zendesk is
a **poll-mode adapter** that feeds into the existing ingest → enrich → notify
pipeline unchanged.

The challenge is Zendesk-specific:
- **Auth sunset.** API tokens stop working 2027-04-30. OAuth 2.0 client
  credentials is the only future-proof server-to-server path.
- **Rate limiting.** Incremental exports are capped at 10 req/min (plan-dependent
  general limit 200–2500 req/min). Comment fetches consume the general budget.
- **Content depth.** A ticket's product signal lives in the full conversation
  thread (description + public comments), not just the initial message.
- **Cursor semantics.** Zendesk's incremental cursor is an opaque token, not a
  monotonic integer — but attune's `LastUID` is `int64`. We need a bridging
  strategy.

---

## Goals

1. Zendesk tickets and their public comments generate feedback items with full
   support metadata (requester, organization, ticket ID, tags, status, priority,
   satisfaction rating, URL).
2. Incremental sync via Zendesk's cursor-based incremental export API with
   replay-safe deduplication.
3. Both API token and OAuth 2.0 client credentials auth supported from day one.
4. Console create/test-connection flow matches the existing Slack pattern.
5. Sync errors are visible in Console with the existing health model
   (healthy/error/paused).
6. No Zendesk credentials or private URLs leak into logs.

## Non-goals

- **Bidirectional sync** (pushing customer requests back to Zendesk tickets) —
  that's #31, tracked separately under the `externalsync` framework.
- **Webhook-triggered sync** (Zendesk triggers/automations pushing events to
  attune) — polling is more reliable for bulk extraction; webhook supplementation
  is a future enhancement.
- **Custom field extraction** — MVP maps standard ticket fields; custom field
  mapping is a configuration-heavy follow-up.
- **Attachment handling** — ticket attachments are noted in metadata but not
  downloaded or stored.

---

## Proposal

### Architecture

```
                    ┌─────────────────────────────────────┐
                    │  internal/inbound/adapter/zendesk/  │
                    │                                     │
                    │  zendesk.go    — Adapter lifecycle   │
                    │  poll.go       — poll loop + sync    │
                    │  config.go     — Config + parsing    │
                    │  client.go     — Zendesk API client  │
                    │  normalize.go  — ticket → IngestInput│
                    │  public.go     — Console exports     │
                    │  ops.go        — error classification │
                    └──────────────┬──────────────────────┘
                                   │
                    deps.Ingest.Ingest(ctx, tenantID, uuid.Nil, in)
                                   │
                    ┌──────────────▼──────────────────────┐
                    │  Existing pipeline (unchanged)      │
                    │  ingest → enrich → outbox → notify  │
                    └─────────────────────────────────────┘
```

### 1. Adapter registration and lifecycle

Follow the Slack adapter pattern exactly:

```go
// internal/inbound/adapter/zendesk/zendesk.go
func init() { inbound.Register(Channel, "Zendesk", NewAdapter) }

const Channel = "zendesk"
```

Poll mode: `Start()` spawns `pollLoop` goroutine with `context.WithCancel`.
`Shutdown()` cancels + waits on WaitGroup. `ShutdownTimeout()` returns 10s.

### 2. API client

Interface-driven, test-injectable:

```go
type apiClient interface {
    // AuthTest validates credentials and returns account info.
    AuthTest(ctx context.Context) (accountInfo, error)
    // IncrementalTickets fetches tickets via cursor-based incremental export.
    IncrementalTickets(ctx context.Context, cursor string, startTime int64) (ticketPage, error)
    // TicketComments fetches all public comments for a ticket.
    TicketComments(ctx context.Context, ticketID int64) ([]comment, error)
}
```

**Auth:** The client supports two auth modes, selected by config:
- **API token**: `Authorization: Basic base64({email}/token:{api_token})`
- **OAuth 2.0**: `Authorization: Bearer {access_token}` with token refresh on 401

**HTTP transport:** `otelhttp.NewTransport(http.DefaultTransport)` — matches
existing adapters. Body reads capped at 4 MiB. Response time tracked.

**Rate limiting:** Read `Retry-After` header on 429. Proactive throttling via
`X-RateLimit-Remaining`. Exponential backoff with jitter (base 60s, max 480s).
Record `transient_err` metric, do not disable source on rate-limit.

**Base URL:** `https://{subdomain}.zendesk.com` — subdomain from config.
Test seam: `var newAPIClient clientFactory` and `SetAPIBaseURL(url)` atomic for
test fixtures.

### 3. Incremental sync via cursor

**First sync:** `GET /api/v2/incremental/tickets/cursor.json?start_time={t}`
where `t` is `now() - syncLookback` (configurable, default 0 = full backfill).

**Subsequent syncs:** `GET /api/v2/incremental/tickets/cursor.json?cursor={c}`
using the persisted cursor.

**Cursor storage:** Zendesk's `after_cursor` is an opaque string (base64-encoded,
~40 chars). attune's `LastUID` is `int64`. Solution: **store the cursor string
in the Config blob** (encrypted at rest), and use `LastUID` for the
`generated_timestamp` of the last processed ticket (for poll-lag metrics and
human-readable progress). This matches the Slack pattern where `ThreadCache` is
stored in Config.

```go
type Config struct {
    Version              int    `json:"version"`
    AuthMode             string `json:"auth_mode"` // "api_token" | "oauth"
    Subdomain            string `json:"subdomain"`
    Email                string `json:"email,omitempty"`           // for api_token auth
    APITokenEncrypted    []byte `json:"api_token_encrypted,omitempty"`
    OAuthTokenEncrypted  []byte `json:"oauth_token_encrypted,omitempty"` // JSON: {access_token, refresh_token, ...}
    SyncCursor           string `json:"sync_cursor,omitempty"`
    StartFrom            string `json:"start_from"`               // "now" | "full"
    // Computed at runtime, not stored:
    // subdomain → baseURL
}
```

**Page processing:** For each ticket in the response:
1. Skip if `status == "deleted"`.
2. Fetch public comments via `GET /api/v2/tickets/{id}/comments.json` (paginated,
   filter `public == true`). Rate-limit aware — if budget exhausted, stop page
   processing and resume next tick.
3. Build `IngestInput` per ticket (not per comment — the ticket is the unit of
   product signal; comments are concatenated into Content).
4. Ingest with idempotency key `zendesk_{subdomain}_{ticketID}`.

**End-of-stream:** When `end_of_stream == true`, persist cursor + advance
`LastUID` to the max `generated_timestamp`. Next poll tick starts from the
persisted cursor.

**Comment budget:** To avoid exhausting rate limits on first sync with large
backlogs, cap comment fetches at `maxCommentFetches` per poll tick (default 50).
Tickets whose comments were not fetched are flagged and retried next tick.

### 4. Ticket normalization

Each ticket becomes one `domain.IngestInput`:

```go
IngestInput{
    Source:         "zendesk",
    Content:        buildContent(ticket, comments), // subject + "\n\n" + description + comment bodies
    SourceUser:     requesterEmail,                  // or requesterName if email unavailable
    PageURL:        ticket.URL,                      // https://{subdomain}.zendesk.com/agent/tickets/{id}
    SourceMeta:     buildZendeskSourceMeta(...),
    IdempotencyKey: fmt.Sprintf("zendesk_%s_%d", subdomain, ticket.ID),
}
```

**Content assembly:** `buildContent` concatenates the ticket subject, description,
and public comment bodies (chronological), separated by `\n\n---\n\n`. Total
content is truncated at 4500 chars (leaving headroom under the 5000-char
IngestInput limit) with a `[truncated]` suffix.

**SourceMeta keys** (all `zendesk_` prefixed):

| Key | Value |
|---|---|
| `inbound_source_id` | source UUID (well-known) |
| `inbound_source_name` | source name (well-known) |
| `zendesk_subdomain` | Zendesk subdomain |
| `zendesk_ticket_id` | ticket integer ID |
| `zendesk_ticket_url` | full agent URL |
| `zendesk_status` | new/open/pending/hold/solved/closed |
| `zendesk_priority` | urgent/high/normal/low or empty |
| `zendesk_type` | problem/incident/question/task |
| `zendesk_tags` | JSON array of tag strings |
| `zendesk_requester_id` | Zendesk user ID (integer) |
| `zendesk_requester_name` | resolved requester name |
| `zendesk_requester_email` | requester email |
| `zendesk_organization_id` | Zendesk org ID (integer) |
| `zendesk_organization_name` | resolved org name |
| `zendesk_satisfaction_score` | good/bad/offered/unoffered |
| `zendesk_via_channel` | web/email/api/voice/chat/... |
| `zendesk_comment_count` | number of public comments ingested |
| `zendesk_created_at` | ticket creation ISO8601 |
| `zendesk_updated_at` | ticket last update ISO8601 |

### 5. User/organization resolution

Build an in-memory cache per poll cycle. The incremental export returns
`requester_id` and `organization_id` as integers. Resolution strategy:

1. Collect unique user/org IDs from the current page.
2. Batch-resolve via `GET /api/v2/users/show_many.json?ids=1,2,3` (up to 100
   per request) and `GET /api/v2/organizations/show_many.json?ids=1,2,3`.
3. Cache resolved names for the duration of the poll tick.
4. On resolution failure (rate limit, not found), fall back to
   `"user:{id}"` / `"org:{id}"` — never block ticket ingestion on metadata
   enrichment.

### 6. Error handling

Follow the Slack adapter's three-tier model:

| Error class | Action | Metric |
|---|---|---|
| **Permanent auth** (401 Unauthorized, invalid OAuth token, revoked API token) | `SetEnabled(false, reason)` | `auth_err` |
| **Transient** (429 rate limit, 5xx, network timeout) | Record `LastError`, retry next tick | `transient_err` |
| **Ingest failure** (validation, content too long) | Log + skip ticket | `validate_err` |
| **Internal** (config parse error, unexpected API shape) | Record `LastError` | `internal_err` |

**Permanent error detection:** Zendesk returns structured error bodies. HTTP 401
with `error: "Couldn't authenticate you"` is always permanent. HTTP 403 may be
permanent (invalid scope) or transient (rate-limited trial plan).

### 7. Console integration

**Proto changes** (`proto/attune/v1/inbound_source.proto`):

```protobuf
message ZendeskConnConfig {
  string subdomain = 1;     // "mycompany" → mycompany.zendesk.com
  string auth_mode = 2;     // "api_token" | "oauth"
  // api_token fields
  optional string email = 3;
  optional string api_token = 4;
  // oauth fields
  optional string oauth_client_id = 5;
  optional string oauth_client_secret = 6;
}

// Add to CreateInboundSourceRequest:
optional ZendeskConnConfig zendesk_config = 6;
// Add to TestInboundConnectionRequest:
optional ZendeskConnConfig zendesk_config = 4;
```

**Backend handlers:**
- `inbound_create_zendesk.go` — validate config, encrypt credentials, insert
- `inbound_test_connection.go` — add `"zendesk"` case calling `zendesk.AuthTest`
- `inbound_handler.go` — add `channelZendesk` constant, test seam function

**Frontend:**
- `ZendeskFieldset` component in `create-dialog.tsx` — subdomain, auth mode
  selector (API token / OAuth), conditional fields, test connection button
- Update `Channel` type union, `buildBody`, `handleTest`
- i18n keys under `inbound_sources.create.zendesk.*`
- Channel icon: `Headphones` from lucide-react

### 8. Metrics

Three new metric labels (reusing existing metric names):

| Metric | New labels |
|---|---|
| `attune_inbound_total` | `channel="zendesk"` |
| `attune_inbound_latency_seconds` | `channel="zendesk"` |
| `attune_inbound_source_state` | `channel="zendesk"` |
| `attune_inbound_poll_lag_seconds` | `channel="zendesk"` |

No new metric names — the inbound metrics framework is channel-agnostic.

### 9. Source vocabulary

- Register `"zendesk"` with display `"Zendesk"` via `inbound.Register()`.
- Add to `DefaultSourceSet` in `internal/domain/feedback.go`.
- Append-only rule (#95) applies: once `"zendesk"` is written to
  `user_feedback.source`, it cannot be renamed or repurposed.

### 10. Security

- API token and OAuth credentials are double-encrypted (inner credential +
  outer config envelope via Tink AEAD).
- Credentials wiped from memory after use (`wipeBytes`).
- Subdomain validated against `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`.
- SSRF: Only HTTPS connections to `*.zendesk.com` (plus test seam override).
  Base URL validation rejects non-zendesk.com hosts in production.
- No raw credentials, ticket content, or private URLs in log output. Subdomain
  and ticket IDs are logged; content is not.

---

## Alternatives considered

### A. Webhook-push instead of poll

Zendesk supports trigger-fired webhooks. This would match the existing webhook
adapter and provide near-real-time ingestion. **Rejected because:**
- Webhooks require per-tenant Zendesk admin configuration (triggers + target).
- Webhook payloads are customizable but limited — no guaranteed comment history.
- Ordering is not guaranteed; replay after downtime requires manual re-triggers.
- The incremental export API is purpose-built for bulk extraction with native
  cursor resumption.

Poll with incremental export is more reliable, operationally simpler, and
standard across VoC tooling.

### B. External sync framework instead of inbound adapter

The `externalsync` package already has GitHub and Jira providers with
Check/Discover/Pull/Push. **Deferred because:**
- External sync is designed for bidirectional object-level sync (customer
  requests ↔ external issues), not for high-volume unidirectional feedback
  extraction.
- The inbound framework provides exactly the right abstractions: poll loop,
  source store, cursor state, health metrics, Console CRUD.
- Bidirectional Zendesk sync (#31) belongs in `externalsync` and can share the
  API client package.

### C. Store cursor in `LastUID` via hash

Encode the opaque cursor string as a hash/checksum in the int64 `LastUID`.
**Rejected because:** hash collisions would cause data loss (skipped tickets),
and the cursor value needs to be recoverable for the next API call.

### D. OAuth-only auth

Skip API token support since it's sunsetting. **Rejected because:** API tokens
work until 2027-04-30, many Zendesk instances already have tokens configured, and
requiring OAuth setup raises the barrier to first use. Support both; deprecation
warning in Console UI.

---

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| Rate limiting on comment fetches for large backlogs | `maxCommentFetches` per tick; backpressure flag; progress visible in Console |
| API token sunset (2027-04-30) | OAuth support from day one; Console deprecation banner for API token auth |
| Zendesk API changes (v2 → v3) | Client abstracted behind interface; version in config for future migration |
| Large ticket content exceeding 5000-char limit | Truncation with `[truncated]` marker; preserves subject + description + early comments |
| OAuth token refresh failures | Retry with exponential backoff; disable source after N consecutive refresh failures |
| Cursor stored in encrypted Config blob | Cursor persisted on every successful page; worst-case re-processes one page on crash |

---

## Implementation plan

### Phase 1: Adapter core (backend)

1. **`internal/inbound/adapter/zendesk/`** — 7 files:
   - `zendesk.go` — registration, adapter struct, lifecycle
   - `poll.go` — poll loop, sync orchestration, cursor management
   - `config.go` — Config struct, double-encryption parsing, validation
   - `client.go` — API client interface + HTTP implementation
   - `normalize.go` — ticket + comments → IngestInput
   - `public.go` — exported types and Console-facing helpers
   - `ops.go` — error classification (permanent vs transient)

2. **`cmd/attune/main.go`** — blank-import
   `_ "github.com/Phixsura/attune/internal/inbound/adapter/zendesk"`

3. **`internal/domain/feedback.go`** — add `"zendesk": "Zendesk"` to
   `DefaultSourceSet`.

### Phase 2: Console backend

4. **Proto** (`proto/attune/v1/inbound_source.proto`) — add
   `ZendeskConnConfig`, extend `CreateInboundSourceRequest` and
   `TestInboundConnectionRequest`. Run `make proto`.

5. **Handlers** (`internal/handlers/console/inbound/`):
   - `inbound_create_zendesk.go` — create flow
   - `inbound_test_connection.go` — add zendesk case
   - `inbound_handler.go` — constant + test seam
   - `inbound_create.go` — switch case

### Phase 3: Console frontend

6. **`console/src/features/inbound-sources/components/create-dialog.tsx`** —
   `ZendeskFieldset`, channel selector, buildBody, handleTest
7. **`console/src/i18n/zh-CN.json`** — zendesk i18n keys
8. **`console/src/features/inbound-sources/components/sources-table.tsx`** —
   `ChannelPill` for zendesk (Headphones icon)

### Phase 4: Tests

9. **Conformance test** — `conformance_test.go` calling
   `inboundtest.TestAdapterContract`
10. **API client tests** — httptest server with fixture responses for
    incremental export, comments, auth test, rate limiting, error responses
11. **Poll/sync tests** — mock client + fake stores; verify cursor advancement,
    error handling, source disabling, comment budget
12. **Normalize tests** — ticket+comments → IngestInput shape, truncation,
    meta keys
13. **Handler tests** — create, test-connection for zendesk channel
14. **Console tests** — component tests for ZendeskFieldset
15. **Source vocabulary golden test** — update
    `TestSourceVocabulary_AppendOnly` frozen set
16. **Composition-root test** — `TestBuildSourceSet_Conformance` auto-passes
    with blank-import

### Phase 5: Documentation

17. **CHANGELOG.md** — `### Added` entry
18. **README.md** — mention Zendesk in supported sources
19. **Private deploy docs** — Zendesk configuration section

---

## Verification

1. **Unit tests:** `go test ./internal/inbound/adapter/zendesk/...`
2. **Conformance:** adapter contract test passes all 6 gates
3. **Handler tests:** `go test ./internal/handlers/console/inbound/...`
4. **Console tests:** `pnpm vitest run` in `console/`
5. **Quality gates:** `make ci-check` passes (vet, build, lint, duplication,
   complexity, raw-ptr, error-code, integration layout, artifact hygiene)
6. **Proto sync:** `make proto && git diff --exit-code`
7. **Source vocabulary:** golden test updated and passing
8. **Local E2E:** Create zendesk source via Console → verify poll logs →
   confirm feedback items appear in Console list (using httptest stub or
   real Zendesk sandbox)

---

## References

- [Zendesk Incremental Export API](https://developer.zendesk.com/api-reference/ticketing/ticket-management/incremental_exports/)
- [Zendesk ticket comments API](https://developer.zendesk.com/api-reference/ticketing/tickets/ticket_comments/)
- [Zendesk auth sunset timeline](https://support.zendesk.com/hc/en-us/articles/10851263566234)
- [Zendesk OAuth guide](https://developer.zendesk.com/documentation/api-basics/authentication/oauth-vs-api-tokens/)
- [Zendesk rate limits](https://developer.zendesk.com/api-reference/introduction/rate-limits/)
- Slack inbound adapter (internal reference): `internal/inbound/adapter/slack/`
- Inbound framework proposal: `docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md`
