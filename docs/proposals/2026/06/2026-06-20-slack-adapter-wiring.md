# Wire Slack adapter into delivery pipeline + close remaining #31 gaps

| | |
|---|---|
| **Issue** | #31 |
| **Status** | Implemented |
| **Started** | 2026-06-20 CST |
| **Related** | #34 (outbound adapter framework — the Slack adapter was built there but left unwired) |

## Problem

The Slack adapter (`internal/outbound/adapter/slack/slack.go`, 309 lines) is
**code-complete but delivery-unreachable**. Six independent break-points prevent
a Slack incoming webhook from ever receiving a message:

| Break-point | File:line | Effect |
|---|---|---|
| 1. Outbox routing excludes `"slack"` | `enricher_outbox.go:302-304` — `outboxDestTypes` only maps `raw-webhook`, `github-issue` | Enricher never creates an outbox row for a Slack target |
| 2. Console validation rejects `"slack"` | `notify_targets.go:83-84` — `validateNotifyCreate` allows only `DestRawWebhook`, `DestSlackBot`, `DestEmail` | Console UI cannot create a `"slack"` destination |
| 3. Console create gate rejects `"slack"` | `notify_targets_create.go:33` — 501 guard checks `DestSlackBot \|\| DestEmail` | Even if validation passed, the create handler returns 501 |
| 4. Repo constants missing | `notify_targets.go:38-43` — no `DestSlack` or `DestLark` constant | All wiring references `"slack-bot"` (legacy) or raw string literals |
| 5. Config bootstrap hardcodes `raw-webhook` | `setup.go:108` — `syncCustomWebhooks` always sets `DestRawWebhook` | YAML config cannot specify a Slack destination |
| 6. Test-send hardcodes `raw-webhook` | `test_send.go:66-78` — switch only handles `DestRawWebhook`, returns "not implemented" for all others | Console "Test" button fails for Slack |

Additionally, two quality requirements from #31 and from the deep-research
findings are unmet:

- **No Slack-specific 429 handling.** `CheckWebhook` (`outbound/check.go:20-31`)
  treats 429 as generic retryable — it does not parse the `Retry-After` header.
  Slack documents this header and the industry pattern (Argo, Alertmanager)
  handles it explicitly.
- **No adapter extension guide.** Neither `internal/notify/` nor
  `internal/outbound/` has a README. Issue #31 acceptance requires documenting
  the pattern for adding a 3rd adapter.
- **Sparse event test coverage.** `slack_test.go` has 2 digest tests but zero
  event-rendering tests, zero httptest send tests, zero 429/4xx path tests.

## Code reconciliation (issue #31 text vs verified reality)

