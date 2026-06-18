# Purge intermediate-state artifacts + durable guard + AI-collaboration upgrades

| | |
|---|---|
| **Issue** | #64 |
| **Status** | Accepted |
| **Started** | 2026-06-18 |
| **Related** | #9 (lint-slog `--strict` — the gate-pattern this mirrors), #48 (logext facade / depguard allow-list pattern), #72 (proposal-location pre-commit check this extends), #7 (private-deploy doc that retired the "coming in a follow-up" language), #66 (inbound framework — origin of several swept `Phase`/`Layer` markers) |

> **Scope note (accepted 2026-06-18).** The maintainer folded two scope expansions
> into this issue beyond the original #64 charter: (WS2) convert workflow state
> names to `I18nString` *now* (not a follow-up), and (WS3) ship the AI-skills
> directory + `SKILL.md`. WS2 is a **breaking** proto + DB change (see §Risks).
> Decided knowingly to land all three workstreams in one PR.

## Problem

attune is an English, Apache-2.0 OSS project built collaboratively in Chinese and
in phases, so AI-assisted changes have historically left **intermediate-state
scaffolding** in shipped code and docs. #64 (flagged **P0**) asks for a one-time
sweep of three categories *and*, more importantly, a **durable guard** so future
agents can't re-introduce them ("an AI cannot forget what CI rejects").

**The sweep was needed after all — `scripts/lint-artifacts.sh` found, against
`main` (`b3754e76`):**

1. **9 `Phase N` roadmap markers** in shipped `*.go` (`Phase 3.3` in the rate
   limiter, `Phase 3.2` in the outbox worker + notify-target repo, `#66 Phase 1`
   in the inbound source enum, `Phase 4` in an auth test). The issue's original
   `git grep` — and an early ad-hoc `rg` of mine — under-counted these; the linter
   is authoritative. This PR's one-time sweep removes them.
2. **2 leaked Chinese Console labels** in `docs/private-deploy.md` (`通知目标`,
   `日报摘要`) — now English. (`docs/openapi/openapi.yaml` is clean; transitional
   language is advisory-only and left as legitimate prose.)
3. **Legitimate localized CJK stays, file-allowed:**
   `internal/service/enrich/enricher_prompt.go` (the `defaultPromptTmplZh`
   language-aware prompt), `internal/domain/semantic_pack.go` (zh/en/ja display
   data), and — after WS2 — `internal/service/workflow/workflow.go` (the seed
   state `zh` labels now inside `I18nString`).

So the still-open work is the **durable guard + the policy doc** (neither exists:
`scripts/lint-artifacts.sh` is absent; `AGENTS.md` exists as a full mirror of
`CLAUDE.md` but neither carries the rule), plus the two accepted expansions.

**Benchmarking (proposal depth bar).** A 10-project industry sweep (deep-research,
2026-06-18) confirms the approach: the closest stack-twin **Fider** (Go+React+PG)
ships a root `CLAUDE.md`; **Helicone** ships `AGENTS.md`+`CLAUDE.md`+pre-commit —
the exact trio attune runs. The directly-overlapping feedback tools (Fider, Astuto,
Formbricks) ship **no** artifact/CJK CI guard → this gate puts attune ahead.
Transferable next-level patterns adopted here: **Supabase agent-skills**' hard
"run tests before completing" gate (→ WS1 item 1), **n8n**'s `.agents/skills/` +
`/n8n:*` namespaced commands and **Langfuse**'s Anthropic `SKILL.md` (→ WS3).
OpenAI Codex's change-size cap was considered and **rejected** as a hard gate
(verified soft "should", 2-1). `I18nString` for localized names mirrors attune's
own `internal/domain/semantic_pack.go` "stable key + i18n display" doctrine (WS2).

## Goals

- **WS1 — sweep clean + durable guard + policy doc.** `scripts/lint-artifacts.sh`
  exists, house-contract (warn → `--strict`; per-line + file-level allow
  directives; `git ls-files`; bash-3.2-safe; `perl -CSD` for Han), wired into
  pre-commit + `make ci-check` + a `ci-gate` job; repo passes `--strict`. Policy in
  **both** `CLAUDE.md` and `AGENTS.md` (§1 table row + "Shipped-artifact hygiene"
  section). Plus a Supabase-style **verification line**: claim done only after
  `make ci-check` with cited output.
- **WS2 — workflow state names → `I18nString`.** Per-tenant workflow state display
  names become localized (default/zh/en), via a **key/display split** mirroring
  dimensions. Breaking proto + DB change with backfill; existing tenants survive.
- **WS3 — AI-skills directory.** `.agents/skills/` with namespaced `/attune:*`
  commands and a `SKILL.md` packaging attune's proposal→code→PR→changelog flow,
  consumable by Claude Code (and any AGENTS.md-aware tool).

