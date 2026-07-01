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
   explicitly classify every HTTP status it can receive.
9. Add `conformance_test.go` in the adapter package and call
   `outboundtest.TestEventChannel` and/or `outboundtest.TestDigestChannel`.
10. Add stable request snapshots under `testdata/` and verify them without
    `ATTUNE_UPDATE_GOLDEN=1`.
11. Run `bash scripts/lint-outbound-conformance.sh` and
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
under `internal/outbound/adapter/`.

Current matrix:

| Adapter | Event | Digest | URL credential | Active mention surface | Response profile |
|---|---:|---:|---:|---:|---|
| `raw-webhook` (`generic`) | Yes | Yes | No | No | Generic webhook: 2xx success, 408/429 retryable, other 4xx terminal, 5xx retryable |
| `github-issue` | Yes | No | No | Yes | GitHub: 200/201 success, rate-limit 403 retryable, other 4xx terminal, 5xx retryable |
| `slack` | Yes | Yes | Yes | Yes | Chat webhook: 2xx success, 408/429 retryable, other 4xx terminal, 5xx retryable |
| `discord` | Yes | Yes | Yes | Yes | Chat webhook: 2xx success including 204, 408/429 retryable, other 4xx terminal, 5xx retryable |
| `lark` | Yes | Yes | Yes | Yes | Lark: `StatusCode:0` success, `9499` retryable, other provider codes terminal, HTTP 429 retryable |

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
