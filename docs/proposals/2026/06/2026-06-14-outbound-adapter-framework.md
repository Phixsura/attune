# Outbound channel-adapter framework (pluggable Slack / Lark / … delivery)

| | |
|---|---|
| **Issue** | #34 |
| **Status** | Implemented |
| **Started** | 2026-06-14 CST |
| **Revised** | 2026-06-15 CST |
| **Related** | #27 (daily digest — first multi-channel consumer; its rendering rides this framework), #66 (inbound channel-agnostic framework — the in-house pattern this mirrors; Plan T17 deleted the inline notifier with the note "raw-webhook delivers via outbox; #34 will re-add"), #25/#26 (outbox/worker paradigm + v2 envelope), #109 (managed LLM routing — digest theme naming), #114 (semantic clustering — digest themes) |

> **2026-06-15 revision.** Code review identified four design flaws in the
> original proposal: (1) sealed `Message` type is a Go anti-pattern requiring
> runtime type-switches; (2) single digest target per tenant is a product
> limitation encoded in schema; (3) v2 envelope byte-passthrough defers the
> field-order contract problem instead of solving it; (4) `Rendered` struct
> cannot express Lark's in-body signing. This revision addresses all four.

> **Why now.** #27 asked for the digest in "Lark / Slack". A code-verified review
> showed Lark is hard-deleted, Slack is an enum stub, and *every* outbound body is
> hand-marshaled behind a hardcoded `switch` — adding a channel means editing
> central delivery code. The digest's three render targets (generic webhook, Lark
> card, Slack Block Kit) are the forcing function to build the **outbound adapter
> SDK** the codebase has been deferring to #34. This proposal designs that SDK as
> the exact mirror of the existing `internal/inbound` framework, and makes the
> digest its first consumer.

## Problem

attune can deliver an outbound notification exactly one way today: marshal a JSON
envelope and POST it to a `raw-webhook`, or open a `github-issue`. The selection
is a hardcoded `switch` and every body is hand-built:

- **Adding a channel edits central code.** `sendByDestType`
  (`internal/service/outbox/outbox_worker_send.go:28-44`) is a `switch` over
  `destination_type`; `supportedOutboxDestTypes`
  (`outbox_worker.go:111-118`) is a hardcoded set. A new channel touches both,
  plus `selectOutboxTargets` (`enricher_outbox.go:311-329`), plus the console
  validation list. There is no registry.
- **The Console select is locked.** `FIXED_DEST_TYPE = 'raw-webhook'`
  (`console/src/features/notify-targets/components/dialogs.tsx:25-27`) with the
  literal comment *"#34 will reintroduce a typed select when the outbound-adapter
  SDK lands."* `slack-bot` / `email` are accepted by create-validation
  (`notify_targets.go:72-74`) but **rejected at delivery** (`sendByDestType`
  default → `ErrTerminal`) and at test-send (`test_send.go:80`). They are enum
  stubs that silently dead-letter.
- **Three copy-pasted response classifiers.** `checkRawResponse`
  (`raw_webhook.go:205-228`), `checkOutboxResponse`
  (`outbox_worker_send.go:84-108`), `checkDigestResponse` (`sender.go:47-62`) are
  byte-for-byte the same 2xx/408·429/4xx/5xx logic. A fix to one misses the others.
- **A whole dead inline path.** `buildNotifier` returns `nil`
  (`cmd/attune/setup.go:97-113`); so `Notifier` (`notify/notifier.go:16-19`),
  `MultiNotifier` (`outbox/notifier.go`), `RawWebhookRouter`
  (`raw_webhook.go`), the **v1 envelope** (`buildRawEnvelope`), and
  `Enricher.fanOut` are all unreachable scaffolding kept "for #34". Two envelope
  versions (v1 dead, v2 live) coexist; if the dead path were ever re-enabled it
  would ship v1 to a v2-only GitHub reader (`github_issue.go` unmarshals v2).
- **The digest needs three body shapes over one transport.** A Lark incoming
  webhook wants an interactive card; Slack wants Block Kit; a generic receiver
  wants our envelope+markdown. All three are the same HTTP POST — only the body
  and the per-channel signing differ. Without a render abstraction this becomes a
  fourth copy of the switch.

