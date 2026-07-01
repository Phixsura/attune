# `internal/outbound` — channel-agnostic outbound delivery framework

This package is the outbound mirror of `internal/inbound`. Each delivery
channel (Slack, Lark, raw webhook, GitHub Issue, ...) is a self-registering
adapter that owns **rendering** (how a message looks on that channel) while one
shared `notify.Transport` owns **shipping** (POST with retry + backoff).

## Adding a new adapter

1. Create `internal/outbound/adapter/<channel>/` with a single Go file.
2. Define a package-level type that implements `outbound.EventChannel` and/or
   `outbound.DigestChannel`:

   ```go
   type channel struct{}

   func (c *channel) ID() string { return "<channel-id>" }

   func (c *channel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
       // Build the channel-specific HTTP request body.
       // Return Rendered{Build, Check} — Build constructs the *http.Request,
       // Check classifies the response (2xx ok, 4xx terminal, 5xx retry).
       ...
   }
   ```

3. Self-register in `init()`:

   ```go
   func init() {
       outbound.Register(ptrext.Of(channel{}))
   }
   ```

4. Blank-import in `cmd/attune/main.go` — the **only** legal import site:

   ```go
   _ "github.com/Phixsura/attune/internal/outbound/adapter/<channel>"
   ```

5. Add a `Dest<Channel>` constant to `internal/repo/notifytarget/notify_targets.go`.
6. Add the constant to `outboxDestTypes` in
   `internal/service/enrich/enricher_outbox.go` so the enricher creates outbox
   rows for the new channel.
7. Add a DB migration widening the `destination_type` CHECK constraint on
   `tenant_notify_targets`.
8. Provide a `ResponseChecker` — there is no default. Each adapter must
   explicitly classify every HTTP status it can receive, then wire the matching
   `outboundtest` response profile into conformance tests.
9. Add `conformance_test.go` in the adapter package and call
   `outboundtest.TestEventChannel` and/or `outboundtest.TestDigestChannel`.
10. Add `provider_mock_test.go` in the adapter package and use
    `outboundtest.NewProvider` to deliver a real rendered request to a local
    provider-shaped server. Use `ProviderScenario.Check` for request assertions
    so failures are captured and reported after the HTTP response.
11. Set the appropriate `ProviderShape` in conformance tests so the shared
    runner validates provider-specific payload structure.
12. Add stable request snapshots under `testdata/` and verify them without
    `ATTUNE_UPDATE_GOLDEN=1`.
13. Run `bash scripts/lint-outbound-conformance.sh` and
    `go test ./internal/outbound/...`.

## Architecture

```
cmd/attune/main.go         blank-imports adapters (the only legal site)
  |
  v
internal/outbound/         framework root: interfaces + registry
  adapter/generic/         raw-webhook envelope + HMAC signing
  adapter/lark/            Lark interactive card + in-body signing
  adapter/slack/           Slack Block Kit + no signing (URL is secret)
  adapter/discord/         Discord embeds + no signing (URL is secret)
  adapter/githubissue/     GitHub Issue creation via REST API
  |
  v
internal/notify/           shared Transport (POST + retry + backoff)
```

## Boundary rules (enforced by golangci-lint depguard)

- **`outbound-boundary`**: nothing under `service/`, `handlers/`, `repo/`,
  `infra/`, `notify/`, `domain/` may import `internal/outbound/adapter/*`.
- **`outbound-framework-isolation`**: `internal/outbound/` (root) imports
  nothing from `service/`, `repo/`, `handlers/`, `notify/`.
- Adapters import the `outbound` root only — never a sibling adapter, never
  `service/`, `repo/`, `handlers/`, or `notify/`.

## Key types

| Type | Purpose |
|---|---|
| `EventChannel` | Renders per-feedback notifications (the outbox path) |
| `DigestChannel` | Renders daily/weekly roll-ups |
| `Rendered` | `Build func` (full request control) + `Check` (response classifier) |
| `Envelope` | The v2 event payload — adapters render this into channel-specific formats |
| `Target` | Destination metadata (URL, secret, signature version) — decoupled from repo |
| `ResponseChecker` | Maps HTTP status to success / retryable / terminal |

