# Proposal — `.husky/pre-commit` local quality gate

| | |
|---|---|
| **Issue** | #3 |
| **Status** | Accepted |
| **Started** | 2026-06-05 15:56 CST |
| **Related** | #1 (CI — same checks as last-line defense), #4 (`lint-slog.sh`, invoked here), #9 (clears lint-slog's known warnings) |

## Problem

attune's quality gates (CLAUDE.md §1) run only in CI. A violation (`go vet` error,
stray large blob, slog/OTel mistake) is caught minutes later after push, costing a
CI round-trip and a context switch. §1 itself anticipates a *"pre-commit hook
enforces a subset locally; CI enforces all"* — but that hook didn't exist. #3 makes
it real: a fast (~seconds), dependency-free local gate on **staged** changes.

## Goals

- Fast local feedback on the quick §1 checks; **a strict subset of CI, never stricter**.
- **Zero runtime dependency** for Go contributors: no global pnpm, no `node_modules`,
  works on a fresh clone (CLAUDE.md §8 minimal-deps posture).
- Cover both Go **and** console, without forcing Node on Go-only contributors.
- An opt-in enable and a documented emergency bypass.

## Non-goals

- Replacing CI (it stays authoritative: build/test/golangci/lizard/console/codeql/…).
- Heavy checks in the commit path (full `go test`, lizard, jscpd, `tsc -b`) — too slow
  or dependency-heavy; they stay in CI.

## Proposal

A hand-written `.husky/pre-commit`, enabled via `git config core.hooksPath .husky`.
Checks, on staged ACM files only:

1. **Large-file guard** — a NEWLY-ADDED file whose staged blob > 500 KB
   (`git cat-file -s`, `--diff-filter=A`), matching pre-commit's
   `check-added-large-files` (which scopes to *added* files). Blocks. Catches stray
   binaries/dumps. Scoping to adds is deliberate: a tracked file that grows past the
   limit (e.g. `console/pnpm-lock.yaml` — 177 KB today and climbing, or `go.sum`) is
   a *modification*, so dep bumps are never blocked. Escapes for a legitimate large
   *add*: git-lfs (its pointer blob is ~130 B → passes automatically; also keeps git
   history lean) or a one-off bypass. No bespoke allowlist — that would be new,
   fallible surface for a problem these two standard escapes already cover.
