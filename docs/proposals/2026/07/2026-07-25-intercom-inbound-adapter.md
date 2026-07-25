# Intercom inbound adapter — extract product signals from conversations

| | |
|---|---|
| **Issue** | [#230](https://github.com/Phixsura/attune/issues/230) |
| **Status** | Implemented |
| **Started** | 2026-07-25T10:30:00+08:00 |
| **Related** | [#66](../06/2026-06-08-channel-agnostic-inbound.md) (inbound framework), [#202](https://github.com/Phixsura/attune/issues/202) (industry gap closure), [#12](https://github.com/Phixsura/attune/issues/12) (support ticket auto-extraction), [#229](2026-07-24-zendesk-inbound-adapter.md) (Zendesk adapter — the structural template), [#32](https://github.com/Phixsura/attune/issues/32) (Intercom bidirectional — future) |

---

## Problem

Intercom conversations are one of the highest-volume raw product-signal streams
in B2B SaaS — feature requests, bug reports, and churn signals arrive as chat
threads that never reach the product backlog unless someone copies them by
hand. Every benchmarked VoC platform (Enterpret, Productboard, Canny, Dovetail,
Unwrap) ships an Intercom connector as table stakes; Enterpret's integration
page lists it beside Zendesk as a tier-1 source
(`docs/research/2026-06-20-voc-landscape.md` §3).

attune's inbound framework (#66) now has four channels — webhook, email,
Slack, Zendesk — behind one `Adapter` port, and #229 established the complete
poll-adapter playbook: shared infra client, cursor sync, backoff, sync-now,
sync stats, Console CRUD + test-connection + recent preview. Intercom is the
structural sibling of Zendesk with different API mechanics.

What is genuinely different about Intercom (verified against the official
OpenAPI 2.16 spec and Intercom developer docs):

- **No incremental-export API.** Bulk extraction goes through
  `POST /conversations/search` filtered on `updated_at`, sorted ascending,
  with stateless `starting_after` cursor pagination (max 150/page).
- **The watermark is a native int64.** `updated_at` is a unix-seconds
  integer — it fits the framework's `LastUID int64` directly. Unlike
  Zendesk's opaque cursor, no config-blob cursor storage is needed.
- **Search timestamps are date-indexed.** Airbyte's production connector
  floors the search start to UTC midnight and re-filters client-side at
  second precision (`is_client_side_incremental`), because sub-day
  precision on `updated_at` filters is not reliable. We adopt the same
  defensive design.
- **Full thread in one call.** `GET /conversations/{id}?display_as=plaintext`
  returns the conversation plus up to 500 parts with plain-text bodies —
  one API call per conversation, no HTML stripping, no comment pagination.
- **Three regional hosts.** `api.intercom.io` (US), `api.eu.intercom.io`
  (EU), `api.au.intercom.io` (AU). Region is a required connection
  parameter; EU/AU workspaces fail cross-region calls.
- **Simpler auth.** A private-app Access Token is a plain Bearer token.
  OAuth exists for public/multi-workspace apps only (#32 territory).
- **Internal notes are private.** `part_type == "note"` is an internal
  admin annotation. The phase-2 platform research
  (`docs/research/2026-07-13-public-feedback-platform-phase-2-roadmap.md`)
  records the industry consensus: support connectors do not mirror private
  notes by default. Notes are excluded from ingested content.

---

## Goals

1. Intercom conversations generate feedback items with full support metadata
   (contact, company, conversation ID, teammate, tags, state, rating,
   permalink) and a plain-text transcript of customer + agent messages.
2. Incremental sync driven by an `updated_at` watermark in `LastUID`, with
   replay-safe dedup (re-ingesting the same conversation at the same
   `updated_at` is a no-op; a later update produces a new feedback row —
   same evolution-capture semantics as Zendesk #229).
3. Access-token auth with test-connection returning the workspace name +
   region, matching the Zendesk Console flow.
4. Conversation backlinks (Intercom inbox permalink) visible to operators in
   the feedback detail's source metadata.
5. Sync health (healthy/error/paused), sync stats, sync-now, and recent
   preview reuse the existing Console machinery unchanged.
6. No Intercom credentials or private note content in logs.

## Non-goals

- **Bidirectional sync** (pushing request status back into Intercom) — #32,
  will reuse `internal/infra/intercomclient`.
- **OAuth / public-app flow** — paste-token private app is the deployment
  model for a self-hosted tool; OAuth is deferred to #32 where
  multi-workspace access actually matters.
- **Webhook-push ingestion** — Intercom webhooks deliver per-event payloads
  without reliable replay after downtime; polling with an `updated_at`
  watermark is the bulk-extraction pattern used by Airbyte's production
  connector. Webhook supplementation can layer on later without schema
  changes.
- **Automatic customer-request candidate creation** — decided in #229's
  system-level redesign (§3): feedback rows are promotable/linkable to
  customer requests through the existing `PromoteFeedback` / `LinkFeedback`
  Console flows; a fully automatic draft pipeline is a separate issue that
  applies to all channels, not an Intercom special.
- **Fin/AI-agent analytics** — `ai_agent` fields are captured in SourceMeta
  but no Fin-specific dashboards.
- **Attachment download** — attachments noted in metadata only.

---

## Proposal

### Architecture

```
internal/infra/intercomclient/        — shared HTTP client (infra layer)
  client.go    — Client interface + implementation
  types.go     — Conversation, Part, Contact, Company, AccountInfo
  egress.go    — SetEgressPolicy / test base-URL seam

internal/inbound/adapter/intercom/    — poll-mode adapter
  intercom.go  — registration, lifecycle (mirrors zendesk.go)
  poll.go      — watermark sync loop
  config.go    — Config + encrypted token parsing
  client.go    — type aliases into intercomclient
  normalize.go — conversation + parts → IngestInput
  public.go    — Console-facing exports (AuthTest)
  ops.go       — error classification

deps.Ingest.Ingest(ctx, tenantID, uuid.Nil, in)
        │
existing pipeline unchanged: ingest → enrich → outbox → notify
```

Same layering as `zendeskclient` (#229 system-level redesign §1): the infra
client is exported and reusable by #32; the adapter owns config, polling,
and normalization. A depguard rule `intercom-client-boundary` mirrors the
zendesk one.

### 1. API client (`internal/infra/intercomclient`)

```go
type Client interface {
    // AuthTest validates the token and returns workspace info (GET /me).
    AuthTest(ctx context.Context) (AccountInfo, error)
    // SearchConversations returns one page of conversations with
    // updated_at >= startTime, sorted by updated_at ascending.
    SearchConversations(ctx context.Context, startTime int64, startingAfter string) (ConversationPage, error)
    // GetConversation fetches the full thread with plain-text parts.
    GetConversation(ctx context.Context, id string) (Conversation, error)
    // SearchContacts batch-resolves contact IDs (id IN [...]).
    SearchContacts(ctx context.Context, ids []string) ([]Contact, error)
}
```

- **Auth:** `Authorization: Bearer {token}`; pinned `Intercom-Version: 2.16`
  header on every request (Intercom versions per-request; pinning prevents
  silent behavior drift when the workspace default changes).
- **Base URL by region:** `region ∈ {us, eu, au}` →
  `https://api.intercom.io` / `https://api.eu.intercom.io` /
  `https://api.au.intercom.io`. Host validation rejects anything not
  `*.intercom.io` in production (test seam override, same as zendesk).
- **Transport:** `otelhttp.NewTransport`, `nethardening.Policy` dial hook
  wired in `applyRuntimeHardening`, body reads capped at 4 MiB.
- **Rate limiting:** parse `X-RateLimit-Remaining` / `X-RateLimit-Reset`;
  on 429 return `RateLimitError{RetryAfter}` derived from `X-RateLimit-Reset`
  (Intercom does not send `Retry-After`). Budget note: private apps get
  10,000 req/min enforced in 10-second windows (≈1,666/10s) — the
  per-tick page cap keeps us far below it.

### 2. Incremental sync (poll.go)

Watermark model — no opaque cursor, so simpler than Zendesk:

1. Watermark = `Source.State.LastUID` (unix seconds of the newest fully
   processed `updated_at`). First sync seeds from `StartFrom`
   (`"now"` → now−5m, `"full"` → 0), same vocabulary as email/zendesk.
2. Each tick: `SearchConversations` with
   `updated_at > floorToUTCDay(watermark)` **AND** `updated_at < now`,
   `sort: updated_at ascending`, paginate with `starting_after` up to
   `maxPagesPerTick = 10` (≤1,500 conversations/tick).
   - **Day-floor + client-side filter:** the request start is floored to
     UTC midnight of the watermark day (search timestamps are
     date-indexed — benchmarked from Airbyte's connector); conversations
     with `updated_at <= watermark` are skipped client-side before any
     detail fetch, so the re-listed same-day head costs list pages only.
3. For each qualifying conversation: `GetConversation(id)` (plaintext,
   capped by `maxDetailFetches` per tick, default 50 — the Intercom
   analogue of Zendesk's comment budget; conversations over budget are
   picked up next tick because the watermark only advances past
   *processed* items).
4. Batch-resolve contact refs via `SearchContacts` (per-page unique IDs,
   IN query, chunked ≤25 — search `IN` arrays are bounded by the 15-filter
   group limit, 25 ids per chunk stays comfortably one filter). Resolution
   failure falls back to the part-author name/email already inline in the
   thread — never blocks ingestion.
5. Ingest with idempotency key
   `intercom_{workspaceID}_{conversationID}_{updatedAt}` — same
   evolution-capture semantics as Zendesk (#229 redesign §4a): an update
   to a conversation produces a new feedback row; a replayed page dedups.
6. Watermark advances to the max processed `updated_at` only after the
   page's qualifying conversations are ingested or skipped; pagination
   statelessness (documented Intercom behavior) is therefore harmless —
   a missed record re-appears in the next tick's window, a duplicated
   record dedups on the idempotency key.

Poll cadence, per-source timeout, exponential backoff (3+ consecutive
failures → doubling interval capped at 15 min), sync-now channel, sync
stats (`conversations_synced`, `backfill_done`) — all identical to the
Zendesk adapter mechanics; `SyncStats` persists in the encrypted Config
blob, surfaced through the existing generic proto fields.

### 3. Normalization (normalize.go)

One `IngestInput` per conversation snapshot:

```go
IngestInput{
    Source:         "intercom",
    Content:        buildContent(conv),   // title/subject + tagged transcript
    SourceUser:     contactEmail or contactName,
    PageURL:        permalink,            // https://app.intercom.com/a/inbox/{workspace}/inbox/conversation/{id}
    SourceMeta:     buildIntercomSourceMeta(...),
    IdempotencyKey: fmt.Sprintf("intercom_%s_%s_%d", workspaceID, conv.ID, conv.UpdatedAt),
}
```

**Content assembly** (mirrors Zendesk §1.1/1.4 customer-vs-agent tagging +
smart truncation):

```
{title or source.subject}

[customer] I can't export my dashboard as PDF — the button is greyed out.
---
[agent] Thanks for flagging! Which plan are you on?
---
[customer] Team plan. This is blocking our Monday reporting.
```

- Seed message = `conversation.source.subject` + `source.body`
  (plaintext via `display_as=plaintext`), tagged by its author type.
- Parts included: `part_type ∈ {comment, assignment-with-body…}` with
  non-empty body; **`note*` part types excluded** (internal), redacted
  parts skipped, bot parts (`author.type == "bot"` or `from_ai_agent`)
  tagged `[bot]` and included only when space permits (they carry Fin's
  restatement of the customer problem — signal, but lowest priority).
- Tag = `[customer]` for contact/lead/user authors, `[agent]` for
  admin/team, `[bot]` as above.
- Smart truncation at 4,500 chars: always keep title + seed message;
  keep first 3 + last 2 customer messages; `[... N messages omitted ...]`
  marker; agent/bot messages fill remaining space.
- `intercom_customer_message_count` / `intercom_agent_message_count`
  emitted so enrichment can weight customer signal (parity with Zendesk).

**SourceMeta keys** (all `intercom_` prefixed + the two well-known keys):

| Key | Value |
|---|---|
| `inbound_source_id` / `inbound_source_name` | well-known source identity |
| `intercom_region` | us / eu / au |
| `intercom_workspace_id` | app id_code from AuthTest |
| `intercom_conversation_id` | conversation ID (string) |
| `intercom_conversation_url` | inbox permalink |
| `intercom_state` | open / closed / snoozed |
| `intercom_priority` | priority or empty |
| `intercom_created_at` / `intercom_updated_at` | ISO8601 |
| `intercom_tags` | JSON array of tag names |
| `intercom_contact_id` / `intercom_contact_name` / `intercom_contact_email` | primary contact (first in contacts list) |
| `intercom_contact_external_id` | customer's own user ID — the profile join key |
| `intercom_company_id` / `intercom_company_name` | conversation company if present |
| `intercom_admin_assignee_id` / `intercom_team_assignee_id` | routing context |
| `intercom_rating` / `intercom_rating_remark` | conversation CSAT if present |
| `intercom_ai_agent_participated` | bool |
| `intercom_source_type` | conversation / email / whatsapp / phone_call / … |
| `intercom_customer_message_count` / `intercom_agent_message_count` | ints |

`intercom_contact_external_id` + email are what operators use to attach the
conversation to customer profiles via the existing `LinkCustomer` flow —
acceptance criterion "customer/company context attaches to profiles" is
satisfied by carrying the identity keys, not by inventing a new profile
store.

### 4. Error handling (ops.go)

Zendesk's three-tier model, Intercom error codes:

| Class | Trigger | Action | Metric |
|---|---|---|---|
| Permanent auth | 401 `unauthorized` (token revoked/invalid) | `SetEnabled(false, reason)` | `auth_err` |
| Permanent plan | 403 `api_plan_restricted` | `SetEnabled(false, reason)` | `auth_err` |
| Transient | 429, 5xx, network | record `LastError`, backoff | `transient_err` |
| Per-item | detail fetch 404 (deleted conversation) | skip item, continue | `validate_err` |
| Internal | config parse, unexpected shape | record `LastError` | `internal_err` |

Detail-fetch auth failures degrade per-conversation (skip + warn), never
disable the source — the #229 lesson (redesign §4b) applied from day one.

### 5. Console integration

**Proto** (`proto/attune/v1/inbound_source.proto`, additive only — buf
breaking is file-level and required):

```protobuf
message IntercomConnConfig {
  string region = 1;        // "us" | "eu" | "au"
  string access_token = 2;  // private-app token, write-only
  optional string start_from = 3;             // "now" | "full"
  repeated string filter_states = 4;          // open/closed/snoozed, empty = all
  optional int32 max_detail_fetches = 5;      // advanced, default 50
}

// CreateInboundSourceRequest: optional IntercomConnConfig intercom_config = 7;
// TestInboundConnectionRequest: optional IntercomConnConfig intercom_config = 5;
```

**Backend handlers** (`internal/handlers/console/inbound/`):
`inbound_create_intercom.go` (validate region/token → encrypt → insert),
test-connection case calling `intercom.AuthTest` (returns workspace name so
the toast can say "Connected to {workspace}"), channel constant + seam.

**Frontend** (`create-dialog.tsx` + i18n + `sources-table.tsx`):
`IntercomFieldset` — region select, access-token password field with
Developer-Hub help link, start-from radio, advanced collapsible (state
multi-select, detail budget); channel icon `MessageCircle`; recent preview
and sync-now reuse the generic panels.

### 6. Source vocabulary, metrics, wiring

- `inbound.Register("intercom", "Intercom", NewAdapter)`; blank import in
  `cmd/attune`; `"intercom"` added to `DefaultSourceSet` and the
  `TestSourceVocabulary_AppendOnly` frozen golden (append-only, #95).
- Metrics: existing channel-agnostic names pick up `channel="intercom"` —
  no new metric names, so no catalog/dashboard drift.
- `applyRuntimeHardening` calls `intercomclient.SetEgressPolicy(p)`.

---

## Alternatives considered

### A. Webhook-push (topics: conversation.user.created, …)

Near-real-time, but: per-tenant webhook configuration in the Developer Hub,
payloads carry single parts without guaranteed thread context, no replay
after downtime, and ordering is not guaranteed. Airbyte, Fivetran, and
Enterpret-class ingest all poll the search API for bulk extraction. Poll
now; webhook supplementation later if latency matters.

### B. `GET /conversations` list endpoint instead of search

The list endpoint paginates everything with no `updated_at` filter — a full
re-list per tick at scale. Search with an `updated_at` window is what the
benchmark connector (Airbyte) uses. Rejected.

### C. Store a search cursor in Config (Zendesk pattern)

`starting_after` cursors are stateless page pointers scoped to one query,
not resumable sync positions. The `updated_at` watermark in `LastUID` is
the durable cursor; carrying page cursors across ticks would break on any
mid-pagination update. Rejected — and it makes Intercom *simpler* than
Zendesk (no cursor in the config blob).

### D. One feedback row per conversation part

Maximum granularity, but destroys thread context that the enricher needs
(a feature request is usually spread across 2-3 customer messages), and
multiplies row volume ~8x. The conversation is the unit of product signal;
`updated_at` in the idempotency key already captures evolution. Rejected
(same call as Zendesk made for tickets).

### E. OAuth from day one

Intercom OAuth exists for public apps distributed via their App Store. A
self-hosted attune instance connecting to its own workspace is exactly the
private-app Access Token use case (Intercom's own docs say so). OAuth adds
a redirect flow + client registration for zero deployment benefit today;
#32 (bidirectional, potentially multi-workspace) is where it earns its
complexity.

---

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| Date-indexed search granularity (sub-day filters unreliable) | Floor query start to UTC midnight + client-side second-precision filter (Airbyte-benchmarked); same-day head re-lists cost list pages only |
| Stateless pagination duplicates/misses on concurrent updates | Ascending sort + watermark re-cover next tick + idempotency dedup |
| High-churn conversations re-ingest on every update | By design (evolution capture, §229); enricher clustering collapses them; state filter lets operators sync `closed` only |
| 500-part API cap on huge threads | Accepted; smart truncation already keeps head+tail customer messages |
| Detail budget starves large backfills | Watermark only advances past processed items; backfill progresses 50 conversations/tick minimum and is visible in sync stats |
| EU/AU data residency mistakes | Region is explicit + host allowlist per region; AuthTest verifies before create |
| Token leak via logs | Token encrypted (double-envelope like Zendesk), wiped after use, never logged; content never logged |

---

## Implementation plan

1. **`internal/infra/intercomclient/`** — client, types, egress, tests
   (httptest fixtures for search/detail/contacts/me/429/401/403).
2. **`internal/inbound/adapter/intercom/`** — lifecycle, poll, config,
   normalize, ops, public + conformance test + fixture tests.
3. **Wiring** — blank import, `DefaultSourceSet`, golden test,
   `applyRuntimeHardening`, depguard rule.
4. **Proto** — `IntercomConnConfig`, create/test request fields,
   `make proto`, commit generated Go/TS/OpenAPI.
5. **Console backend** — create + test-connection handlers + audit action
   wiring (both Go validActions and the DB CHECK migration if a new
   audit action is introduced; reuse the existing generic
   `inbound_source.*` actions if sufficient).
6. **Console frontend** — `IntercomFieldset`, channel pill, i18n,
   component tests.
7. **Docs** — CHANGELOG `### Added`, README sources list, private-deploy
   Intercom section.

---

## Verification

1. `go test ./internal/infra/intercomclient/... ./internal/inbound/adapter/intercom/...`
   (fixtures: search page, detail with notes/bot/redacted parts, contact
   resolution, 429 with X-RateLimit-Reset, 401/403, day-floor windowing).
2. Conformance: `inboundtest.TestAdapterContract` all gates.
3. Handler + Console tests; source-vocabulary golden updated.
4. `make ci-check` green; `make proto && git diff --exit-code`.
5. Local E2E: create an Intercom source in Console against an httptest
   stub (or a real dev workspace if a token is available) → poll logs →
   feedback rows appear with `intercom_*` metadata and working permalink →
   re-run sync-now → no duplicate rows (replay-safe dedupe criterion).

---

## References

- Intercom OpenAPI 2.16 (official, MIT): https://github.com/intercom/Intercom-OpenAPI — conversations/search, retrieve-with-parts, contacts/search, /me schemas
- Search conversations: https://developers.intercom.com/docs/references/rest-api/api.intercom.io/conversations/searchconversations
- Pagination (search, statelessness caveat): https://developers.intercom.com/docs/build-an-integration/learn-more/rest-apis/pagination
- Rate limiting (10k/min, 10s windows, X-RateLimit headers): https://developers.intercom.com/docs/references/rest-api/errors/rate-limiting
- Authentication (access token vs OAuth): https://developers.intercom.com/docs/build-an-integration/learn-more/authentication
- Regional hosting (US/EU/AU): https://developers.intercom.com/docs/build-an-integration/learn-more/rest-apis
- Airbyte source-intercom manifest (production connector; day-floor + client-side incremental + updated_at ascending): https://github.com/airbytehq/airbyte/blob/master/airbyte-integrations/connectors/source-intercom/manifest.yaml
- Zendesk adapter proposals (structural template): [MVP](2026-07-24-zendesk-inbound-adapter.md) · [world-class upgrade](2026-07-24-zendesk-world-class-upgrade.md) · [system-level redesign](2026-07-24-zendesk-system-level-redesign.md)
- VoC landscape benchmark: `docs/research/2026-06-20-voc-landscape.md`
