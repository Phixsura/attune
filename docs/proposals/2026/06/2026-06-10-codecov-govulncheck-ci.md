# Coverage review/upload + scheduled govulncheck SARIF

| Field   | Value |
|---------|-------|
| Issue   | #16 |
| Status  | **Implemented** |
| Started | 2026-06-10T10:07:21+08:00 |
| Related | #1 (GitHub Actions CI), #11 (initial CI implementation), AGENTS.md §1 (quality gates), AGENTS.md §9 (AI assistant workflow), AGENTS.md §10 (one proposal per issue) |

## Problem

Issue #16 tracks two post-#1 CI follow-ups that were intentionally split out of
the initial CI rollout:

1. The Go CI job produces `coverage.txt` and invokes Codecov, but the repository
   has no `CODECOV_TOKEN` secret configured. The upload step is also
   `continue-on-error`, so CI stays green even when Codecov rejects the upload.
   A recent CI run confirmed this exact failure mode: the Codecov CLI reported
   `Token length: 0` and `Token required - not valid tokenless upload`.
   The console CI job also runs Vitest coverage, but it only prints text output
   and writes local HTML. Nothing from the TypeScript/React surface is preserved
   as a review artifact, compared against `main`, commented on PRs, or uploaded
   to Codecov today.
2. The PR/push `govulncheck` job is informational only. That is correct for PR
   gating, because standard-library findings drift with the Go vulnerability DB
   and the runner Go patch version. But unchanged code can become vulnerable
   after a new advisory is published, and today those findings only appear in CI
   logs for PRs that happen to touch Go paths. They do not land in GitHub code
   scanning on a schedule.

Current state after fast-forwarding `main` to `e3bfc82`:

- `.github/workflows/ci.yml` runs `go test -race -covermode=atomic
  -coverprofile=coverage.txt ./...`.
- `.github/workflows/ci.yml` invokes `codecov/codecov-action@v6.0.1` with
  `files: ./coverage.txt` and `flags: go`, but without a token.
- `console/vite.config.ts` configures Vitest coverage with `text` and `html`
  reporters plus per-file thresholds, but no `json-summary` reporter for
  base-vs-PR comparison and no `lcov` reporter for Codecov.
- `.github/workflows/ci.yml` runs `pnpm vitest run --coverage` in the `console`
  job, but there is no dedicated frontend coverage workflow, artifact, PR
  comment, regression check, or Codecov upload step for console coverage.
- `gh api repos/Phixsura/attune/actions/secrets` returns `total_count: 0`.
- `.github/workflows/ci.yml` runs `govulncheck ./...` on Go changes with
  `continue-on-error: true`.
- Existing security workflows already publish SARIF to code scanning:
  `.github/workflows/scorecard.yml` uses `security-events: write` plus
  `github/codeql-action/upload-sarif`.
- Local verification showed `govulncheck -format sarif ./...` writes SARIF
  2.1.0 and exits 0, even when the text-mode CI scan would report reachable
  vulnerabilities.

### Precedent from mature repositories

The proposal intentionally follows the shape used by larger Go and mixed
Go/TypeScript repositories, scaled down to attune's size:

- `cli/cli` keeps a standalone `govulncheck.yml` with `schedule` plus
  `workflow_dispatch`, grants only `contents: read` and
  `security-events: write`, runs `govulncheck -format sarif ./...`, and uploads
  the result with `github/codeql-action/upload-sarif`.
- `prometheus/prometheus` keeps govulncheck separate from its main CI and runs
  it on `push`, selected PRs, and a daily schedule via
  `golang/govulncheck-action`.
- `grafana/grafana` treats frontend coverage as its own first-class workflow:
  it computes frontend coverage separately on `main` and the PR branch, compares
  the two summaries, uploads HTML coverage artifacts, posts a sticky PR comment,
  supports an explicit skip label, and can fail on coverage decrease for
  opted-in code owners.

