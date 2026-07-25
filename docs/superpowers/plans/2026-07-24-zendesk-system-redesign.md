# Zendesk System-Level Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 8 architectural issues in the Zendesk integration — extract a shared API client, redesign OAuth, wire enricher Type hints, fix idempotency/error-handling/UI gaps.

**Architecture:** Extract `internal/infra/zendeskclient/` as a shared HTTP client (parallel to `infra/llmclient`). Inbound adapter becomes a thin consumer. OAuth redesign changes UI from client-credentials to paste-mode tokens. Enricher gains a Type hint in the LLM prompt. Adapter fixes address idempotency, error degradation, and Console exposure.

**Tech Stack:** Go 1.26, React/TypeScript, protobuf (buf), chi router, Tink AEAD encryption, nethardening SSRF policy.

## Global Constraints

- All logging through `logext.Infof/Warnf/Errorf` with `ctx` first arg (CLAUDE.md §7)
- All pointers through `ptrext.Of/Indirect/IndirectOr` (CLAUDE.md §7b)
- HTTP transports wrap `otelhttp.NewTransport` (CLAUDE.md §7)
- Proto changes must be additive; run `make proto` and commit output (CLAUDE.md §11)
- Functions: CCN ≤ 15, NLOC ≤ 100 (CLAUDE.md §1)
- No raw `log/slog` in business code (depguard `slog-facade`)
- CHANGELOG entry required for code PRs (CLAUDE.md §2)

---

### Task 1: Extract `internal/infra/zendeskclient/` — types + egress

**Files:**
- Create: `internal/infra/zendeskclient/types.go`
- Create: `internal/infra/zendeskclient/egress.go`
- Create: `internal/infra/zendeskclient/types_test.go`

**Interfaces:**
- Consumes: nothing (foundational)
- Produces: exported types `Ticket`, `TicketPage`, `Comment`, `User`, `Organization`, `AccountInfo`, `OAuthToken`, `CustomField`, `TicketVia`, `SatisfactionRating`, `RateLimitError`, `APIError`; `SetEgressPolicy(p nethardening.Policy)`, `GuardedTransport() http.RoundTripper`

- [ ] **Step 1: Create types.go with all exported types**

Move and export the types currently in `internal/inbound/adapter/zendesk/client.go` (lines 45-109, 512-560). Each type gains an uppercase name. Field names and JSON tags stay identical.

```go
// internal/infra/zendeskclient/types.go
package zendeskclient

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type TicketPage struct {
	Tickets     []Ticket `json:"tickets"`
	AfterCursor string   `json:"after_cursor"`
	EndOfStream bool     `json:"end_of_stream"`
}

type Ticket struct {
	ID                 int64              `json:"id"`
	URL                string             `json:"url"`
	Subject            string             `json:"subject"`
	Description        string             `json:"description"`
	Status             string             `json:"status"`
	Priority           string             `json:"priority"`
	Type               string             `json:"type"`
	Tags               []string           `json:"tags"`
	CustomFields       []CustomField      `json:"custom_fields"`
	RequesterID        int64              `json:"requester_id"`
	SubmitterID        int64              `json:"submitter_id"`
	AssigneeID         int64              `json:"assignee_id"`
	OrganizationID     int64              `json:"organization_id"`
	GroupID            int64              `json:"group_id"`
	CreatedAt          string             `json:"created_at"`
	UpdatedAt          string             `json:"updated_at"`
	GeneratedTimestamp int64              `json:"generated_timestamp"`
	Via                TicketVia          `json:"via"`
	SatisfactionRating SatisfactionRating `json:"satisfaction_rating"`
}

type TicketVia struct {
	Channel string `json:"channel"`
}

type SatisfactionRating struct {
	Score   string `json:"score"`
	Comment string `json:"comment"`
}

type CustomField struct {
	ID    int64 `json:"id"`
	Value any   `json:"value"`
}

type Comment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	Public    bool   `json:"public"`
	AuthorID  int64  `json:"author_id"`
	CreatedAt string `json:"created_at"`
}

type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Organization struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type AccountInfo struct {
	Subdomain string
	AccountID int64
	URL       string
}

type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}

type APIError struct {
	Method string
	Status int
	Code   string
}

func (e APIError) Error() string {
	if e.Code == "" {
		return "zendesk " + e.Method + " failed"
	}
	if e.Status > 0 {
		return "zendesk " + e.Method + ": " + e.Code + " status=" + strconv.Itoa(e.Status)
	}
	return "zendesk " + e.Method + ": " + e.Code
}

func (e APIError) Permanent() bool {
	switch e.Code {
	case "unauthorized", "forbidden":
		return true
	default:
		return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	}
}

type RateLimitError struct {
	Method     string
	RetryAfter time.Duration
}

func (e RateLimitError) Error() string {
	return fmt.Sprintf("zendesk %s: rate limited (retry after %s)", e.Method, e.RetryAfter)
}
```

