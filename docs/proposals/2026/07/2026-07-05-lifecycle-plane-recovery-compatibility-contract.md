<!-- markdownlint-disable MD013 -->

# Lifecycle plane: recovery, rollback, and compatibility contract

| Field | Value |
| --- | --- |
| **Issue** | N/A (platform maturity subtrack under [#202](https://github.com/Phixsura/attune/issues/202)) |
| **Status** | Implemented |
| **Started** | 2026-07-05 |
| **Related** | [#149](https://github.com/Phixsura/attune/issues/149) (production readiness preflight), [#151](https://github.com/Phixsura/attune/issues/151) (backup / restore drill), [#152](https://github.com/Phixsura/attune/issues/152) (audit evidence export), [#168](https://github.com/Phixsura/attune/issues/168) (API / SDK release gates), [platform maturity program](./2026-07-05-platform-maturity-program.md) |

---

## Problem

Attune already documents production expectations and already has parts of the
recovery story:

- production readiness preflight;
- backup / restore drill work;
- audit evidence export;
- API / SDK release-gate work;
- Helm deployment docs.

The gap is that these pieces are still separate. Mature systems make lifecycle
events boring by turning them into contracts:

- can we restore this system and prove it works?
- can we roll back safely?
- can we upgrade a long-lived worker without guessing?
- do runtime contracts have a compatibility story?
- does the operator know what support window they are in?

GitLab and Temporal are especially relevant here. One is the model for backup /
restore and maintenance policy; the other is the model for long-lived workflow
and worker evolution.

## Goals

- Define one lifecycle contract spanning deploy, upgrade, rollback, restore,
  and support windows.
- Make restore evidence a normal production artifact rather than a manual
  exercise.
- Give long-lived workers and public contracts a documented compatibility
  story.
- Align startup checks, preflight, and deployment docs with the same lifecycle
  rules.
- Make release gates explicit for API / SDK and data-contract changes.

## Non-goals

- Do not replace the existing backup engine or restore tooling.
- Do not build a full zero-downtime orchestrator.
- Do not require every lifecycle event to be automated in the first version.
- Do not merge lifecycle policy with unrelated auth or observability work.

## Proposal

### 1. Define lifecycle states

Introduce a small lifecycle vocabulary for platform-managed surfaces:

- `supported`
- `deprecated`
- `migrating`
- `recovering`
- `blocked`

The point is to make support and compatibility status visible in docs, UI, and
tests.

### 2. Treat restore as a verified contract

Restore should mean more than "a database came back up". The lifecycle plane
should require:

- restore drill evidence;
- schema / migration verification;
- application-level decryptability where encrypted data exists;
- clear pass/fail reporting;
- retention of the latest good restore result.

Attune now exposes the latest restore-drill state through a dedicated admin
recovery endpoint and shares the grading policy between preflight and Console.
The Reliability page also surfaces the latest backup reference, drill duration,
and freshness window, so the recovery contract is visible instead of being
inferred from readiness checks.

### 3. Add compatibility policy for long-lived contracts

Public APIs, SDKs, worker versions, and persisted runtime shapes should all
have an explicit compatibility policy:

- what is additive;
- what is breaking;
- what gets a deprecation window;
- what needs a migration step;
- what can be kept compatible through shims.

### 4. Connect lifecycle checks to startup and preflight

Lifecycle policy should not live only in a release checklist.

If a configuration or environment cannot satisfy the lifecycle contract, the
startup path and preflight should say so in the same language.

## Alternatives considered

### Keep backup / restore as documentation only

Rejected. The whole point of the lifecycle plane is to make recovery testable.

### Handle compatibility separately for every subsystem

Rejected. That creates inconsistent support windows and incompatible operator
expectations.

### Make the release process the only lifecycle gate

Rejected. Release gates are important, but they are not enough for restore and
rollback confidence.

## Risks / Tradeoffs

- More explicit lifecycle states mean more documentation work.
- Compatibility policies can be overconstrained if written too early.
- Restore drills can become noisy if the evidence format is not stable.

## Implementation Plan

1. Define the lifecycle vocabulary and support-window contract.
2. Tie restore evidence into the production readiness story.
3. Publish compatibility rules for APIs, SDKs, and workers.
4. Align startup checks and preflight with the same lifecycle vocabulary.
5. Add regression tests for restore, rollback, and compatibility transitions.

## Verification

- Restore-drill tests and integration coverage.
- Migration / schema verification tests.
- Compatibility tests for API / SDK and worker version boundaries.
- Operator documentation review for support windows and lifecycle states.

## References

- [GitLab backup and restore](https://docs.gitlab.com/administration/backup_restore/)
- [GitLab maintenance policy](https://docs.gitlab.com/policy/maintenance/)
- [Temporal visibility](https://docs.temporal.io/visibility)
- [Temporal worker versioning](https://docs.temporal.io/worker-versioning)
- [Temporal continue-as-new](https://docs.temporal.io/workflow-execution/continue-as-new)
- [Argo CD declarative setup](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/)
