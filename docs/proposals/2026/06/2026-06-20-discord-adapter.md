# Add Discord webhook outbound adapter

| | |
|---|---|
| **Issue** | #32 |
| **Status** | Accepted |
| **Started** | 2026-06-20 CST |
| **Related** | #31 (Slack adapter wiring — the pattern this copy-adapts), #34 (outbound adapter framework), #95 (registry-driven source names); benchmarking in [`docs/research/2026-06-20-voc-landscape.md`](../../../research/2026-06-20-voc-landscape.md) |

## Problem

Discord is the second-most-requested outbound channel for OSS / indie / gaming /
developer-tool communities. attune already delivers to raw-webhook, GitHub Issue,
Lark, and (as of #31) Slack through the `internal/outbound` adapter framework.
Discord is a clean "add one more adapter" against that now-established framework —
**not** the break-point-fixing exercise #31 was.

## Strategic framing (from the VoC landscape research)

The deep-research benchmark ([`docs/research/2026-06-20-voc-landscape.md`](../../../research/2026-06-20-voc-landscape.md),
103 agents / 21 verified claims) is explicit that **outbound channel count is not
where the category's moat lives** — the verified moats are inbound ingest breadth
(Enterpret 50+ sources), adaptive taxonomy, and cross-channel semantic dedup.
Conclusion #1 of that research: *"More outbound channels will never catch Enterpret; the real gap is inbound sources."*

So Discord is scoped here as **parity / common-channel coverage**, deliberately
low-cost (≈1 person-day, copy-adapt from Slack), and explicitly **not** a place to
over-invest. The strategic outbound differentiator attune already owns is the
`draft-reply` content the envelope carries (a clean category whitespace per the
research) — Discord should *surface* that, not add new outbound machinery.

## Code reconciliation (issue #32 text vs verified reality)

Issue #32 was written before #34/#31 landed; several of its assumptions are stale.

| #32 says | Verified reality (code-checked 2026-06-20) | Decision |
|---|---|---|
| New file `internal/notify/discord_webhook.go` | Adapters live at `internal/outbound/adapter/<channel>/`; `internal/notify` is now shipping-only (Transport) | New pkg `internal/outbound/adapter/discord/discord.go` |
| Implement `Notifier` interface | Replaced by `EventChannel` + `DigestChannel` ([outbound.go:22-32](../../../../internal/outbound/outbound.go)) | Implement both (mirror Slack) |
| Wire through `buildNotifier` | Replaced by `outbound.Register` (init) + `outbound.LookupEvent` registry + outbox worker | Self-register; blank-import in `cmd/attune/main.go` |
| **"Color-code by severity: P0 red, P1 orange, P2 yellow, P3 gray"** | **Default severity taxonomy is `critical`/`major`/`minor`** ([semantic_pack.go:40-45](../../../../internal/domain/semantic_pack.go)), and it is **tenant-configurable / metadata-driven** — P0–P3 do not exist | Map by severity *string* with a gray fallback for unknown/custom values; `is_urgent` forces red. See "Color mapping" below |
| HTTP send tests with `Retry-After-Ms` | Discord returns 429 + `retry_after` (JSON body, seconds) + `Retry-After` header; the outbox backoff (30s→2m→10m→1h→dead) already owns retry *scheduling* | `checkDiscord` classifies 429/408 retryable, 4xx terminal; we do **not** parse the precise delay (backoff owns it), matching the Slack decision in #31 |
| TestSend switch needs a Discord case | TestSend is now **registry-driven** ([test_send.go:46](../../../../internal/notify/test_send.go) `outbound.LookupEvent`) | **No TestSend change** — Discord's "Test" button works automatically once registered. One fewer wiring point than #31 |

## Industry benchmarking (Discord-specific, code-relevant)

| Dimension | Discord platform reality | attune design |
|---|---|---|
| **Endpoint** | `POST <webhook-url>`; success is **`204 No Content`** (not 200) | `checkDiscord` accepts any 2xx (204 included) — no special-casing needed |
| **Payload** | `embeds[]` (≤10); embed = `title` + `description` + `color` (decimal int) + `fields[]` + `footer` + `timestamp`. Optional top-level `content` | One embed per event; digest uses one embed with fields. Reuse envelope mapping from Slack's dual-path probe |
| **Hard limits** | title 256, description 4096, field name 256 / value 1024, footer 2048, ≤25 fields, **6000 total chars/embed** | Rune-safe `truncate` per field (reuse Slack's helper shape); cap fields |
| **Mention injection** | Embeds **do not** trigger `@everyone`/`@here` pings by default; only top-level `content` does | We render into embeds (already safe) **and** set `allowed_mentions:{parse:[]}` as belt-and-suspenders — the Discord analog of Slack's `escapeMrkdwn` |
| **Security** | Webhook URL contains the token in its path → URL *is* the credential; no request signing | `secretOptionalDestTypes += discord`; redact URL in logs via `nethardening.RedactURL`; hash URL for audit (same as Slack) |

## Goals / Non-goals

**Goals**
- A `discord` destination type, delivery-reachable end-to-end (enrich → outbox → Transport → Discord), for both per-event and daily-digest paths.
- Severity-string → embed color mapping with a safe fallback for tenant-custom taxonomies; `is_urgent` override.
- Rate-limit-aware retry (429/408 retryable, 4xx terminal) via a Discord `ResponseChecker`.
- No-mention guarantee (`allowed_mentions:{parse:[]}`) and embed-limit-safe truncation.
- Full test coverage: event/digest embed structure, httptest send (204/429/4xx), config disable/typo-guard, three-channel e2e (Discord + Slack + Lark all registered).

**Non-goals**
- Discord bot / slash-command / gateway integration (this is incoming-webhook only).
- Inbound Discord ingest (that is the strategically valuable direction — separate issue, not #32).
- Parsing the precise `retry_after` delay (the outbox backoff owns retry timing).
- Threading / file attachments / interactive components.

## Proposal

### New adapter — `internal/outbound/adapter/discord/discord.go`

Mirror `slack.go`'s shape: `channel struct{}` implementing `EventChannel` +
`DigestChannel`, `init()` → `outbound.Register(ptrext.Of(channel{}))`,
`channelID = "discord"`, a `render` helper returning `outbound.Rendered{Build, Check}`.

**Color mapping** (the one genuinely new piece of logic):

```go
// severityColor maps a severity taxonomy value to a Discord embed color
// (decimal RGB). Unknown / tenant-custom values fall back to gray so a
// reconfigured taxonomy never breaks rendering. is_urgent overrides to red.
func severityColor(severity string, isUrgent bool) int {
    if isUrgent {
        return colorRed
    }
    switch strings.ToLower(strings.TrimSpace(severity)) {
    case "critical": return colorRed     // 0xE01E5A
    case "major":    return colorOrange  // 0xE8912D
    case "minor":    return colorYellow  // 0xECB22E
    default:         return colorGray    // 0x9AA0A6
    }
}
```

Severity/category extraction reuses Slack's dual-path probe
(`extractSeverityCategory`, [slack.go:397-412](../../../../internal/outbound/adapter/slack/slack.go)):
outbox nests these in `feedback.enriched.attrs`, TestSend puts them at top level.

**Response checker** — `checkDiscord(label)` mirrors `checkSlack`: 2xx → nil,
408/429 → retryable error, 4xx → `outbound.ErrTerminal`, else generic.

### Wiring (7 points — one fewer than #31, TestSend is now automatic)

1. **Repo constant** — `DestDiscord = "discord"` in [notify_targets.go:38-45](../../../../internal/repo/notifytarget/notify_targets.go).
2. **Outbox routing** — add `notifytarget.DestDiscord: true` to `outboxDestTypes` ([enricher_outbox.go:302-307](../../../../internal/service/enrich/enricher_outbox.go)).
3. **Config validation** — add `"discord"` to `validDestTypes` **and** `secretOptionalDestTypes` ([custom_webhooks.go:34-46](../../../../internal/infra/config/custom_webhooks.go)); update the error-message allow-list string.
4. **Console CRUD validation** — add `notifytarget.DestDiscord` to the `validateNotifyCreate` switch ([notify_targets.go:85-90](../../../../internal/handlers/console/notifytarget/notify_targets.go)).
5. **DB migration** — `058_discord_dest_type.sql`: `DROP … IF EXISTS` + `ADD CONSTRAINT … CHECK (destination_type IN (… , 'discord'))` (idempotent, keep all existing values), following [033_lark_slack_dest_types.sql](../../../../internal/infra/database/migrations/033_lark_slack_dest_types.sql).
6. **Blank-import** — add `_ ".../internal/outbound/adapter/discord"` to [cmd/attune/main.go:40-43](../../../../cmd/attune/main.go) (the only legal site).
7. **Console frontend + docs** — add `discord` to the destination-type select in `console/` (if a static option list exists) and document `destination_type: discord` in `config.example.yaml`.

**Verification of "no TestSend change":** [test_send.go:46](../../../../internal/notify/test_send.go)
already dispatches via `outbound.LookupEvent(target.DestinationType)`, so a
registered Discord channel is testable with zero edits — assert this with a test
rather than assuming it.

## Alternatives considered

1. **Use the generic raw-webhook adapter with a Discord-shaped payload template.**
   Rejected: Discord needs a typed embed body, a 204-success checker, the
   `allowed_mentions` guard, and color logic — a template can't express the
   response classification, and it would push Discord-specific concerns into
   tenant config.
2. **Hardcode P0–P3 colors as issue #32 literally specifies.** Rejected: those
   values don't exist in attune's taxonomy and the taxonomy is tenant-mutable;
   a string-map with gray fallback is the only correct design.
3. **Parse `retry_after` for precise backoff.** Deferred: the outbox backoff
   schedule already bounds retries; precise per-response delay is a cross-adapter
   concern better solved once in the Transport layer than per-adapter.

## Risks / tradeoffs

- **Taxonomy drift:** a tenant renaming severity values silently degrades to gray.
  Acceptable (rendering never breaks); documented in the adapter comment. A future
  enhancement could read colors from the semantic pack.
- **Embed total-char (6000) limit** is a *sum* across fields, not per-field — a
  pathological digest could breach it even with per-field truncation. Mitigation:
  cap field count and budget the description; test asserts the assembled embed
  stays ≤6000 runes.
- **204 vs 200:** a checker that only accepted 200 would treat every successful
  Discord delivery as a failure. The `2xx` range guard prevents this; an explicit
  204 test locks it in.

## Implementation plan

1. Write the proposal (this doc); get it **Accepted** before code (per project gate).
2. TDD the adapter: `discord_test.go` first (color map, embed structure, 204/429/4xx checker, allowed_mentions, truncation), then `discord.go`.
3. Apply the 7 wiring points + migration 058.
4. Three-channel e2e test (Discord + Slack + Lark all registered, outbox routes each).
5. `CHANGELOG.md` `### Added` entry (#32).
6. Real-LLM end-to-end run per project acceptance: ingest → enrich → Discord webhook receives a colored embed (capture the delivered JSON as evidence).

## Verification

- `go test -race ./internal/outbound/adapter/discord/...` + touched packages.
- `make ci-check` (or the relevant subset) green, output cited in the PR — no asserted-without-evidence gates.
- Config integration: empty Discord URL disables cleanly; unknown `destination_type` rejected (typo guard).
- Audit-action note: this adds no new audited action (reuses existing notify-target CRUD actions), so no `validActions` / `chk_audit_action_value` change is needed — confirm by test rather than assumption.
- Real Discord incoming webhook receives a severity-colored embed end-to-end.

## References

- [`docs/research/2026-06-20-voc-landscape.md`](../../../research/2026-06-20-voc-landscape.md) — outbound-positioning conclusion driving the parity framing.
- [`2026-06-20-slack-adapter-wiring.md`](2026-06-20-slack-adapter-wiring.md) — the immediate precedent; this proposal is its copy-adapt.
- [`internal/outbound/README.md`](../../../../internal/outbound/README.md) — the 8-step adapter checklist.
- Discord — Execute Webhook & embed object: https://discord.com/developers/docs/resources/webhook#execute-webhook
