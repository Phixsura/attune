# Slack inbound adapter

| Field | Value |
|-------|-------|
| Issue | #202 (gap #1) |
| Status | Implemented |
| Started | 2026-06-29 |
| Related | #66 inbound adapter framework |

## Problem

attune's inbound framework supports webhook and email channels. Slack is
the most requested feedback source — teams discuss product issues in
Slack channels daily, but that signal never reaches the feedback pipeline.
Every competitor in the space offers Slack integration.

## Goals

- Receive Slack Events API webhooks (message, app_mention, reaction_added).
- Verify request signatures using Slack's HMAC-SHA256 protocol.
- Handle url_verification challenge during Slack app setup.
- Filter bot messages to avoid feedback loops.
- Fit within the existing inbound adapter framework with zero framework changes.

## Non-goals

- Interactive Slack commands or shortcuts.
- Slack Socket Mode (pull instead of push).
- Two-way Slack conversations (reply from attune → Slack).

## Proposal

### Push-mode adapter

`internal/inbound/adapter/slack/` implements `inbound.Adapter`:
- `Channel() = "slack"`
- `Start()` mounts `POST /v1/inbound/slack/{tenant-slug}/{source-slug}`
- `Shutdown()` is a no-op (push mode, no goroutines)
- Implements `ShutdownTimeouter` returning 0

### Request flow

1. Read + cap body (64 KiB)
2. Look up source by tenant/channel/source slugs
3. Decrypt signing secret from encrypted config
4. Verify Slack signature: `v0=HMAC-SHA256(secret, "v0:timestamp:body")`
5. Reject timestamps older than 5 minutes (replay protection)
6. Route by `type`:
   - `url_verification` → echo challenge token
   - `event_callback` → extract content, ingest

### Content extraction

| Event type | Content |
|-----------|---------|
| message | `event.text` |
| app_mention | `event.text` |
| reaction_added | `:emoji: reaction on message` |

Bot messages (`event.bot_id` present) are silently dropped.

### Config encryption

Two-layer, matching the webhook adapter pattern:
- Outer: JSON envelope encrypted via `SecretStore`
- Inner: `signing_secret_encrypted` field encrypted via `SecretStore`

## Verification

- 22 unit tests including 6 conformance contract tests
- URL verification, message/mention/reaction events, bot filtering,
  signature verification, timestamp expiry, unknown source, config parsing
- `golangci-lint` clean, `go test -race` clean