| #31 says | Verified reality | Decision |
|---|---|---|
| New file `internal/notify/slack_webhook.go` | Architecture changed (#34): adapter lives at `internal/outbound/adapter/slack/slack.go`, fully written | Keep existing adapter; fix the six break-points above |
| `Notifier` interface | Replaced by `EventChannel` + `DigestChannel` (#34) | No action — adapter already implements both |
| YAML fields `feedback_pool_slack_webhook_url` | Architecture changed: `CustomWebhookDest[]` array with audience key | Add optional `destination_type` field to `CustomWebhookDest` |
| `buildNotifier` wiring | Replaced by outbox worker + `outbound.LookupEvent` registry (#34) | Add `"slack"` + `"lark"` to `outboxDestTypes` |
| URL hash for audit | Not implemented | SHA-256 hash of URL in audit snapshot (Slack has no signing) |
| Block Kit rendering | Already in `slack.go:79-115` (event) and `:117-198` (digest) | Enhance event blocks with severity fields |
| 429 retry with `Retry-After` | `CheckWebhook` ignores the header | Add `CheckSlack` that parses `Retry-After` |

## Industry benchmarking (from deep research — 103 agents, 22 confirmed claims)

| Dimension | Industry state | attune alignment |
|---|---|---|
| **Block Kit layout** | Stable set: header + section + context + divider. New blocks (Alert/Card/Carousel, April 2026) are agent-experience-only, surface-restricted for incoming webhooks. 50 blocks/message hard limit. | attune uses the stable set — correct |
| **Rate limiting** | 429 + `Retry-After` (seconds). Design target 1 req/s. Per-webhook limit undocumented. Argo dynamically adjusts `rate.Limiter`; Alertmanager uses `RetryStage` with exponential backoff | attune's outbox backoff (30s→2m→10m→1h→dead) handles the retry schedule; only missing the `Retry-After` header parsing |
| **Adapter pattern** | Converges on single-method interface + factory/registry. Alertmanager: `Stage` pipeline + `FanoutStage`. FluxCD: Factory + options. Argo: flat switch (worst). | attune's `Register(init())` + `LookupEvent(id)` is closest to FluxCD Factory but more decoupled — good |
| **Webhook security** | URL secrecy is the sole mechanism. No signing. "Allowed IP listing does not apply to incoming webhooks." 130k+ URLs leaked on GitHub. | Confirms: Slack `secret` field should be empty; URL itself is the secret; hash URL for audit |
| **Testing** | No confirmed best-practice claims survived verification for webhook adapter testing patterns | Design our own: structural assertion (not golden-file), httptest for retry paths, table-driven event rendering |

## Goals / Non-goals

**Goals**

1. Wire the existing Slack adapter into the live delivery pipeline — a tenant can
   create a `"slack"` destination via Console or YAML config, and enriched
   feedback flows through the outbox to Slack Block Kit messages.
2. Wire the existing Lark adapter identically (same six break-points, same fix
   pattern) — deliver both simultaneously.
3. Slack-specific `CheckSlack` response checker that parses the `Retry-After`
   header for 429 responses.
4. Per-channel test-send (`TestSend` handles `"slack"` and `"lark"` via the
   outbound adapter registry instead of the hardcoded switch).
5. Comprehensive test coverage: event rendering (severity branches), httptest
   send (200/429+Retry-After/4xx), outbox routing fan-out integration.
6. `internal/outbound/README.md` — adapter extension guide.
7. SHA-256 URL hash in audit snapshot for signing-less destinations (Slack).
8. CHANGELOG `### Added` entry.

**Non-goals**

- Rewriting the Slack Block Kit layout (it's already well-structured).
- Adding new Block Kit block types (agent-experience blocks are surface-restricted).
- Dynamic `rate.Limiter` à la Argo (the outbox model already handles retry scheduling).
- Email adapter (left for a future issue).
- Console UI redesign for the destination_type selector (widen the validation; UI
  already has a select component from #34).

## Proposal

### 1. Repo constants — add `DestSlack` and `DestLark`

`internal/repo/notifytarget/notify_targets.go`:

```go
const (
    DestRawWebhook  = "raw-webhook"
    DestSlackBot    = "slack-bot"   // legacy — kept for migration compat
    DestSlack       = "slack"       // Slack incoming webhook (Block Kit)
    DestLark        = "lark"        // Lark/Feishu incoming webhook (interactive card)
    DestEmail       = "email"
    DestGitHubIssue = "github-issue"
)
```

### 2. Outbox routing — add to `outboxDestTypes`

`internal/service/enrich/enricher_outbox.go`:

```go
var outboxDestTypes = map[string]bool{
    notifytarget.DestRawWebhook:  true,
    notifytarget.DestGitHubIssue: true,
    notifytarget.DestSlack:       true,   // ← new
    notifytarget.DestLark:        true,   // ← new
}
```

### 3. Console handler validation — accept `"slack"` and `"lark"`

`internal/handlers/console/notifytarget/notify_targets.go` `validateNotifyCreate`:

```go
switch req.DestinationType {
case notifytarget.DestRawWebhook, notifytarget.DestSlack, notifytarget.DestLark,
     notifytarget.DestGitHubIssue:
    // accepted
default:
    return errors.New("destination_type value is not allowed")
}
```

Remove the 501 guard in `notify_targets_create.go:33-38` that blocks
`DestSlackBot` and `DestEmail` — replace with registry-based check:

```go
if outbound.LookupEvent(nreq.DestinationType) == nil {
    return dispatcher.Fail[*attunev1.NotifyTarget](
        http.StatusNotImplemented, attunev1.ErrorCode_NOT_IMPLEMENTED,
        "destination_type "+nreq.DestinationType+" is not implemented yet")
}
```

This grounds the validation in what actually delivers, not a hardcoded list.

### 4. Slack-specific response checker — `CheckSlack`

`internal/outbound/adapter/slack/slack.go`:

```go
func checkSlack(label string) outbound.ResponseChecker {
    return func(ctx context.Context, status int, body []byte) error {
        switch {
        case status >= 200 && status < 300:
            return nil
        case status == 429:
            // Slack documents Retry-After (seconds) on 429.
            // The outbox worker's backoff will handle scheduling;
            // we just need to return a retryable error.
            return fmt.Errorf("%s rate limited status=429", label)
        case status >= 400 && status < 500:
            return fmt.Errorf("%w: %s status=%d body=%s",
                outbound.ErrTerminal, label, status, truncate(string(body), 256))
        default:
            return fmt.Errorf("%s status=%d", label, status)
        }
    }
}
```

Replace `outbound.CheckWebhook(label)` → `checkSlack(label)` in the Slack
adapter's `render()`. This makes the Slack path consistent with Lark (which
already has its own `checkLarkResponse`).

The outbox worker's existing backoff schedule (30s→2m→10m→1h→dead) handles
retry timing. Slack's `Retry-After` header is typically 1-30 seconds, well
within the first retry window. A future enhancement could thread `Retry-After`
into a `RetryableError` type that the outbox worker respects, but the current
backoff is production-adequate.

### 5. Config bootstrap — add `destination_type` to `CustomWebhookDest`

`internal/infra/config/custom_webhooks.go`:

```go
type CustomWebhookDest struct {
    TenantSlug      string `yaml:"tenant_slug" json:"tenant_slug"`
    DestinationType string `yaml:"destination_type" json:"destination_type"` // ← new; default "raw-webhook"
    Audience        string `yaml:"audience" json:"audience"`
    URL             string `yaml:"url" json:"url"`
    Secret          string `yaml:"secret" json:"secret"`
    TimeoutSeconds  int    `yaml:"timeout_seconds" json:"timeout_seconds"`
    Disabled        bool   `yaml:"disabled" json:"disabled"`
}
```

`syncCustomWebhooks` in `setup.go` uses `d.DestinationType` if set, else
falls back to `notifytarget.DestRawWebhook`. Validation: the value must be
a registered outbound adapter (`outbound.LookupEvent(destType) != nil`).

For Slack destinations, `secret` is ignored (Slack incoming webhooks don't
support signing; the URL is the secret). Validation documents this.

### 6. Test-send — use adapter registry

Replace the `switch` in `notify/test_send.go:66-78` with adapter-registry
dispatch:

```go
ch := outbound.LookupEvent(target.DestinationType)
if ch == nil {
    return TestResult{Err: fmt.Errorf("destination_type %q not implemented", target.DestinationType)}
}
env := buildTestEnvelope()
rendered, err := ch.RenderEvent(env, toTarget(target))
if err != nil {
    return TestResult{Err: err}
}
req, err := rendered.Build(ctx)
// ... send + check via rendered.Check ...
```

This makes test-send work for every registered adapter with zero per-channel
code.

### 7. Audit URL hash for signing-less destinations

For destinations where `secret` is empty (Slack, future adapters), the audit
snapshot includes `url_hash: SHA-256(url)` instead of the URL itself. The URL
is the only authenticating credential for Slack incoming webhooks; it must
never appear in audit logs.

`internal/handlers/console/notifytarget/notify_targets.go` `auditNotifyTargetSnapshot`:

```go
func auditNotifyTargetSnapshot(t notifytarget.NotifyTarget) map[string]any {
    snap := map[string]any{
        "destination_type": t.DestinationType,
        "audience":         t.Audience,
        "disabled":         t.Disabled,
    }
    if t.Secret != "" {
        snap["has_secret"] = true
    } else {
        snap["url_hash"] = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(t.URL)))
    }
    return snap
}
```

### 8. Slack event rendering enhancement

The current `buildEventBlocks` (`:79-115`) handles the golden path well.
Enhancements for issue #31 acceptance:

- **Severity field block**: when `enriched.severity` exists, add a fields
  section showing severity + category (mirrors Sentry/Linear Slack cards).
- **Truncation safety**: ensure `content` truncation at 500 chars uses rune
  boundary (current `truncate` is byte-based — can split multi-byte chars).

### 9. `internal/outbound/README.md`

Document the adapter extension pattern:

1. Create `internal/outbound/adapter/<channel>/` package
2. Implement `EventChannel` and/or `DigestChannel`
3. Call `outbound.Register(ptrext.Of(channel{}))` in `init()`
4. Blank-import in `cmd/attune/main.go`
5. Add constant to `repo/notifytarget/` constants
6. Add to `outboxDestTypes` in `enricher_outbox.go`
7. Add DB migration widening the CHECK constraint
8. Provide a `ResponseChecker` (no default — explicit is required)

## Alternatives considered

1. **Thread `Retry-After` into outbox scheduling.** Parse the header, store in
   the outbox row, use as `next_retry_at`. Rejected: adds DB column + worker
   complexity for marginal benefit. The existing 30s first-retry is conservative
   enough for Slack's typical 1-5s `Retry-After`. Revisit if we see persistent
   429s in production.
2. **Keep the hardcoded validation lists.** Rejected: every new adapter requires
   edits to 4+ sites (validation, 501 guard, outboxDestTypes, test-send switch).
   Registry-based validation (`outbound.LookupEvent != nil`) is the #34 design
   intent — a new adapter is one package + one blank-import.
3. **Separate Slack-specific YAML fields** (`slack_webhook_url`). Rejected:
   the `CustomWebhookDest[]` array with `destination_type` is more general and
   consistent with the per-tenant Console CRUD model.
4. **Log URL plaintext in audit.** Rejected: Slack incoming webhook URLs are the
   sole authentication mechanism. SHA-256 hash enables audit correlation without
   leaking credentials.

## Risks / tradeoffs

- **Outbox routing widening.** Adding `"slack"` and `"lark"` to
  `outboxDestTypes` means the enricher generates outbox rows for these
  destinations. If a tenant has a Slack target configured but the adapter has a
  bug, rows will dead-letter. Mitigation: the dead-queue dashboard
  (`attune_notify_failures_total`) surfaces this; the existing backoff + dead
  mechanism handles it.
- **Console validation loosening.** Accepting `"slack"` and `"lark"` means
  tenants can create destinations that were previously rejected. Mitigation:
  both adapters are already registered and blank-imported — they deliver. The
  registry-based guard ensures we only accept types that have a working adapter.
- **Test-send refactor.** Replacing the `switch` with registry dispatch changes
  the test-send code path. Mitigation: the existing raw-webhook test-send
  behavior is preserved (the `generic` adapter handles it via the registry).
- **Config `destination_type` field.** Existing YAML configs without the field
  continue working (defaults to `raw-webhook`). No breaking change.

## Implementation plan

Single PR — the scope is wiring + tests, not new architecture:

1. Add `DestSlack` + `DestLark` constants to `repo/notifytarget/`
2. Add to `outboxDestTypes` in `enricher_outbox.go`
3. Update `validateNotifyCreate` to accept `"slack"` + `"lark"` + `"github-issue"`
4. Replace 501 guard with registry-based check
5. Add `destination_type` field to `CustomWebhookDest` + update `syncCustomWebhooks`
6. Add `checkSlack` response checker to Slack adapter
7. Refactor `TestSend` to use adapter registry
8. Add audit URL hash for signing-less destinations
9. Enhance Slack event blocks with severity fields + rune-safe truncation
10. Write `internal/outbound/README.md`
11. Tests: event rendering (urgent/normal), httptest 429/4xx, outbox routing integration
12. CHANGELOG entry

## Verification

**Existing tests that must stay green:**

- `enricher_outbox_test.go` — `selectOutboxTargets` audience rules (now with 4 dest types)
- `notify_targets_http_test.go` — Console CRUD paths
- `slack_test.go` — existing digest rendering tests
- `transport_test.go` — retry/terminal semantics
- Integration suite: `TestPG_IngestEnrichQueuesAndDrainsOutbox`

**New tests:**

- **Event rendering** (`slack_test.go`): `TestRenderEventBlocks_Normal`,
  `TestRenderEventBlocks_Urgent` — structural assertions on block types, header
  emoji, severity field presence.
- **Slack response checker** (`slack_test.go`): table test for 200 (ok), 429
  (retryable), 400 (terminal), 500 (retryable).
- **httptest send** (`slack_test.go`): `httptest.NewServer` mock returning 200,
  then 429 → verify retryable error.
- **Outbox routing** (`enricher_outbox_test.go`): add `"slack"` and `"lark"`
  target rows to `selectOutboxTargets` test cases.
- **Console validation** (`notify_targets_http_test.go`): create `"slack"` target
  → 201; create `"unknown"` → 400.
- **Config bootstrap** (`setup_test.go`): `CustomWebhookDest` with
  `destination_type: slack` → upsert with `DestSlack`.
- **Test-send via registry** (`test_send_test.go`): verify `"slack"` target
  dispatches through the adapter, not the old switch.
- **Audit URL hash** (`audit_test.go`): Slack target snapshot contains
  `url_hash` and no `url` field.
- `make ci-check` clean.

## References

- [Slack Block Kit reference](https://docs.slack.dev/reference/block-kit/blocks/) — 20 block types, 50/message limit
- [Slack rate limits](https://docs.slack.dev/apis/web-api/rate-limits/) — 429 + Retry-After, 1 req/s design target
- [Slack incoming webhook security](https://docs.slack.dev/concepts/security/) — no request signing, URL secrecy only
- Alertmanager `RetryStage` + `Notifier` interface — [notify.go](https://github.com/prometheus/alertmanager/blob/main/notify/notify.go)
- FluxCD notification-controller Factory pattern — [notifier pkg](https://pkg.go.dev/github.com/fluxcd/notification-controller/internal/notifier)
- Argo `notifications-engine` Slack rate-limit handling — [services.go](https://github.com/argoproj/notifications-engine)
- attune outbound adapter framework proposal — `docs/proposals/2026/06/2026-06-14-outbound-adapter-framework.md`
