# Email outbound adapter

| Field | Value |
|-------|-------|
| Issue | #202 (gap #23) |
| Status | Implemented |
| Started | 2026-06-29 |
| Related | #34 outbound adapter framework |

## Problem

attune registers `DestEmail = "email"` as a destination type but has no
outbound adapter to deliver email notifications. Users wanting email
alerts must set up an external relay (Zapier → email, or a custom
webhook → email bridge). Every competitor in the feedback/VOC space
ships email notifications out of the box.

## Goals

- Deliver per-event and digest email notifications via SMTP.
- Support STARTTLS (port 587) and implicit TLS (port 465).
- Fit within the existing outbound adapter framework with minimal
  framework changes.
- SSRF-guard the SMTP host (block metadata/link-local IPs).

## Non-goals

- HTML-formatted emails (plain text is sufficient for v1).
- Multiple recipients per target (one `to` per NotifyTarget row).
- Inline attachments or rich media.

## Proposal

### Framework extension: DirectSender interfaces

The outbound framework's `Rendered.Build` returns `*http.Request`,
making it HTTP-only. Rather than refactoring the entire framework,
we add two optional interfaces:

```go
type DirectEventSender interface {
    SendEvent(ctx context.Context, envelope *Envelope, dst Target) error
}

type DirectDigestSender interface {
    SendDigest(ctx context.Context, view any, dst Target) error
}
```

The outbox worker and digest sender check for these interfaces before
falling through to the HTTP transport. This is backward-compatible —
existing HTTP adapters are unchanged.

### Email adapter

`internal/outbound/adapter/email/` implements:
- `EventChannel` + `DirectEventSender` (per-event SMTP delivery)
- `DigestChannel` + `DirectDigestSender` (digest summary SMTP delivery)
- Self-registers via `init()` with `outbound.Register()`
- Blank-imported in `cmd/attune/main.go`

Configuration lives in `Target.Config`:
- `smtp_host` (required) — SMTP server hostname
- `smtp_port` (default "587") — SMTP port
- `smtp_implicit_tls` (default false) — use TLS on connect vs STARTTLS
- `smtp_username` — SMTP auth username
- `from` (required) — sender address
- `to` — recipient (falls back to `Target.URL`)

`Target.Secret` carries the SMTP password.

### SSRF guard

Constructs `smtp://host:port` and validates via
`nethardening.Policy{AllowLoopback: true, AllowPrivate: true}.ValidateURL()`
— blocks metadata/link-local IPs, allows on-prem RFC1918 networks.

## Alternatives considered

1. **HTTP-only (email API services)** — Forces users to sign up for
   SendGrid/Mailgun. Bad for self-hosted OSS.
2. **Refactor framework to be transport-agnostic** — Much larger change
   for one adapter. DirectSender is minimal and non-breaking.
3. **Built-in HTTP-to-SMTP relay** — Unnecessary complexity.

## Risks / tradeoffs

- SMTP errors are not as structured as HTTP status codes — the adapter
  wraps them as `ErrTerminal` (auth failures) or retryable (network).
- No HTML emails yet — acceptable for v1; can be added later.

## Verification

- 20 unit tests covering config parsing, rendering, interface
  compliance, and error paths.
- `go build ./...` clean.
- `golangci-lint` clean on changed packages.
- `go test -race ./internal/outbound/... ./internal/service/outbox/... ./internal/service/digest/...` all pass.