Those examples point to three design choices for attune:

- Keep scheduled vulnerability monitoring separate from path-gated CI.
- Upload Go coverage to Codecov, and treat console coverage as its own review
  signal instead of pretending a Go-only report represents the whole repository.
- Borrow Grafana's frontend coverage operating model, scaled down for a
  single-console app instead of a large code-owner matrix.

## Goals

- Make Go coverage uploads to Codecov real for `pull_request` and `push` events
  where a valid repository secret is available.
- Keep coverage reports visible in CI logs so coverage remains observable even
  if the external Codecov upload has a transient outage.
- Add a Grafana-style console coverage workflow that compares PR coverage
  against `main`, uploads the PR branch HTML coverage report as an artifact,
  comments the result on the PR, and fails when aggregate console coverage
  decreases.
- Keep console coverage path-gated to TypeScript/React/package changes, with a
  deliberate skip label for exceptional PRs.
- Add a weekly scheduled `govulncheck` SARIF workflow that uploads findings to
  GitHub code scanning.
- Keep PR `govulncheck` informational and non-blocking.
- Reuse the repository's existing GitHub Actions conventions:
  SHA-pinned actions, built-in setup-go caching, minimal token permissions, and
  `persist-credentials: false`.
- Keep scheduled vulnerability scanning independent from path-gated CI, so it
  still runs when the code has not changed.
- Add a manual `workflow_dispatch` trigger for the scheduled scan workflow so
  maintainers can verify the pipeline immediately after merging.

## Non-goals

- Do not make Codecov a required merge gate in this issue.
- Do not introduce new global coverage thresholds or Codecov status checks yet.
  The current local Go unit coverage baseline is roughly 28.0% statements, and
  the console suite already has targeted per-file Vitest thresholds. Any broader
  ratchet should be introduced separately once the desired baseline and policy
  are agreed.
- Do not add Codecov PR comments, patch/project gates, badges, or repository
  config in this issue. Go coverage uses Codecov ingestion only; console coverage
  gets its own GitHub PR comment and artifact workflow.
- Do not include integration or live-test coverage in the first Codecov upload.
  The Go upload remains the default unit package coverage from `go test ./...`;
  `make test-integration` and `test/live/...` stay separate tiers.
- Do not introduce Grafana's code-owner matrix yet. attune's console is still
  small enough for one aggregate frontend coverage comparison.
- Do not fix the current Go standard-library vulnerability findings here. Those
  findings are valuable proof that SARIF ingestion is useful, but the actual
  remediation is a Go toolchain/dependency maintenance task.
- Do not change branch protection. `ci-gate` remains the only required status
  check for `.github/workflows/ci.yml`.

## Proposal

### 1. Configure Codecov token usage

Add `token: ${{ secrets.CODECOV_TOKEN }}` to the existing Codecov step in
`go-checks`:

```yaml
- name: Upload coverage to Codecov
  uses: codecov/codecov-action@... # v6.0.1
  continue-on-error: true
  with:
    token: ${{ secrets.CODECOV_TOKEN }}
    files: ./coverage.txt
    flags: go
```

This keeps the current non-blocking behavior but removes the known no-token
failure mode once the repository secret is configured.

The repository owner must add the actual secret out of band:

```sh
gh secret set CODECOV_TOKEN --repo Phixsura/attune
```

The PR description should call this out as an external setup step. If the secret
is not present, the workflow change alone cannot satisfy the Codecov acceptance
criterion.

### 2. Print a Go coverage summary in CI logs

Add a small step after the test command and before the Codecov upload:

```yaml
- name: Go coverage summary
  run: go tool cover -func=coverage.txt | tail -1
```

This prints a stable `total: (statements) X%` line in the job log. It is not a
threshold and should not fail the job unless the coverage file is missing, which
would indicate the preceding test step did not produce the expected artifact.

### 3. Add Grafana-style console coverage review