What is missing is the symmetric twin of `internal/inbound`: a **channel-agnostic
outbound framework** where each channel is a self-registering plugin that owns
"render this message for this channel + how to authenticate", while one shared
transport owns "POST with retry".

## Code reconciliation (issue text vs. verified code)

| #34 / #27 says | Verified reality | Decision |
|---|---|---|
| "render as Lark / Slack" | Lark hard-deleted (migration `015`); Slack/email are enum stubs that dead-letter at delivery + test-send | Make `lark` / `slack` **first-class channel adapters**; `raw-webhook` becomes the `generic` channel; `github-issue` ports as-is. |
| "outbound adapter SDK (#34)" | inline `Notifier`/`MultiNotifier`/`RawWebhookRouter`/v1-envelope/`fanOut` are **dead** (`buildNotifier`→nil, `setup.go:97-113`) | **Delete** the dead inline scaffolding; the framework *is* the SDK that comment promised. |
| per-event delivery | single live path = outbox worker `sendByDestType` switch over **v2 envelope** (`enricher_outbox.go:353-413`); github-issue consumes v2 | Replace the switch with `outbound.Lookup`; **preserve the exact v2 envelope bytes** for `generic` (customer verifiers depend on field order — `enricher_outbox.go:350-352`). |
| digest "output format" | digest hardcodes `GetByTenantAudience(tenant,'raw-webhook','digest')` (`worker.go:278`) and always renders markdown | **Collapse format into channel**: the digest target's `destination_type` (generic/lark/slack) selects the renderer. **No `output_format` field.** |
| retry policy per channel | three identical `check*Response` funcs; backoff `30s·2m·10m·1h` (`outbox_worker.go:239-252`) | One framework `DefaultCheck`; adapters override only the exceptions (github 201-created, 403 secondary-rate-limit). Backoff stays the outbox's. |
| metrics | `attune_notify_failures_total{destination_type,reason}`, `attune_outbox_lag_seconds` (`metrics.go`) | Keep both; the `destination_type` label now spans the new channels for free. |

## Industry benchmarking

Benchmarked four multi-channel notification systems (two Go, one Python, one
TS/infra) for the *plugin mechanism*, *channel selection*, *render⟂deliver split*,
and *retry classification* — the four decisions this framework must make.

| System | Plugin mechanism | Selection | Render ⟂ deliver | Retry classification |
|---|---|---|---|---|
| **Apprise** (Python, 100+ services) | each service subclasses `NotifyBase`, sets `service_name` + `protocol`; auto-discovered (built-ins + `plugin_paths`) | **URL scheme** (`slack://`, `discord://`) | each plugin owns format **and** send | per-plugin |
| **shoutrrr** (Go) | `ServiceRouter` built from URLs; each service implements a common `Service`; `GetScheme()` routes | **URL scheme** (`service://…`) | service owns format + send; Go-template message | per-service |
| **Alertmanager** (Go) | each receiver = a `Notifier` with `Notify(ctx, …) (retry bool, err error)`; wrapped in an `Integration`(name,idx,receiver); a `MultiStage` pipeline (Wait·Dedup·Retry·SetNotifies) drives them | **receiver name** in the routing tree | notifier owns templating + send | **the `retry bool` return is the single classification point** — not copy-pasted |
| **Novu** (TS, infra) | provider per channel; two levels: **channel** (email/sms/chat/push) ⟂ **provider** (Sendgrid/Twilio/Slack) | workflow step's channel | **explicit: content/template defined once, delivered through channels independently** | per-provider |

**Conclusions** (each maps to a decision below):

1. **Self-registering per-channel plugin, selected by an id** (Apprise/shoutrrr).
   attune already has this shape in-house — `internal/inbound` adapters call
   `Register(channel, factory)` in `init()` and are blank-imported only by
   `cmd/attune` (`registry.go:40`, `main.go:34-35`). → Mirror it as
   `internal/outbound`; the channel id **is** `destination_type` (attune's
   equivalent of a URL scheme), so no new column to select a renderer.