- [ ] **Step 2: Create egress.go following llmclient/egress.go pattern**

```go
// internal/infra/zendeskclient/egress.go
package zendeskclient

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

var egressPolicy = nethardening.Policy{}

// SetEgressPolicy overrides the SSRF dial policy. Called from
// cmd/attune/server.go:applyRuntimeHardening.
func SetEgressPolicy(p nethardening.Policy) { egressPolicy = p }

// GuardedTransport returns an OTel-instrumented, SSRF-hardened transport.
func GuardedTransport() http.RoundTripper {
	return otelhttp.NewTransport(egressPolicy.NewHTTPTransport())
}
```

- [ ] **Step 3: Create types_test.go**

```go
package zendeskclient_test

import (
	"net/http"
	"testing"

	"github.com/Phixsura/attune/internal/infra/zendeskclient"
)

func TestAPIError_Permanent(t *testing.T) {
	tests := []struct {
		name string
		err  zendeskclient.APIError
		want bool
	}{
		{"unauthorized code", zendeskclient.APIError{Code: "unauthorized"}, true},
		{"forbidden code", zendeskclient.APIError{Code: "forbidden"}, true},
		{"401 status", zendeskclient.APIError{Status: http.StatusUnauthorized}, true},
		{"403 status", zendeskclient.APIError{Status: http.StatusForbidden}, true},
		{"500 status", zendeskclient.APIError{Status: 500}, false},
		{"rate_limited", zendeskclient.APIError{Code: "rate_limited"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Permanent(); got != tc.want {
				t.Errorf("Permanent() = %v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/infra/zendeskclient/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/infra/zendeskclient/
git commit -m "refactor(zendeskclient): extract shared types + egress from inbound adapter (#229)"
```

---

### Task 2: Extract `zendeskclient/` — Client interface + HTTP implementation

**Files:**
- Create: `internal/infra/zendeskclient/client.go`
- Create: `internal/infra/zendeskclient/client_test.go`

**Interfaces:**
- Consumes: types from Task 1 (`Ticket`, `Comment`, `User`, `Organization`, `AccountInfo`, `OAuthToken`, `APIError`, `RateLimitError`, `GuardedTransport`)
- Produces: `Client` interface, `Credential` struct, `New(baseURL string, cred Credential) Client`, `ValidateHost(baseURL string) error`, `ParseRetryAfter(h http.Header) time.Duration`, `SetTestBaseURL(u string)`, `AuthModeAPIToken`, `AuthModeOAuth` constants

- [ ] **Step 1: Create client.go**

Move the HTTP logic from `internal/inbound/adapter/zendesk/client.go` (lines 119-503) into exported functions. Key changes:
- `credential` → `Credential` (exported, with new `ClientID` and `ClientSecret` fields for OAuth)
- `apiClient` interface → `Client` interface (exported)
- `RefreshOAuthToken` gains `clientID, clientSecret string` params (spec §2)
- `newClient` → `New`
- `validateHost` → `ValidateHost` (exported)
- `buildURL`, `do`, `getJSON`, `postForm` become methods on unexported `httpClient`
- `parseRetryAfter` → `ParseRetryAfter` (exported)

The `Client` interface:
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

The `Credential` struct:
```go
type Credential struct {
    Mode         string // AuthModeAPIToken or AuthModeOAuth
    Email        string // for api_token Basic auth
    APIToken     []byte // plaintext api token
    AccessToken  string // OAuth access token
    RefreshToken string // OAuth refresh token (may be empty)
    ClientID     string // OAuth client ID (for refresh grant)
    ClientSecret string // OAuth client secret (for refresh grant)
}
```

