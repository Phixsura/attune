# Feature-based package organization (backend hybrid + console migration)

| Field    | Value |
|----------|-------|
| Issue    | #19 (host PR #67) — original IDL-contract scope was broadened per user direction to also land the architecture refactor |
| Status   | **Implemented** (2026-06-06) — see §10 below for exactly what shipped vs deferred |
| Started  | 2026-06-06 |
| Shipped  | 2026-06-06 (PR #67, 13 commits, mergeState=CLEAN) |
| Follow-ups | #68 (polish · 9 items) · #69 (handlers/console subpackage split, deferred) · #70 (altitude backlog · 3 items) |
| Related  | #66 (inbound-adapter framework, separately deferred) · #10 (per-tenant enrich config, downstream) |

> **Author's note.** Engineering recommendation was to defer the backend split until roadmap pressure showed `service/` had become a grab-bag, and to land architecture refactor in a separate PR from #19's IDL contract. User explicitly chose the more aggressive path on both counts after hearing the trade-offs. This proposal honors that choice and documents the costs in §6 so they are visible to future readers.

---

## 1 · Problem

A 2026-06-06 architecture review (cross-validated against four top-tier Go repos and `bulletproof-react`) confirmed the layering contract in CLAUDE.md §5 is sound and **all four cross-layer rules hold in code** (verified by `Explore` agent, grep evidence). But four specific structural gaps remain:

1. **`internal/service/` is a 14-file flat directory** mixing six unrelated concerns — `enricher*.go` (4 files), `outbox_worker*.go` (3), `apikey.go`, `ingestor.go`, `eval*.go` (2), `digest_weekly.go`, `notifier.go`, `triage.go`. A reviewer cannot tell, by looking at the tree, which feature any new file relates to. None of `gitea/services/`, `prometheus/`, `memos/internal/`, or `pocketbase/core/` organize a flat 14-file layer at this size — they all sub-feature inside the layer.
2. **`internal/repo/` has the same shape at smaller scale** (11 files) — `feedback*.go` (3), `notify_targets*.go` (3), `apikey.go`, `outbox.go`, `tenant*.go` (2), `lark_install.go`.
3. **Console SPA is type-based** (`src/components/feedback/`, `src/components/notify-targets/`, plus a 201-line `src/api/queries.ts` holding every resource's queries). Each console feature is fragmented across three places. `bulletproof-react`'s consensus is feature-based (`src/features/<x>/{api,components}`) with co-located React Query options.
4. **No tooling enforces unidirectional imports in the console.** CLAUDE.md §5 codifies backend layering; the console has no equivalent. `bulletproof-react` uses ESLint `import/no-restricted-paths` for `shared → features → app` direction and cross-feature isolation. attune's console uses Biome, which has no direct equivalent (verified — see §4-B).

A fifth, smaller naming asymmetry: `internal/observability/` sits at top level while its sibling `internal/infra/trace/` and `internal/infra/metrics/` are under `infra/`. Semantically defensible (`observability/` is bootstrap-only, called once by `cmd/`), but inconsistent with everything else under `infra/`.

---

## 2 · Goals

- Make **feature** the primary organizing axis *inside* each backend layer (gitea hybrid: layer outside, feature inside) and *at the top level* in the console (bulletproof-react style).
- Preserve the CLAUDE.md §5 four-layer contract. **Do not collapse layers.** They are tested, enforced, and proven (architecture review verified all four rules).
- Add **CI-enforced unidirectional import boundaries** in the console — at minimum `shared → features → app` direction and no cross-feature imports.
- Normalize `internal/observability/` placement under `internal/infra/observability/` to match siblings.
- Update CLAUDE.md §5 + README §Package layout + CHANGELOG to reflect the new shape.

## 3 · Non-goals

- **Not changing the HTTP wire contract.** All URLs, fields, status codes unchanged. This is purely structural.
- **Not collapsing `handlers/service/repo/notify` into one folder per feature** (Ben Johnson root-domain style). That model is also good — see §5 — but it discards the existing §5 contract and that is a separate conversation.
- **Not abolishing `cmd/attune/`** as the composition root. Untouched.
- **Not rewriting any tests.** Tests follow their unit; only import paths update.
- **No new features.** The PR has zero `Added` content. Only `Changed`.
- **Not migrating routing.** TanStack Router file-based routing in `src/routes/` stays as the source of truth; route files become thin glue that import from `src/features/`.

## 4 · Proposal

### A · Backend — hybrid layer-outside / feature-inside (gitea pattern)

The current four-layer contract is preserved at the *outer* level. Each layer grows feature subpackages. **All current file content is moved, not rewritten** — only `package` declarations and import paths change.

**Target tree** (file-level mapping verified against current `ls` output):

```
internal/
  domain/                          (unchanged — pure types, 3 files)
  service/
    enrich/                        ← enricher.go, enricher_helpers.go, enricher_parse.go,
                                     enricher_outbox.go, triage.go
    ingest/                        ← ingestor.go
    outbox/                        ← outbox_worker.go, outbox_worker_alerts.go,
                                     outbox_worker_send.go, notifier.go, digest_weekly.go
    apikey/                        ← apikey.go
    eval/                          ← eval.go, eval_report.go
  repo/
    feedback/                      ← feedback.go, feedback_console.go,
                                     feedback_console_stats.go
    outbox/                        ← outbox.go
    apikey/                        ← apikey.go
    notify/                        ← notify_targets.go, notify_targets_alerts.go,
                                     notify_targets_crud.go
    tenant/                        ← tenants.go, tenant_users.go
    lark/                          ← lark_install.go
  notify/                          (transport layer split)
    transport.go                   (kept at root — shared Transport)
    testsend.go                    (kept at root — shared)
    adapter/
      rawwebhook/                  ← raw_webhook.go
      larkcard/                    ← lark_card.go
      larkwebhook/                 ← lark_webhook.go
      githubissue/                 ← github_issue.go
  handlers/
    ingest.go                      (kept flat — single public endpoint)
    lark.go                        (kept flat — single public endpoint, #66 boundary)
    console/
      router.go                    (kept at root — wires the chi tree)
      respond.go                   (kept at root — shared helper)
      session/                     ← auth.go, oauth.go, dev_login.go, me.go
      apikey/                      ← api_keys.go
      feedback/                    ← feedback.go
      notifytarget/                ← notify_targets.go, notify_targets_patch.go,
                                     notify_targets_write.go
      usage/                       ← usage.go
  infra/
    apikey/  config/  database/  lark/  llmclient/
    metrics/  ratelimit/  trace/  observability/   ← moved from internal/observability/
  logext/                          (unchanged — pure slog wrapper)
  proto/attune/v1/                 (unchanged — generated)
```

**Why hybrid (not pure feature):**

- The §5 four-layer contract is already enforced and verified clean. Replacing it forces a contract rewrite the proposal explicitly avoids (Goals §2).
- **gitea** runs this exact pattern at large scale: `services/issue/`, `services/pull/`, `services/repository/` … (40 feature subpackages) and `models/issue/`, `models/repo/` … . Verified by direct tree inspection.
- **memos** uses dependency-grouped infra (`internal/{ai,storage,webhook}`) with file-per-entity flat layer (`store/memo.go`, `store/user.go`) — a lighter variant of the same hybrid idea.
- Pure feature packages (Ben Johnson `postgres/` `http/` adapters) require a deep rewrite of imports and the §5 contract; rejected as an alternative (§5).

**Layer-by-layer change density** (so reviewer knows where the weight is):

| Layer | Files moved | Approx LOC churn | Risk |
|---|---|---|---|
| `service/` → feature subdirs | 14 | high (all callers update) | highest — biggest blast radius |
| `repo/` → feature subdirs | 11 | medium | medium — fewer callers (only `service/` and `handlers/console/`) |
| `notify/` → `notify/adapter/<adapter>/` | 4 of 6 | low | low — `notify` package already mostly internal |
| `handlers/console/` → feature subdirs | 9 of 12 | low | low — only `router.go` calls them; routes already grouped by resource |
| `internal/observability/` → `infra/observability/` | 4 | trivial | trivial — only `cmd/attune/main.go` and `server.go` import it |

**Naming choices** (Go-idiomatic, no stutter, no dashes):

- `notifytarget` not `notify-target` — Go directory ⇒ package name, hyphens illegal.
- `notify/adapter/rawwebhook/` not `notify/raw_webhook/` — Go package names are lowercase, no underscores.
- `session/` not `auth/` for console — matches the proto `SessionService` we just landed.
- `notifytarget/` (target CRUD) is distinct from `notify/` (outbound delivery transport) — same root noun, different layers, no name collision.

**Cross-layer rules — unchanged contract, re-verified after each layer's move:**

```
handlers  →  service  →  repo
                       →  notify (transport in infra)
                       →  infra/llmclient
handlers  →  domain  (pure types, any direction)
handlers  →  infra/apikey (middleware) → service (via Verifier interface)
```

Plus the four rules:
1. handlers never writes SQL
2. service never writes HTTP (response writer)
3. notify never imports service
4. infra never imports service or repo

The CLAUDE.md §5 block stays the same — it talks about *layers* and the layers still exist. The diagram in README's "Package layout" gets one new indent level showing feature subpackages.

### B · Console — feature-based + co-located React Query + dependency-cruiser

**Target tree** (file-level mapping):

```
console/src/
  app/                                  (new — bulletproof's `app/` layer)
    provider.tsx                        (extracted: QueryClientProvider, i18n, etc.)
  features/
    feedback/
      api/                              ← split from src/api/queries.ts
        feedback-keys.ts                 (queryKey factory)
        get-feedback-detail.ts
        list-feedback-infinite.ts
        get-feedback-stats.ts
      components/                       ← from src/components/feedback/
        badges.tsx
        detail-sheet.tsx
        kind-donut.tsx
    notify-targets/
      api/
        notify-target-keys.ts
        list-notify-targets.ts
        create-notify-target.ts
        update-notify-target.ts
        delete-notify-target.ts
        test-notify-target.ts
      components/                       ← from src/components/notify-targets/
        dialogs.tsx
        edit-dialog.tsx
        table.tsx
    api-keys/
      api/
        api-key-keys.ts
        list-api-keys.ts
        create-api-key.ts
        delete-api-key.ts
      components/                       ← from src/components/api-keys/
        dialogs.tsx
    usage/
      api/
        usage-keys.ts
        get-usage.ts
      components/                       ← from src/components/usage/
        bar-chart.tsx
        sparkline.tsx
    session/
      api/
        session-keys.ts
        get-me.ts
        logout.ts
  components/                           (shared only — bulletproof's `ui/+layouts`)
    ui/                                 (kept — design-system primitives)
    brand/                              (kept)
    empty-state.tsx                     (was components/empty-state.tsx — shared)
    topbar.tsx                          (was components/topbar.tsx — shared layout)
  lib/
    api-client.ts                       ← from src/api/client.ts
    utils.ts                            (kept)
  proto/                                (unchanged — generated, biome-excluded)
  routes/                               (unchanged location — TanStack file-based)
    __root.tsx, _authed.tsx, login.tsx, index.tsx
    _authed.feedback.tsx                (thin — re-exports from features/feedback)
    _authed.notify-targets.tsx
    _authed.api-keys.tsx
    _authed.usage.tsx
  i18n/, styles/                        (unchanged)
```

**Three structural changes:**

1. **Dissolve `src/api/queries.ts`** into per-feature `features/<x>/api/` files. Each feature exposes:
   - a queryKey factory (`feedbackKeys.detail(id) → ['feedback','detail',id]`)
   - typed fetchers calling `lib/api-client.ts`
   - `queryOptions()` blocks (so routes can prefetch with the same key)
   - thin `useX()` hooks
   This matches bulletproof-react's `features/discussions/api/get-discussions.ts` shape verbatim.
2. **Move `src/api/client.ts` to `src/lib/api-client.ts`** — bulletproof puts the single configured client instance in `lib/`. `src/api/` directory goes away.
3. **Routes become thin glue.** `_authed.feedback.tsx` etc. stay in `src/routes/` (TanStack Router requires it) but import every component/hook from `@/features/feedback`. No business logic in route files.

**Boundary enforcement — `dependency-cruiser`** (not Biome, not ESLint side-by-side). After researching three options (Biome `noRestrictedImports` + overrides; `eslint-plugin-boundaries`; `dependency-cruiser`), the decision is dependency-cruiser. Rationale:

- **Biome native is the cheap nominal first choice but disqualified twice:** (a) `noRestrictedImports` matches the literal import specifier — no tsconfig alias resolution — so a contributor using `../foo` bypasses any rule written against `@/features/foo`. Confirmed by reading the rule's Rust source. (b) No capture-group / per-source-folder rule; "no feature imports any other feature" devolves into an O(n²) `overrides` matrix in `biome.json`.
- **ESLint plugins (boundaries / Sheriff) need 3–4 deps** plus a second linter that has to be neutered to avoid double-reporting with Biome — two CI steps, two configs, friction.
- **dependency-cruiser** is **one** dev dependency, no ESLint, resolves tsconfig aliases natively, supports regex `$1` capture for cross-feature isolation, and runs as `depcruise --config .dependency-cruiser.cjs src` — single CI line. Zero overlap with Biome's responsibility.

Initial ruleset (`console/.dependency-cruiser.cjs`):

```js
module.exports = {
  forbidden: [
    {
      name: "no-cross-feature",
      severity: "error",
      comment: "A feature may not import another feature (bulletproof-react §unidirectional).",
      from: { path: "^src/features/([^/]+)/.+" },
      to:   { path: "^src/features/([^/]+)/.+",
              pathNot: "^src/features/$1/.+" },
    },
    {
      name: "shared-no-up",
      severity: "error",
      comment: "Shared layers (components/lib/proto) must not import features or routes.",
      from: { path: "^src/(components|lib|proto)/.+" },
      to:   { path: "^src/(features|routes|app)/.+" },
    },
    {
      name: "features-no-app",
      severity: "error",
      comment: "Features must not reach into app/ or routes/ (shared→features→app one-way).",
      from: { path: "^src/features/.+" },
      to:   { path: "^src/(app|routes)/.+" },
    },
    { name: "no-circular", severity: "error", from: {}, to: { circular: true } },
  ],
  options: {
    tsConfig: { fileName: "tsconfig.json" },
    tsPreCompilationDeps: true,
    doNotFollow: { path: "node_modules" },
    exclude: { path: "src/proto/" },
  },
};
```

`console/package.json` gets `"arch": "depcruise --config .dependency-cruiser.cjs src"`. CI `console` job adds one step:
```yaml
- run: pnpm arch
```

### C · Observability alignment

- `git mv internal/observability internal/infra/observability`.
- Update `cmd/attune/main.go` and `cmd/attune/server.go` imports.
- Update CLAUDE.md §5 (the layer rules mention `infra/*` — no functional change) and README §Package layout (one path update).

This is the smallest change and lands in its own commit to keep the diff isolated.

### D · CLAUDE.md §5 + README updates

CLAUDE.md §5 is **expanded, not replaced**. The current block describes layer-level rules; we add a one-paragraph "feature subpackages inside each layer" note plus the updated layer diagram. The four cross-layer rules stay verbatim.

README's "Package layout" tree gets the new indent level showing feature subpackages.

CHANGELOG `[Unreleased] · Changed`:
- Backend reorganized into hybrid layer/feature packages (#19) — `service/`, `repo/`, `notify/adapter/`, `handlers/console/` now have feature subpackages. Cross-layer rules unchanged.
- Console migrated to feature-based `src/features/<x>/` layout (#19) — per bulletproof-react conventions. React Query options co-located per feature.
- `internal/observability/` → `internal/infra/observability/` for consistency.

CHANGELOG `[Unreleased] · Added`:
- `dependency-cruiser` CI gate enforcing console unidirectional import boundaries.

---

## 5 · Alternatives considered

| # | Alternative | Verdict |
|---|---|---|
| 1 | **Status quo — don't refactor** | Engineering recommended this until roadmap pressure justified the churn. User chose to refactor preemptively. Documented in Risks §6. |
| 2 | **Pure feature packages** (Ben Johnson — root domain + adapters by dep, no `handlers/service/repo/` split) | Cleaner end state. But forces full rewrite of CLAUDE.md §5 layering contract, all four cross-layer rules need new expressions, and it's a much bigger blast radius than hybrid. Rejected because §5 contract is *already proven and tested* — replacing it has no concrete payoff over hybrid for the team's current size. |
| 3 | **Pure flat packages by capability** (prometheus pattern — `tsdb/ promql/ scrape/`) | Works for capability-bounded subsystems (storage engine, query parser). Doesn't fit attune's HTTP-CRUD-with-async-fanout shape — features cross layers, capabilities don't. |
| 4 | **Biome-native boundary enforcement** | Disqualified — no alias resolution, no capture-group cross-feature rule, O(n²) overrides matrix. Detailed in §4-B. |
| 5 | **eslint-plugin-boundaries / Sheriff side-by-side with Biome** | Works, but 3–4 dev deps + second linter to neuter + second CI step. dependency-cruiser is 1 dep + 0 linters. |
| 6 | **Keep `internal/observability/` at top level** | Defensible (bootstrap-only). But inconsistent with `infra/trace`, `infra/metrics`. Cost of moving = 5 edits; benefit = naming consistency. Move. |
| 7 | **Land in separate PR, not #67** | Engineering recommendation. Avoids coupling IDL completion to refactor risk; keeps #67 small and reviewable. User chose fold-into-#67 after hearing this. Documented in Risks §6. |

---

## 6 · Risks / tradeoffs

This is a structural refactor with no behavior change but a **large mechanical diff**. Honest accounting:

1. **Folded into PR #67 against engineering recommendation.** PR #67 currently is 4 IDL commits, `mergeState=CLEAN`, all 19 CI checks green. Adding 6–10 refactor commits:
   - **Couples IDL completion to refactor risk.** A bug introduced by the refactor blocks the IDL contract from merging.
   - **Stretches #19's semantics.** Issue #19 = "protobuf IDL contract". The PR will no longer match the issue scope; requires retitling both.
   - **Large diff harms reviewability.** Every backend file's `package` declaration + every import path + every console import = thousands of mechanical lines.
   - **Mitigation:** one feature per commit, gates green between every commit, refuse to push until local fully green.
2. **`service/` split before roadmap pressure tested the seams.** Engineering judgment was to wait until at least one of {clustering, daily digest, multi-channel} landed and showed which sub-features actually grouped. Pre-splitting risks **the wrong split**: e.g. if `enricher_outbox.go` later becomes "the hand-off pipeline" worth its own package, but we've already put it in `service/enrich/`. Mitigation: chosen sub-packages are conservative (track concrete file co-occurrence patterns) and reversible.
3. **Console feature boundaries before features have grown.** Same risk shape: today's `feedback` feature has 3 components and ~3 queries; the boundary may need redrawing once RBAC / audit / workflow (v1.0 roadmap) land. Mitigation: dependency-cruiser rules express *intent* not file lists — they keep working as features grow.
4. **dependency-cruiser baseline.** First run on the migrated tree must show **zero** violations. If migration leaves a real cross-feature import (e.g. `notify-targets` re-using a `feedback` badge component), the rule fails. Mitigation: if it surfaces, the offending component is moved to `src/components/` (= shared); rule wins, no waivers.
5. **CLAUDE.md §5 stays the same wording**, but the tree under each layer changes. Anyone reading §5 then `ls internal/service` will need to also `ls internal/service/<feature>`. Mitigation: §5 gets one expansion paragraph.
6. **Generated proto code is not touched.** `internal/proto/` and `console/src/proto/` keep their current generated layout. (ts-proto already generates one file per `.proto`, so per-feature reorganization would require regenerator changes — out of scope.)
7. **Reverse: nothing prevents future drift.** Hybrid is harder to drift than pure-flat because each feature has its own directory; but Go doesn't enforce subdirectory boundaries the way it enforces `internal/`. Mitigation: CLAUDE.md §5 names the pattern; reviewer enforces by inspection on new PRs.

---

## 7 · Implementation plan

**Each commit is independently green** (build/vet/test/lint/biome/tsc/depcruise — whichever apply to that commit's scope). No commit is allowed to push until local is fully green.

| # | Commit | Scope | Layers touched | Verify |
|---|---|---|---|---|
| 1 | **observability move** | `git mv internal/observability internal/infra/observability` + import updates in `cmd/attune/{main,server}.go` | infra/ | go build/vet/test |
| 2 | **dependency-cruiser baseline** | Add `dependency-cruiser` devDep to console, create `console/.dependency-cruiser.cjs` with rules from §4-B, add `pnpm arch` script. **NOT yet in CI**. Verify it reports 0 violations on the *current* type-based tree (the rules forbid moves that don't exist yet, so it should pass). | console toolchain only | `pnpm arch` exits 0 |
| 3 | **console: feedback feature** | Create `src/features/feedback/{api,components}`. Move `src/components/feedback/*` → `features/feedback/components/`. Split feedback portions of `src/api/queries.ts` → `features/feedback/api/*.ts`. Update `routes/_authed.feedback.tsx` imports. | console | biome+tsc+vite build+`pnpm arch` |
| 4 | **console: notify-targets feature** | Same shape, notify-targets resource. | console | same |
| 5 | **console: api-keys + usage + session** | Three smaller features. Last to migrate; `src/api/queries.ts` and `src/api/client.ts` deleted at the end of this commit. Move `src/components/{topbar,empty-state}.tsx` semantics decided (shared = stay in `components/`). Add `src/lib/api-client.ts`. | console | same |
| 6 | **console: turn on depcruise CI gate** | Add `- run: pnpm arch` to `.github/workflows/ci.yml` console job. | CI | CI passes |
| 7 | **backend: service/ feature subdirs** | Create `service/{enrich,ingest,outbox,apikey,eval}/`. Move files per §4-A mapping. Update `package` declarations + all imports across `cmd/attune/`, `handlers/`, callers. | service + callers | go build/vet/test, golangci, lint-slog, buf lint, proto-sync |
| 8 | **backend: repo/ feature subdirs** | Create `repo/{feedback,outbox,apikey,notify,tenant,lark}/`. Move files. Update imports in `service/` and `handlers/console/`. | repo + callers | same |
| 9 | **backend: notify/adapter + handlers/console feature subdirs** | `notify/adapter/{rawwebhook,larkcard,larkwebhook,githubissue}/` + `handlers/console/{session,apikey,feedback,notifytarget,usage}/`. | notify + handlers | same |
| 10 | **CLAUDE.md §5 + README + CHANGELOG** | Update docs. Re-verify the four cross-layer rules by grep (same script the §1 review used). | docs | grep evidence in commit message |

**Total: 10 commits on top of the current 4 already pushed to `feat/proto-idl-contract` (PR #67).** Stop after each; if any commit fails CI we stop and diagnose, not pile on.

**PR title change** (before commit 1 pushes): "feat(idl): adopt protobuf IDL contract across the HTTP API (#19)" → **"feat: protobuf IDL contract + feature-based package organization (#19)"** — or split title and body to make this clear. **Issue #19 needs retitling and scope update.** Confirmation gate before any push.

---

## 8 · Verification

After commit 10:

- **All 4 §5 cross-layer rules hold** — verified by the same `grep -rn "internal/service" internal/notify` / `internal/handlers` / `internal/infra` script as the architecture review. Result captured in CHANGELOG.
- **All existing tests pass.** No test file content changes — only import paths.
- **proto-sync clean.** `buf generate` is idempotent; no Go/TS/OpenAPI drift introduced.
- **dependency-cruiser 0 violations** on the migrated console tree.
- **CI mergeState=CLEAN** on PR #67.
- **README package-layout diagram matches `find internal -type d` output.**

---

## 9 · References

**Top-tier repos inspected via GitHub API (architecture review, 2026-06-06):**
- `go-gitea/gitea` — hybrid layer-outside / feature-inside: `services/{issue,pull,repository,...}` (40 subpackages), `models/{issue,repo,user,...}`. **This proposal's direct model.**
- `prometheus/prometheus` — package-by-capability: `tsdb/ promql/ scrape/ rules/ storage/`. Considered as alternative §5-3, rejected for shape mismatch.
- `usememos/memos` — `internal/{ai,storage,webhook}` by dependency + `store/memo.go` file-per-entity. Lighter hybrid variant.
- `pocketbase/pocketbase` — flat `apis/` + `core/` (domain+DB merged). Considered as alternative §5-2, rejected because attune §5 contract is already proven.

**Authoritative Go layout guidance:**
- go.dev — [Organizing a Go module](https://go.dev/doc/modules/layout). Endorses `internal/` + `cmd/<name>/main.go`; **rejects `pkg/`** for most projects. attune already complies.
- [`golang-standards/project-layout` issue #117](https://github.com/golang-standards/project-layout/issues/117) — Russ Cox's critique that the "standard" is not standard and most Go services should be *simpler* than its tree suggests.
- Ben Johnson — [Standard Package Layout](https://medium.com/@benbjohnson/standard-package-layout-7cdbc8391fc1). Root domain + adapters by dependency. Considered as alternative §5-2.
- Mat Ryer — [How I write HTTP services in Go after 13 years](https://grafana.com/blog/how-i-write-http-services-in-go-after-13-years/). `NewServer` composition root, `routes.go`, handler-returns-handler. attune's `cmd/attune/server.go` + `cmd/attune/router.go` already follow this shape.

**React architecture:**
- [`alan2207/bulletproof-react`](https://github.com/alan2207/bulletproof-react) — `src/features/<x>/{api,components,hooks,types}`, shared in `src/components/ui/` + `src/lib/`, single `app/provider.tsx` composition root, ESLint `import/no-restricted-paths` for unidirectional rules. **This proposal's direct model.**
- [bulletproof-react `docs/api-layer.md`](https://github.com/alan2207/bulletproof-react/blob/master/docs/api-layer.md) — per-feature `api/<operation>.ts` files: typed fetcher + `queryOptions()` + `useX()` hook. **This proposal's React Query co-location pattern.**
- Robin Wieruch — [React Folder Structure](https://www.robinwieruch.de/react-folder-structure/). Type-based → feature-based escalator; attune is at the escalation point.

**Boundary enforcement tools (researched):**
- [Biome `noRestrictedImports`](https://biomejs.dev/linter/rules/no-restricted-imports/) — limits documented above (§4-B), confirmed via Rust source.
- [dependency-cruiser rules reference](https://github.com/sverweij/dependency-cruiser/blob/main/doc/rules-reference.md) — the chosen tool. `$1` capture group enables single-rule cross-feature isolation.
- [eslint-plugin-boundaries](https://github.com/javierbrea/eslint-plugin-boundaries) and [Sheriff](https://github.com/softarc-consulting/sheriff) — considered as side-by-side ESLint options; rejected for dep weight (§4-B).

**This repo:**
- CLAUDE.md §5 — current four-layer contract being preserved.
- CLAUDE.md §10 — proposal acceptance gate this document satisfies.
- `docs/proposals/2026-06-06-inbound-adapter-framework.md` — sibling proposal for #66 (de-rooting Lark), unrelated to this refactor but referenced because it would land *into* the new `handlers/` shape.

---

## 10 · Post-implementation notes (2026-06-06)

Status flipped from **Accepted → Implemented**. What actually shipped vs the proposal, plus what we deliberately deferred and where to track it.

### Delivered in PR #67 (13 commits, mergeState=CLEAN)

| Proposal item | Status | Where |
|---|---|---|
| §4-A backend `service/` feature subpackages (`enrich, ingest, outbox, apikey, eval`) | ✅ | commit 7 of refactor |
| §4-A backend `repo/` feature subpackages (`feedback, apikey, outbox, notifytarget, tenant, lark`) | ✅ | commit 8 |
| §4-A `notify/adapter/` feature subpackages (`rawwebhook, larkwebhook, githubissue`) | ✅ | commit 9 |
| §4-A `Notifier` interface moved to `service/enrich` (consumer-defines-interface) | ✅ | commit 7 (also see backlog #70 — long-term may move to `notify/` root) |
| §4-A `apikeyrepo` / `outboxrepo` / `larkrepo` alias convention | ✅ | commits 7-9 |
| §4-B console `src/features/<x>/{api,components}` (feedback, notify-targets, api-keys, usage, session) | ✅ | commits 3-5 |
| §4-B React Query co-located per feature (queryOptions + hook per operation) | ✅ | commits 3-5 |
| §4-B `dependency-cruiser` CI gate (4 rules, 0 violations on 72 modules / 228 deps) | ✅ | commits 2 + 6 |
| §4-C `internal/observability/` → `internal/infra/observability/` | ✅ | commit 1 |
| §4-D CLAUDE.md §5 + README + CHANGELOG updates | ✅ | commit 10 |
| §1-4 all four §5 cross-layer rules verified clean by grep after every backend layer move | ✅ | per-commit |
| Post-merge max-effort review (9 angles + verifier + sweep) | ✅ | 15 findings; 5 🔴 bugs fixed in #67 (commits `fb97e8d` + `9d47f97`) |

### Deferred (tracked separately)

- **`handlers/console/` feature subpackage split** (proposal §4-A — the §4-A target tree included it). Deferred because the `respond` / `auth` helpers shared across every handler would create a console root ↔ sub-package cycle that resolves only by adding an `handlers/console/internal/respond/` layer, with negligible §5 payoff vs the diff cost. Tracked in **#69**; trigger conditions documented there (Wave 3 RBAC / handler count > 25 / second shared util).

### Follow-ups from post-merge review

- **#68** — 9 polish items from review findings (data-race in fanOut, unbounded TouchLastUsed goroutine, rationale field, ResponseChecker ctx, github_issue_test global var, MultiNotifier doc drift, list-feedback-infinite queryOptions inconsistency, _authed.notify-targets dead comment, ~32 stale path/rule references in comments).
- **#69** — handlers/console split (above).
- **#70** — 3 long-term altitude improvements (Notifier interface relocation to notify root, `internal/notify/sig/` extraction for HMAC/envelope helpers, `internal/repo/pgxutil/` extraction for pgx helpers). Each item names its trigger condition.

### Honest accounting

- Engineering recommendation at the start of this proposal was to land architecture refactor in a separate PR from #19's IDL contract. User explicitly chose the more aggressive path. Final cost: PR #67 grew to 13 commits across 145 files. We avoided the predicted "refactor blocks IDL completion" risk because every commit was independently green, but the diff IS larger than what a calm code reviewer would prefer.
- The bulk-sed approach in commit 8 (repo split) caused a class of corruption where method calls like `s.repo.X` were rewritten to `s.<feature>.X`. Caught by go build and fixed; documented in the commit message as a tooling-lesson note for future refactors (BSD sed + lack of `\b`; use `gofmt -r` for AST-aware identifier rewriting where possible).
- Max-effort post-merge review surfaced 5 bugs that would have shipped to production had we trusted CI greenness alone. Every one of those 5 was fixed in #67's final two commits before the merge button.