Extend the Vitest coverage reporters in `console/vite.config.ts`:

```ts
reporter: ['text', 'html', 'json-summary'],
```

`text` keeps the existing CI log summary, `html` keeps the local inspection
artifact, and `json-summary` writes `console/coverage/coverage-summary.json`,
which a lightweight comparison script can read.

Add a dedicated `.github/workflows/console-coverage.yml` modeled after
Grafana's frontend coverage check, scaled down from a code-owner matrix to one
aggregate console report:

```yaml
name: Console Coverage

on:
  pull_request:
    branches: [main]
    types: [opened, synchronize, reopened, labeled, unlabeled]
    paths:
      - "console/**"
      - ".github/workflows/console-coverage.yml"

permissions:
  contents: read

jobs:
  coverage:
    if: "!contains(github.event.pull_request.labels.*.name, 'skip-console-coverage')"
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - name: Checkout main
        uses: actions/checkout@... # v6.0.3
        with:
          ref: ${{ github.event.pull_request.base.sha }}
          path: main
          persist-credentials: false
      - name: Checkout PR
        uses: actions/checkout@... # v6.0.3
        with:
          repository: ${{ github.event.pull_request.head.repo.full_name }}
          ref: ${{ github.event.pull_request.head.sha }}
          path: pr
          persist-credentials: false
      - name: Setup pnpm
        uses: pnpm/action-setup@... # v6.0.8
        with:
          package_json_file: pr/console/package.json
      - name: Setup Node
        uses: actions/setup-node@... # v6.4.0
        with:
          node-version: 22
          cache: pnpm
          cache-dependency-path: pr/console/pnpm-lock.yaml
      - name: Test coverage on main
        working-directory: main/console
        run: pnpm install --frozen-lockfile && pnpm vitest run --coverage
      - name: Test coverage on PR
        working-directory: pr/console
        run: pnpm install --frozen-lockfile && pnpm vitest run --coverage
      - name: Compare coverage
        run: node pr/scripts/compare-console-coverage.mjs \
          main/console/coverage/coverage-summary.json \
          pr/console/coverage/coverage-summary.json \
          console-coverage.md
      - name: Upload PR HTML coverage
        uses: actions/upload-artifact@... # v4.x
        with:
          name: console-coverage-html
          path: pr/console/coverage
          retention-days: 7
      - name: Comment coverage
        uses: marocchino/sticky-pull-request-comment@... # v2.x
        with:
          header: console-coverage
          path: console-coverage.md
      - name: Fail if coverage decreased
        run: test "$(jq -r '.passed' console-coverage-result.json)" = "true"
```

The exact action SHAs and versions should be pinned at implementation time to
match the repository convention. The workflow intentionally uses two checkouts
instead of relying on Codecov's patch/project status features: reviewers see a
direct before/after comparison in GitHub, and the comparison works even before
any Codecov repository configuration exists.

Add `scripts/compare-console-coverage.mjs` (or the nearest existing scripts
location if the implementation finds a stronger local convention) to:

- read the `total` block from each `coverage-summary.json`,
- compare `lines`, `statements`, `branches`, and `functions`,
- fail if any total percentage decreases,
- write a compact Markdown table for the PR comment,
- write a small machine-readable `console-coverage-result.json` with
  `{ "passed": true | false }`.

Use `skip-console-coverage` as the explicit escape hatch label for exceptional
PRs. When the label is present, the workflow skips the coverage comparison. A
future follow-up can add Grafana's extra cleanup jobs that delete stale comments
when the label is added or removed; this proposal keeps the first pass smaller
while preserving the same operating model.

Keep the existing `console` job in `.github/workflows/ci.yml` running
`pnpm vitest run --coverage`; that remains the regular console quality gate.
The new workflow is the richer review signal for console-changing PRs.

### 4. Upload console coverage to Codecov on main

For `push` to `main`, upload console coverage to Codecov as a repository trend
signal. Extend Vitest reporters with `lcov` as well as `json-summary`:

```ts
reporter: ['text', 'html', 'json-summary', 'lcov'],
```

Then add a Codecov upload step to the existing `console` job after
`pnpm vitest run --coverage`, guarded to avoid secret access surprises on
untrusted PRs:

```yaml
- name: Upload console coverage to Codecov
  if: github.event_name == 'push'
  uses: codecov/codecov-action@... # v6.0.1
  continue-on-error: true
  with:
    token: ${{ secrets.CODECOV_TOKEN }}
    files: ./console/coverage/lcov.info
    flags: console
```

Codecov remains the long-running dashboard; the Grafana-style PR workflow is the
review gate. The `console` flag keeps the TypeScript/React trend separate from
the Go trend.

### 5. Add a scheduled govulncheck SARIF workflow

Create `.github/workflows/govulncheck-sarif.yml` as a standalone security
workflow:

```yaml
name: Govulncheck SARIF

on:
  schedule:
    - cron: "41 4 * * 1" # weekly (Mon 04:41 UTC)
  workflow_dispatch:

permissions:
  contents: read

jobs:
  analyze:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      security-events: write
    steps:
      - uses: actions/checkout@... # v6.0.3
        with:
          persist-credentials: false
      - uses: actions/setup-go@... # v6.4.0
        with:
          go-version-file: go.mod
          cache-dependency-path: go.sum
      - run: go install golang.org/x/vuln/cmd/govulncheck@v1.3.0
      - name: govulncheck SARIF
        run: govulncheck -format sarif ./... > govulncheck.sarif
      - name: Upload to code-scanning
        uses: github/codeql-action/upload-sarif@... # v4.36.0
        with:
          sarif_file: govulncheck.sarif
```

The schedule intentionally does not reuse the exact minute used by CodeQL,
Scorecard, or secret scanning. This spreads weekly security workload across the
runner fleet while keeping the scan on Monday UTC with the rest of the security
jobs.

Do not add this job to `ci-gate`: it is not part of PR gating and only runs on
schedule/manual dispatch.

### 6. Leave the existing PR govulncheck job in place

Keep the existing `.github/workflows/ci.yml` `govulncheck` job:

- It gives PR authors immediate text-mode feedback when Go code changes.
- It remains `continue-on-error` so newly published advisories do not block
  unrelated PRs.
- The scheduled SARIF job complements it by creating persistent Security tab
  findings.

## Alternatives considered

### Codecov OIDC instead of CODECOV_TOKEN

Codecov action v6 supports OIDC tokenless uploads when configured with
`use_oidc: true` and job `id-token: write`. That would avoid storing a Codecov
secret in GitHub.

This proposal does not choose OIDC for the first fix because the issue
explicitly asks to add `CODECOV_TOKEN`, and the current failure mode is the
missing token. A future cleanup can switch to OIDC if repository policy prefers
short-lived credentials over stored service tokens.

### Fail CI when Codecov upload fails

Making Codecov blocking would catch misconfigured secrets immediately. It would
also make an external coverage service a merge dependency.

This proposal keeps the existing non-blocking behavior because the original CI
design deliberately avoided blocking on a coverage-upload hiccup. The coverage
summary step gives maintainers an in-log fallback, and the first post-merge run
can be checked manually to confirm Codecov ingestion.

Console coverage is different: the Grafana-style GitHub workflow compares PR
coverage directly against `main` and can fail without depending on Codecov.

### Put scheduled SARIF in `.github/workflows/ci.yml`

The existing CI workflow is path-gated around PR/push validation. Scheduled
vulnerability scanning has different semantics: it should run even when no Go
files changed. A separate workflow is simpler, easier to inspect in the Actions
UI, and avoids expanding the `ci-gate` dependency graph.

### Upload SARIF from the existing PR govulncheck job