2. **`go vet`** on the packages touched by staged `.go` (space-safe array). Blocks.
3. **`scripts/lint-slog.sh`** (#4) — slog/OTel lint. **Warn-only** (matches its exit-0
   contract; the 3 known warnings shouldn't block commits until #9).
4. **`biome`** on staged `console/src` files — runs console's own
   `node_modules/.bin/biome` **only if installed**; else prints a hint and skips.
   Blocks on lint errors. Go-only contributors never touch Node.

Bypass: `git -c core.hooksPath=/dev/null commit …`.

### Why hand-written `core.hooksPath` (not lefthook / pre-commit / husky)

Researched the 2025–26 landscape (see References). Findings:

- **husky** is a Node devDependency → violates the no-`node_modules` constraint (ironic
  given the issue title). **pre-commit** needs Python. Both add a runtime to a Go shop.
- **lefthook** (Go single binary) is the "advanced" pick *for polyglot monorepos*
  (grafana uses it) — but: (a) its big win is unifying Go+JS config, which we mostly
  scope out (console is a separate sub-project); (b) a contributor without lefthook
  installed gets a **silent `exit 0`** shim — the local gate no-ops invisibly; (c) it
  adds a third-party binary every contributor runs per-commit, against attune's
  supply-chain hygiene (scorecard/zizmor/trufflehog) and §8.
- **Precedent**: the benchmark Go repos attune's CI is modeled on — cli/cli, gitea,
  golangci-lint, kubernetes, terraform, prometheus — use **Makefile + CI, no hook
  framework**. Only grafana (huge Go+TS monorepo) uses lefthook.

So for a Go-primary repo with a separate JS sub-project, hand-written `core.hooksPath`
is the precedent-aligned, zero-dependency, explicit-opt-in (no silent skip) choice.
The console step is folded in **best-effort** (guarded on installed deps) — giving the
"Go + console unified" experience without lefthook's costs.

### Why a byte-based large-file guard, not a ≤300-line source cap

The issue specified "staged `.go` ≤ 300 lines (CLAUDE.md §1)". Investigation found:

- **Misattributed** — CLAUDE.md §1 caps *functions* (lizard NLOC ≤ 100 / CCN ≤ 15), not
  files. No file-line rule there. (The README *Quality gates* table did carry a stale,
  unenforced "≤ 300 lines" row — removed in this change.)
- **Not a Go idiom** — Go/Google style explicitly set *no* fixed file length ("prefer
  refactoring"); golangci-lint has no default file-line linter; ESLint's `max-lines`
  default of 300 is a JS-world number and is off by default. Other file-length tools
  default far higher (Checkstyle 2000).
- **Stricter than CI** — CI does not check file lines, so a 300-line *block* in the hook
  would reject commits CI happily passes. A pre-commit gate should be a subset of CI.
- **Near-binding** — attune's largest file is 293 lines.

Industry pre-commit practice for "large files" is **byte-based** (`check-added-large-files`,
default 500 KB) to catch stray binaries — a different, validated purpose. We adopt that
instead; "code size" stays a CI/lizard (function-level) concern.

## Alternatives considered

- **lefthook / prek / pre-commit / husky** — rejected above (deps, silent-skip,
  supply-chain, precedent).
- **300-line source block** — rejected (misattributed, non-idiomatic, stricter-than-CI).
- **tsc in pre-commit** — rejected; `tsc -b` is whole-project and slow → stays in CI.
- **lint-slog scoped to staged files** — would cut noise (it scans all tracked `.go`,
  so the 3 known warnings print every commit until #9), but needs a `lint-slog.sh`
  change; deferred. Warn-only, so harmless meanwhile.

## Risks / tradeoffs

- **Silent skip if not enabled / not `+x`** — git skips a non-executable hook silently.
  Mitigated: committed mode 100755 (fresh clones inherit it); README/​header call it out;
  CI is the real gate regardless.
- **Staged-vs-worktree** — `go vet` (package-level) and `biome` read the working tree, not
  the staged blob; partial staging (`git add -p`) may vet/lint un-staged sibling content.
  Inherent to file/package linters in pre-commit; the large-file guard *does* use the
  staged blob.
- **console requires `pnpm -C console install`** for the local biome step; otherwise it
  warns and skips (never blocks a Go-only contributor).

## Implementation plan

1. `.husky/pre-commit` (executable).
2. README "Local development" section; remove the stale ≤300-line gate row.
3. This proposal; comment on #3 noting the two deviations (console folded in, line→byte).
4. Changelog skipped (CLAUDE.md §2, `type/chore`); PR `Closes #3`.

## Verification — adversarial e2e, **14 / 14 pass**

An isolated git repo wired with `core.hooksPath`, attacked with designed-to-break cases
(`/tmp/precommit_e2e/run.sh`):

- **Blocks (real catches):** `go vet` error · staged file > 500 KB · console/src biome
  lint error.
- **Passes correctly:** clean commit · exactly 500 KB (boundary) · `lint-slog` warning
  (warn-only, shown not blocked) · file deletion (ACM excludes `D`) · pure-Go change
  (biome not invoked) · root-package `go vet ./.`.
- **Guards hold:** `console/package.json` **not** sent to biome (src-scope fix) ·
  biome absent → warn + skip (not blocked) · bypass via `core.hooksPath=/dev/null`.
- The run **drove two fixes**: (a) biome scoped to `console/src` (was over-broad);
  (b) `go vet` switched to a space-safe array (dropped fragile `xargs`). It also
  confirmed a space-in-dir Go path is invalid Go that `go vet ./...` rejects too — so
  blocking it is consistent with CI, and that a non-`+x` hook is silently skipped.

Real-repo smoke after landing: enable, commit a trivial change → hook runs green.

## References

- Git hook framework landscape (2026): andymadge.com/2026/03/10/git-hooks-comparison;
  evilmartians/lefthook; j178/prek; pre-commit/pre-commit-hooks (`check-added-large-files`).
- File-length norms: Google Go & C++ style guides (no fixed length); golangci-lint
  discussion #2881 (no file-line linter); ESLint `max-lines` (default 300, off);
  Checkstyle `FileLength` (default 2000).
- #1 (CI), #4 (`lint-slog.sh`), #9 (clears known warnings), CLAUDE.md §1/§7/§8.