`RefreshOAuthToken` implementation sends `client_id` and `client_secret` in the form body:
```go
form := url.Values{
    "grant_type":    {"refresh_token"},
    "refresh_token": {refreshToken},
    "client_id":     {clientID},
    "client_secret": {clientSecret},
}
```

- [ ] **Step 2: Create client_test.go with httptest tests**

Mirror the existing tests from `internal/inbound/adapter/zendesk/client_test.go` but using exported types. Key tests:
- `TestAuthTest_Success`
- `TestIncrementalTickets_WithCursor`
- `TestTicketComments_TwoPage`
- `TestRefreshOAuthToken_Success` — verify POST method, form body includes `client_id` + `client_secret`
- `TestValidateHost`
- `TestParseRetryAfter`

- [ ] **Step 3: Run tests**

Run: `go test ./internal/infra/zendeskclient/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/infra/zendeskclient/
git commit -m "refactor(zendeskclient): extract Client interface + HTTP impl (#229)"
```

---

### Task 3: Rewire inbound adapter to use `zendeskclient`

**Files:**
- Modify: `internal/inbound/adapter/zendesk/client.go` — gut and replace with thin wrapper
- Modify: `internal/inbound/adapter/zendesk/config.go` — `credential` uses `zendeskclient.Credential`
- Modify: `internal/inbound/adapter/zendesk/poll.go` — use `zendeskclient.Client`
- Modify: `internal/inbound/adapter/zendesk/normalize.go` — use `zendeskclient.Ticket` etc.
- Modify: `internal/inbound/adapter/zendesk/ops.go` — use `zendeskclient.APIError`
- Modify: `internal/inbound/adapter/zendesk/public.go` — use `zendeskclient.Credential`
- Modify: `cmd/attune/server.go` — add `zendeskclient.SetEgressPolicy(egress)` to `applyRuntimeHardening`
- Modify: `.golangci.yml` — add `zendesk-client-boundary` depguard rule
- Modify: all `*_test.go` — update type references

**Interfaces:**
- Consumes: `zendeskclient.Client`, `zendeskclient.New`, `zendeskclient.Credential`, all exported types
- Produces: adapter still satisfies `inbound.Adapter`; handler seams unchanged

- [ ] **Step 1: Replace adapter's client.go with thin wrapper**

The adapter's `client.go` shrinks to:
- type alias `type apiClient = zendeskclient.Client`
- `newAPIClient` factory calling `zendeskclient.New`
- `SetAPIBaseURL` delegating to `zendeskclient.SetTestBaseURL`

All HTTP types, methods, error types, `validateHost`, `buildURL`, `setAuth`, etc. are deleted from adapter.

- [ ] **Step 2: Update config.go**

`credential` struct replaced by `zendeskclient.Credential`. `parseConfig` returns `zendeskclient.Credential`. `oauthToken` replaced by `zendeskclient.OAuthToken`.

- [ ] **Step 3: Update normalize.go**

Replace `ticket` → `zendeskclient.Ticket`, `comment` → `zendeskclient.Comment`, `zendeskUser` → `zendeskclient.User`, `zendeskOrganization` → `zendeskclient.Organization`, `customField` → `zendeskclient.CustomField`, `satisfactionRating` → `zendeskclient.SatisfactionRating`, `ticketVia` → `zendeskclient.TicketVia`. The `tagged` struct stays local (content assembly is adapter-specific).

- [ ] **Step 4: Update ops.go**

Replace `apiError` → `zendeskclient.APIError`, `rateLimitError` → `zendeskclient.RateLimitError`.

- [ ] **Step 5: Update poll.go**

Replace all type references. `tryOAuthRefresh` uses `zendeskclient.OAuthToken`.

- [ ] **Step 6: Update public.go**

`AuthTestAPIToken` and `AuthTestOAuth` construct `zendeskclient.Credential` and call `zendeskclient.New`.

- [ ] **Step 7: Wire egress in server.go**

Add to `applyRuntimeHardening` (after line 236):
```go
zendeskclient.SetEgressPolicy(egress)
```

Add import: `"github.com/Phixsura/attune/internal/infra/zendeskclient"`

- [ ] **Step 8: Add depguard rule**