Uploading SARIF on every PR would require `security-events: write` in PR CI and
care around fork permissions. The acceptance criterion asks for a scheduled
SARIF job, not PR SARIF upload. Keeping PR feedback text-only is lower risk.

### Upload only Go coverage first

Uploading only Go coverage would fix the current Codecov step but leave the
TypeScript/React console invisible as a review signal. That is a weak
interpretation of the acceptance criterion now that the console suite has real
coverage and thresholds. This proposal gives console coverage its own PR
workflow and also uploads the main-branch console trend to Codecov.

### Only upload console coverage to Codecov

Uploading `lcov.info` to Codecov is useful for long-term trend visualization,
but it does not give reviewers the same immediate, local before/after signal as
Grafana's coverage workflow. This proposal uses Codecov as the dashboard and a
GitHub-native comment/gate as the PR review tool.

### Full Grafana code-owner matrix

Grafana's frontend workflow runs per opted-in code owner. attune does not yet
have enough console ownership segmentation to justify that machinery. A single
aggregate console comparison gives the same operating model with much less YAML
and script surface.

### Add a Go coverage threshold now

A threshold is useful once the baseline and ratchet policy are agreed. Starting
with a global Go threshold in this issue would mix observability plumbing with
test coverage policy. Console already gets a relative no-regression gate by
comparing PR coverage to `main`; Go thresholding can follow once the baseline is
agreed.

## Risks / tradeoffs

- **Codecov secret remains an external dependency.** The workflow change cannot
  create the secret. Acceptance requires a maintainer to add `CODECOV_TOKEN` and
  verify Codecov receives a PR/main upload.
- **Forked PRs may not upload coverage.** GitHub does not expose repository
  secrets to untrusted forks. Internal PRs and `push` to `main` should upload;
  fork PR behavior should be documented as a limitation unless OIDC is adopted.
- **Console coverage comments need PR write permission.** The Grafana-style
  sticky comment uses `pull-requests: write`, so forked PRs may need to skip the
  comment step or run with reduced behavior. The comparison and artifact upload
  can still run with read permissions.
- **Console coverage doubles Vitest runtime on console PRs.** The workflow runs
  coverage once on `main` and once on the PR branch. The current console suite is
  small enough for this, and the richer review signal is worth the extra runtime.
- **Coverage can decrease for legitimate reasons.** The `skip-console-coverage`
  label provides an explicit escape hatch. The PR description should explain why
  the decrease is acceptable when the label is used.
- **Console coverage needs JSON and LCOV reporters.** Vitest already produces
  text/html output, but the comparison script needs `coverage-summary.json` and
  Codecov ingestion is simplest with `console/coverage/lcov.info`.
- **Scheduled SARIF may surface many existing findings at once.** Recent CI logs
  on Go 1.25.0 showed 27 reachable standard-library vulnerabilities. Uploading
  SARIF will make those visible in Security tab. That is desired, but maintainers
  should expect initial noise until the Go toolchain is patched.
- **GitHub code scanning availability differs by repository settings.** The repo
  is public and existing CodeQL/Scorecard workflows already upload SARIF, so the
  required feature path is already exercised.
- **Action pin maintenance.** The new workflow must follow the repository
  convention of full SHA pins plus trailing version comments so Dependabot can
  keep it current.
- **SARIF upload should fail on pipeline errors.** Unlike vulnerability findings,
  tool installation, SARIF generation failure, or upload failure should fail the
  scheduled run so maintainers notice the monitoring pipeline is broken.

## Implementation plan

1. Add `docs/proposals/2026/06/2026-06-10-codecov-govulncheck-ci.md` with
   status `Implemented`.
2. Update `.github/workflows/ci.yml`:
   - add `go tool cover -func=coverage.txt | tail -1`,
   - pass `${{ secrets.CODECOV_TOKEN }}` to the Codecov action.
3. Update `console/vite.config.ts`:
   - add `json-summary` and `lcov` reporters while keeping `text` and `html`.