## Conformance contract

Every adapter package must include `conformance_test.go` and call the shared
`internal/outbound/outboundtest` runner. The script
`scripts/lint-outbound-conformance.sh` enforces that shape for every package
under `internal/outbound/adapter/`. The gate requires a golden snapshot,
`ProviderShape`, and `ResponseCases` in conformance tests. Each adapter package
must also include a `provider_mock_test.go` that uses `outboundtest.NewProvider`
with `ProviderScenario.Check` so the rendered request is delivered to a
provider-shaped local HTTP server, not only inspected as a static request.

Current matrix:

| Adapter | Event | Digest | URL credential | Active mention surface | Response profile |
|---|---:|---:|---:|---:|---|
| `raw-webhook` (`generic`) | Yes | Yes | No | No | Generic webhook: 2xx success, 408/429 retryable, other 4xx terminal, 5xx retryable |
| `github-issue` | Yes | No | No | Yes | GitHub: 200/201 success, rate-limit 403 retryable, other 4xx terminal, 5xx retryable |
| `slack` | Yes | Yes | Yes | Yes | Chat webhook: 2xx success, 408/429 retryable, other 4xx terminal, 5xx retryable |
| `discord` | Yes | Yes | Yes | Yes | Chat webhook: 2xx success including 204, 408/429 retryable, other 4xx terminal, 5xx retryable |
| `lark` | Yes | Yes | Yes | Yes | Lark: `StatusCode:0` success, `9499` retryable, other provider codes terminal, HTTP 429 retryable |

### Provider shape checks

`outboundtest.ProviderShape` is the shared structural contract for provider
payloads:

| Shape | Required payload contract |
|---|---|
| `ProviderShapeRawWebhook` | JSON object with `event_type` and `tenant_id`. |
| `ProviderShapeSlack` | Slack Block Kit payload with a non-empty `blocks` array and typed block objects. |
| `ProviderShapeDiscord` | Discord payload with non-empty `embeds` and `allowed_mentions.parse: []`. |
| `ProviderShapeLark` | Interactive card payload with `msg_type: interactive`, card header title, elements, and string note content; signed targets include `timestamp` and `sign`. |
| `ProviderShapeGitHubIssue` | Issue payload with non-empty `title`, `body`, and safe labels capped to provider limits. |

The shape check runs before golden snapshot comparison. This catches provider
schema drift even when a snapshot would still look plausible to a human reviewer.

### Fixture snapshots

Stable request snapshots live under each adapter's `testdata/` directory:

```
internal/outbound/adapter/<channel>/testdata/event_request.json
internal/outbound/adapter/<channel>/testdata/digest_request.json
```

The snapshot normalizes dynamic values before comparison:

- `Authorization` headers become `<authorization>`
- signatures become `<signature>`
- generated timestamps become `<timestamp>`
- URLs are host-only (`scheme://host`) so webhook path tokens are never stored

Update snapshots only when intentional rendering drift is part of the change:

```sh
ATTUNE_UPDATE_GOLDEN=1 go test ./internal/outbound/...
go test ./internal/outbound/...
```

### Redaction and mention safety

Slack, Lark, and Discord incoming-webhook URLs carry credentials in the URL path.
Adapters for those channels must log only `nethardening.RedactURL(dst.URL)`.
GitHub adapters must never log the generated issue body because it contains
customer feedback. Raw webhook may preserve the envelope in the request body, but
it logs only the byte count.

Adapters that render into active mention surfaces must neutralize
user-controlled mention syntax. Slack escapes mrkdwn control tokens, Discord
sets `allowed_mentions.parse` to an empty list, Lark escapes `lark_md` tag
syntax, and GitHub Issue neutralizes user-controlled `@` tokens in issue title
and body fields.

### Provider retry hints

`notify.Transport` owns retry timing. Adapters still classify provider responses
as success, retryable, or terminal through `ResponseChecker`; when a retryable
response also includes a valid `Retry-After` header, the transport uses that
delay for the next attempt and clamps it to `RetryPolicy.MaxDelay` when a max is
configured. Terminal responses never sleep or retry, even if the provider sends
`Retry-After`.

## Observability

Transport-level delivery metrics are emitted for every adapter path:

