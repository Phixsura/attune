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

## Architecture

```
cmd/attune/main.go         blank-imports adapters (the only legal site)
  |
  v
internal/outbound/         framework root: interfaces + registry
  adapter/generic/         raw-webhook envelope + HMAC signing
  adapter/lark/            Lark interactive card + in-body signing
  adapter/slack/           Slack Block Kit + no signing (URL is secret)
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