4. Add `scripts/compare-console-coverage.mjs`:
   - compare total `lines`, `statements`, `branches`, and `functions`,
   - emit a Markdown table and machine-readable pass/fail JSON,
   - fail on any aggregate percentage decrease.
5. Add `.github/workflows/console-coverage.yml`:
   - run on console-changing PRs,
   - support `skip-console-coverage`,
   - checkout `main` and PR revisions,
   - run `pnpm vitest run --coverage` on both,
   - upload PR HTML coverage as an artifact,
   - post a sticky PR comment,
   - fail when aggregate console coverage decreases.
6. Update the `.github/workflows/ci.yml` `console` job:
   - on `push`, upload `./console/coverage/lcov.info` to Codecov with
     `flags: console`,
   - use the same `${{ secrets.CODECOV_TOKEN }}` secret,
   - keep the upload non-blocking.
7. Add `.github/workflows/govulncheck-sarif.yml`:
   - weekly `schedule`,
   - `workflow_dispatch`,
   - minimal permissions,
   - checkout/setup-go,
   - `govulncheck -format sarif ./...`,
   - `github/codeql-action/upload-sarif`.
8. Run `actionlint` if available locally; otherwise rely on existing workflow
   lint CI and inspect YAML manually.
9. Update this proposal status to `Implemented` when the workflow changes land.
10. In the PR description:
   - `Closes #16`,
   - note that changelog is skipped because this is a `ci:`/`type/chore`
     workflow-only change,
   - call out the required external `CODECOV_TOKEN` repository secret setup.
11. After merge, a maintainer verifies:
   - Codecov shows new Go and console coverage uploads for `main`,
   - a console-changing PR gets a coverage artifact and sticky comment,
   - manually dispatching `Govulncheck SARIF` uploads findings to Security ->
     Code scanning.

## Verification

Pre-merge local checks:

- `git diff --check`
- `cd console && pnpm vitest run --coverage`
- `test -s console/coverage/coverage-summary.json`
- `test -s console/coverage/lcov.info`
- `node scripts/compare-console-coverage.mjs \
  console/coverage/coverage-summary.json \
  console/coverage/coverage-summary.json \
  /tmp/console-coverage.md`
- `govulncheck -format sarif ./... > /tmp/attune-govulncheck.sarif`
- `test -s /tmp/attune-govulncheck.sarif`
- optional, if installed: `actionlint`

Pre-merge CI checks:

- Existing CI should pass.
- Workflow Lint should accept the new workflow.
- CodeQL, Scorecard, and Secret Scan workflows are unchanged.

Post-merge/manual checks:

- Confirm `CODECOV_TOKEN` exists in repository secrets.
- Confirm the next `go-checks` run logs a Go coverage `total:` line.
- Confirm the Codecov step no longer logs `Token length: 0` or `Token required`.
- Confirm Codecov shows coverage for the relevant commit/PR with the `go` flag.
- Confirm a console-changing PR posts a sticky coverage comment and uploads the
  PR HTML coverage artifact.
- Confirm a deliberate console coverage decrease fails the `Console Coverage`
  workflow unless `skip-console-coverage` is present.
- Confirm a `main` run uploads `console/coverage/lcov.info` with the `console`
  flag.
- Dispatch `Govulncheck SARIF` manually.
- Confirm the run uploads `govulncheck.sarif` successfully.
- Confirm govulncheck findings appear under GitHub Security -> Code scanning.

## References

- Issue #16: https://github.com/Phixsura/attune/issues/16
- Issue #1: https://github.com/Phixsura/attune/issues/1
- PR #11: https://github.com/Phixsura/attune/pull/11
- Codecov GitHub Action: https://github.com/codecov/codecov-action
- govulncheck command docs: https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck
- GitHub SARIF upload docs: https://docs.github.com/en/code-security/code-scanning/integrating-with-code-scanning/uploading-a-sarif-file-to-github