| Metric | Labels | Use |
|---|---|---|
| `attune_outbound_delivery_attempts_total` | `destination_type`, `result`, `status` | Attempt-level provider response accounting. Use `result=retryable` with status to identify throttling or transient provider errors. |
| `attune_outbound_delivery_duration_seconds` | `destination_type`, `result` | End-to-end `Transport.Send` duration, including retry waits. Use the p95 split to spot slow providers. |
| `attune_outbound_retry_after_total` | `destination_type` | Provider responses where `Retry-After` was honored by the transport. |
| `attune_notify_failures_total` | `destination_type`, `reason` | Outbox-level failure accounting for transport vs terminal dead-letter causes. |
| `attune_outbox_lag_seconds` | none | Oldest pending delivery age. |
| `attune_outbox_dead_rows` | none | Terminal dead-letter queue depth. |

`destination_type` is bounded to `raw-webhook`, `slack`, `discord`, `lark`,
`github-issue`, or `other`. Do not add URLs, tenant ids, webhook ids, or raw
error text as metric labels.

## Provider troubleshooting

| Provider | Common status/body | Meaning | First checks |
|---|---|---|---|
| `raw-webhook` | `2xx` | Delivery accepted. | Confirm consumer signature validation and idempotency key handling. |
| `raw-webhook` | `400`/`401`/`403` | Terminal consumer rejection. | Check configured URL, HMAC secret, and consumer payload schema. |
| `raw-webhook` | `408`/`429`/`5xx` | Retryable consumer or network pressure. | Inspect `Retry-After`, consumer rate limits, and outbox lag. |
| `slack` | `200 ok` | Delivery accepted. | Confirm the sandbox channel received the Block Kit message. |
| `slack` | `400 invalid_payload` | Terminal payload/schema rejection. | Run conformance tests and inspect Block Kit structure. |
| `slack` | `404`/`410` | Terminal webhook URL issue. | Rotate the incoming webhook URL. |
| `discord` | `204` | Delivery accepted. | Confirm the sandbox channel received the embed. |
| `discord` | `400` | Terminal payload/schema rejection. | Check embed limits and `allowed_mentions.parse`. |
| `discord` | `429` | Retryable rate limit. | Confirm `Retry-After` is being honored and reduce delivery burst size. |
| `lark` | HTTP `200` with `StatusCode:0` | Delivery accepted. | Confirm the sandbox chat received the card. |
| `lark` | HTTP `200` with non-zero code other than `9499` | Terminal provider rejection. | Check webhook URL, optional secret, card shape, and tenant permissions. |
| `lark` | `9499` or HTTP `429` | Retryable provider pressure. | Inspect `Retry-After` and provider rate limits. |
| `github-issue` | `201` | Issue created. | Confirm labels and issue body are acceptable for the target repository. |
| `github-issue` | `401`/`404`/non-rate-limit `403` | Terminal auth/repository rejection. | Check token scopes, repository URL, and org policy. |
| `github-issue` | rate-limit `403`/`429`/`5xx` | Retryable provider pressure. | Inspect GitHub rate-limit headers and delivery burst size. |

## Live smoke tests

Provider live tests live under `test/live/outbound/` and require the `live` build
tag. They are skipped unless their env vars are set:

```sh
make test-live-list
go test -tags=live -count=1 -run '^TestLive_Outbound' ./test/live/outbound
```

Supported env vars:

- `E2E_OUTBOUND_RAW_WEBHOOK_URL` and optional `E2E_OUTBOUND_RAW_WEBHOOK_SECRET`
- `E2E_OUTBOUND_SLACK_WEBHOOK_URL`
- `E2E_OUTBOUND_DISCORD_WEBHOOK_URL`
- `E2E_OUTBOUND_LARK_WEBHOOK_URL` and optional `E2E_OUTBOUND_LARK_SECRET`
- `E2E_OUTBOUND_GITHUB_REPO_URL`, `E2E_OUTBOUND_GITHUB_TOKEN`, and
  `E2E_OUTBOUND_GITHUB_CREATE_ISSUE=1`

Use sandbox webhooks and repositories. The GitHub test creates one issue through
the adapter and closes it during cleanup.