## Non-goals / preserve

- **Intentional i18n stays:** `console/src/i18n/zh-CN.json` + machinery;
  `semantic_pack.go`; the language-aware prompt; non-English **test fixtures**
  (`test/**`, `*_test.go`). After WS2, `workflow.go`'s seed `zh` literals are
  *also* legitimate localized data → file-allowed.
- **Commit history** is out of scope — the working tree is the surface (#64).
- **Localized workflow *transition labels*** — transitions carry no text today;
  adding labels is a separate additive change. Deferred.
- **De-duping `CLAUDE.md` ↔ `AGENTS.md`** (they're full mirrors) — separate concern.
- **Markdown phase-marker scanning of internal design docs** —
  `docs/proposals/**` and `docs/superpowers/plans/**` legitimately quote `Phase N`
  / CJK examples (this file does); the md scan excludes them.

## Proposal

One `chore(repo)`/`feat` branch, three delineated workstreams (separate commits
for reviewability). WS2 is the spine (proto regen gates everything downstream).

### WS1 — artifact guard + policy

- **`scripts/lint-artifacts.sh`**, mirroring `scripts/lint-slog.sh` exactly:
  - **Check A — roadmap markers.** ERE `\bPhase[[:space:]]*[0-9]` over `*.go`,
    `docs/openapi/openapi.yaml`, shipped `*.md` (README + `docs/**` minus
    `docs/proposals/**`, `docs/superpowers/**`). `Phase` is the reliable anchor;
    bare `Layer N` / `M6` are too false-positive-prone (documented limitation).
  - **Check B — non-i18n CJK (Han).** `\p{Han}` via **`perl -CSD`** (portable
    where BSD grep lacks `-P`). Scope: `*.go` minus `*_test.go`, `openapi.yaml`,
    the same `*.md` set.
  - **Check C — transitional language** (`coming in a follow-up|lands with
    #[0-9]|\bfor now\b|\bdeferred\b`): advisory (printed, non-failing) to avoid
    prose false positives; A + B gate (D3).
  - Allow: per-line `// lint-artifacts:allow <reason>` + whole-file
    `// lint-artifacts:file-allow <reason>` (mirrors `lint-rawptr`'s
    `// ptrext:file-allow`). File-allow the three legitimate CJK files:
    `enricher_prompt.go` (zh prompt), `semantic_pack.go` (localized data),
    `workflow.go` (localized seed data, after WS2).
- **Wiring:** add a toolchain-free `lint-artifacts` job to `ci.yml` (copy the
  `lint-slog` job at `ci.yml:196-204`) running **unconditionally** (drop the
  `needs.changes.outputs.go` gate — artifacts leak across `.go` + `openapi.yaml` +
  `.md`); add it to `ci-gate.needs`. Add `bash scripts/lint-artifacts.sh --strict
  || fail=1` to `.husky/pre-commit`; add a block to `Makefile` `ci-check`.
- **Policy (both files):** §1 quality-gate row `Shipped-artifact hygiene | 0 leaked
  CJK / roadmap markers (allow-list aside) | scripts/lint-artifacts.sh --strict`; a
  "Shipped-artifact hygiene" subsection (English is canonical in shipped
  code/comments/API docs; no phase/stage/roadmap markers or transitional language;
  sweep scaffolding per change; allow-list enumerated); and the verification line
  *"Before claiming done, run `make ci-check` (or the relevant subset) and cite the
  output."*

### WS2 — workflow state name → `I18nString` (key/display split)

**Why a split:** `name` is today a **stable key**, not just display text — unique
index `idx_ws_tenant_name` (`030_workflow_states.sql:15`), upsert conflict target
(`workflowstate.go:356`), and the seed builds the transition graph by name
(`workflow.go:274-294` `nameToID`). So we follow the dimensions doctrine
(`dimension.go`: stable machine `Value`/`Name` + editable `DisplayName I18nString`):
introduce a stable `key` and move human text into `display_name JSONB`.

Ordered (impact map verified against code):

1. **Proto** (`proto/attune/v1/workflow.proto`): `import "attune/v1/common.proto"`;
   `WorkflowState.name string` → add `string key` + `I18nString display_name`;
   `CreateStateRequest`/`UpdateStateRequest` carry `key` (create) + `I18nString`
   display name. `make proto` regenerates Go/TS/OpenAPI (never hand-edited;
   `proto-sync` enforces).
2. **Migration `053_workflow_state_i18n.sql`**: `ADD COLUMN display_name JSONB NOT
   NULL DEFAULT '{}'` + backfill `jsonb_build_object('default',name,'zh',name)`
   (mirror `019`'s `||` backfill); `ADD COLUMN key TEXT`, backfill (seed names →
   english slugs `pending/triaged/in_progress/fixed/wont_fix`; `id::text` fallback
   for tenant-authored Unicode names — assumption surfaced); repoint
   `idx_ws_tenant_name` → `idx_ws_tenant_key` on `(tenant_id,key)`; drop `name` +
   its CHECKs. Transition tables + `user_feedback.workflow_state_id` reference `id`
   — untouched.
3. **Repo** (`internal/repo/workflowstate/workflowstate.go`): `WorkflowState.Name
   string` → `Name domain.I18nString` + `Key string`; scan `display_name`→`[]byte`
   →`json.Unmarshal` (mirror `tenant/enrich_config.go:53,63`); marshal on
   Create/Update; upsert conflict → `(tenant_id,key)`; `ErrNameConflict` = key
   conflict. A non-nil-map guard so empty display serializes `{}` not `null`.
4. **Service** (`workflow.go`): `seedState{Key, Name I18nString, …}`,
   `seedTransition{FromKey,ToKey}`; the five Chinese names move into
   `I18nString{"default":"Pending","zh":"待处理","en":"Pending"}` etc.;
   `nameToID`→`keyToID`. Audit `OldValue/NewValue` (`workflow.go:141-142`,
   TEXT columns) resolve `Name.Resolve(["default"])` to a string.
5. **Handlers:** add `i18nToProto`/`i18nFromProto` (copy
   `enrichconfig/enrich_config.go:90-99,165-174`); `workflow/handler.go` StateToProto
   + Create/Update validation (`NonEmpty()` instead of `len(name)`); `workflow/audit.go`
   snapshots resolve/embed the map. **Abstract the duplicate** state→proto mapping
   (`workflow/handler.go:42` and `feedback/workflow.go:18`) into one exported helper
   (CLAUDE.md §6.2).
6. **Console:** resolve display via `useDisplayName()` (`lib/i18n-resolve.ts`) at
   render sites (`workflow-state-badge`, `transition-matrix`, `workflow-transition-select`,
   `workflow-settings-page`, feedback route badge); **state create/edit form**
   (`state-form-dialog.tsx`) gains a multi-locale name input (reuse the dimension
   display-name editor pattern) and posts `{key, display_name:{entries:{…}}}`; api
   request types updated (responses are pass-through — `unwrap` already accepts
   `{entries}`).
7. **Tests:** Go (`workflowstate`, `service/workflow`, `handlers/console/workflow/*`,
   `handlers/console/feedback/workflow_test`) + TS (workflow components/api) updated
   to `I18nString` + `key`. New migration smoke under integration if warranted.

### WS3 — AI-skills directory

- `.agents/skills/` with attune-specific skills as `SKILL.md`-fronted folders
  (Anthropic Agent-Skills format: frontmatter always-loaded, body on demand),
  surfaced as `/attune:*` namespaced commands (n8n pattern). Initial set:
  `/attune:proposal` (scaffold a §10 proposal from an issue), `/attune:create-pr`
  (Conventional-Commit title + changelog reminder + `Closes #N`),
  `/attune:preflight` (run `make ci-check`, summarize). `CLAUDE.md`/`AGENTS.md`
  reference the directory. Keep minimal and real (3 skills), not aspirational.

## Decisions (from #64) — resolved

- **D1 — CN allow-list:** English-only in code/comments/API docs; allowed CJK =
  i18n locale files, localized product data (`semantic_pack.go`, post-WS2
  `workflow.go`), language-aware prompts (`enricher_prompt.go`), `test/**`
  fixtures. Dashboard market labels live in generated dashboards/i18n, outside the
  scanned set. **Adopted.**
- **D2 — workflow seed names:** **(c) `I18nString` now**, via key/display split
  (WS2). Overrides the original "translate / defer" options. Breaking; see Risks.
- **D3 — strictness:** repo already passes → **gate `--strict` immediately** (CI +
  pre-commit), like `lint-rawptr`. Check C stays advisory.
- **D4 — AGENTS.md form:** keep the full mirror; add the rule to both.

## Alternatives considered

- **golangci-lint custom analyzer / depguard for CJK+markers.** depguard sees
  imports, not comment/string content; a custom analyzer is Go-only (misses
  `openapi.yaml` + `*.md`). Shell linter matches the `lint-slog` contract and spans
  all file types. Rejected as over-engineering.
- **`grep -P '\p{Han}'`** — GNU-only; breaks on macOS BSD grep (local/pre-commit).
  `perl -CSD` is portable. Rejected.
- **Gate `Layer N`/`M[0-9]`** — too many legitimate hits; `Phase\s*[0-9]` catches
  every real marker #64 cites. Documented limitation.
- **WS2 alternatives — translate seeds to English (D2a) / keep CN + file-allow
  (D2b) / defer I18nString to a follow-up.** All rejected by the maintainer in
  favour of doing the principled i18n conversion now (D2c), accepting the breaking
  cost.
- **Split the three workstreams into separate PRs** (this proposal's earlier
  recommendation). Overridden — land together.

## Risks / tradeoffs

- **WS2 is a breaking change** (proto wire `name:string`→`I18nString`; DB schema).
  Pre-1.0, but per CLAUDE.md §3 a breaking field/schema change — flag prominently
  in the changelog (`### Changed`, note breaking) and consider a MINOR bump with an
  explicit breaking call-out. API clients reading `state.name` as a string break.
- **Migration backfill of tenant-authored Unicode names** can't always yield a
  clean ASCII `key`; fallback = `id::text`. Existing transition graphs +
  `user_feedback.workflow_state_id` reference `id`, so they survive untouched.
- **Large PR / mixed concerns.** Three workstreams in one PR hurts reviewability;
  mitigated by separate commits per workstream and this proposal as the map.
- **CJK regex Han-only** may miss a stray kana leak; avoids flagging legitimate
  ja/ko i18n. Acceptable; widen later if needed.
- **Unconditional CI job / `perl` dependency** — sub-second grep, no toolchain;
  perl ships on macOS + `ubuntu-latest`. Negligible.
- **Pre-commit scans whole-repo state** (`git ls-files`) like `lint-slog`; no-op
  post-sweep; bypass `git -c core.hooksPath=/dev/null commit`.

## Implementation plan

1. WS2 proto edit → `make proto` (regen Go/TS/OpenAPI).
2. WS2 migration `053`; repo; service (seeds + `workflow.go` file-allow comment);
   handlers (+ helper abstraction); Go tests.
3. WS2 console (render sites + multi-locale form + api types); TS tests.
4. WS1 `scripts/lint-artifacts.sh`; file-allow `enricher_prompt.go` +
   `semantic_pack.go`; wire ci.yml/pre-commit/Makefile; CLAUDE.md + AGENTS.md
   policy + verification line.
5. WS3 `.agents/skills/` (3 skills + `SKILL.md`); reference from CLAUDE/AGENTS.
6. CHANGELOG `[Unreleased]`: `### Changed` (breaking workflow i18n) + `### Added`
   (artifact guard, AI-skills dir).
7. Verify: `make proto` (no drift) · `bash scripts/lint-artifacts.sh --strict`
   clean · `make ci-check` green (vet/build/test-race/golangci/lizard/jscpd +
   console tsc/biome/vitest) · `make test-integration` for the migration if Docker
   available. Sync decisions to issue #64.

## Verification

- `bash scripts/lint-artifacts.sh --strict` exits 0; injecting `// Phase 7`,
  business-code `// 测试`, or `openapi.yaml` CJK each fails it; the same inside a
  file-allowed file or `docs/proposals/**` does not.
- `make proto` leaves no diff (committed regen).
- New tenant seeded → 5 states with `I18nString` names + english `key`s, transition
  graph intact (keyed by `key`); console renders localized names via `useDisplayName`.
- Migration on a DB with pre-existing Chinese-named states → names preserved as
  `display_name.default`/`.zh`, `key` backfilled, unique constraint on `key`,
  feedback↔state FKs intact.
- `go build ./... && go test -race -short ./...` and console `vitest` green; CI
  `lint-artifacts` job required via `ci-gate`; pre-commit blocks a seeded violation.

## References

- Issue #64; this proposal closes it.
- House pattern: `scripts/lint-slog.sh`, `cmd/lint-rawptr` (`// ptrext:file-allow`),
  `.husky/pre-commit` (#72), `Makefile:79-141`, `ci.yml:196-204` + `ci-gate.needs`.
- I18nString template: `internal/domain/i18n.go`; `internal/repo/tenant/enrich_config.go`
  (JSONB scan/marshal); `proto/attune/v1/common.proto:114` (`I18nString`);
  `internal/handlers/console/enrichconfig/enrich_config.go:90-174` (proto mapping);
  `console/src/lib/i18n-resolve.ts` (`useDisplayName`/`unwrap`); dimensions doctrine
  `internal/domain/dimension.go`.
- Workflow as-is: `proto/attune/v1/workflow.proto`,
  `internal/infra/database/migrations/030_workflow_states.sql`,
  `internal/repo/workflowstate/workflowstate.go`, `internal/service/workflow/workflow.go`,
  `internal/handlers/console/{workflow,feedback/workflow}.go`,
  `console/src/features/workflow/**`.
- Benchmarking (deep-research, 2026-06-18): Helicone (AGENTS+CLAUDE+pre-commit),
  Fider (root CLAUDE.md), Supabase agent-skills (test-gate), n8n `.agents/skills/`
  + `/n8n:*`, Langfuse Anthropic `SKILL.md`. <https://agents.md/>.