Add to `.golangci.yml` after `outbound-framework-isolation`:
```yaml
zendesk-client-boundary:
  list-mode: lax
  files:
    - "**/internal/infra/zendeskclient/*.go"
  deny:
    - pkg: github.com/Phixsura/attune/internal/service
      desc: "zendeskclient is infra; must not import service"
    - pkg: github.com/Phixsura/attune/internal/repo
      desc: "zendeskclient is infra; must not import repo"
    - pkg: github.com/Phixsura/attune/internal/handlers
      desc: "zendeskclient is infra; must not import handlers"
    - pkg: github.com/Phixsura/attune/internal/inbound
      desc: "zendeskclient is infra; must not import inbound"
```

- [ ] **Step 9: Update all test files**

Replace all unexported type references with `zendeskclient.` prefixed exported types. `fakeAPIClient` implements `zendeskclient.Client`. Fixture tests use `zendeskclient.Ticket` etc.

- [ ] **Step 10: Run full test suite**

Run: `go test ./internal/infra/zendeskclient/... ./internal/inbound/adapter/zendesk/... ./cmd/attune/ -count=1`
Expected: all PASS

Run: `golangci-lint run --timeout=120s`
Expected: 0 issues

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "refactor(zendesk): rewire adapter to use shared zendeskclient package (#229)"
```

---

### Task 4: OAuth redesign

**Files:**
- Modify: `proto/attune/v1/inbound_source.proto` — replace OAuth fields in `ZendeskConnConfig`
- Modify: `internal/inbound/adapter/zendesk/config.go` — `parseConfig` builds `Credential` with all 4 OAuth fields
- Modify: `internal/inbound/adapter/zendesk/public.go` — `ValidateConnConfig` accepts 4 OAuth fields; `AuthTestOAuth` uses access_token
- Modify: `internal/handlers/console/inbound/inbound_create_zendesk.go` — store all 4 fields in encrypted config
- Modify: `internal/handlers/console/inbound/inbound_test_connection.go` — auth test uses access_token
- Modify: `console/src/features/inbound-sources/components/create-dialog.tsx` — ZendeskFieldset OAuth section: 4 fields
- Modify: `console/src/i18n/zh-CN.json` — new labels
- Run: `make proto`

**Interfaces:**
- Consumes: `zendeskclient.Credential` with `ClientID`, `ClientSecret`, `RefreshToken` fields
- Produces: working OAuth create → test → poll → refresh → retry cycle

- [ ] **Step 1: Update proto**

Replace `ZendeskConnConfig` fields 5-6 with:
```protobuf
optional string oauth_access_token = 5;
optional string oauth_refresh_token = 6;
optional string oauth_client_id = 7;
optional string oauth_client_secret = 8;
```

Run: `make proto`

- [ ] **Step 2: Update config.go parseConfig OAuth path**

When `AuthModeOAuth`: decrypt `OAuthTokenEncrypted`, unmarshal into `zendeskclient.OAuthToken` (which now has `ClientID` + `ClientSecret` fields), build `zendeskclient.Credential` with all fields populated.

- [ ] **Step 3: Update public.go**

`ValidateConnConfig` accepts `accessToken, refreshToken, clientID, clientSecret string` instead of `oauthClientID, oauthClientSecret`. `AuthTestOAuth` uses `accessToken` as Bearer (not clientSecret).

- [ ] **Step 4: Update handler create path**

`encryptZendeskConfig` stores all 4 OAuth values in the `OAuthToken` JSON blob before encryption.

- [ ] **Step 5: Update handler test-connection**

Auth test for OAuth calls `zendesk.AuthTestOAuth(ctx, subdomain, inputs.OAuthAccessToken)`.

- [ ] **Step 6: Update Console ZendeskFieldset**

Replace `oauthClientId` + `oauthClientSecret` fields with:
- Access Token (required, password input)
- Refresh Token (optional, password input)
- Client ID (required if refresh token provided)
- Client Secret (required if refresh token provided)

Update `ZendeskFields` interface, `defaultZendesk`, `buildBody`, `handleTest`, `isFormComplete`.

- [ ] **Step 7: Update i18n**

Replace old keys with:
```json
"oauth_access_token": "Access Token",
"oauth_refresh_token": "Refresh Token（可选）",
"oauth_client_id": "Client ID",
"oauth_client_secret": "Client Secret",
"oauth_paste_help": "在 Zendesk 管理中心创建 OAuth App，使用授权码流程获取 token，然后粘贴到这里。"
```

- [ ] **Step 8: Run tests**

Run: `go test ./internal/inbound/adapter/zendesk/... ./internal/handlers/console/inbound/... -count=1`
Run: `cd console && pnpm tsc -b --noEmit && pnpm biome check && pnpm vitest run src/features/inbound-sources/`
Expected: all PASS

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "fix(zendesk): redesign OAuth flow with paste-mode tokens (#229)"
```

