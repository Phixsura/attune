# Reconcile quality-gate docs with CI

| Field   | Value |
|---------|-------|
| Issue   | #15 |
| Status  | **Implemented** |
| Started | 2026-06-10T12:25:39+08:00 |
| Related | #1 (CI), #4 (lint-slog origin), #9 (lint-slog strict), #16 (coverage + govulncheck follow-up), #48 (logext facade), AGENTS.md §1, CLAUDE.md §1 |

## Problem

Issue #15 was opened when `CLAUDE.md` section 1 no longer matched the quality
gates implemented by CI. Since then, `main` has moved forward: several concerns
from the original issue have already been resolved in code, while the remaining
drift has changed shape.

Verified against `main` on 2026-06-10 after fast-forwarding to `origin/main`:

- `jscpd` is now a CI gate, but `CLAUDE.md` still documents the old command
  shape. CI runs `npx -y jscpd . -f go -i '**/*.pb.go' -t 4 --silent`, while
  the docs still use `--pattern '**/*.go' --threshold 5`.
- `lint-slog` is now a strict CI gate. CI runs `bash scripts/lint-slog.sh
  --strict`.
- `lint-slog` rule 1 has been retired from that script and replaced by
  golangci-lint depguard's `slog-facade` rule for direct `log/slog` imports.
  `AGENTS.md` still points logging enforcement at `scripts/lint-slog.sh` rule 1.
- `go vet ./...` is still run directly in CI, and `golangci-lint` also runs
  `govet`. The original issue's statement that `go vet` was only subsumed by
  golangci-lint is no longer the whole current truth.
- `CLAUDE.md` now includes Go and console gates, but it omits several real CI
  gates: `golangci-lint`, `lint-errorcode`, integration layout, proto drift,
  Docker smoke build, compose config validation, and changelog enforcement.
- `AGENTS.md` and `CLAUDE.md` have diverged. The repository now contains two
  assistant-facing engineering contracts with different quality-gate tables.
- The `Internal info | grep` row still has no obvious CI or script owner. The
  repository does have TruffleHog secret scanning, CodeQL, dependency review,
  and workflow linting, but those are not equivalent to the documented grep
  check for IPs, `/opt` paths, and brand names.

The result is still the same class of problem as #15: contributors cannot treat
the quality-gate table as the source of truth without cross-checking workflow
YAML by hand.

## Project value

Although #15 was filed early and several facts in the original issue have been
overtaken by later CI work, the underlying project value is still strong:

- **Contributor trust.** `CLAUDE.md` / `AGENTS.md` are the first place humans and
  AI assistants look before changing the repository. If they describe gates that
  do not exist, omit gates that do exist, or disagree with each other, every PR
  starts with avoidable uncertainty.
- **Faster reviews.** A correct quality-gate table lets reviewers focus on the
  substance of a change instead of rediscovering which checks are authoritative.
  This matters more as the repo now has Go, React, proto, integration,
  deployment, and security workflows.
- **Fewer phantom requirements.** Removing unimplemented rows such as the
  internal-info grep gate avoids a false sense of enforcement. If the project
  wants that check, it should be designed as a real script or workflow in a
  separate issue.
- **Better AI collaboration.** The repository explicitly asks AI assistants to
  follow its engineering contract. Keeping `AGENTS.md` and `CLAUDE.md` aligned
  reduces the chance that different tools make different assumptions about
  tests, changelog requirements, or CI behavior.
- **CI maintainability.** Treating CI as the source of truth for the table gives
  the project a clean maintenance rule: when a gate is added, removed, or made
  informational, the contract changes with it. That makes future drift easier to
  spot and cheaper to fix.
- **Accurate release hygiene.** The changelog and proposal rules are part of the
  project's release discipline. Documenting them alongside the implemented CI
  checks makes it clearer why a PR failed and what a contributor must do next.

In short, this is not documentation churn. It turns an early, partially stale
issue into a small source-of-truth cleanup that improves day-to-day contribution
flow and reduces policy ambiguity before the repository grows further.

## Goals

- Make `CLAUDE.md` section 1 match the current enforced PR gates.
- Keep `AGENTS.md` aligned with `CLAUDE.md` so Codex, Claude Code, Cursor, and
  human contributors read the same quality contract.
- Resolve each gate called out in #15:
  - `jscpd`: keep it documented because it is now in CI.
  - `lint-slog`: keep it documented, but describe the current rule split.
  - `go vet`: keep it documented because it still runs directly in CI.
  - removed file-length gate: keep it out of the table.
  - CI-only gates: add them where they are hard PR gates.
- Distinguish blocking PR gates from informational or scheduled security
  monitors.
- Keep the table compact enough to stay useful as an engineering contract rather
  than becoming a line-by-line copy of `.github/workflows/ci.yml`.
- Close #15 as a docs-only reconciliation.

## Non-goals

- Do not add or remove CI jobs in this issue. This is a documentation
  reconciliation, not a CI behavior change.
