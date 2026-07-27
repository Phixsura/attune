# Zendesk integration — system-level redesign

| | |
|---|---|
| **Issue** | [#229](https://github.com/Phixsura/attune/issues/229) |
| **Status** | Implemented |
| **Started** | 2026-07-24T22:00:00+08:00 |
| **Related** | [zendesk-inbound-adapter](2026-07-24-zendesk-inbound-adapter.md) (MVP), [zendesk-world-class-upgrade](2026-07-24-zendesk-world-class-upgrade.md) (Phase A-E), [#31](https://github.com/Phixsura/attune/issues/31) (Zendesk bidirectional), [#66](../06/2026-06-08-channel-agnostic-inbound.md) (inbound framework) |

---

## Problem

After multiple rounds of implementation and audit, the Zendesk adapter has
functional code for 21 world-class features. But a project-wide review
revealed that the gaps are architectural, not functional:

1. **OAuth is structurally broken** — UI collects wrong values (client
   credentials labeled as tokens), handler discards client_id, refresh
   can never succeed.
2. **IngestInput.Type is dead weight** — `inferType` writes type hints
   that nobody reads; the enricher ignores the Type field entirely.
3. **Zendesk API client is an unexported silo** — all types in
   `inbound/adapter/zendesk/client.go` are package-private; #31
   (externalsync) will duplicate everything.
4. **Egress policy not wired** — `applyRuntimeHardening` skips Zendesk;
   on-prem with `allow_loopback_egress` breaks.
5. **Console UI is decorative** — filter/comment-budget Config structs
   exist but the UI has no inputs; operators can't configure them.
6. **Idempotency key drops ticket updates** — `zendesk_{sub}_{id}` never
   changes; incremental export re-delivers updated tickets that get
   silently rejected as duplicates.
7. **fetchComments disables entire source** — a single ticket's comment
   auth failure (e.g., restricted ticket) disables all syncing.
8. **SyncStats.LastTicketID is actually a timestamp** — confusing for
   operators.

This spec addresses all 8 issues in a cohesive design.

---

## 1. Shared Zendesk API client

### What

Extract `internal/infra/zendeskclient/` — a shared, exported Zendesk
HTTP client package at the `infra` layer (parallel to `infra/llmclient`).

### Files

```
internal/infra/zendeskclient/
  client.go      — Client interface + HTTP implementation
  types.go       — Ticket, Comment, User, Organization, OAuthToken
  auth.go        — API token Basic auth / OAuth Bearer / token refresh
  ratelimit.go   — RateLimitError + ParseRetryAfter
  egress.go      — SetEgressPolicy (same pattern as notify/llmclient)
```

### Interface

```go
type Client interface {
    AuthTest(ctx context.Context) (AccountInfo, error)
    IncrementalTickets(ctx context.Context, cursor string, startTime int64) (TicketPage, error)
    TicketComments(ctx context.Context, ticketID int64) ([]Comment, error)
    ShowUsers(ctx context.Context, ids []int64) ([]User, error)
    ShowOrganizations(ctx context.Context, ids []int64) ([]Organization, error)
    RefreshOAuthToken(ctx context.Context, refreshToken, clientID, clientSecret string) (OAuthToken, error)
}
```

### Consumers

- `internal/inbound/adapter/zendesk/` — imports `zendeskclient.Client`
  for poll + normalize. Adapter owns Config, poll loop, content assembly.
- `internal/externalsync/adapter/zendesk/` (future #31) — imports the
  same Client for bidirectional push/pull.

### Boundary rules

- `zendeskclient` is `infra` layer — must not import `service`, `repo`,
  `handlers`, `inbound`, `notify`, `domain`.
- Add depguard rule `zendesk-client-boundary` in `.golangci.yml`.
- `cmd/attune/server.go:applyRuntimeHardening` calls
  `zendeskclient.SetEgressPolicy(p)`.

### Migration from current code

- Move HTTP logic, types, auth, rate-limit from
  `inbound/adapter/zendesk/client.go` → `infra/zendeskclient/`.
- Adapter's `client.go` becomes a thin wrapper: `newClient` calls
  `zendeskclient.New(baseURL, cred)`.
- `validateHost` moves to `zendeskclient` (production host validation).
- `buildURL`, `postForm`, `do`, `getJSON` move to `zendeskclient`.

---

## 2. OAuth redesign

### Current state (broken)

UI labels "OAuth Client ID" + "OAuth Client Secret". Handler treats
Client Secret as Access Token. No refresh_token stored. Refresh request
lacks client credentials. Token refresh always fails.

### New design: paste-mode (MVP)

UI OAuth section collects 4 fields:

| Field | Label | Required | Purpose |
|---|---|---|---|
| `oauthAccessToken` | Access Token | yes | Bearer auth for API calls |
| `oauthRefreshToken` | Refresh Token | no | Auto-renewal when access token expires |
| `oauthClientId` | Client ID | yes if refresh_token provided | Used in refresh grant |
| `oauthClientSecret` | Client Secret | yes if refresh_token provided | Used in refresh grant |

Help text guides the user: "在 Zendesk 管理中心创建 OAuth App，使用授权码
流程获取 access_token 和 refresh_token，然后粘贴到这里。"

### Config shape

`OAuthTokenEncrypted` JSON:

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "client_id": "...",
  "client_secret": "..."
}
```

All 4 values in one encrypted blob. `credential` struct gains
`clientID` and `clientSecret` fields populated from this blob.

### Auth test

Uses `oauthAccessToken` as Bearer token (not client_secret). Correct
semantic: we're testing whether the access token works.

### Refresh flow

```
401 on API call
  → cred.mode == AuthModeOAuth?
    → has refresh_token + client_id + client_secret?
      → POST /oauth/tokens {
          grant_type: refresh_token,
          refresh_token: ...,
          client_id: ...,
          client_secret: ...
        }
      → success: update access_token + refresh_token, persist, retry
      → fail: disable source
    → missing refresh_token: disable source ("no refresh token")
```

### Proto changes

`ZendeskConnConfig` replaces `oauth_client_id` + `oauth_client_secret`
with:

```protobuf
optional string oauth_access_token = 5;
optional string oauth_refresh_token = 6;
optional string oauth_client_id = 7;
optional string oauth_client_secret = 8;
```

### Future: in-app OAuth redirect

Reserve `auth_mode: "oauth_redirect"`. When implemented: Console shows
"授权" button → redirect to Zendesk → callback fills the 4 fields
automatically. Config shape identical — only the acquisition flow changes.

---

## 3. Enricher consumes Type hint

### What

The enricher's `classifyConfigFromRow` currently ignores `row.Type`. Add
a conditional hint to the LLM system prompt when Type is non-empty:

```
The submitter has pre-classified this as: {type}. Consider this hint
but override if the content clearly indicates otherwise.
```

### Scope

- One function change in `internal/service/enrich/enricher.go`
  (`classifyConfigFromRow` or the prompt builder it calls).
- All sources benefit — API ingest can also pass `type`.
- No schema changes.

### Not in scope: automatic customer request candidates

This requires a new table + new enrichment fan-out step. Track as a
separate issue. Document in this proposal for traceability:

> **Future issue**: After enrichment, when `type ∈ {feature_request,
> bug_report}`, auto-insert a `customer_request_draft` row. Requires:
> draft table migration, enricher fan-out extension, Console draft
> review UI.

---

## 4. Adapter-level fixes

### 4a. Idempotency key includes update timestamp

Current: `zendesk_{subdomain}_{ticketID}`
New: `zendesk_{subdomain}_{ticketID}_{generatedTimestamp}`

`GeneratedTimestamp` changes on every ticket update in Zendesk's
incremental export. Same ticket updated 3 times → 3 feedback rows, each
enriched independently. Captures conversation evolution.

### 4b. fetchComments degrades gracefully

Current: permanent error on any ticket's comments → `SetEnabled(false)`
for the entire source.

New: permanent error on comments → skip that ticket, log warning with
ticket ID, emit `comment_auth_err` metric. Only the **incremental export
API itself** returning a permanent error disables the source.

### 4c. Console advanced config UI

ZendeskFieldset gains a collapsible "高级选项" section:

| Field | Type | Default |
|---|---|---|
| Include tags | comma-separated text | empty (no filter) |
| Exclude tags | comma-separated text | empty |
| Status filter | multi-select (open/pending/solved/closed) | all |
| Comment budget per tick | number input | 50 |

Proto `ZendeskConnConfig` adds:

```protobuf
repeated string filter_tags = 9;
repeated string filter_exclude_tags = 10;
repeated string filter_statuses = 11;
optional int32 max_comment_fetches = 12;
optional string start_from = 13;  // already exists, renumber
```

Handler stores these in `Config.Filter` and `Config.MaxCommentFetches`.

### 4d. SyncStats.LastTicketID semantic fix

Track real ticket ID (`ticket.ID`) instead of `GeneratedTimestamp`.
`LastUID` continues using `GeneratedTimestamp` for poll-lag metrics.
Console shows "#12345" not "1753430400".

### 4e. Egress policy wiring

Handled by §1 — `zendeskclient.SetEgressPolicy(p)` in
`applyRuntimeHardening`. All consumers of the shared client inherit the
correct policy.

---

## Implementation order

| Phase | Items | Dependency |
|---|---|---|
| **I** | Extract `zendeskclient/` package | None — pure refactor |
| **II** | OAuth redesign (UI + handler + Config + refresh) | Phase I (uses shared client) |
| **III** | Adapter fixes (4a-4d) | Phase I |
| **IV** | Enricher Type hint | None — independent |
| **V** | Console advanced config UI | Phase III (Config fields exist) |

Phases I and IV can be parallelized. II and III depend on I. V depends
on III.

---

## Verification

Per phase:
1. Unit tests for every changed function
2. `go test ./internal/infra/zendeskclient/... ./internal/inbound/adapter/zendesk/...`
3. Fixture tests updated for new idempotency key format
4. `make ci-check` green
5. Browser E2E for Console OAuth + advanced config changes
6. Existing Slack/email adapter tests unaffected (no shared code changed)

---

## Out of scope

- Automatic customer request candidate creation (future issue)
- In-app OAuth redirect flow (future, `auth_mode: "oauth_redirect"`)
- Zendesk bidirectional sync (#31 — uses shared `zendeskclient`)
- Custom field mapping UI (externalsync V2 territory)