---

### Task 5: Adapter fixes (idempotency, fetchComments, SyncStats)

**Files:**
- Modify: `internal/inbound/adapter/zendesk/normalize.go` — `zendeskIdempotencyKey` includes `GeneratedTimestamp`
- Modify: `internal/inbound/adapter/zendesk/poll.go` — `fetchComments` degrades, `SyncStats.LastTicketID` uses real ticket ID
- Modify: `internal/inbound/adapter/zendesk/normalize_test.go` — update key test
- Modify: `internal/inbound/adapter/zendesk/fixture_test.go` — update key assertions

**Interfaces:**
- Consumes: `zendeskclient.Ticket.GeneratedTimestamp`, `zendeskclient.Ticket.ID`
- Produces: correct idempotency keys, graceful comment degradation, accurate SyncStats

- [ ] **Step 1: Fix idempotency key**

In `normalize.go`, change `zendeskIdempotencyKey`:
```go
func zendeskIdempotencyKey(subdomain string, ticketID, generatedTimestamp int64) string {
    return "zendesk_" + sanitizeKeyPart(subdomain) + "_" +
        strconv.FormatInt(ticketID, 10) + "_" +
        strconv.FormatInt(generatedTimestamp, 10)
}
```

Update `buildIngestInput` call to pass `t.GeneratedTimestamp`.

- [ ] **Step 2: Fix fetchComments**

Replace `SetEnabled(false)` on permanent comment error with:
```go
logext.Warnf(ctx, "[%s] comments auth failed,skipping ticket,source_id:%s,ticket_id:%d,err:%s",
    where, src.ID, ticketID, cerr.Error())
a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "comment_auth_err")
return nil, res // skip ticket, don't disable source
```

- [ ] **Step 3: Fix SyncStats.LastTicketID**

In `poll.go` `syncPages`, change:
```go
// Before: cfg.SyncStats.LastTicketID = result.lastGenTS
// After:
cfg.SyncStats.LastTicketID = result.lastTicketID
```

Add `lastTicketID int64` to `pageResult`. Set it in `processTicketPage` from `t.ID`.

- [ ] **Step 4: Update tests**

