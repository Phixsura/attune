# Zendesk inbound adapter — world-class upgrade spec

| | |
|---|---|
| **Issue** | [#229](https://github.com/Phixsura/attune/issues/229) |
| **Status** | Proposed |
| **Started** | 2026-07-24T16:00:00+08:00 |
| **Related** | [zendesk-inbound-adapter](2026-07-24-zendesk-inbound-adapter.md) (MVP, Implemented) |

---

## Problem

The MVP Zendesk adapter (#229) delivers basic poll-based ticket extraction.
A structured audit against Productboard, Canny, Medallia, Enterpret, and
Qualtrics identified 21 gaps across 5 dimensions. This spec upgrades the
adapter from MVP to world-class, addressing all 21 gaps in a single pass.

---

## 1. Content intelligence

### 1.1 Customer-vs-agent comment differentiation

Compare each `comment.AuthorID` against `ticket.RequesterID`. Tag each
comment in the assembled content:

```
Cannot log in after password reset

[customer] I reset my password but it doesn't work. Getting 500 errors.
---
[agent] Hi Alice, we've identified an issue. Try again in 30 min.
---
[customer] Still not working. This is blocking my entire team.
```

Add `zendesk_customer_message_count` and `zendesk_agent_message_count` to
SourceMeta so enrichment can weight customer signal.

### 1.2 Ticket metadata → enrichment type hint

Map Zendesk fields to `IngestInput.Type`:

| priority | type | satisfaction | → IngestInput.Type |
|---|---|---|---|
| urgent/high | incident/problem | any | `bug_report` |
| any | question | any | `feature_request` |
| any | task | any | `task` |
| any | any | bad | `complaint` |
| fallback | | | (empty — LLM decides) |

### 1.3 Custom fields

Add `CustomFields []customField` to the `ticket` struct:

```go
type customField struct {
    ID    int64  `json:"id"`
    Value any    `json:"value"`
}
```

Store as `zendesk_custom_fields` in SourceMeta (JSON string).

### 1.4 Smart truncation

Replace naive `content[:4500]` with structured truncation:

- Always keep: subject + description
- Keep first 3 customer messages + last 2 customer messages
- Between them: `[... N messages omitted ...]`
- Agent messages included only if space permits
- Total cap: 4500 chars

Fetch strategy: request comments with `sort_order=asc` + limit per page,
and separately `sort_order=desc` + limit=3 for the tail. Avoids fetching
5000 comments just to discard most.

---

## 2. Operational resilience

### 2.1 Rate-limit retry with Retry-After

`getJSON` parses `Retry-After` header on 429 and returns a structured
`rateLimitError{retryAfter time.Duration}`. `pollSource` recognizes it
and sleeps for the specified duration before continuing (not waiting for
the next 60s tick).

### 2.2 Exponential backoff / circuit breaker

Per-source consecutive-failure counter in the adapter struct:

- 0 failures → 60s interval (normal)
- 1-2 failures → 60s (transient, retry soon)
- 3+ failures → interval doubles: 120s → 240s → 480s → max 900s
- Any success → resets counter and interval to 60s

Simpler than a full circuit-breaker library; fits the existing poll model.

### 2.3 Multi-page continuous pagination

When `EndOfStream == false`, continue fetching the next page within the
same `pollSource` call. Cap at `maxPagesPerTick = 10` to bound per-tick
duration. Backfill speed: 1 page/60s → 10 pages/60s (10x improvement).

### 2.4 Configurable comment budget

Move `maxCommentFetches` from package constant to `Config.MaxCommentFetches`
(default 50). Console "Advanced" section exposes this.

### 2.5 Smart comment fetching

Instead of fetching all comments then truncating:

1. Fetch first page (asc, size=10) for description + early customer messages
2. Fetch last page (desc, size=5) for most recent customer messages
3. Skip middle pages entirely

Reduces API calls from potentially 50 to 2 per ticket.

### 2.6 Backfill progress logging

After each page: `logext.Infof(ctx, "[%s] progress,source_id:%s,tickets_synced:%d,cursor:%s,end_of_stream:%v", ...)`.

Persist `SyncStats{TicketsSynced, LastTicketID, BackfillDone}` in Config
blob alongside cursor.

---

## 3. Sync sophistication

### 3.1 Ticket filtering

Config gains a `Filter` struct:

```go
type Filter struct {
    Tags        []string `json:"tags,omitempty"`
    ExcludeTags []string `json:"exclude_tags,omitempty"`
    Statuses    []string `json:"statuses,omitempty"`
}
```

`processTicketPage` applies `matchesFilter(t, cfg.Filter)`. Non-matching
tickets are skipped but cursor still advances.

Console: "Advanced" section adds tag input (comma-separated) and status
multi-select (checkboxes).

Not doing view/group/brand filtering — incremental export API doesn't
support it natively; tag+status covers 90% of use cases.

### 3.2 Operator-visible sync progress

Proto `InboundSource` gains optional fields:

```protobuf
optional int64 tickets_synced = 12;
optional int64 last_synced_ticket_id = 13;
optional bool backfill_done = 14;
```

Console detail panel shows: "已同步 12,345 条 · 初始回填已完成" or
"已同步 3,200 条 · 回填中..."

### 3.3 Sync Now

New endpoint: `POST /inbound/sources/{id}/sync-now`

Adapter interface gains `TriggerSync(sourceID string)`. Implementation:
adapter holds a `syncNow chan string`. `pollLoop` selects on both ticker
and `syncNow`. Handler calls `manager.TriggerSync(id)` which dispatches
to the registered adapter.

Inbound framework changes:

```go
// In inbound.go
type Triggerable interface {
    TriggerSync(sourceID string)
}
```

Console: detail panel adds "立即同步" button.

### 3.4 Custom field passthrough

Custom fields stored in SourceMeta as JSON (§1.3). No custom mapping
UI — that's externalsync V2 territory. Operators can reference
`zendesk_custom_fields` in enrichment dimension configuration.

---

## 4. Security hardening

### 4.1 URL construction + SSRF

- Replace string concatenation in `do()` with `net/url.URL` +
  `url.Values.Encode()` for query params.
- Production host validation: `newClient` verifies the base URL host
  ends with `.zendesk.com` unless the test-override flag is set via
  `SetAPIBaseURL`.
- Add `nethardening.Policy` to the HTTP transport dial function (same
  pattern as email adapter and externalsync).

### 4.2 OAuth token refresh

Flow on 401 when `cred.mode == AuthModeOAuth`:

```
401 received
  → has refreshToken?
    → yes: POST /oauth/tokens {grant_type: refresh_token, ...}
      → success: update cred, retry original request, persist new token
      → fail: disable source ("OAuth refresh failed")
    → no: disable source ("OAuth token expired, no refresh token")
```

Config adds `OAuthClientID` and `OAuthClientSecret` (encrypted), needed
for the refresh grant. Console ZendeskFieldset OAuth section captures
these (already has the fields — they're currently used as the initial
token, but the flow should be: operator does OAuth authorization flow
externally, pastes the resulting access+refresh tokens, we store
client_id+client_secret for refresh).

### 4.3 403 as permanent error

`apiError.Permanent()` adds `"forbidden"` and status 403 to the permanent
set. `friendlyZendeskError` already maps 403 → user-friendly message.

---

## 5. Console UX

### 5.1 Recent sync preview

New endpoint: `GET /inbound/sources/{id}/recent`

Returns last 5 feedback items for a source:

```json
{
  "items": [
    {
      "id": 123,
      "content_preview": "Cannot log in after password reset...",
      "zendesk_ticket_id": 42,
      "zendesk_status": "open",
      "created_at": "2026-07-24T10:00:00Z"
    }
  ]
}
```

Console detail panel renders this below sync stats.

### 5.2 Post-connection summary

On create success, toast shows "已连接到 {subdomain}.zendesk.com".
No proto change needed — subdomain is already in the create request.

### 5.3 Onboarding guidance

ZendeskFieldset help text gains external links:

- Subdomain: "在浏览器地址栏查看，如 `mycompany.zendesk.com` 中的 mycompany"
- API Token: "在 Zendesk 管理中心 > 应用和集成 > API 创建" +
  `https://support.zendesk.com/hc/en-us/articles/4408889192858`
- OAuth: "在 Zendesk 管理中心 > 应用和集成 > API > OAuth 客户端 创建"

### 5.4 Sync Now button

Detail panel action row adds "立即同步" (§3.3 backend).

---

## Implementation order

| Phase | Items | Files | Risk |
|---|---|---|---|
| **A: Security** | 4.1, 4.2, 4.3 | client.go, config.go, poll.go, ops.go | CRITICAL — must ship first |
| **B: Resilience** | 2.1, 2.2, 2.3, 2.6 | client.go, poll.go, zendesk.go | CRITICAL+IMPORTANT — core reliability |
| **C: Content** | 1.1, 1.2, 1.3, 1.4, 2.5 | client.go, normalize.go, config.go | IMPORTANT — quality of extracted signal |
| **D: Sync** | 3.1, 3.2, 3.3, 2.4 | config.go, poll.go, proto, handler, inbound.go | IMPORTANT — operator control |
| **E: Console** | 5.1, 5.2, 5.3, 5.4, 3.4 | handler, proto, create-dialog.tsx, sources-table, i18n | IMPORTANT+NICE-TO-HAVE — UX polish |

Each phase is independently shippable. Phase A is a prerequisite for
production deployment; B-E can be parallelized.

---

## Verification

Per phase:
1. Unit tests for every new code path
2. Fixture tests updated with new fields/behavior
3. `go test ./internal/inbound/adapter/zendesk/ -cover` ≥ 85%
4. `make ci-check` green
5. Browser E2E via Chrome DevTools MCP for Console changes
6. Security review for Phase A changes