- Do not change branch protection. `ci-gate` remains the required aggregator for
  the main CI workflow.
- Do not make `govulncheck` blocking. The PR job remains informational because
  standard-library findings can drift independently of attune code changes.
- Do not turn Codecov upload failures into merge blockers.
- Do not redesign the pre-commit hook. Local hooks remain a fast subset or
  near-subset of CI.
- Do not add a new internal-info grep gate unless a separate issue decides the
  exact leak taxonomy and false-positive handling.
- Do not update `CHANGELOG.md`, because this proposal and the follow-up table
  edit are documentation-only work exempt under the changelog rule.

## Proposal

### 1. Treat CI as the source of truth for the table

Rewrite `CLAUDE.md` section 1 around the current workflows rather than the
historical checklist. The table should list the gates contributors can expect to
block a PR, grouped by area:

- Go backend
- Console TypeScript/React
- Contracts, deploy, and generated artifacts
- Security and supply chain
- Process

The table should stay human-readable and point to the owning command or workflow
job, not duplicate every YAML detail.

### 2. Update the Go backend rows

Keep these rows, but align their wording with CI:

| Check | Threshold | How |
|---|---|---|
| `go vet ./...` | 0 warnings | `.github/workflows/ci.yml` `go-checks` |
| `go build ./...` | 0 errors | `.github/workflows/ci.yml` `go-checks` |
| `go test -race ./...` | all pass | `.github/workflows/ci.yml` `go-checks` |
| `golangci-lint` | 0 findings | `golangci-lint-action`, including `govet`, `depguard`, `bodyclose`, `noctx`, etc. |
| Function CCN | <= 15 | `lizard . -l go -C 15 -T nloc=100 --warnings_only` |
| Function NLOC | <= 100 | `lizard . -l go -C 15 -T nloc=100 --warnings_only` |
| Code duplication | < 4% | `npx -y jscpd . -f go -i '**/*.pb.go' -t 4 --silent` |
| Logging facade | no direct `log/slog` in business code | `golangci-lint` depguard `slog-facade` |
| Log field / outbound HTTP hygiene | 0 `lint-slog` findings | `scripts/lint-slog.sh --strict` rules 2 and 3 |
| Raw pointer ops | 0 unapproved bare `*p` / `&x` | `scripts/lint-rawptr.sh` |
| Error response codes | `ErrorResponse.code` comes from the enum | `scripts/lint-errorcode.sh` |
| Integration layout | 0 misplaced integration-tagged tests | `scripts/lint-integration-layout.sh` |
| PostgreSQL integration tests | all pass on Go changes | `make test-integration` in CI |

This explicitly resolves the lint-slog split: rule 1 is no longer a script rule;
direct slog import enforcement belongs to golangci-lint depguard.

### 3. Keep and sharpen the console rows

The current console rows in `CLAUDE.md` are directionally right. Preserve them,
but include the Vite build smoke test because CI requires it before TypeScript:

| Check | Threshold | How |
|---|---|---|
| `pnpm biome check` | 0 errors | console CI job; staged subset in pre-commit |
| `pnpm exec vite build` | 0 errors | console CI job |
| `pnpm tsc -b --noEmit` | 0 errors | console CI job |
| `pnpm vitest run --coverage` | all pass + configured thresholds | console CI job |
| `pnpm arch` | 0 dependency-cruiser violations | console CI job |

Coverage regression workflows may be mentioned separately as PR review gates,
because they are in dedicated workflows rather than the main `ci-gate`.

### 4. Add contract/deploy/process rows

Add rows for CI jobs that currently block PRs when their paths are touched:

| Check | Threshold | How |
|---|---|---|
| Proto contract | lint clean, no breaking drift, generated output committed | `buf lint`, `buf breaking`, `buf generate && git diff --exit-code` |
| Docker image | smoke build succeeds | `docker/build-push-action` with `push: false` |
| Compose config | base + obs overlay parses | `docker compose ... config -q` |
| Go module files | tidy output committed | `go mod tidy && git diff --exit-code go.mod go.sum` |
| Changelog | code PRs update `CHANGELOG.md` unless exempt | `changelog` job |
| PR title | Conventional Commit shape | `semantic-pull-request` workflow |

These rows reflect implemented CI behavior that #15 specifically called out as
missing from the old table.

### 5. Replace the unimplemented internal-info row

Remove `Internal info | 0 leaks (IPs, /opt paths, brand names) | grep` from the
quality-gate table unless a real script or workflow is added first.

Add security/supply-chain rows for the checks that actually exist:

| Check | Threshold | How |
|---|---|---|
| Secrets | no verified or unknown committed secrets | TruffleHog `Secret Scan` workflow |
| Code scanning | CodeQL completes for Go and JS/TS | `CodeQL` workflow |
| Dependency review | no high-severity vulnerable deps; no denied copyleft licenses | `Dependency Review` workflow |
| Workflow security | 0 zizmor findings | `Workflow Lint` workflow |

