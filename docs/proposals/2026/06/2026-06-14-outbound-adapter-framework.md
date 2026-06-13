# Outbound channel-adapter framework (pluggable Slack / Lark / … delivery)

| | |
|---|---|
| **Issue** | #34 |
| **Status** | Proposed |
| **Started** | 2026-06-14 CST |
| **Related** | #27 (daily digest — first multi-channel consumer; its rendering rides this framework), #66 (inbound channel-agnostic framework — the in-house pattern this mirrors; Plan T17 deleted the inline notifier with the note "raw-webhook delivers via outbox; #34 will re-add"), #25/#26 (outbox/worker paradigm + v2 envelope), #109 (managed LLM routing — digest theme naming), #114 (semantic clustering — digest themes) |

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
   onto it **with byte-identical wire output** for existing customers, and delete
   the dead inline scaffolding (`Notifier`/`MultiNotifier`/`RawWebhookRouter`/v1
   envelope/`fanOut`/`buildNotifier`).
3. Two new channel plugins: `lark` (interactive card) and `slack` (Block Kit),
   each owning its own signing.
4. One shared retry/terminal classifier; one shared transport
   (`notify.Transport`, unchanged).
5. The digest renders through the framework, selected by its target's
   `destination_type` — no `output_format` field; the locked Console select
   becomes a typed channel picker.
6. Per-channel test-send (the Console "Test" button works for every channel, not
   just raw-webhook).

**Non-goals**

- `email` (SMTP) delivery — the framework leaves an obvious slot; not built here.
- Provider-level fan-out à la Novu (one channel, many providers); attune stays
  one-level (channel = `destination_type`).