2. **Classification lives in the framework, once** (Alertmanager's `retry bool`).
   → One `outbound.DefaultCheck`; adapters override only true exceptions. Kills
   the three-way copy-paste.
3. **Content ⟂ delivery** (Novu). → A neutral `Message` (what to say) that each
   `Channel` renders (how to say it on this channel) + a shared `Transport` (how
   to ship bytes). The digest's `DigestView` and the per-event v2 envelope are two
   `Message` kinds; adapters type-switch.
4. **Don't adopt a URL-scheme DSL** (Apprise/shoutrrr) despite its elegance —
   attune already models a target as a typed row (`tenant_notify_targets`) with a
   `destination_type`, secret, audience, timeout. A `slack://tok@chan` string would
   *replace* a working, validated, multi-tenant schema with stringly-typed config.
   Keep the row; treat `destination_type` as the scheme. (Alertmanager/Novu also
   keep structured config, not URLs.)

## Goals / Non-goals

**Goals**

1. A self-registering outbound channel framework — adding a channel = a new
   package + one blank-import line in `cmd/attune`, **zero edits** to any driver,
   switch, or registry call site. Symmetric to `internal/inbound`.
2. Port the two live channels (`generic` = today's raw-webhook, `github-issue`)
   onto it and delete the dead inline scaffolding (`Notifier`/`MultiNotifier`/
   `RawWebhookRouter`/v1 envelope/`fanOut`/`buildNotifier`).
3. Two new channel plugins: `lark` (interactive card) and `slack` (Block Kit),
   each owning its own signing.
4. One shared transport (`notify.Transport`, unchanged); each channel provides
   its own `ResponseChecker` (no `DefaultCheck` fallback — explicit is better).
5. **Multi-target digest fan-out** — a tenant can subscribe to digest delivery
   on multiple channels (Lark card + Slack + email) via `digest_subscriptions`.
6. **Content-hash envelope signing** — replace byte-order-dependent HMAC with
   `sha256(canonical(envelope))` so field order no longer matters.
7. Per-channel test-send (the Console "Test" button works for every channel).

**Non-goals**

- `email` (SMTP) delivery — the framework leaves an obvious slot; not built here.
- Provider-level fan-out à la Novu (one channel, many providers); attune stays
  one-level (channel = `destination_type`).
- A URL-scheme config DSL; arbitrary user-supplied adapters at runtime (plugins
  are compile-time blank-imports, exactly like inbound).
- Reworking the outbox backoff schedule.

## Proposal

### The framework — `internal/outbound` (mirror of `internal/inbound`)

```
internal/outbound/                  framework root — channel-agnostic
  channel.go      EventChannel + DigestChannel + Factory + Rendered
  registry.go     Register(id, Factory) / Lookup(id) / Channels()   ← copies inbound/registry.go:40
  target.go       Target (destination row projection)
  sign.go         ContentHashSign(envelope, secret) — the new signing scheme
internal/outbound/adapter/generic/  init(){ outbound.Register("raw-webhook", New) }  envelope+markdown
internal/outbound/adapter/githubissue/  ported SendGitHubIssue; token auth
internal/outbound/adapter/lark/     Lark interactive card + in-body timestamp-sign
internal/outbound/adapter/slack/    Slack Block Kit (incoming-webhook; URL is the secret)
cmd/attune/main.go                  _ "…/outbound/adapter/{generic,githubissue,lark,slack}"  ← only legal site
```

Core types (Go-shaped pseudocode; final signatures land in code):

```go
// EventChannel — renders per-feedback notifications (the outbox path).
// Channels that don't support events (e.g. a future email-digest-only channel)
// simply don't implement this interface.
type EventChannel interface {
    ID() string
    RenderEvent(envelope *Envelope, dst Target) (Rendered, error)
}

// DigestChannel — renders daily/weekly roll-ups.
// Channels that don't support digests (e.g. github-issue) don't implement this.
type DigestChannel interface {
    ID() string
    RenderDigest(view any, dst Target) (Rendered, error)  // view is service-defined
}

// Factory — adapter init() calls Register(id, factory); dup id panics (inbound idiom).
// The returned value implements EventChannel, DigestChannel, or both.
type Factory func() any

// Rendered — the channel has full control over request construction.
// This accommodates Lark's in-body signing, GitHub's different URL, etc.
type Rendered struct {
    Build func(ctx context.Context) (*http.Request, error)  // full control
    Check ResponseChecker                                    // required, no default
}

// Envelope — the structured v2 envelope. Channels receive structure, not bytes.
// The framework handles content-hash signing; channels don't touch raw bytes.
type Envelope struct {
    Version     string         `json:"version"`
    EventType   string         `json:"event_type"`
    TraceID     string         `json:"trace_id"`
    DeliveredAt time.Time      `json:"delivered_at"`
    Feedback    FeedbackData   `json:"feedback"`
}

// ContentHashSign computes sha256(canonical-json(envelope)) and signs that.
// Field order no longer matters — the canonical form is sorted alphabetically.
func ContentHashSign(env *Envelope, secret string) string
```

`registry.go` is a near-verbatim copy of `internal/inbound/registry.go` (mutex +
`map[string]Factory`, `Register` panics on dup, `Channels()` returns a sorted
snapshot) — minus the `Manager`/Start/Shutdown lifecycle (rendering is on-demand,
not long-running). Selection is `Lookup(id)`, never a `switch`.

**Why composition interfaces, not sealed Message.** The original design used a
`Message` sealed type that adapters type-switch on. This is a Go anti-pattern:
Go has no exhaustiveness checking for type-switches, so adding a new message
kind (e.g. `AlertMessage`) silently fails at runtime. Composition interfaces
(`EventChannel` + `DigestChannel`) make capability explicit at compile time —
`github-issue` implements only `EventChannel`; the digest worker only calls
channels that implement `DigestChannel`.

**Why content-hash signing.** The original design passed v2 envelope bytes
through unchanged because customers verify HMAC against raw bytes, which depend
on JSON field order. This defers the problem: any future envelope change risks
breaking customer verifiers. Content-hash signing (`sha256(canonical(env))`)
uses a deterministic JSON serialization (keys sorted alphabetically), so field
order no longer matters. This is a **breaking change** for existing verifiers —
migration path in §Data model.

### Per-channel signing (owned by the adapter)

| Channel | Auth / signing | Source |
|---|---|---|
| `raw-webhook` (generic) | `X-Attune-Signature: sha256=…` HMAC over content-hash, if secret set | `outbound.ContentHashSign` — **content-hash of canonical envelope** |
| `github-issue` | `Authorization: Bearer <token>` | ported from `github_issue.go:88-96` |
| `lark` | Lark custom-bot `timestamp` + `sign` (HMAC-SHA256 of `timestamp\nsecret`, base64) **in the JSON body**, only when the bot has signature-verification on | new, via `Build func` (full request control) |
| `slack` | none — the incoming-webhook URL is the secret | n/a |

Signing is per-channel data, so it belongs in the adapter, not a shared signer.
The framework exposes `ContentHashSign` (canonical JSON → hash → HMAC) for
generic channels; Lark's in-body signing uses `Build func` to construct the
request with embedded `timestamp` + `sign` fields.

### Boundary rules (depguard — mirror inbound's two)

Two new `.golangci.yml` rules, copied from `inbound-boundary` +
`inbound-framework-isolation`:

- **`outbound-boundary`** — nothing under `service` / `handlers` / `repo` /
  `notify` may import `internal/outbound/adapter/*`. Only `cmd/attune`
  blank-imports them (the one legal site, exactly like inbound).
- **`outbound-framework-isolation`** — `internal/outbound` (root) imports nothing
  from `service` / `repo` / `handlers`; `internal/outbound/adapter/<ch>` imports
  the `outbound` root **only** — never a sibling adapter, never `service` /
  `repo` / `handlers` / `notify`. Drivers (digest worker, outbox worker) live in
  `service` and bridge `Rendered` → `notify.Transport.Send`.

### Two drivers, one registry

Both delivery drivers collapse to *resolve target → Lookup(id) → Render → ship*:

```go
// outbox worker (per-event) — replaces sendByDestType switch
ch := outbound.LookupEvent(row.DestinationType)       // was: switch row.DestinationType
if ch == nil { return fmt.Errorf("%w: no event channel %q", notify.ErrTerminal, row.DestinationType) }
env := &outbound.Envelope{Payload: row.Payload, SignVersion: target.SignatureVersion}
r, err := ch.RenderEvent(env, target)
req, err := r.Build(ctx)                              // adapter controls full request
return w.transport.Send(ctx, label, func(ctx) (*http.Request, error) { return req, nil }, r.Check)

// digest worker — replaces hardcoded raw-webhook + RenderPayload; multi-target fan-out
subs := w.subs.ListByTenant(ctx, tenantID)           // digest_subscriptions junction
for _, sub := range subs {
    ch := outbound.LookupDigest(sub.Target.DestinationType)   // generic | lark | slack
    if ch == nil { continue }                                   // channel doesn't support digest
    r, err := ch.RenderDigest(view, sub.Target)
    req, _ := r.Build(ctx)
    if err := w.transport.Send(ctx, label, …, r.Check); err != nil { … }
}
```

`shipped`/`ship` is one shared helper that turns a `Rendered` into a
`RequestBuilder` + `ResponseCheck` and calls the existing
`notify.Transport.Send` (`transport.go:94`) — the retry loop, backoff, and
`attune_notify_failures_total` accounting are untouched.

### Data model

One new table, two schema changes:

```sql
-- 029: digest_subscriptions — multi-target digest delivery.
-- A tenant can subscribe to digest on multiple channels (Lark + Slack + email).
-- Each subscription references a notify_target; the worker fans out to all.
CREATE TABLE IF NOT EXISTS digest_subscriptions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    notify_target_id UUID NOT NULL REFERENCES tenant_notify_targets(id) ON DELETE CASCADE,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, notify_target_id)
);

CREATE INDEX IF NOT EXISTS idx_digest_subscriptions_tenant
  ON digest_subscriptions (tenant_id) WHERE enabled = TRUE;

-- 029: widen the channel enum to the registered adapters.
-- NOTE: We do NOT add a CHECK constraint here. The registry (outbound.Channels())
-- is the source of truth; the Console select is populated from it. Adding a CHECK
-- would require a migration every time we add a channel.
-- Validation happens at the application layer via outbound.Lookup().

-- 029: envelope signature version column for migration.
ALTER TABLE tenant_notify_targets
  ADD COLUMN IF NOT EXISTS signature_version TEXT NOT NULL DEFAULT 'v2-content-hash';
-- Existing rows start on v2-content-hash; during migration window, ops can set
-- 'v2-bytes' for customers not yet upgraded. Worker checks this column.
```

**Multi-target digest fan-out.** The original design forced one digest target per
tenant via a partial unique index. This encoded a product limitation in schema —
ops wanting Lark + Slack + email would need three tenants. The new design:

1. `digest_subscriptions` is a junction table: one row per (tenant, target).
2. Digest worker queries `SELECT nt.* FROM digest_subscriptions ds JOIN
   tenant_notify_targets nt ON nt.id = ds.notify_target_id WHERE ds.tenant_id=$1
   AND ds.enabled` — returns 0..N targets.
3. Worker fans out to each, one `digest_runs` row per (tenant, run_date, target).
4. Console gains a "Digest Channels" section listing subscribed targets.

**Content-hash signing migration.** The `signature_version` column enables
gradual rollout:

| Value | Behavior |
|-------|----------|
| `v2-bytes` | Legacy: sign raw JSON bytes (field-order dependent) |
| `v2-content-hash` | New: sign `sha256(canonical(envelope))` |

Migration path:
1. Deploy with both signing codepaths.
2. New targets default to `v2-content-hash`.
3. Existing targets stay on `v2-bytes` until customer confirms upgrade.
4. After migration window, remove `v2-bytes` codepath; column becomes vestigial.

### Digest enrichment that the new card channels surface (the #27 "太拉" fixes)

`DigestView` carries the data the rich channels render and the markdown升级 needs —
each is a small, bounded addition computed **once** in the aggregator:

- **Period-over-period Δ** — totals for `[from,to)` and the prior window; the view
  exposes `Feedback`, `Urgent`, … each with a delta + direction. (One extra
  windowed `COUNT` query.)
- **Representative example per theme** — populate the **already-existing but
  unrendered** `themeOut.ExampleTitles` (`render.go:21`) from the cluster/naive
  example rows; the card shows one quote per theme. (No new query — the example
  IDs are already fetched.)
- **Theme lifecycle** — compare this window's theme keys to the prior window's:
  `new` vs `ongoing` (and `regressed` if absent ≥1 window then back). (One prior-
  window theme-key query.) Mirrors Sentry's substatus badges.
- **7-day sparkline** — trailing daily counts as a unicode block series. (One
  `GROUP BY day` query.)
- **Deep links** — a process-level Console base URL (config) → per-theme/per-item
  Console links. Not per-subscription.

The `generic` channel's markdown is rewritten severity-first with Δ arrows,
quotes, links, top-N + "+N more", and the awkward "1 rows" copy fixed; `lark` and
`slack` render the same `DigestView` as native cards.

### Console

- `dialogs.tsx` / `edit-dialog.tsx`: replace `FIXED_DEST_TYPE='raw-webhook'` with a
  typed `destination_type` select (`raw-webhook` / `lark` / `slack` /
  `github-issue`), gated help text per channel (e.g. Slack/Lark incoming-webhook
  URL hints). This is the change the file's own comment anticipated.
- The digest settings page drops any format control; it gains a hint: "create a
  notify target with audience=digest in your chosen channel (raw-webhook / Lark /
  Slack)".
- Backend create/patch validation (`notify_targets.go:72-74`) already lists the
  enum; align it with the migration set and the registry (`outbound.Channels()`
  becomes the source of truth, so validation can't drift from what actually
  delivers).

## Alternatives considered

1. **Keep the `switch`, add cases** (status quo + 2). Rejected: the user's explicit
   ask is a plugin architecture; the switch already spans four sites
   (`sendByDestType`, `supportedOutboxDestTypes`, `selectOutboxTargets`, console
   validation) and would gain a fifth per channel. The registry makes those one.
2. **`output_format` on the subscription** (earlier draft). Rejected once
   channels are first-class: the target's `destination_type` already names the
   channel; a second selector is redundant and lets the two disagree.
3. **URL-scheme DSL** (`slack://tok@chan`, Apprise/shoutrrr). Rejected: replaces a
   validated multi-tenant typed row with stringly-typed config; Alertmanager and
   Novu also keep structured config. We treat `destination_type` *as* the scheme.
4. **Keep the dead inline path** (`Notifier`/`MultiNotifier`/v1). Rejected: it is
   unreachable, untested, and ships the wrong envelope version; the framework is
   precisely the "#34 will re-add" it was a placeholder for. Delete it.
5. **`init()` self-registration vs. an explicit registry built in `cmd/attune`.**
   Chose self-registration to match `internal/inbound` exactly (one codebase
   idiom, the depguard rules already exist to copy). The blank-import in
   `cmd/attune` keeps wiring explicit and greppable.
6. **One mega-PR.** Rejected (user chose stacked): a single PR touching every
   delivery path is unreviewable and maximizes regression blast radius. Land as a
   stack, each independently green.
7. **Sealed `Message` type with type-switch** (original proposal). Rejected: Go
   has no exhaustiveness checking for type-switches, so adding a new message kind
   silently fails at runtime. Composition interfaces (`EventChannel` +
   `DigestChannel`) make capability explicit at compile time.
8. **`Rendered` struct with `Body []byte` + `Headers`** (original proposal).
   Rejected: cannot express Lark's in-body signing (`timestamp` + `sign` fields
   in JSON body). `Build func` gives channels full control over request
   construction.
9. **Byte-passthrough for v2 envelope** (original proposal). Rejected: defers
   the field-order contract problem. Content-hash signing solves it — customers
   sign against a deterministic canonical form, not raw bytes.
10. **One digest target per tenant** (original proposal). Rejected: encodes a
    product limitation in schema. `digest_subscriptions` junction table enables
    multi-channel fan-out (Lark + Slack + email).
11. **`DefaultCheck` fallback for `ResponseChecker`** (original proposal).
    Rejected: adapter authors forget to set `Check`, silently using the default,
    which misclassifies GitHub 201 or Lark-specific codes. Explicit is better —
    `Check` is required, no default.

## Risks / tradeoffs

- **Live-path regression (highest).** The migration rewires the *only* production
  delivery path (outbox). Mitigation: `generic` adapter passes content-hash signed
  envelope; every existing delivery test (below) must stay green **before** the
  switch is deleted; land behind the stack so per-event migration is its own
  reviewable PR with the full integration suite re-run.
- **Content-hash migration.** Changing the signing scheme requires customers to
  update their verification code. Mitigation: `signature_version` column enables
  gradual rollout; existing rows keep `v2-bytes` until customer confirms upgrade;
  new rows default to `v2-content-hash`.
- **Lark signing correctness.** Lark's in-body signing (`timestamp` + `sign`) is
  easy to get subtly wrong (newline, base64, 1-hour validity). Mitigation: unit
  vectors from Lark docs; `Build func` gives the adapter full control over request
  construction, including body fields.
- **Console UX shift.** The locked select becoming typed can surface previously
  hidden stub channels. Mitigation: the select is sourced from the registry, so it
  only ever lists channels that actually deliver.
- **Composition interface explosion.** Adding new message capabilities (email, SMS)
  adds new interfaces. Acceptable: each interface is opt-in, capability is explicit
  at compile time, adapters implement only what they support.
- **Stacked-PR coordination.** Digest PRs rebase onto the framework. Mitigation:
  the digest's existing behavior is preserved until its own stack step wires the
  `DigestChannel` implementation.
- **Multi-target fan-out complexity.** `digest_subscriptions` junction table adds
  another join. Mitigation: the query is straightforward (one join, indexed by
  `tenant_id`); the flexibility (Lark + Slack + email per tenant) justifies the
  schema overhead.
- **Scope.** This is a multi-PR effort (~6–9 person-days) well beyond #27.
  Mitigated by the stack and by the large dead-code deletion that nets the diff
  down.

## Implementation plan (stacked PRs, each independently green)

1. **Framework root** — `internal/outbound/{channel,registry,envelope,render,
   sign}.go` + tests:
   - `EventChannel` + `DigestChannel` composition interfaces
   - `Rendered` with `Build func` + required `Check`
   - Registry (dup-panic, `Lookup` miss → terminal, `Channels()` sorted)
   - Content-hash signing (`sha256(canonical(envelope))`)
   - Two depguard rules (mirrors inbound)
   - No driver changes yet *(inert)*
2. **Port live channels** — `adapter/generic` (content-hash signing, EventChannel
   only for now) + `adapter/githubissue` (ported `SendGitHubIssue`); swap
   `sendByDestType` for `outbound.Lookup` in the outbox worker; **delete**
   `Notifier`, `MultiNotifier`(+test), `RawWebhookRouter`+v1 envelope+
   `checkRawResponse`, `Enricher.fanOut`/`SetNotifier`, `buildNotifier`. Re-run
   the **entire** delivery suite (§Verification).
3. **Signature migration** — migration `029` adds `signature_version` column;
   new rows default to `v2-content-hash`; existing rows stay on `v2-bytes` until
   customer confirms upgrade; worker checks column and applies appropriate signing.
4. **New channels** — `adapter/lark` (card + in-body sign via `Build func`) +
   `adapter/slack` (Block Kit); per-channel test-send; widen `destination_type`
   enum sourced from `outbound.Channels()`.
5. **Console** — typed `destination_type` select (`dialogs.tsx`/`edit-dialog.tsx`),
   per-channel help text, validation aligned to the registry; vitest + msw.
6. **Digest schema** — migration `030` adds `digest_subscriptions` junction table;
   `DigestChannel` interface enables multi-target fan-out; worker queries the
   junction table, fans out to each target, one `digest_runs` row per target.
7. **Digest enrichment (#27 "太拉" fixes)** — `DigestView` gains Δ / quotes /
   lifecycle / sparkline / deep-links; markdown rewritten; Lark + Slack digest
   cards via `DigestChannel`. `CHANGELOG` + proposals flipped to `Implemented`;
   full e2e.

PRs 1–5 are pure #34; 6–7 close the #27 follow-up. Each PR updates `CHANGELOG`
(`### Added`/`### Changed`/`### Removed` for the dead code).

## Verification

**Must stay green (per-event migration, PR 2) — the existing suite:**

- `enricher_outbox_test.go` — v2 envelope shape + `selectOutboxTargets` audience
  rules (pool always / radar-if-urgent / all / digest-never / drops non-outbox
  dests). Canonical form verified via content-hash signing.
- `raw_webhook_test.go` — v1 envelope tests **removed** with the dead code;
  replaced by `adapter/generic` tests asserting content-hash signed v2 envelope.
- `notifier_test.go` (MultiNotifier) — **removed** with the dead code.
- `test/integration/postgres/ingest` `TestPG_IngestEnrichQueuesAndDrainsOutbox` —
  full ingest→enrich→outbox→POST receiver path, now through the registry.
- `test/integration/postgres/tenant` `TestPG_TenantAndNotifyTargetCRUD` — target
  CRUD + `(tenant,dest_type,audience)` uniqueness across the widened enum.

**New:**

- Registry: dup-`Register` panics; `Lookup` miss → terminal; `Channels()` sorted;
  composition interfaces (`EventChannel` / `DigestChannel`) queried at runtime.
- Response checking: table test (2xx ok; 408/429 retry; 4xx terminal; 5xx retry);
  github override (201 ok; 403 secondary-rate-limit demoted to retry); **no
  default** — each `Rendered.Check` is required.
- Content-hash signing: `sha256(canonical(envelope))` — stable across marshal/
  unmarshal; `signature_version` column controls which signing codepath.
- `generic`: content-hash signed v2 envelope, both `EventChannel.RenderEvent`
  and `DigestChannel.RenderDigest`.
- `lark` / `slack`: golden card/Block-Kit JSON; Lark in-body sign vectors from
  docs; `Build func` constructs request with body-embedded signature fields.
- Per-channel test-send: each channel's "Test" ping builds + classifies.
- Console: typed select renders the registry's channels; per-channel validation.
- Digest fan-out: `digest_subscriptions` junction table; worker queries and fans
  out to each subscribed target; one `digest_runs` row per (tenant, date, target).
- **Real-LLM e2e** (attune bar): the digest theme path against a live provider,
  delivered as a **Lark card** and a **Slack Block Kit** message to local
  receivers, plus the generic envelope — one run documented in the PR.
- **Browser e2e** (chrome-devtools MCP): create a `lark` digest target via the
  typed select; configure the digest; fire; verify the card body delivered.

## References

**OSS** — [Apprise plugin model](https://github.com/caronc/apprise/wiki/DemoPlugin_Basic)
(`NotifyBase`, `protocol`/URL-scheme, `plugin_paths`) and
[plugins dir](https://github.com/caronc/apprise/tree/master/apprise/plugins);
[shoutrrr](https://containrrr.dev/shoutrrr/) `ServiceRouter` + URL scheme;
[Alertmanager `notify`](https://github.com/prometheus/alertmanager/blob/main/notify/notify.go)
(`Notifier.Notify → (retry bool, err)`, `Integration`, `MultiStage`) +
[notification integrations](https://deepwiki.com/grafana/prometheus-alertmanager/4.1-notification-integrations);
[Novu channel⟂provider](https://novu.mintlify.app/getting-started/how-novu-works).

**attune in-house precedent** — `internal/inbound/registry.go:40`
(`Register`/`Factory`/dup-panic), `internal/inbound/inbound.go` (`Adapter` port),
`cmd/attune/main.go:34-35` (blank-import site), `.golangci.yml`
(`inbound-boundary` + `inbound-framework-isolation`).

**attune code (verified for the migration)** —
`internal/service/outbox/outbox_worker_send.go:28-44` (`sendByDestType`),
`:84-108` (`checkOutboxResponse`), `outbox_worker.go:111-118`
(`supportedOutboxDestTypes`), `:239-252` (backoff);
`internal/notify/adapter/rawwebhook/raw_webhook.go` (dead inline router + v1
envelope + `checkRawResponse:205-228`); `internal/notify/notifier.go:16-19`
(`Notifier`), `internal/service/outbox/notifier.go` (`MultiNotifier`),
`cmd/attune/setup.go:97-113` (`buildNotifier`→nil), `server.go:132-138`;
`internal/service/enrich/enricher_outbox.go:353-413` (v2 envelope), `:311-329`
(`selectOutboxTargets`), `:350-352` (field-order contract);
`internal/notify/adapter/githubissue/github_issue.go:65-113,142-180,324-349`;
`internal/notify/sig/sig.go:21-30` (`SignRaw`/`EnvelopeVersion`);
`internal/notify/transport.go:67-73,94-124` (transport contract);
`internal/notify/test_send.go:39-146`;
`internal/service/digest/{worker.go:274-295,sender.go:47-62,render.go:21-117}`;
`internal/repo/notifytarget/notify_targets.go:29-43`;
`internal/infra/metrics/metrics.go` (`attune_notify_failures_total`,
`attune_outbox_lag_seconds`).