Treat OpenSSF Scorecard and scheduled govulncheck SARIF as monitoring rather
than PR quality gates unless maintainers later make them required checks.

### 6. Keep informational monitors out of the red-gate list

Document these near the table, but do not present them as blocking quality
gates:

- PR `govulncheck ./...` in main CI: informational, `continue-on-error`.
- Scheduled `Govulncheck SARIF`: uploads to code scanning; findings do not
  block a PR directly.
- Codecov uploads: useful trend signal; upload failures are `continue-on-error`.
- OpenSSF Scorecard: push/scheduled supply-chain posture monitor.

This prevents the table from implying stricter merge behavior than the repo
actually has.

### 7. Synchronize `AGENTS.md`

After `CLAUDE.md` is updated, copy the section 1 gate table and the explanatory
note into `AGENTS.md`, preserving only assistant-name wording differences. This
is necessary because the repository now uses `AGENTS.md` as the Codex-facing
contract while issue #15 only names `CLAUDE.md`.

## Alternatives considered

- **Only update `CLAUDE.md`.** Rejected: Codex reads `AGENTS.md`, and leaving it
  stale would preserve an assistant-facing source of drift.
- **Delete `AGENTS.md` and keep only `CLAUDE.md`.** Rejected for this issue:
  removing a tool-facing contract is broader than a quality-gate reconciliation.
- **Add a new grep-based internal-info CI gate.** Rejected for now: the row has
  no implemented owner, and the exact taxonomy needs careful false-positive
  design. Secret scanning and CodeQL cover adjacent risks but not the same one.
- **List every workflow as a red gate.** Rejected: scheduled and informational
  workflows do not always block PRs, so listing them as merge gates would be
  inaccurate.
- **Drop `go vet` because `golangci-lint` includes `govet`.** Rejected for the
  current repository state: CI still runs `go vet ./...` directly, so the docs
  should say so until the workflow changes.

## Risks / tradeoffs

- **The table becomes longer.** The current CI surface is larger than the old
  table. Grouping by area keeps it scannable while still honoring #15's request
  that docs match implemented gates.
- **Independent workflows may or may not be required in branch protection.**
  The proposal phrases them as workflows that fail on PRs, while keeping
  `ci-gate` as the required aggregator for the main CI workflow.
- **Security tooling can produce environment-dependent results.** The table
  should distinguish hard workflow failures from uploaded findings or
  informational scans.
- **`CLAUDE.md` and `AGENTS.md` may drift again.** The implementation should
  keep their section 1 tables textually close. A future follow-up could add a
  docs lint that compares the two sections, but that is outside this issue.

## Implementation plan

1. Update `CLAUDE.md` section 1 with grouped rows that match current CI.
2. Remove the unimplemented internal-info grep row or move it to a non-gated
   security note.
3. Add rows for `golangci-lint`, `lint-errorcode`, integration layout,
   integration tests, proto, Docker build, compose config, go module tidy,
   changelog enforcement, semantic PR titles, and implemented security
   workflows.
4. Clarify that `lint-slog` covers rules 2 and 3, while direct `log/slog`
   imports are covered by golangci-lint depguard.
5. Synchronize `AGENTS.md` section 1 with the new `CLAUDE.md` table.
6. Leave `CHANGELOG.md` untouched and note in the PR description that this is a
   docs-only, changelog-exempt change.
7. Mark this proposal `Implemented` when the table reconciliation lands.

## Verification

- Manually compare `CLAUDE.md` section 1 against:
  - `.github/workflows/ci.yml`
  - `.github/workflows/codeql.yml`
  - `.github/workflows/dependency-review.yml`
  - `.github/workflows/secret-scan.yml`
  - `.github/workflows/semantic-pull-request.yml`
  - `.github/workflows/workflow-lint.yml`
  - `.github/workflows/go-coverage.yml`
  - `.github/workflows/console-coverage.yml`
- Verify every row in `CLAUDE.md` names an implemented command, script, or
  workflow.
- Verify every blocking CI job has a corresponding row or explanatory note.
- Verify `AGENTS.md` section 1 matches `CLAUDE.md` section 1 except for
  assistant-name wording.
- Run `git diff --check`.
- No Go, console, or generated-code tests are required for the docs-only
  follow-up.

## References

- Issue #15.
- `.github/workflows/ci.yml`.
- `.github/workflows/codeql.yml`.
- `.github/workflows/dependency-review.yml`.
- `.github/workflows/secret-scan.yml`.
- `.github/workflows/semantic-pull-request.yml`.
- `.github/workflows/workflow-lint.yml`.
- `.github/workflows/go-coverage.yml`.
- `.github/workflows/console-coverage.yml`.
- `scripts/lint-slog.sh`.
- `.golangci.yml`.
- `.husky/pre-commit`.
- `CLAUDE.md` section 1.
- `AGENTS.md` section 1.
