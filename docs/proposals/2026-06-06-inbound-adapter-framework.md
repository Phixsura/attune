# Inbound channel-adapter framework (ports & adapters)

| | |
|---|---|
| **Issue** | #66 |
| **Status** | Proposed |
| **Started** | 2026-06-06 |
| **Related** | #19 (the canonical contract this normalizes into), #34 (symmetric outbound adapter SDK), #35 (email = an adapter under this), #31/#32/#33 (outbound notify) |

## Problem

The core ingest path bakes channel specifics into the domain: the `source` allow-list hardcodes Lark surfaces (`lark-group`, `lark-bitable`, `lark-approval`, `lark-helpdesk`, `lark-form`) and `/v1/lark/event` is a Lark-specific route in core routing. **Lark is a first-class citizen.** Every new channel (generic webhook, Slack, email — #35, web widget) would leak its specifics into the core domain, validators, and contract — coupling that compounds with each channel added.

## Goals

- **A channel-agnostic core** — the ingest/enrich pipeline consumes only the canonical `IngestRequest` (#19) and never knows which channel produced an event.
- **Pluggable inbound adapters** — each channel validates + normalizes its native payload into the canonical request at the edge; adding a channel is *additive* (new package + one registration line), never a core change.
- **Long-term enforceability** — the core↔adapter boundary is guarded by **CI, not convention**.
- Symmetry with the outbound adapter SDK (#34): inbound normalizes → core → outbound dispatches.

## Non-goals

- The canonical contract *format* (owned by #19).
- Outbound dispatch (#34 + the notify issues).
- A **dynamic / binary plugin** system (Go `plugin`, WASM, sidecars). Adapters are compiled-in packages behind a registry; revisit only if out-of-tree third-party adapters become a real need.

## Design

### The ports (interfaces)

Two small interfaces draw the boundary:

```go
// Inbound adapter — one per channel. Owns its ingress + native→canonical mapping.
type Adapter interface {
    Channel() string        // adapter-declared identity: "lark", "slack", "webhook", …
    Routes() http.Handler   // its own HTTP ingress + signature/secret verification
}

// The channel-agnostic core port. Adapters depend on this; the core never depends on adapters.
type Ingestor interface {
    Ingest(ctx context.Context, req *pb.IngestRequest) (id int64, err error)
}
```

Inside its routes an adapter **verifies** the request (signature/secret — the adapter owns its security), **normalizes** the native payload into `*pb.IngestRequest` (the #19 contract), then calls `Ingestor.Ingest`. Dependencies flow **one way**: adapter → (contract + `Ingestor`); the core → only `Ingestor`.

### Layout & the boundary guard (the longevity guarantee)

```
internal/ingest/            ← channel-agnostic core (implements Ingestor)
internal/ingest/adapters/
    lark/                   ← first adapter (refactored from /v1/lark/event)
    webhook/   (Phase 2)
    email/     (Phase 2 — folds in #35)
    slack/     (Phase 2)
```

- The core (`internal/ingest`, the `service` enrich path) **must not import** any `internal/ingest/adapters/*` package. **Enforced in CI** via golangci-lint `depguard` (attune already runs depguard for the §5 layering rules) plus a CLAUDE.md §5 clause. A violating import *fails the build* — this is what keeps the core channel-agnostic **for good**, rather than by reviewer vigilance.
- Adapters are wired in exactly one place (the router / a registry), so a new channel = a new package + one registration line.

### Normalization contract

Every adapter maps its native payload → canonical `IngestRequest`:

- `source` = the adapter's `Channel()` identity (`lark`, `slack`, …).
- Channel-specific detail (Lark surface, webhook provider, email headers) → `source_meta`.
- `content` / `source_user` / `page_url` filled from the native payload.

The core's existing validation then runs on the normalized request — **one code path for every channel**.

### Generic webhook adapter (Phase 2)

A `webhook` adapter with **per-source transforms** (à la Hookdeck / Svix): a small declarative transform maps `{provider} → IngestRequest`. A new webhook provider is a transform addition, not a new code path.

### Registry & contributor ergonomics (Phase 3)

An adapter registry so `RegisterAdapter(lark.New(...))` is the only wiring touch-point; a **"How to add an inbound channel"** guide; and a shared **conformance test suite** (golden native payload → expected `IngestRequest`) that every adapter runs.

## Alternatives considered

- **Keep per-channel handlers ad hoc (status quo).** Rejected: Lark stays first-class, every channel re-couples to the core, and nothing *enforces* the boundary.
- **Dynamic plugins** (Go `plugin` / WASM / sidecar adapters). Rejected for now: operational complexity and ABI fragility outweigh the benefit while all adapters are in-tree. The registry keeps the door open if out-of-tree adapters are ever needed.
- **A message bus between adapters and core** (adapters publish canonical events to a queue; core consumes). Deferred: valuable at scale (decoupling, replay, backpressure) but premature — the in-process `Ingestor` port is the *same* boundary and can grow a bus behind it later without touching any adapter.

## Risks / tradeoffs

- **Refactor risk on the live Lark path.** Mitigated by capturing today's behavior in an adapter conformance test (golden Lark payload → expected `IngestRequest`) *before* the move.
- **Boundary-guard friction.** The depguard rule may flag genuinely shared code; the fix is to extract the shared piece into the contract/core, **not** to exempt an adapter into the core.
- **Over-abstraction with one adapter.** With only Lark today the interface is deliberately tiny; surface is added only as the 2nd/3rd channel demands (YAGNI).
- **De-rooting + data migration (compat-preserving).** The core drops `lark-*`; an **ingest-edge legacy-alias map** keeps the public endpoint accepting them — `lark-bitable` / `approval` / `helpdesk` / `form` are sent by customers' 飞书自动化 and must not break — normalizing to `source = 'lark'` + `source_meta.lark_surface`. A data migration rewrites existing `user_feedback` rows; console source filters + any `lark-*` dashboard update. **Net customer-facing impact: none.** The legacy aliases are flagged for a later, coordinated sunset.

## Implementation plan

**Phase 1 — the deliverable:** the `Adapter` + `Ingestor` ports; the `internal/ingest` core; refactor `/v1/lark/event` into `internal/ingest/adapters/lark` behind the port; **de-root Lark from the core** — remove `lark-*` from the core `ValidSources` + `SourceDisplayName` mappings; keep a **legacy-alias map at the ingest edge** (`lark-* → source = "lark"` + `source_meta.lark_surface`, marked legacy/sunset) so existing customer 飞书自动化 integrations keep working unchanged; the `/v1/lark/event` adapter emits the normalized form directly; a **data migration rewrites existing `lark-*` rows**; the CI depguard boundary guard (core ⊥ adapters) + the CLAUDE.md §5 clause; a Lark conformance test; CHANGELOG `### Changed` (core de-rooting + data migration) + `### Deprecated` (legacy `lark-*` source aliases).

**Phase 2:** generic webhook adapter (per-source transforms); email adapter (#35); Slack inbound.

**Phase 3:** registry + contributor guide + shared conformance suite; per-adapter metrics/traces.

## Verification

- The Lark path behaves identically post-refactor — conformance test on a captured payload + the existing Lark e2e stay green.
- **CI fails** if `internal/ingest` (core) imports any `adapters/*` package — proven by a deliberately-added bad import in the test.
- `attune_ingest_total{source=…}` stays bounded (the existing security invariant) across channels.
- Adding a trivial second adapter touches only its own package + one registration line.

## References

- OpenClaw — every channel (incl. Lark/Feishu) is an adapter/plugin; the core runtime is channel-agnostic: <https://github.com/openclaw/openclaw>
- Hookdeck / Svix — edge normalization of provider payloads into one canonical schema: <https://hookdeck.com/webhooks/guides/webhook-gateway>
- Hexagonal architecture (Cockburn): <https://en.wikipedia.org/wiki/Hexagonal_architecture_(software)>
- attune: the existing `/v1/lark/event` handler, `cmd/attune/router.go`, the canonical contract (#19), the symmetric outbound side (#34).
