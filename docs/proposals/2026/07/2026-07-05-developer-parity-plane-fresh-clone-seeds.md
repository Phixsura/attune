<!-- markdownlint-disable MD013 -->

# Developer parity plane: fresh-clone bootstrap, seeds, and smoke loops

| Field | Value |
| --- | --- |
| **Issue** | N/A (platform maturity subtrack under [#202](https://github.com/Phixsura/attune/issues/202)) |
| **Status** | Implemented |
| **Started** | 2026-07-05 |
| **Related** | [#5](https://github.com/Phixsura/attune/issues/5) (private deploy), [#19](https://github.com/Phixsura/attune/issues/19) (proto contract), [#149](https://github.com/Phixsura/attune/issues/149) (preflight), [platform maturity program](./2026-07-05-platform-maturity-program.md) |

---

## Problem

Attune is already reasonably approachable for contributors who know the repo.
It is not yet at the point where a fresh clone can reliably reach a useful
local state without context.

Supabase is a useful benchmark because it treats local development as part of
the product:

- clean bootstrap;
- deterministic seed data;
- generated types in sync with contracts;
- reproducible testing;
- explicit reset / teardown paths.

Attune has pieces of that story already, but they are spread across docs,
Compose, Helm, CI, and ad hoc commands.

## Goals

- Make fresh-clone bootstrap repeatable.
- Make deterministic seed data part of the developer flow.
- Keep generated client types in sync with API contracts.
- Provide a clear reset / teardown loop.
- Keep a smoke path that validates the happy local setup.

## Non-goals

- Do not replace the existing Compose / Helm deployment model.
- Do not build a cloud IDE or remote development platform.
- Do not require heavy full-stack emulation for every contributor task.
- Do not conflate local parity with production parity.

## Proposal

### 1. Define a named developer bootstrap path

Attune should expose a small set of named commands for local setup:

- bootstrap dependencies;
- start local services;
- apply or reset state;
- run a smoke check.

The exact command names can stay aligned with the repo's current `make`
conventions, but the workflow should be explicit and documented.

### 2. Add deterministic seed and reset behavior

Local development should be able to reset to a known-good state.

That means:

- seeded reference tenants or workspaces;
- predictable admin/test identities;
- deterministic fixtures for search and workbench flows;
- a reset step that recreates the local baseline.

### 3. Keep generated clients in sync

If the public contract changes, the local developer loop should make the type
regeneration step obvious.

This applies to any generated Go / TS / OpenAPI artifacts that a contributor
needs to keep aligned with the runtime contract.

### 4. Add a local smoke check

Fresh clone parity should not rely on manual guessing.

A smoke loop should confirm:

- the service starts;
- the main local pages load;
- the primary contracts are present;
- the repo can be reset and started again.

## Alternatives considered

### Leave local setup as documentation only

Rejected. Documentation alone does not remove environment drift.

### Build a fully managed development environment

Rejected. Too large for the current gap and unnecessary for the first pass.

### Tie parity only to CI

Rejected. CI is necessary, but contributors still need a fast local loop.

## Risks / Tradeoffs

- Seed data can become stale if it is not reviewed.
- Local smoke checks can become too slow if they try to cover everything.
- A bootstrap script can hide underlying setup issues if it is too magical.

## Implementation Plan

1. Define the canonical local bootstrap and reset commands.
   These now exist as `attune demo seed|reset|bootstrap` and matching `make`
   targets for the local demo workspace.
2. Add deterministic seed data and fixtures.
3. Add type-generation checks for API-contract changes.
4. Add a local smoke loop with a small set of critical routes or commands.
5. Document the reset and recovery path in the developer docs.

## Verification

- Fresh-clone smoke on a clean environment.
- Deterministic reset / restart behavior.
- Contract-generation drift checks.
- A contributor can get to a meaningful local state without tribal knowledge.

## References

- [Supabase local development overview](https://supabase.com/docs/guides/local-development/overview)
- [Supabase CLI getting started](https://supabase.com/docs/guides/local-development/cli/getting-started)
- [Supabase type generation](https://supabase.com/docs/guides/api/rest/generating-types)
- [Supabase local testing](https://supabase.com/docs/guides/local-development/testing/overview)