- Multiple digest targets per tenant / multi-format fan-out of one digest — v1
  digest still resolves **one** `audience='digest'` target (its channel decides
  the format). (Inherited #27 limit.)
- A URL-scheme config DSL; arbitrary user-supplied adapters at runtime (plugins
  are compile-time blank-imports, exactly like inbound).
- Reworking the outbox backoff schedule or the v2 envelope contents.

## Proposal

### The framework — `internal/outbound` (mirror of `internal/inbound`)

```
internal/outbound/                  framework root — channel-agnostic
  channel.go      Channel + Factory + Rendered + the registry-facing types
  registry.go     Register(id, Factory) / Lookup(id) / Channels()   ← copies inbound/registry.go:40
  message.go      Message (sealed) = EventMessage | DigestMessage
  view.go         DigestView (totals+Δ, themes+lifecycle+examples, items, trend, links)
  check.go        DefaultCheck (the one true 2xx/408·429/4xx/5xx classifier) + ErrTerminal re-export
  sign.go         HMACSign(body, secret) wrapper (so adapters never import notify/sig)
internal/outbound/adapter/generic/  init(){ outbound.Register("raw-webhook", New) }  envelope+markdown, HMAC
internal/outbound/adapter/githubissue/  ported SendGitHubIssue; EventMessage→issue body, token auth
internal/outbound/adapter/lark/     Lark interactive card + Lark timestamp-sign
internal/outbound/adapter/slack/    Slack Block Kit (incoming-webhook; URL is the secret)
cmd/attune/main.go                  _ "…/outbound/adapter/{generic,githubissue,lark,slack}"  ← only legal site
```

Core types (Go-shaped pseudocode; final signatures land in code):

```go
// Channel — one outbound destination kind. ID() is the destination_type.
type Channel interface {
    ID() string
    Render(msg Message, dst Target) (Rendered, error) // ErrUnsupportedMessage if N/A
}

// Factory — adapter init() calls Register(id, factory); dup id panics (inbound idiom).
type Factory func() Channel

// Rendered — everything the shared transport needs; the adapter has already
// computed the URL, body, content-type, and signing/auth headers.
type Rendered struct {
    Method      string        // "" ⇒ POST
    URL         string        // adapter may rewrite (github → REST API; webhook → dst.URL)
    Body        []byte
    ContentType string
    Headers     http.Header   // auth + signature already applied
    Check       ResponseCheck // nil ⇒ DefaultCheck
}

// Message — sealed; adapters type-switch exhaustively.
type Message interface{ isMessage() }
type EventMessage  struct { EnvelopeV2 []byte }       // feedback.enriched — exact stored bytes
type DigestMessage struct { View DigestView }          // feedback.digest — structure-first
```

`registry.go` is a near-verbatim copy of `internal/inbound/registry.go` (mutex +
`map[string]Factory`, `Register` panics on dup, `Channels()` returns a sorted
snapshot) — minus the `Manager`/Start/Shutdown lifecycle (rendering is on-demand,
not long-running). Selection is `Lookup(id)`, never a `switch`.

**Why `EventMessage` carries bytes, not structure.** Today the v2 envelope is
built once at enrichment time and stored in `notify_outbox.payload`
(`enricher_outbox.go:70`); the worker POSTs those exact bytes and customer
verifiers depend on the canonical field order (`enricher_outbox.go:350-352`). So
the `generic` adapter passes `EnvelopeV2` through **unchanged** — zero wire drift.
Adapters that need structure (`github`, `lark`, `slack`) unmarshal it, exactly as
`SendGitHubIssue` already does (`github_issue.go:142-180`). The digest is the
opposite (no stored envelope), so `DigestMessage` is structure-first.

### Per-channel signing (owned by the adapter)

| Channel | Auth / signing | Source |
|---|---|---|
| `raw-webhook` (generic) | `X-Attune-Signature: sha256=…` HMAC over body, if secret set | `outbound.HMACSign` (wraps `sig.SignRaw`) — **unchanged from today** |
| `github-issue` | `Authorization: Bearer <token>` | ported from `github_issue.go:88-96` |
| `lark` | Lark custom-bot `timestamp` + `sign` (HMAC-SHA256 of `timestamp\nsecret`, base64) **in the JSON body**, only when the bot has signature-verification on | new, in the lark adapter (stdlib crypto/hmac) |
| `slack` | none — the incoming-webhook URL is the secret | n/a |

Signing is per-channel data, so it belongs in the adapter, not a shared signer.
The framework exposes only the generic `HMACSign` helper so adapters never reach
into `internal/notify/sig`.

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
ch, ok := outbound.Lookup(row.DestinationType)        // was: switch row.DestinationType
if !ok { return fmt.Errorf("%w: no channel %q", notify.ErrTerminal, row.DestinationType) }
r, err := ch.Render(outbound.EventMessage{EnvelopeV2: row.Payload}, target)
return w.shipped(ctx, r, outbound.EventMessage{…})    // builds *http.Request, calls Transport.Send

// digest worker — replaces hardcoded raw-webhook + RenderPayload
target := w.targets.GetByTenantAudience(ctx, tenantID, /* any digest channel */, AudienceDigest)
ch, _ := outbound.Lookup(target.DestinationType)       // generic | lark | slack
r, err := ch.Render(outbound.DigestMessage{View: view}, target)
return w.ship(ctx, target, r)
```

`shipped`/`ship` is one shared helper that turns a `Rendered` into a
`RequestBuilder` + `ResponseCheck` and calls the existing
`notify.Transport.Send` (`transport.go:94`) — the retry loop, backoff, and
`attune_notify_failures_total` accounting are untouched.

### Data model

No new tables. Two narrow changes:

```sql
-- 029: widen the channel enum to the registered adapters.
ALTER TABLE tenant_notify_targets DROP CONSTRAINT IF EXISTS tenant_notify_targets_dest_check;
ALTER TABLE tenant_notify_targets ADD  CONSTRAINT tenant_notify_targets_dest_check
  CHECK (destination_type IN ('raw-webhook','github-issue','lark','slack'));  -- 'email' when built

-- One digest target per tenant, across ALL channels. The base table is
-- UNIQUE(tenant_id, destination_type, audience), which would let a tenant hold
-- both a (lark,digest) and a (raw-webhook,digest) row — ambiguous now that the
-- channel IS the format. This partial index makes "the digest target" singular
-- and makes GetDigestTarget unambiguous (also encodes #27's v1 one-digest-target
-- limit at the DB level instead of by convention).
CREATE UNIQUE INDEX IF NOT EXISTS uq_notify_targets_one_digest
  ON tenant_notify_targets (tenant_id) WHERE audience = 'digest';
```

- **No `digest_subscriptions.output_format`.** The digest target's
  `destination_type` is the format. A tenant who wants the digest as a Lark card
  creates a `destination_type='lark', audience='digest'` target; the worker
  resolves digest **by audience across channels** — drop the hardcoded
  `'raw-webhook'` arg at `worker.go:278` and add `GetDigestTarget(tenant)` =
  `SELECT … WHERE tenant_id=$1 AND audience='digest'`, made single-row by the
  partial unique index above. Switching a tenant's digest channel is then a
  channel change on that one row (or delete+create), which the index enforces.
- `audience='digest'` already exists (#27, migration `027`).

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
7. **Re-marshal the v2 envelope from structure in `generic`.** Rejected: risks
   field-order drift for customer HMAC verifiers; pass the stored bytes through.

## Risks / tradeoffs

- **Live-path regression (highest).** The migration rewires the *only* production
  delivery path (outbox). Mitigation: `generic` passes v2 bytes through unchanged;
  every existing delivery test (below) must stay green **before** the switch is
  deleted; land behind the stack so per-event migration is its own reviewable PR
  with the full integration suite re-run.
- **Customer wire compatibility.** `generic` output must be byte-identical
  (envelope + `X-Attune-Signature`). Asserted by reusing the existing
  `enricher_outbox_test.go` field-order test against the adapter output.
- **Lark signing correctness.** Lark's timestamp-sign is easy to get subtly wrong
  (newline, base64, 1-hour validity). Mitigation: unit vectors from Lark docs; the
  signature path only engages when the operator enables bot signature-verification
  (default off → URL-as-secret works immediately).
- **Console UX shift.** The locked select becoming typed can surface previously
  hidden stub channels. Mitigation: the select is sourced from the registry, so it
  only ever lists channels that actually deliver.
- **Sealed `Message` growth.** A future `email`/`sms` message kind changes the
  type-switch in every adapter. Acceptable pre-1.0; adapters return
  `ErrUnsupportedMessage` for kinds they don't handle, so the compiler + a registry
  conformance test catch gaps.
- **Stacked-PR coordination.** #27/PR #116 (digest) rebases onto the framework.
  Mitigation: the digest's existing behavior is preserved until its own stack step
  swaps `RenderPayload` for `outbound.Lookup`.
- **Scope.** This is a multi-PR effort (~6–9 person-days) well beyond #27.
  Mitigated by the stack and by the large dead-code deletion that nets the diff
  down.

## Implementation plan (stacked PRs, each independently green)

1. **Framework root** — `internal/outbound/{channel,registry,message,view,check,
   sign}.go` + tests (registry dup-panic, `DefaultCheck` table, sealed-message
   conformance); the two depguard rules. No driver changes yet. *(inert)*
2. **Port live channels + delete dead scaffolding** — `adapter/generic` (v2
   bytes-through, HMAC) + `adapter/githubissue` (ported `SendGitHubIssue`); swap
   `sendByDestType` for `outbound.Lookup` in the outbox worker; **delete**
   `Notifier`, `MultiNotifier`(+test), `RawWebhookRouter`+v1 envelope+
   `checkRawResponse`, `Enricher.fanOut`/`SetNotifier`, `buildNotifier`. Re-run
   the **entire** delivery suite (§Verification). *Byte-identical wire output.*
3. **New channels** — `adapter/lark` (card + sign) + `adapter/slack` (Block Kit);
   per-channel test-send so the Console "Test" button works for each; widen the
   `destination_type` enum (migration `029`) sourced from `outbound.Channels()`.
4. **Console** — typed `destination_type` select (`dialogs.tsx`/`edit-dialog.tsx`),
   per-channel help text, validation aligned to the registry; vitest + msw.
5. **Digest onto the framework** — drop `RenderPayload`'s direct use for
   `outbound.Lookup`; `GetDigestTarget(tenant)` resolves `audience='digest'`
   across channels; remove the hardcoded `'raw-webhook'`. Digest behavior
   unchanged for a generic target.
6. **Digest enrichment (#27 "太拉" fixes)** — `DigestView` gains Δ / quotes /
   lifecycle / sparkline / deep-links; markdown rewritten; Lark + Slack digest
   cards. `CHANGELOG` + proposals flipped to `Implemented`; full e2e.

PRs 1–4 are pure #34; 5–6 close the #27 follow-up. Each PR updates `CHANGELOG`
(`### Added`/`### Changed`/`### Removed` for the dead code).

## Verification

**Must stay green (per-event migration, PR 2) — the existing suite:**

- `enricher_outbox_test.go` — v2 envelope shape + **stable field order** +
  `selectOutboxTargets` audience rules (pool always / radar-if-urgent / all /
  digest-never / drops non-outbox dests). Re-asserted against `generic` adapter
  output for byte-identity.
- `raw_webhook_test.go` — v1 envelope tests **removed** with the dead code;
  replaced by `adapter/generic` tests asserting the same v2 bytes the outbox sent.
- `notifier_test.go` (MultiNotifier) — **removed** with the dead code.
- `test/integration/postgres/ingest` `TestPG_IngestEnrichQueuesAndDrainsOutbox` —
  full ingest→enrich→outbox→POST receiver path, now through the registry.
- `test/integration/postgres/tenant` `TestPG_TenantAndNotifyTargetCRUD` — target
  CRUD + `(tenant,dest_type,audience)` uniqueness across the widened enum.

**New:**

- Registry: dup-`Register` panics; `Lookup` miss → terminal; `Channels()` sorted;
  every registered channel handles or cleanly refuses each `Message` kind.
- `DefaultCheck`: table test (2xx ok; 408/429 retry; 4xx terminal; 5xx retry);
  github override (201 ok; 403 secondary-rate-limit demoted to retry).
- `generic`: byte-identical to the current raw-webhook body + signature, both
  `EventMessage` and `DigestMessage`.
- `lark` / `slack`: golden card/Block-Kit JSON; Lark sign vectors from docs.
- Per-channel test-send: each channel's "Test" ping builds + classifies.
- Console: typed select renders the registry's channels; per-channel validation.
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
