# Runtime Smoke Base Image Overrides

| Field | Value |
|---|---|
| Issue | [#236](https://github.com/Phixsura/attune/issues/236) |
| Status | Implemented |
| Started | 2026-08-03 13:20 +08:00 |
| Related | Post-merge release-smoke regression hardening for PR #258 |

## Problem

`make runtime-smoke` hard-coded `docker build -t attune:runtime-smoke .`.
That preserves a simple CI path, but it leaves local release regression blocked
when Docker Hub's anonymous auth endpoint is unreachable even if the required
base images are already present locally with the expected digests.

## Goals

- Keep CI and ordinary Docker builds on the official pinned Dockerfile bases.
- Let local release sweeps pass explicit, verified mirror or local base image
  references through the same `make runtime-smoke` and `make release-smoke`
  entrypoints.
- Keep the runtime smoke execution unchanged after the image is built.
- Cover the argument plumbing with script tests.

## Non-goals

- Add an implicit registry mirror.
- Change the production runtime image contents.
- Relax digest pinning expectations for CI or release artifacts.

## Proposal

Define Dockerfile build arguments for the three stage bases:

- `ATTUNE_NODE_IMAGE`
- `ATTUNE_GO_IMAGE`
- `ATTUNE_RUNTIME_IMAGE`

Add `scripts/runtime-smoke-build.sh` as the Makefile build wrapper. It preserves
the default `docker build -t attune:runtime-smoke .` shape, and only adds
`--build-arg` entries when the caller supplies:

- `ATTUNE_RUNTIME_SMOKE_NODE_IMAGE`
- `ATTUNE_RUNTIME_SMOKE_GO_IMAGE`
- `ATTUNE_RUNTIME_SMOKE_RUNTIME_IMAGE`

`make runtime-smoke` then runs the build wrapper and invokes the existing
`scripts/runtime-smoke.sh` against the same image tag.

## Alternatives Considered

- Configure a machine-wide Docker registry mirror: this fixes one workstation
  but mutates developer infrastructure and does not document the release gate.
- Use a temporary wrapper around `docker build`: it proves the image works, but
  the method is not reusable or reviewable.
- Switch the Dockerfile defaults to a mirror: rejected because release artifacts
  should keep official pinned bases by default.

## Risks / Tradeoffs

- A caller can supply an unverified base image. Documentation calls out that
  overrides are for already verified mirror or local tags, while CI remains on
  pinned official defaults.
- The Dockerfile now uses variable `FROM` statements. BuildKit supports this
  pattern, and tests plus runtime smoke verify the local Makefile path.

## Implementation Plan

1. Add Dockerfile `ARG` defaults for node, golang, and distroless bases.
2. Add `scripts/runtime-smoke-build.sh`.
3. Wire `make runtime-smoke` through the build wrapper.
4. Add node-based script tests for default and override command construction.
5. Document local mirror usage in `docs/testing.md`.

## Verification

- `make script-tests`
- `make runtime-smoke ATTUNE_RUNTIME_SMOKE_NODE_IMAGE=attune-local-node:22-alpine ATTUNE_RUNTIME_SMOKE_GO_IMAGE=attune-local-golang:1.26.5-alpine`
- `make release-smoke ATTUNE_RUNTIME_SMOKE_NODE_IMAGE=attune-local-node:22-alpine ATTUNE_RUNTIME_SMOKE_GO_IMAGE=attune-local-golang:1.26.5-alpine`

## References

- `Dockerfile`
- `Makefile`
- `scripts/runtime-smoke-build.sh`
- `docs/testing.md`
