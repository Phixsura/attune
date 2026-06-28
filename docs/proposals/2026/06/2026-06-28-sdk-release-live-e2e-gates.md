# Live e2e gates for SDK release workflows

| Field | Value |
| --- | --- |
| **Issue** | [#168](https://github.com/Phixsura/attune/issues/168) |
| **Status** | Implemented |
| **Started** | 2026-06-28 |
| **Related** | [#37](https://github.com/Phixsura/attune/issues/37) (Node SDK), [#36](https://github.com/Phixsura/attune/issues/36) (Go SDK), [2026-06-28-node-sdk-browser-smoke.md](./2026-06-28-node-sdk-browser-smoke.md) |

## Problem

Both SDKs now have high-value live verification paths:

- the Node SDK boots a real attune server, installs the packed npm tarball into
  an external consumer, exercises ESM/CJS usage, and now drives a real browser
  across allowed and blocked origins; and
- the Go SDK boots the same kind of real server, runs the build-tagged live
  suite, executes the example CLI, and validates an external consumer against
  management APIs.

Those checks were still outside the release workflows. Tagging an SDK release
therefore proved compilation and unit tests, but not the publish-grade,
artifact-grade integration paths that real users depend on.

## Goals

- Make SDK release workflows fail closed on live end-to-end regressions.
- Reuse the existing local SDK e2e harnesses instead of inventing parallel
  release-only scripts.
- Keep release publishing dry-runnable through `workflow_dispatch`.

## Non-goals

- Running these heavier live e2e flows on every PR in the main CI workflow.
- Replacing the existing fast unit / lint checks.

## Proposal

1. Update the Node SDK release workflow to:
   - set up the repo Go toolchain because `pnpm e2e` builds the attune server;
   - install a Chromium binary for the real-browser smoke path; and
   - run `pnpm e2e` before any publish step.
   - rely on the smoke runner's cache-first browser selection so the workflow
     prefers the just-installed Playwright Chromium over ambient system
     channels.
2. Update the Go SDK release workflow to run `./scripts/e2e.sh` before warming
   the module proxy or creating a GitHub Release.
3. Update `ci.yml` path filters so changes to the SDK release workflows still
   trigger the corresponding SDK CI jobs in PRs.
4. Keep fast static/unit checks in front of the live e2e gate so obvious
   failures short-circuit quickly.

## Alternatives considered

### Trust main CI only

Rejected. The release workflows are the last line of defense before shipping,
and today they are strictly weaker than the local/manual verification path.

### Re-implement lighter release-only smoke tests

Rejected. The repository already has the right harnesses. Duplicating them
would drift and weaken confidence.

## Risks / tradeoffs

- Release workflows will run longer because they now boot real services and
  browsers.
- The Node release workflow now depends on provisioning Chromium at runtime,
  which is slower but much more representative; the smoke runner therefore
  prefers the freshly installed/cache-resolved Playwright browser before
  system browser channels.
- Fixed localhost ports make local/release verification flaky when another SDK
  e2e run or unrelated developer service is already listening; the harnesses
  should therefore select free ports dynamically instead of assuming static
  ones.

## Implementation plan

1. Extend `sdk-release.yml` with Go setup, Chromium install, and `pnpm e2e`.
2. Extend `sdk-go-release.yml` with `./scripts/e2e.sh`.
3. Make the shared SDK e2e harnesses choose free localhost ports so release and
   local verification do not fail spuriously on port collisions.
4. Extend `ci.yml` path filters for SDK release workflow changes.
5. Keep the workflows dry-runnable without publishing.

## Verification

- Local `pnpm --dir sdk/node e2e` passes, including the packed-artifact browser
  smoke and blocked-origin assertion.
- Local `go test ./...` still passes after the supporting workflow updates.
- Workflow YAML remains syntactically valid and diff-clean.

## References

- `sdk/node/scripts/e2e.sh`
- `sdk/go/scripts/e2e.sh`
- `2026-06-28-node-sdk-browser-smoke.md`
