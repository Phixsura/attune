# Registry-driven source set (adapter-declared source names)

| | |
|---|---|
| **Issue** | #95 |
| **Status** | Accepted |
| **Started** | 2026-06-23 |
| **Related** | #66 (channel-agnostic inbound framework — provides the registry this builds on), #93 (MCP integration — owns the non-adapter `mcp` source), #34 (outbound notify adapter SDK — owns one of the two display callers), #6 (metric-label cardinality discipline — `attune_ingest_total.source`) |

## Problem

The #66 inbound framework promises **"adding a channel = a new package + one blank-import line, core packages untouched."** That holds at *runtime* — adapters self-register in `init()` via `inbound.Register` — but it is not yet true for the **source vocabulary**:

- `internal/domain/feedback.go:32` hard-codes `ValidSources = map[string]bool{api, webhook, email, web, mcp, other}`, the input validator's source allow-list.
- `internal/domain/feedback.go:55` hard-codes `SourceDisplayName(source) string`, a parallel `switch` mapping each source key to a human label.

Adding a third channel (Slack, RSS, …) therefore still requires **hand-editing two maps in the pure core domain package** — exactly the coupling #66 set out to remove. This is the documented follow-up to clean up after the first two adapters (webhook + email) landed.

### Why this is `needs-design` and not a trivial mechanical change

Three hard constraints (all verified against the codebase) interlock:

1. **`internal/domain` is a pure package** — it MUST NOT import any `internal/*` package (`feedback.go:1-7`, CLAUDE.md §5). The registry lives in `internal/inbound`, and `internal/inbound/inbound.go:25` already imports `internal/domain`. So `domain → inbound` would be a **direct import cycle**. The issue's literal phrasing — *"`ValidSources` becomes a snapshot of `registry.Sources()`"* — is impossible **inside `domain`**.

