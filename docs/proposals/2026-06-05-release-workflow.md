# Proposal — release workflow (tag → ghcr.io image + GitHub Release)

| | |
|---|---|
| **Issue** | #2 |
| **Status** | Implemented |
| **Started** | 2026-06-05 12:05 CST |
| **Related** | #14 (Dockerfile, reused), #8 (rename → attune, merged), #5 (compose → `:latest`) |

## Problem

CLAUDE.md §2 documents a 4-step release process; step 4 — "the release workflow
auto-builds + pushes the image and creates a GitHub Release whose notes are
pulled from the CHANGELOG" — **does not exist**. Until it does there is no way to
ship a pullable image, which blocks the **v0.2.0** release (the rename + CI work
in `[Unreleased]`) and **#5** (docker-compose, which references the published
`:latest`). #14 (standalone multi-arch Dockerfile) and #8 (rename) are merged, so
#2 is unblocked.

## Goals

On `git push` of a `vX.Y.Z` tag, automatically:
- Build the multi-arch image (`linux/amd64` + `linux/arm64`) from the existing Dockerfile.
- Push to **`ghcr.io/phixsura/attune`** tagged `:X.Y.Z`, `:X.Y`, `:latest` (latest only for stable).
- **Version-stamp** the image (`APP_VERSION`) so it self-reports its version.
- Publish a GitHub Release for the tag, body = the matching `## [X.Y.Z]` CHANGELOG section.

Plus a `workflow_dispatch` **build-only dry-run** (no login/push/release).

## Non-goals

- Cutting the actual v0.2.0 release (steps 1–3 — manual, per §2).
- Attaching binaries as release assets (the *image* is the artifact).
- #5 docker-compose.

## Proposal

### `.github/workflows/release.yml`
Triggers on `push` tags `v*` + `workflow_dispatch`. Top-level `permissions:
contents: read`; the job adds `contents: write` (Release) + `packages: write`
(ghcr). `concurrency` with `cancel-in-progress: false`. Steps: checkout
(`persist-credentials: false`) → setup-buildx → login *(push only)* →
metadata-action (`images: ghcr.io/phixsura/attune`, semver + `latest` tags) →
build-push (both arches; `push` only on a tag; `build-args: APP_VERSION`;
`cache-from: type=gha`) → release-notes → action-gh-release *(push only)*. All
actions SHA-pinned with exact `# vX.Y.Z` comments.

### `scripts/extract-changelog.sh`
`awk` prints the `## [X.Y.Z]` section up to the next `## [` **or** the footer
link-reference block, trims blank lines, and exits non-zero if the section is
missing/empty. `index()` prefix-match means `0.2.0` never matches `0.2.0-rc.1`.

### Release-notes policy
Stable tags **require** a CHANGELOG section (fail otherwise — §2). Pre-release
tags (`-rc`/`-beta`) fall back to a one-line note, so an rc dry-run doesn't fail.

### Dockerfile version stamping
Runtime stage gains `ARG APP_VERSION=dev` + `ENV APP_VERSION=${APP_VERSION}`;
release.yml feeds the tag. Feeds the existing hook at
[server.go:159](../../cmd/attune/server.go) (`envOrDefault("APP_VERSION","dev")`,
which also treats empty as `dev`). Local/CI builds stay `dev`.

### Deltas from the issue's literal steps
- **No `setup-qemu-action`** — the Dockerfile cross-compiles.
- **SHA-pinned** actions (issue showed `@v4/@v3/@v5`) — zizmor stale-ref gate.
- **CHANGELOG notes required**, not "optional" (§2).
- **`workflow_dispatch` dry-run** added.
- Tags `:0.2.0 / :0.2 / :latest` (semver, no `v`) — Docker convention.

## Alternatives considered

- **`setup-qemu` emulation** — rejected; cross-compile is free and faster.
- **Literal `:v0.2.0` tag** (issue's `github.ref_name`) — chose semver `:0.2.0`.
- **Fail on missing notes for *all* tags** — chose stable-required / prerelease-fallback so dry-runs work.
- **PAT for ghcr** — unneeded; the built-in `GITHUB_TOKEN` + `packages: write` suffices.

## Risks / tradeoffs

- New ghcr package defaults to **private** → one-time manual visibility flip to public.
- GHA cache is ref-scoped, so a tag build may miss the cache (best-effort; harmless).
- A pre-release without a CHANGELOG section ships a generic note (intentional).

## Implementation plan

1. `scripts/extract-changelog.sh` (+ verify against the real CHANGELOG).
2. `Dockerfile` — `APP_VERSION` ARG/ENV.
3. `.github/workflows/release.yml`.
4. This proposal.
5. PR `Closes #2`; verify the dry-run + an rc-tag e2e.

## Verification

| Check | Result |
|---|---|
| `extract-changelog.sh 0.1.0` prints the `[0.1.0]` notes (no footer links) | ✅ |
| Missing version exits non-zero | ✅ |
| `0.2.0` does not match a `0.2.0-rc.1` heading | ✅ |
| `workflow_dispatch` dry-run builds both arches, no push | ⬜ on PR |
| rc-tag e2e: push `:0.0.0-rc.1`, pre-release Release, `APP_VERSION` stamped, then cleaned up | ⬜ post-merge |
| One-time: flip ghcr package visibility to public | ⬜ manual |

## References

- #2 (this), #14 (Dockerfile), #8 (rename), #5 (compose). CLAUDE.md §2 (release), §3 (semver).