Update `normalize_test.go` `TestZendeskIdempotencyKey` to expect the new 3-part format.
Update `fixture_test.go` assertions for idempotency key format.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/inbound/adapter/zendesk/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "fix(zendesk): idempotency captures updates, comments degrade gracefully (#229)"
```

---

### Task 6: Enricher Type hint

**Files:**
- Modify: `internal/repo/feedback/feedback.go` — add `Type string` to `EnrichInput`
- Modify: `internal/repo/feedback/feedback.go` — `LoadForEnrich` SQL adds `type` column
- Modify: `internal/service/enrich/enricher.go` — `classifyConfigFromRow` passes Type
- Modify: `internal/service/enrich/enricher_prompt.go` — `ClassifyConfig` gains `TypeHint`; `renderPrompt` appends hint
- Create: `internal/service/enrich/enricher_type_hint_test.go`

**Interfaces:**
- Consumes: `feedback.EnrichInput.Type`
- Produces: LLM prompt includes type hint when `Type` is non-empty

- [ ] **Step 1: Add Type to EnrichInput**

In `feedback.go` `EnrichInput` struct, add:
```go
Type string
```

In `LoadForEnrich` SQL query, add `type` to the SELECT list and Scan.

- [ ] **Step 2: Pass Type through ClassifyConfig**

In `enricher_prompt.go` `ClassifyConfig`, add:
```go
TypeHint string
```

In `enricher.go` `classifyConfigFromRow`, add:
```go
TypeHint: row.Type,
```

- [ ] **Step 3: Append hint in renderPrompt**

In `renderPrompt`, after building the base prompt, if `cfg.TypeHint != ""`:
```go
if cfg.TypeHint != "" {
    prompt += "\n\nThe submitter has pre-classified this as: " + cfg.TypeHint +
        ". Consider this hint but override if the content clearly indicates otherwise."
}
```

- [ ] **Step 4: Write test**

```go
func TestRenderPrompt_TypeHint(t *testing.T) {
    cfg := ClassifyConfig{TypeHint: "bug_report", /* ... minimal fields ... */}
    prompt := renderPrompt(cfg, "test content")
    if !strings.Contains(prompt, "bug_report") {
        t.Error("expected type hint in prompt")
    }

    cfgNoHint := ClassifyConfig{/* ... no TypeHint ... */}
    promptNoHint := renderPrompt(cfgNoHint, "test content")
    if strings.Contains(promptNoHint, "pre-classified") {
        t.Error("should not have type hint when TypeHint is empty")
    }
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/service/enrich/... ./internal/repo/feedback/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(enrich): consume IngestInput.Type as LLM classification hint (#229)"
```

---

### Task 7: Console advanced config UI

**Files:**
- Modify: `proto/attune/v1/inbound_source.proto` — add filter + budget fields to `ZendeskConnConfig`
- Modify: `console/src/features/inbound-sources/components/create-dialog.tsx` — collapsible advanced section
- Modify: `console/src/i18n/zh-CN.json` — new labels
- Modify: `internal/handlers/console/inbound/inbound_create_zendesk.go` — store filter + budget
- Run: `make proto`

**Interfaces:**
- Consumes: `Config.Filter` and `Config.MaxCommentFetches` (already exist in Go)
- Produces: Console UI inputs → proto → handler → Config persistence

- [ ] **Step 1: Update proto**

Add to `ZendeskConnConfig`:
```protobuf
repeated string filter_tags = 9;
repeated string filter_exclude_tags = 10;
repeated string filter_statuses = 11;
optional int32 max_comment_fetches = 12;
```

Run: `make proto`

- [ ] **Step 2: Update handler to store filter + budget**

In `encryptZendeskConfig`, populate `Config.Filter` and `Config.MaxCommentFetches` from proto fields.

- [ ] **Step 3: Add collapsible advanced section to ZendeskFieldset**

Add `ZendeskFields` interface fields: `filterTags`, `filterExcludeTags`, `filterStatuses`, `maxCommentFetches`. Default all to empty/50.

UI: a `<details>` or button-toggled section labeled "高级选项" containing:
- Include tags (text input, comma-separated)
- Exclude tags (text input, comma-separated)
- Status filter (4 checkboxes: open, pending, solved, closed)
- Comment budget (number input, min=1, max=200, default=50)

Update `buildBody` to include these in `zendeskConfig`.

- [ ] **Step 4: Add i18n keys**

```json
"advanced_label": "高级选项",
"filter_tags": "包含标签",
"filter_tags_help": "逗号分隔，只同步含这些标签的工单",
"filter_exclude_tags": "排除标签",
"filter_exclude_tags_help": "逗号分隔，跳过含这些标签的工单",
"filter_statuses": "状态过滤",
"max_comment_fetches": "每轮 Comment 预算",
"max_comment_fetches_help": "每次同步最多获取多少个工单的评论（默认 50）"
```

- [ ] **Step 5: Run tests**

Run: `cd console && pnpm tsc -b --noEmit && pnpm biome check && pnpm vitest run src/features/inbound-sources/`
Run: `go test ./internal/handlers/console/inbound/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(zendesk): Console advanced config for filter + comment budget (#229)"
```

---

### Task 8: CHANGELOG + proposal status + final verification

**Files:**
- Modify: `CHANGELOG.md` — update `[Unreleased]` entry
- Modify: `docs/proposals/2026/07/2026-07-24-zendesk-system-level-redesign.md` — status → Implemented
- Modify: `internal/infra/database/migrate_test.go` — update migration count if changed

**Interfaces:**
- Consumes: all previous tasks
- Produces: merge-ready branch

- [ ] **Step 1: Update CHANGELOG**

Replace the existing Zendesk entry with one that includes the system-level redesign items.

- [ ] **Step 2: Update proposal status**

Change `Status` from `Proposed` to `Implemented`.

- [ ] **Step 3: Run make ci-check**

Run: `make ci-check`
Expected: all gates green (except known flaky tests unrelated to Zendesk)

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs(zendesk): update changelog + proposal status (#229)"
```