2. **The valid set is a union of two populations**, only one of which is adapter-backed:
   - **Adapter channels** — `webhook`, `email` today; future `slack`, `rss`, … — these self-register in the inbound registry. (Verified: only these two implement `Channel()`.)
   - **Non-adapter "core" sources** — `api` (direct `POST /v1/feedback/ingest` default), `web` (in-app JS widget), `mcp` (the #93 MCP HTTP handler, *not* an inbound adapter), `other` (catch-all). None has a `Start`/`Shutdown` lifecycle or a factory; none will ever be in the registry.

3. **`notify` and `service` are forbidden from importing inbound adapters** (depguard `inbound-boundary`, `.golangci.yml:70-82`; `**/internal/domain/**` is in the deny list too). `SourceDisplayName`'s two consumers — `notify/adapter/githubissue/github_issue.go:214` and `outbound/adapter/githubissue/githubissue.go:126` — therefore *cannot* reach registry-derived data by importing anything under `internal/inbound/adapter/*` or `internal/outbound/adapter/*`.

The consumers, fully enumerated:

| Consumer | Site | Uses |
|---|---|---|
| Ingest validation | `domain.IngestInput.Validate()` → caller `internal/service/ingest/ingestor.go:76` | `ValidSources` membership |
| Metric-label bound | `internal/handlers/ingest.go:139` `boundedSource()` | `ValidSources` membership |
| Guard-policy target | `internal/service/guardpolicy/guard_policy.go:199` `validateTarget` (free func, line 195) → `validValueList(..., domain.ValidSources)` (line 249) | `ValidSources` membership |
| GitHub issue body (notify) | `internal/notify/adapter/githubissue/github_issue.go:214` | `SourceDisplayName` |
| GitHub issue body (outbound) | `internal/outbound/adapter/githubissue/githubissue.go:126` | `SourceDisplayName` |

**One simplifier (verified):** `user_feedback.source` is `TEXT NOT NULL DEFAULT 'api'` with **no `CHECK` constraint** (`internal/infra/database/migrations/001_init.sql:59`). Validation is purely application-level — adding a source needs **no migration** and there is **no two-layer DB allow-list** to keep in sync (unlike audited actions).

## Goals / Non-goals

**Goals**

- Make the valid-source set and display labels **registry-derived**: adding a channel = new package + one blank-import line + (at most) one display string at the adapter's existing `Register` call. **Zero edits to a core source map.**
- Preserve every hard constraint: domain purity, the inbound/outbound adapter walls, the bounded `attune_ingest_total.source` label.
- Land **incrementally** (one reviewable PR at a time), never a big-bang breaking change.

**Non-goals**

- Per-tenant or locale-aware source labels (the labels stay English-canonical; #66's deferred i18n note still applies — this change makes a future locale-aware label *easier*, never harder).
- First-class multi-source-per-adapter (`Sources() []string`) — see §Multi-source decision; explicitly declined under YAGNI.
- A codegen toolchain for the source manifest — see Alternative C.
- Touching the DB schema (no constraint exists; none is added).

## Prior-art benchmark — 11 top-tier projects

How does a world-class system let a plugin declare its own identifier, keep an authoritative valid set, and let a *pure* validation layer check membership without depending on every plugin? We surveyed eleven (web-grounded; citations in §References).

| Project | Identifier declaration | Where the valid set lives | Pure-core validation | Codegen / runtime |
|---|---|---|---|---|
| **OpenTelemetry Collector** | `Factory.Type()` (from `metadata.yaml`) | `otelcol.Factories` map assembled in `components()` — **no global** | **DI of an assembled map**; error's "valid values" = `maps.Keys(injected)` | hybrid (codegen at edges, injected map at core) |
| **HashiCorp Vault** | central factory map + `Backend.Type()` | core factory map ∪ `builtinplugins.Registry` ∪ catalog, unioned at mount | **`BuiltinRegistry` *interface* declared in core "to avoid an import cycle", impl injected at wiring** | runtime registry |
| **Kubernetes** (admission) | `plugins.Register(name, factory)` | per-apiserver `*Plugins` value (not a global) | validate against injected `Registered() []string`; startup cross-check | runtime registry + ordered list |
| **CoreDNS** | `plugin.Register("whoami", setup)` | codegen `zdirectives.go []string` (order is semantic) + runtime map | parse takes **injected `validDirectives []string`** (`nil` = allow-all) | hybrid — codegen *only because order is load-bearing* |
| **Caddy v2** | `CaddyModule().ID` self-reg in `init()` | one process-global `map[string]ModuleInfo` | global-registry lookup; core never imports a plugin | pure runtime registry |
| **Telegraf** | `inputs.Add("cpu", creator)` | per-category global map; one blank-import file per plugin | `creator, ok := Inputs[name]`; depends on the registry pkg only | runtime registry |
| **Prometheus** (SD) | `Config.Name()` + `RegisterConfig` | `configNames`/`configFields` globals | `reflect.StructOf` dynamic validating struct; config pkg depends only on framework | runtime registry + reflection |
| **gRPC-Go** | `Builder.Scheme()` / `.Name()` | unexported global `map[string]Builder` | per-dial set checked **before** global; `resolver.Get` | runtime registry |
| **Grafana** | `plugin.json` `id`/`type` + `AliasIDs []string` | `registry.InMemory` built by DI at boot | injected `pluginRegistry` interface; validators never import a plugin | runtime DI |
| **Vector** | `#[configurable_component(sink("socket","desc"))]` | `inventory` registry iterated at runtime | typetag deserialization fails on unknown tag; config crate depends only on the trait | runtime registry (macro emits the reg call) |
| **Envoy** | `Factory.name()` + `configTypes() set<string>` | per-`Base` global `factories()` maps | `Config::Utility` looks up the global; core depends on the abstract `Base` | runtime registry |

### Cross-project patterns the strongest systems agree on

1. **The pure/validation layer never imports a concrete plugin.** It depends on an *abstraction* (interface/trait/framework pkg); the registry-derived concrete data is supplied from the composition root. (OTel injected `Factories`, **Vault `BuiltinRegistry` interface**, k8s injected `*Plugins`, CoreDNS injected `[]string`, Grafana injected `pluginRegistry`.) This is the dominant pattern and the **direct template for attune's domain-purity constraint**.
2. **The valid set is assembled once at the composition root and consumed as immutable plain data**, not read live from a mutable global by each consumer. Projects with attune's *exact* cycle constraint (Vault) **inject** rather than expose a global.
3. **Core/built-in identifiers are not a privileged type-level tier** — they are unconditional entries in the *same* assembled set (OTel "core ones are just unconditional entries", Prometheus `static_configs`, gRPC per-dial set). → `api/web/mcp/other` become a `CoreSources` literal merged into the union, never fake adapters.
4. **Display/human metadata travels with the registry entry**, co-located with the identifier at the plugin's own declaration site (OTel `display_name`, Grafana `JSONData.Name`, Vector macro's second arg). → the display label belongs on the `Register` call, not a second hand-maintained core `switch`.
5. **One-identifier-per-registration is the default**; multiplicity is N registrations, not a list field (OTel single `Type` + at most one deprecated alias; Caddy/CoreDNS/gRPC/Telegraf/k8s-admission/Vector all 1:1). First-class 1:N exists (Envoy `configTypes`, Grafana `AliasIDs`, k8s `Scheme`) but only for back-compat aliasing, behind heavier machinery.
6. **Validation-failure messages derive the "valid values" list from the assembled set** (OTel `maps.Keys(factories)`), not a hardcoded enum.
7. **Codegen is adopted only when a runtime registry cannot give something load-bearing** — semantic *order* (CoreDNS) or a vendorable minimal-binary *manifest* (OTel `ocb`). attune has neither, so a pure runtime/injected snapshot wins.

**The single closest prior art is HashiCorp Vault**: an interface (`BuiltinRegistry`) is declared in the core *specifically to avoid an import cycle*, the concrete implementation is injected at wiring, and the valid set is a union of an in-binary core tier plus a registry. That is attune's situation exactly, and the recommended design is its faithful translation.

## Prior-art benchmark — wave 2: four lenses across 11 product/connector/stdlib analogs

Round 1 settled the *mechanism*. A second wave went deeper, through four lenses that the mechanism survey did not cover, across the systems **closest to attune's actual job** (connector/integration platforms where the identifier is a persisted+wire token) plus the canonical Go stdlib registries: **Benthos / RedPanda Connect, Kafka Connect, Airbyte, Singer/Meltano, Terraform, Sentry, GitLab integrations, Gitea, n8n, Go stdlib (`database/sql`/`image`/`crypto`/`gob`), Backstage.** This wave **validates the Seeded-SourceSet spine with no structural change** — and surfaces four safety rails the mechanism-only design missed.

### Lens 1 — Migration history (enum → registry)

Every project that made this move used exactly attune's shape: **land the new path additively, keep the old identifiers resolving as shims, defer deletion**. Benthos v3→v4 kept the deprecated `Constructors` map + `TypeX` constants compiled for the *whole v3 line* while porting components one-by-one; Terraform 0.13 shipped an implied-default shim + bridge release; GitLab `services`→`integrations` kept a dual REST alias + a **frozen STI discriminator**; Sentry `plugins`→`integrations` used a shared-identifier bridge and disabled-not-deleted; n8n keeps every old node version forever via a pass-through shim. The identifier *strings* were preserved verbatim across each registry migration — only the plumbing changed. **This validates attune's additive-then-delete shape** (add the injected path, preserve every existing source string, remove the old map) at far larger scale; because attune has only 6 sources, it executes that shape as one atomic PR rather than a multi-release shim window (see §Implementation plan). The gap it exposes: nothing currently *proves* the new assembled set equals the old hardcoded set before the old map is deleted (→ cutover-equivalence test, run as the in-PR gate on that deletion).

### Lens 2 — Namespacing & security (the shadowing hole) — **must-fix**

This is the decisive finding. The silent **last-wins overlay map is a shipped, regretted footgun** in Benthos `Set.Add`, n8n `postProcessLoaders`, Gitea `webhookRequesters`, Sentry `register()`, `crypto.RegisterHash`, and `image.RegisterFormat` — every one lets a plugin silently shadow a built-in. The hardened/mature paths **fail-fast** instead: `database/sql` + `encoding/gob` **panic on duplicate**; Backstage throws "already registered" and scope-guards cross-plugin shadowing; Terraform **panics on a reserved/legacy namespace** and makes built-ins structurally unshadowable; Kafka refuses an ambiguous alias so neither wins; Benthos's *newer* `RegisterCustomResource` added a `reservedFieldNames` guard the old path lacks. **attune's current `buildSourceSet` is on the wrong side of this line** — see the must-fix below.

### Lens 3 — Persistence & wire stability (append-only) — **should-fix**

Unanimous across every analog that persists its identifier: **a persisted/wire identifier is frozen once shipped — additive-only, never rename in place, removal is soft-deprecate-and-keep-resolvable, never a hard delete.** Kafka `connector.class` is delete-and-recreate-only; GitLab froze the STI `type` string forever; Sentry persists `vsts` forever behind a "migrate before delete" TODO; Go `gob` (#36345) *refused* to retrofit a rename-alias onto a persisted name. attune's `source` is a dual storage+wire token (`user_feedback.source`, no `CHECK`, **plus** the outbox envelope) — and the repo's existing reflex is the anti-pattern: migration `015_drop_lark.sql:10` is a literal `DELETE FROM user_feedback WHERE source LIKE 'lark-%'`. → codify an append-only rule now, pre-1.0.

### Lens 4 — Metadata/i18n + validation UX + conformance

Deferring per-locale label i18n is the **right call** — none of the 19 benchmarked projects localizes the identifier or its primary display name; localization, where it exists (n8n, Terraform, GitLab), is a *separate lookup keyed by the stable token*, never a mutation of it. attune's plan to print `set.All()` on a validation error already **beats the entire field** (Benthos/Gitea/GitLab/Sentry/Airbyte/n8n/stdlib all emit a bare "not recognised"/404/sentinel). The gap: hand-maintained parallel lists **drift** — Gitea's `AllEvents()` provably lost `Schedule` + `PullRequestReview` despite a source comment forbidding it — so the registry invariant must be *tested*, and attune already owns the home for it (`inboundtest.TestAdapterContract`).

### Net result

The Seeded-SourceSet spine is the unanimous cross-project pattern and **holds unchanged**. Wave 2 adds rails: **(1, must-fix)** a reserved-name fail-fast in `buildSourceSet` — the one place the round-1 draft reasoned *against* the prior art; **(2)** an append-only identifier-stability contract; **(3)** a frozen-golden cutover/conformance test; **(4)** read-path graceful degradation + a validate-at-write/render-at-read rule; with did-you-mean and a named i18n seam as cheap polish (explicitly **not** accept-gates).

## Proposal

Adopt the **"Seeded SourceSet"** design: a pure, dependency-free `domain.SourceSet` interface (Vault's `BuiltinRegistry` seam), a `domain.CoreSources` literal for the never-an-adapter sources, the display label carried on the existing `Register` call (OTel/Grafana/Vector co-location), and the union assembled **once in `cmd/attune`** from `inbound.Factories()` ∪ `domain.CoreSources` and injected into the consumers.

### 1. Domain gains three pure additions (imports nothing internal)

```go
// internal/domain/feedback.go  — stdlib only (sort, fmt)

// SourceSet is the injectable, frozen union of valid sources + display labels.
// An interface PARAMETER is not an import: this is the Vault BuiltinRegistry seam
// that lets a pure package depend on registry-derived data without a cycle.
type SourceSet interface {
    Has(source string) bool      // membership (validation)
    Display(source string) string // human label, never empty, never panics
    All() []string               // sorted keys (for the error "valid values" list)
}

func NewSourceSet(entries map[string]string) SourceSet // immutable sourceSet{m}

// CoreSources: the never-an-adapter sources, now carrying their own labels.
var CoreSources = map[string]string{
    "api": "API client", "web": "Web Widget", "mcp": "MCP", "other": "Other",
}

// IsReservedSource reports whether a channel name is a core source an inbound
// adapter may never claim. One source of truth: a future core-source addition
// extends the reservation (and the boot-time collision guard) automatically,
// with no second hand-maintained list to drift. (Terraform reserves
// builtin/terraform.io; Backstage reserves core/backstage.io; Benthos has a
// reservedFieldNames set — wave-2 lens 2.)
func IsReservedSource(channel string) bool { _, ok := CoreSources[channel]; return ok }

// DefaultSourceSet reproduces TODAY's exact set for any caller without an
// injected one (pure-package tests + the SourceDisplayName fallback below).
func DefaultSourceSet() SourceSet // NewSourceSet(CoreSources ∪ {webhook:"Webhook", email:"Email"})
```

`sourceSet.Display` preserves today's contract: `if d, ok := m[s]; ok && d != "" { return d }; return s` (never empty, never panics on an unknown key).

`IngestInput.Validate()` changes signature to `Validate(set SourceSet) error`, with a **nil-guard** so callers migrate without lockstep:

```go
func (in IngestInput) Validate(set SourceSet) error {
    if set == nil { set = DefaultSourceSet() }
    // ...content checks unchanged...
    if !set.Has(in.Source) {
        return fmt.Errorf("invalid source %q (valid: %v)", in.Source, set.All())
    }
    return nil
}
```

In the single-PR cutover, **`domain.ValidSources` (map) is deleted** — all three consumers move to the injected set in the same change, so leaving it would be dead scaffolding (CLAUDE.md shipped-artifact hygiene). **`domain.SourceDisplayName` is kept**, reimplemented to delegate to `DefaultSourceSet().Display` — it is the *permanent* pure read-path fallback the github-issue renderers and the render-at-read rule (§5) require for historical / queued / retired tokens, not a transitional shim. **domain still imports nothing internal.**

### 2. The `Register` call carries the display label (not the `Adapter` interface)

```go
// internal/inbound/registry.go
func Register(channel, display string, factory Factory) // was Register(channel, factory)
type Entry struct { Channel, Display string; Factory Factory } // gains Display
```

The two adapters change one line each:
`inbound.Register(channelName, "Webhook", NewAdapter)` / `inbound.Register(channelName, "Email", NewAdapter)`.

The **`Adapter` interface is unchanged** — no `Sources()` and no `DisplayName()` method. The display label is *registration metadata*, not runtime behaviour, so it belongs on the registration record (OTel `metadata.yaml` / Grafana `JSONData.Name` / Vector macro-arg co-location). Putting it on `Register` avoids touching the `Adapter` interface, every concrete adapter struct, and the `inboundtest` fakes. `Channel()` stays the single runtime identity used by `Manager.StartAll`'s error messages.

### 3. `cmd/attune` assembles the union once and injects it

```go
// cmd/attune — runs AFTER main.go's adapter blank-imports have populated the
// registry in init(), so inbound.Factories() is complete.
func buildSourceSet(ctx context.Context) domain.SourceSet {
    m := maps.Clone(domain.CoreSources)        // api/web/mcp/other + labels
    for _, e := range inbound.Factories() {    // sorted snapshot, already exists
        // FAIL-FAST collision guard (wave-2, must-fix). The overlay below is a
        // silent last-wins shadow, NOT a tautology: CoreSources keys never pass
        // through inbound.Register, so Register's duplicate-channel panic does
        // not cover a CoreSources∩Factories() collision. Refuse it at boot.
        if domain.IsReservedSource(e.Channel) {
            logext.Errorf(ctx, "inbound adapter channel %q collides with a reserved core source; rename the adapter channel", e.Channel)
            os.Exit(1)
        }
        if _, dup := m[e.Channel]; dup {
            logext.Errorf(ctx, "inbound adapter channel %q registered twice", e.Channel)
            os.Exit(1)
        }
        m[e.Channel] = e.Display               // webhook→Webhook, email→Email, future rss→…
    }
    return domain.NewSourceSet(m)
}
```

`inbound.Factories()` is the **only** registry read, in the **only** legal blank-import site. The result is threaded into the three validators below.

The collision guard uses `logext.Errorf` + `os.Exit(1)` — **not** `log.Fatalf` — matching `cmd/attune`'s verified startup-failure convention (`main.go:73-92`) and CLAUDE.md §7's ban on direct `log/slog`. `domain.IsReservedSource` is a pure domain predicate (`CoreSources` is already a domain map), reachable from `cmd/attune` and `inbound` alike, so the guard lives at the composition root where both populations and the overlay are in scope. The complementary `factory().Channel() == registered channel` typo check is **additive**, not a substitute — it catches a different bug. A boot crash is correct here: the set is fixed forever at startup, so there is no request-path or availability cost.

### 4. Validation path (the three set-membership consumers)

> **The synthesis's first draft wired this wrong; the corrected, code-verified wiring is below.**

1. **Ingest validate** — `service/ingest.Ingestor` gains a `sources domain.SourceSet` field (new `NewIngestor` param); `IngestRow` calls `in.Validate(i.sources)`. **One** production call site (`ingestor.go:76`).

2. **Metric-label bound** — `handlers.IngestHandler` gains a `sources domain.SourceSet` field (new `NewIngestHandler` param); the free function `boundedSource(s)` becomes a method `func (h *IngestHandler) boundedSource(s string) string { if h.sources.Has(s) { return s }; return "invalid" }`. The happy-path record at `ingest.go:109` uses raw `in.Source` and is **left as-is** — it runs only after validation, so it is provably bounded. The `attune_ingest_total.source` label stays bounded by `srcSet.All()` (finite, fixed at startup) — **no metric-drift-gate impact** (no new metric; the label-value set is still bounded).

3. **Guard-policy target** — this is the consumer the first draft mis-wired. The validator lives in **`internal/service/guardpolicy.Service`** (`NewService(store Store)` at `guard_policy.go:63`, wired at **`cmd/attune/setup.go:214`**), **not** the repo `guardpolicy.New(pool)` at `server.go:252` (that is the runtime guard resolver behind `llmguard.NewClient` — a different object). The change:
   - `NewService(store Store)` → `NewService(store Store, sources domain.SourceSet)`; store the set on `Service`.
   - `validateTarget` is a **free function** (`guard_policy.go:195`), so convert it to a method `func (s *Service) validateTarget(t llmguard.Target) error` (or thread the set explicitly through its callers `createTenantPolicy`/`updateTenantPolicy`/`ReplaceTenantPolicies`).
   - `validValueList(values []string, allowed map[string]bool)` → `validValueList(values []string, allowed domain.SourceSet)`; body `!allowed[value]` → `!allowed.Has(value)`. `MaxTargetValues`/byte bounds untouched.
   - **Validation-UX upgrade (wave-2):** `validateTarget` today returns a **bare `ErrTargetInvalid` sentinel** with no offending value and no valid list — *strictly worse* than the ingest path. It has **three** distinct `ErrTargetInvalid` branches (`TenantID` set; bad `Channels`; the combined UUID/tag/env/purpose/stage block — `guard_policy.go:195-205`). **Only the `Channels` branch** is upgraded to surface the offending value + `set.All()`; the other two branches keep their opaque sentinel, so the source-list message never masks a UUID/tag/purpose/stage failure (the Backstage #4267 masking bug).

All three read the *same* immutable value, so adding a channel updates validate + metric-clamp + guard-policy at once with **zero core-map edits** — the #95 goal.

### 5. Display path (the two GitHub-issue consumers)

This is the wrinkle the panel underweighted, resolved against the actual code. Both display callers are **stateless `init()`-registered channels with no injection seam**: `outbound/adapter/githubissue` is reached via `RenderEvent(env, dst)` (no constructor/field to inject into without changing the `outbound.Channel` interface); `notify/adapter/githubissue` renders inside `buildIssueBody(env)` from a parsed JSON envelope. Neither can take a `SourceSet`, and neither may import an inbound/outbound adapter.

**Resolution — compute the label upstream where the `SourceSet` is injected, and carry it on the envelope.** The envelope builder is `buildOutboxEnvelope(s domain.Snapshot, traceID string)` (`enricher_outbox.go:354`, free function), emitting a **hand-rolled local struct `envelopeOut`** (`:375`) — *not* proto-generated — consumed as JSON by notify and `map[string]any` by outbound. So `source_display` is an **additive JSON field requiring no `make proto`**.

Recommended seam — **pre-stamp the label on `domain.Snapshot`** (consistent with its existing `DisplayTitle`/`DisplayRationale`/`DisplayLocale` fields): inject `srcSet` into the enrich runner; when it builds the `Snapshot`, set a new `Snapshot.SourceDisplay = srcSet.Display(source)`; `buildOutboxEnvelope` reads `s.SourceDisplay` (no signature change). The envelope gains a `SourceDisplay` string field with JSON tag `source_display,omitempty`.
- *notify githubissue:* add a `SourceDisplay` field (JSON tag `source_display`) to `attuneFeedback`, then `display := f.SourceDisplay; if display == "" { display = domain.SourceDisplayName(f.Source) }`.
- *outbound githubissue:* read `feedback["source_display"]` with the same fallback.

Both callers thus import **only `domain`** (for the fallback shim) and read a string field off the envelope — the inbound/outbound walls hold with **zero depguard rule changes**. `domain.SourceDisplayName` survives as the pure fallback for old in-flight outbox rows.

**i18n seam — named, not built (wave-2).** The seam *location* is now decided: the label is computed where both `s.Source` and `s.DisplayLocale` are in scope — the enrich-runner `Snapshot.SourceDisplay` pre-stamp (equivalently `buildOutboxEnvelope`, `enricher_outbox.go:354`, the only envelope-build site holding both). It is `set.Display(s.Source)` today and becomes `localize(s.Source, s.DisplayLocale)` at the *same* site later — the seam does not move. The load-bearing invariant: **the `source` string itself is never locale-bearing** (it is persisted in `user_feedback.source` and on the wire); localization is a lookup *keyed by* the stable token, never a mutation *of* it (n8n / Terraform / GitLab all do exactly this). This issue **names** the seam and stops there — no `localize()` function, no `DisplayLocale` plumbing.

**Read-path graceful degradation (wave-2).** `domain.SourceSet.Display(s)` **must** fall back to the raw key for any `s` not in the set (today's `SourceDisplayName` default branch, `feedback.go:69-71`), and the github-issue renderers **must** keep rendering a `Source` no longer in the live set. This is load-bearing, not incidental: the outbox is persisted-then-replayed, so a queued envelope can carry a `source` that was retired between enqueue and delivery, and the live set is now *dynamic* (`CoreSources ∪ Factories()`). Meltano #6359 / n8n `UnrecognizedNodeTypeError` are the anti-patterns (hard-fail on an orphaned persisted identifier). A test renders an envelope whose `Source` is **not** in `DefaultSourceSet()` and asserts the raw token renders rather than erroring/emitting empty.

**Validate-at-WRITE, render-at-READ (wave-2).** `SourceSet.Has` membership is checked **only at ingest**. The outbox delivery worker and every renderer treat `feedback.source` as **opaque** and **must never** re-validate it against the live set at delivery time — the envelope freezes its `source` at enqueue (`Version: "2"`, `enricher_outbox.go:389`), so a delivery-time `set.Has(source)` re-check would silently drop legitimate queued envelopes whose source was retired in the interim. A one-line note at the envelope-construction site records this so a future contributor does not "helpfully" add that re-check.

### Multi-source decision — keep `Channel()`-as-source (YAGNI)

One adapter declares exactly one source string; **do not add `Sources() []string`**. Justification grounded in both code and prior art: (1) attune has two adapters, each one channel — the RSS `rss/rss-atom/rss-json` case is hypothetical. (2) Prior art overwhelmingly defaults to 1:1 (OTel/Caddy/CoreDNS/gRPC/Telegraf/k8s-admission/Vector); first-class 1:N is reserved for back-compat aliasing behind heavier machinery. (3) Adding `Sources()` touches the interface, both adapters, every `inboundtest` fake, and the conformance suite for a capability with no caller. (4) **It forecloses nothing**: `inbound.Register` already supports N registrations from one package's `init()` — a future RSS package calls `Register` three times (each with its own display label, all backed by the same factory) and `buildSourceSet`'s union picks every one up automatically. If a real need lands, an *additive* `Aliases() []string` (Grafana's canonical-id + synonyms) can be grafted then.

### Layering proof

- **No `domain → internal` import.** Domain gains only the `SourceSet` interface, `NewSourceSet`/`sourceSet` (over `map[string]string`), and the `CoreSources`/`DefaultSourceSet` literals — all stdlib. An interface *parameter* on `Validate(set SourceSet)` is not an import; domain never names `inbound` (which would cycle, since `inbound.go:25` imports `domain`). depguard `inbound-boundary` lists `**/internal/domain/**` and denies `internal/inbound/adapter` — domain imports neither, so the rule is satisfied **unchanged**.
- **No `service/notify/handlers/repo → inbound/adapter` import.** The concrete `SourceSet` is assembled in `cmd/attune` (the only legal blank-import site, which already imports both `domain` and `inbound`) via `inbound.Factories()`, and injected *down* as a `domain.SourceSet` value. `service/ingest`, `handlers/ingest`, `service/guardpolicy`, and the enrich runner hold only the `domain.SourceSet` interface type. The two githubissue callers import only `domain` and read a string field. Both depguard walls hold with **zero rule changes**.

This mirrors Vault's `BuiltinRegistry` interface seam, OTel's injected factories map, and k8s's injected `*Plugins`.

### Identifier stability — the append-only contract (wave-2)

`source` is a **dual storage + wire token**: persisted in `user_feedback.source` (no `CHECK`) and replayed verbatim on the `notify_outbox` envelope that the github-issue adapter renders later. Every connector/integration analog that persists its identifier converged on the same rule, so attune adopts it explicitly:

> **A `source` string, once shipped in `domain.CoreSources` or any adapter `Channel()`, is frozen.** The valid set only **grows**: never rename a source in place, never repurpose its meaning, never hard-`DELETE` rows that carry it. Removal is **soft** — drop it from write-path validation only after it is absent from `user_feedback.source` **and** every queued `notify_outbox` envelope, and keep it resolvable on the read path via the `SourceDisplayName` fallback. A bare rename is a **rejection-grade** change; if a rename is ever truly needed, add a *new* token plus an explicit alias (Grafana `AliasIDs`-style).

Prior art: Kafka `connector.class` (delete-and-recreate only) + additive `.plugin.version`; GitLab's frozen STI discriminator; Sentry persisting `vsts` forever behind "migrate before delete"; Go `gob` #36345 (refused a rename-alias retrofit). The explicit anti-example is in *this* repo: migration `015_drop_lark.sql:10`'s `DELETE FROM user_feedback WHERE source LIKE 'lark-%'`. This rule should be mirrored as a one-paragraph CLAUDE.md §5b-equivalent (recommended follow-up, not a blocker), and is enforced mechanically by the frozen-golden `TestSourceVocabulary_AppendOnly` test rather than a lint script (test-only is sufficient for a list that changes ~4× in the project's life).

## Alternatives considered

**A. Injected `inbound.SourceCatalog`; delete *both* domain maps; typed `domain.Source`; inject a `SourceLabeler` into the display callers.** *Why not:* the outbound githubissue caller is a stateless `init()`-registered channel reached via `RenderEvent` with **no injection seam** — a `SourceLabeler` cannot be threaded in without changing the `outbound.Channel` interface (the design hand-waves this), and deleting `SourceDisplayName` outright removes the read-path fallback the render-at-read rule (§5) needs for historical / queued / retired tokens. The recommended design also does a single-PR cutover, but it *keeps* `SourceDisplayName` as that fallback and carries the label on the envelope, so it has no renderer-rewiring problem. A typed `domain.Source` adds `NewSource()` conversion noise for no safety gain at six sources (the OTel/Prometheus "flat map, not dynamic structs" lesson). A's good ideas (single-source-of-truth per channel, label co-located at `Register`) are **absorbed** here without its renderer-seam break.

**B. New `internal/inbound/sourcecatalog` leaf package imported directly by the display callers.** *Why not:* it *is* depguard-legal (the `inbound-boundary` deny targets `internal/inbound/adapter` only; the files glob `internal/inbound/*.go` does not cover subpackages), so it compiles — but it creates a **third home** for the same data (domain has the type, `cmd/attune` assembles, and now a leaf holds a package-global), reintroduces a global the validators could read *instead of* the injected value (two ways to ask one question), and still does not solve the `RenderEvent` seam — the envelope-carried label is needed regardless. Strictly more moving parts.

**C. Codegen a `sources.cfg` manifest → generated catalog + blank-import list + CI drift gate (CoreDNS school).** *Why not:* CoreDNS codegens **only because plugin order is semantically load-bearing** and Go needs an explicit import list; attune's sources have **no ordering**, removing codegen's entire justification (OTel/Caddy/Telegraf research explicitly warns this scale does not need it). It adds a toolchain, a make target, a CI drift gate, a committed generated artifact, and a Vector-style "name declared twice, must match" footgun — for ~6 sources, where a 12-line `buildSourceSet` union is already trivially reviewable. Over-engineering.

**D. Keep `domain.ValidSources` mutable; have `cmd/attune` append registry channels into it at startup.** *Why not:* reintroduces a **mutable package-global in the pure layer** initialized by side-effect ordering — the exact anti-pattern Vault/k8s injection avoids. Concurrency-fragile (read by validators while `cmd` mutates at boot), un-resettable for tests, init-order-dependent. The injected-immutable-value approach is the cross-project consensus precisely to avoid this.

## Risks / tradeoffs

- **`Validate` gains a parameter** — a signature change to the one production caller (`ingestor.go:76`, which passes the injected set) and to `feedback_test.go:81` (calls `Validate()` with zero args). Mitigated by the nil-guard defaulting to `DefaultSourceSet()`, which keeps `Validate` callable from pure-package domain tests with no wiring.
- **`source_display` is a new envelope JSON field.** In-flight outbox rows enqueued before the change won't have it; the fallback to `domain.SourceDisplayName` covers them — a test must exercise the missing-field path. Must stay additive-only (never rename `Source`).
- **Adapter display labels now live on the `Register` call.** A new adapter author must remember to pass a label. Mitigated by the `Display`-non-empty conformance assertion and the never-empty `Display` fallback to the raw key.
- **Single-PR review surface.** The cutover touches `domain` + `inbound` + `cmd/attune` + 3 validators + 2 renderers in one PR, so it loses the per-step revertibility a 5-PR split would give. Accepted deliberately: at 6 sources / ~10 touch points it stays reviewable, and the atomic cutover *eliminates* the transitional window in which the old maps and the injected set both exist (no "two ways to ask the same question", nothing half-migrated to sweep). The frozen-golden equivalence test makes the in-PR `ValidSources` deletion provably non-breaking.
- **`boundedSource` free-func → method** touches the metric-recording call sites in `ingestError`; a mistake could record a raw source and inflate `attune_ingest_total` cardinality. Covered by a test asserting an unknown source records `"invalid"`.
- **Silent shadowing of a reserved core source (must-fix; wave-2, verified).** `buildSourceSet`'s `m[e.Channel] = e.Display` overlay on `maps.Clone(CoreSources)` is a **silent last-wins shadow** — an adapter registering `api`/`web`/`mcp`/`other` overwrites a core entry with **zero signal**. `inbound.Register`'s duplicate-channel panic (`registry.go:43`) does **not** cover it, because core keys never pass through `Register`. Because `source` is the DB default *and* a wire value the github-issue renderer shows operators, this is persisted+wire identity confusion, and `boundedSource` would still treat the shadowed value as "known". *(My round-1 draft wrongly called the disjointness check "tautological" and proposed dropping it — that reasoning conflated membership, which is tautological, with non-collision, which the overlay actively violates. Reversed.)* **Mitigation:** the `IsReservedSource` fail-fast in `buildSourceSet` (§3) + the disjointness conformance test. Every hardened analog (`database/sql`/`gob` panic-on-dup, Terraform reserved-namespace panic, Backstage "already registered", k8s cross-check) fails-fast here; the silent-overlay camp (Benthos/Gitea/n8n/Sentry/`crypto.RegisterHash`) ships this exact bug.
- **The repo's existing reflex for source removal is a hard DELETE.** Migration `015_drop_lark.sql:10` is `DELETE FROM user_feedback WHERE source LIKE 'lark-%'` (+ outbox/target deletes), which would orphan queued envelopes and already-rendered issues. **Mitigation:** the append-only / soft-deprecate contract (§Identifier stability), codified now, pre-1.0, before real data makes a destructive DELETE catastrophic.
- **An existing comment-vs-map drift** already lives at `feedback.go:24-26`: the comment says the set is `{api, webhook, email, web, other}` — it **omits `mcp`**, which the map (`:37`) and `SourceDisplayName` (`:65`) both include. This is precisely the hand-maintained-parallel-list drift Gitea's `AllEvents()` suffered. **Mitigation:** the cutover-equivalence test asserts `ValidSources` keys == `SourceDisplayName` coverage == `DefaultSourceSet().All()`, closing this drift as a side effect; `All()` is derived solely from the assembled map.
- **`guardpolicy.validateTarget` returns a bare `ErrTargetInvalid` sentinel** (no value, no list) on three branches. Upgrading only the ingest message would leave guard-target validation strictly worse. **Mitigation:** the channels-branch-only error upgrade (§4), with the other two branches left opaque.
- **Introducing a *new* source string value is the one genuinely irreversible step** — there is no DB `CHECK` to retract it and old rows orphan. A whole-PR revert undoes the plumbing but not a shipped source string. **Mitigation:** this PR preserves the existing six source strings verbatim and introduces **no** new source; adding a source is a deliberate, separately-reviewed act, out of scope here.

## Implementation plan (single PR, ordered task list)

> Get this proposal **Accepted** before implementation, per the project's proposal-acceptance gate.

This ships as **one atomic PR**. At attune's scale — 6 sources, ~10 touch points — a single cutover is well within reviewable size, and it is *more* consistent with the decisions than a multi-PR split: there is no transitional window in which the old hardcoded maps and the injected set both exist (the "two ways to ask the same question" the shim approach tolerates), and no half-migrated intermediate state to sweep (CLAUDE.md shipped-artifact hygiene). The wave-2 migration analysis's incremental shim-and-defer recommendation was calibrated for hundreds-of-components migrations; here the full cutover is the cleaner option and keeps every decision below intact. The work is ordered as a task list, not as separate PRs:

1. **Domain types.** Add `SourceSet` + `NewSourceSet`/`sourceSet`, `CoreSources`, `IsReservedSource`, `DefaultSourceSet`. Change `Validate()` → `Validate(set SourceSet)` with the nil-guard (now serving pure-package tests + a sane default, no longer a cross-PR migration aid) and the `(valid: …)` list. **Delete `domain.ValidSources`** (all three consumers move to the injected set in this PR). **Keep `domain.SourceDisplayName`** — reimplemented to delegate to `DefaultSourceSet().Display` — as the permanent pure read-path fallback the renderers and the render-at-read rule require. Fix the `feedback.go:24-26` comment to include `mcp`.
2. **Registry carries display.** `Register(channel, factory)` → `Register(channel, display, factory)`; add `Entry.Display`, carry through `Factories()`; preserve the duplicate-channel panic; add the `factory().Channel() == registered` startup assertion. Extend `inboundtest.TestAdapterContract` (`contract.go`) to assert the registered `Display` is non-empty. Update webhook + email `init()` (one line each), the `inboundtest` fakes, **`inboundtest/contract.go:95,101`** (the `DuplicateRegisterPanics` path), `inbound_test.go`, `registry_test.go`.
3. **Assemble + inject.** Add `cmd/attune.buildSourceSet(ctx)` **with the `IsReservedSource` + duplicate fail-fast guard** (`logext.Errorf` + `os.Exit(1)`). Thread `srcSet` into `ingest.NewIngestor` (→ `in.Validate(i.sources)`, the single caller `ingestor.go:76`), `handlers.NewIngestHandler` (`boundedSource` → method), and **`service/guardpolicy.NewService(store, srcSet)` at `setup.go:214`** (convert `validateTarget` to a method; widen `validValueList` to `domain.SourceSet`; upgrade **only** the channels-branch error to surface value + `set.All()`, leaving the other two `ErrTargetInvalid` branches opaque).
4. **Registry-aware display on the envelope.** Inject `srcSet` into the enrich runner; add `domain.Snapshot.SourceDisplay = srcSet.Display(source)`; add additive `source_display` to `envelopeOut` (`enricher_outbox.go`); add the field + `SourceDisplayName` fallback to notify `attuneFeedback` and outbound `feedback["source_display"]`; add the one-line *validate-at-write/render-at-read* note at the envelope-construction site. Update the `enricher_outbox_test.go` call sites.
5. **Tests + callers.** Land all of §Verification, notably the frozen-golden `TestSourceVocabulary_AppendOnly` (the gate on the `ValidSources` deletion in step 1) and the cmd/attune-level disjointness test. Update `feedback_test.go:81` and any remaining `ValidSources`/`SourceDisplayName` test references.

**CHANGELOG:** one `### Changed` entry (registry-driven source set + display). **Gate:** `go vet`, `go build ./...`, `go test -race ./...`, `golangci-lint` (confirm **no** `.golangci.yml`/depguard change), `lizard`/`jscpd`, full `make ci-check`, plus a **real-LLM e2e ingest→enrich→github-issue** run confirming the label renders.

**Rollback:** the whole change reverts as one PR. The change is plumbing only — it preserves the existing six source strings verbatim and introduces **no new source value** (the one genuinely irreversible act, since a shipped `source` becomes a persisted+wire token with no DB `CHECK` to retract it). Adding a *new* source is explicitly out of scope here and stays a deliberate, separately-reviewed act.

**Down-scoped to nice-to-have follow-up (explicitly NOT accept-gates):** a `did-you-mean` on the validation error (an inline ~15-line stdlib Levenshtein over the 6-7 element set — **no** new `strext` package, **no** fuzzy-match dependency; pure UX polish riding on #95); mirroring the append-only rule into a CLAUDE.md §5b-equivalent (good hygiene, but the frozen-golden test is sufficient enforcement — do **not** build a lint script for a list that changes ~4 times in the project's life).

## Verification

- **Frozen-golden conformance (`TestSourceVocabulary_AppendOnly`, the gate on the in-PR `ValidSources` deletion):** assert (a) `DefaultSourceSet().All()` == the golden `{api,email,mcp,other,web,webhook}`; (b) `DefaultSourceSet().Display(k)` == today's `SourceDisplayName(k)` for every `k` (pin against the golden labels, since `ValidSources` is removed in this PR); (c) `SourceDisplayName` coverage == `All()` (closes the existing `feedback.go:24-26` comment-vs-map `mcp` drift); (d) `Display(x)` non-empty for every member; (e) no duplicate keys. `All()` derives solely from the assembled map. A comment records that removing a golden entry is a documented breaking change.
- **Disjointness (cmd/attune-level):** assert `domain.CoreSources` keys ∩ `inbound.Factories()` channels == ∅ — the test-time form of the boot guard. It lives at `cmd/attune` (not `inboundtest`, which imports only `inbound` and cannot see `CoreSources`), where both the populated registry and `CoreSources` are visible.
- **Read-path degradation:** a renderer test with a `Source` **not** in `DefaultSourceSet()` (retired/queued token) asserts the raw token renders, never an error or empty; plus the `source_display`-missing fallback path.
- **Other unit:** `SourceSet.Has/Display/All`; `Validate(nil)` ≡ old behaviour and the `(valid: …)` list; `Entry.Display` round-trip + the `factory().Channel()==registered` assertion + `Display`-non-empty contract; `guardpolicy` rejects an unknown target channel **with** value + `set.All()` on the channels branch while the other `ErrTargetInvalid` causes stay opaque; `boundedSource` records `"invalid"` for an unknown source.
- **Integration / gates:** `go vet`, `go build`, `go test -race ./...`, `golangci-lint` (depguard unchanged), `lizard`/`jscpd`, `make ci-check`.
- **Boundary proof:** confirm `golangci-lint` still passes with **no `.golangci.yml` change** — evidence the layering holds.
- **Real-LLM e2e:** one ingest → enrich → GitHub-issue run showing the source label rendered from the registry-derived set, per the project's real-LLM acceptance bar.

## Decisions for reviewer sign-off

**Wave-2 decisions (recommended; need explicit acceptance):**

1. **Collision / reserved-name policy — adopt fail-fast.** `CoreSources` keys are RESERVED; an inbound adapter channel that collides is a fatal boot error in `buildSourceSet` (via `domain.IsReservedSource`), never a silent overlay. This **reverses** the round-1 draft's "tautological, drop it." Accept the reversal + the new predicate.
2. **Identifier-immutability — adopt the append-only contract** (§Identifier stability), codified in the proposal and recommended for a CLAUDE.md §5b mirror, with migration 015 as the named anti-example.
3. **Validate-at-write / render-at-read** — membership checked only at ingest; outbox delivery + renderers treat `source` as opaque.
4. **i18n seam location** — pinned at the enrich-runner `Snapshot.SourceDisplay` pre-stamp; the source string is never locale-bearing. (Names the seam; builds no i18n.)
5. **Conformance/cutover-equivalence test as a hard gate** on the in-PR `ValidSources` deletion (it proves `DefaultSourceSet()` equals today's set before the old map is removed); extend `inboundtest.TestAdapterContract` with `Display`-non-empty.

**Genuinely open:**

6. **`CoreSources` home:** `domain` (recommended — pure tests, `DefaultSourceSet`, and now `IsReservedSource` all need it) vs. a `cmd/attune` literal. Recommend `domain`.
7. **Display-seam injection point:** the *location* is decided (`Snapshot.SourceDisplay` pre-stamp); only the exact enrich-runner `srcSet` injection point remains, confirmed during implementation.
8. **`log.Fatalf` vs `panic`** at the guard — resolved to `logext.Errorf` + `os.Exit(1)` to match `cmd/attune` (`main.go:73-92`) and CLAUDE.md §7. Flagged for confirmation only.
9. **Proto:** confirmed `envelopeOut` is hand-rolled JSON/`map[string]any` — `source_display` is additive, **no `make proto`**. Reviewer to confirm no SDK wire type (`sdk/go`, `@phixsura/attune`) mirrors `feedbackOut`; if one does, the field is still additive but should be noted.
10. **Multi-source roadmap:** is an RSS-style 1:N adapter imminent? If yes, an additive `Aliases() []string` may beat N `Register` calls. Assumes **not** (YAGNI) — confirm against the tracker.
11. **`did-you-mean`:** include the inline ~15-line Levenshtein now (UX polish, **not** an accept-gate) or defer? Recommend defer to a follow-up.

## Post-implementation hardening (10-project code review)

After the implementation landed green, the actual code was benchmarked against ten top Go registries (OTel Collector, Vault, Caddy, Telegraf, Kubernetes, CoreDNS, gRPC-Go, Go stdlib `database/sql`/`gob`, Grafana, Benthos). The core seam was judged at/above bar (injected immutable `SourceSet`, sorted-deterministic `All()`, panic-on-duplicate `Register`, `testing.Testing()`-gated reset, pure-domain interface). Three cross-project-consensus gaps were fixed:

1. **`buildSourceSet` returns `(SourceSet, error)` instead of `os.Exit` inside the helper** (k8s/OTel `MakeFactoryMap` discipline; flagged by 8/10). The pure `assembleSourceSet(core, entries)` is the single dedup authority and is now directly unit-testable on the reserved-collision and duplicate-channel branches; the terminal exit stays at `main.go` (`logext.Errorf` + `os.Exit`, convention preserved).
2. **`domain.CoreSources` unexported** behind `IsReservedSource` + a clone-returning `CoreSources()` accessor (no benchmarked Go registry ships a mutable exported backing map; concurrent mutation of the per-feedback render path was an unrecoverable hazard; flagged by 7/10).
3. **`inbound.Register` rejects a nil factory / empty channel / empty display** at the single write point (the `sql.Register`/`gob.Register`/`RegisterModule` guarantee; `channel` is a frozen persisted+wire token; flagged by 8/10).

Plus a `DefaultSourceSet`-vs-assembled-set drift-guard test and comma-joined valid-source error strings. Deliberately **rejected as over-engineering at 6 sources**: a channel-shape regex, a typed `Source` newtype, `Sources() []string`, did-you-mean, alias-ID rename resolution, case-folding, a shared `internal/pkg/registry[V]` extraction, and fat `SourceSet` accessors.

## References

- #66 — channel-agnostic inbound framework: `docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md` (provides `inbound.Register`/`Factories`/`Adapter`).
- Code: `internal/domain/feedback.go:32,55`; `internal/inbound/registry.go`; `internal/inbound/adapter/{webhook,email}`; `internal/service/ingest/ingestor.go:76`; `internal/handlers/ingest.go:139`; `internal/service/guardpolicy/guard_policy.go:63,195,249`; `cmd/attune/setup.go:214`, `server.go:252`; `internal/service/enrich/enricher_outbox.go:354,375`; `internal/{notify,outbound}/adapter/githubissue`.
- Prior art (verified during this proposal):
  - OpenTelemetry Collector — `component/identifiable.go`, `otelcol/factories.go`, `otelcol/internal/configunmarshaler/configs.go`.
  - HashiCorp Vault — `helper/builtinplugins/registry.go` (the `BuiltinRegistry` interface seam).
  - Kubernetes — `apiserver/pkg/admission/plugins.go`.
  - CoreDNS — `plugin.cfg` → generated `zdirectives.go`.
  - Caddy v2 — `modules.go`. Telegraf — `plugins/inputs/registry.go`. Prometheus — `discovery/registry.go`. gRPC-Go — `resolver/resolver.go`. Grafana — `pkg/plugins/`. Vector — `lib/vector-config/`. Envoy — `envoy/registry/registry.h`.
- Wave-2 prior art (four lenses — migration / namespacing / persistence / conformance):
  - Benthos / RedPanda Connect — `internal/bundle` Set.Add (silent last-wins) + the newer `RegisterCustomResource` `reservedFieldNames` guard; v3→v4 deprecated-constants shim.
  - Go stdlib — `database/sql` `Register` + `encoding/gob` (panic-on-dup); `image.RegisterFormat`, `crypto.RegisterHash` (silent-shadow cautionary).
  - Terraform — `hashicorp/terraform-registry-address` (`tfaddr`), reserved `builtin`/`terraform.io`, 0.13 namespacing migration, tfstate-persisted provider addresses.
  - Kafka Connect — connector `class` FQCN + `plugin.discovery` HYBRID_FAIL cutover gate. GitLab — `services`→`integrations` rename, frozen STI `type`. Sentry — `plugins`→`integrations`, `vsts`-forever. Gitea — `AllEvents()` parallel-list drift (the cautionary tale). n8n — versioned node types, `UnrecognizedNodeTypeError`. Airbyte — immutable connector `definitionId`. Backstage — reserved `core`/`backstage.io`, "already registered" guard.
