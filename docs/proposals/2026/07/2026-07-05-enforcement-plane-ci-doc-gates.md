<!-- markdownlint-disable MD013 -->

# Enforcement plane: CI, docs, and release gates for the maturity contract

| Field | Value |
| --- | --- |
| **Issue** | N/A (platform maturity subtrack under [#202](https://github.com/Phixsura/attune/issues/202)) |
| **Status** | Implemented |
| **Started** | 2026-07-05 |
| **Related** | [#149](https://github.com/Phixsura/attune/issues/149) (preflight), [#151](https://github.com/Phixsura/attune/issues/151) (restore drill), [#156](https://github.com/Phixsura/attune/issues/156) (SLO burn-rate / tenant impact), [#168](https://github.com/Phixsura/attune/issues/168) (API / SDK release gates), [#171](https://github.com/Phixsura/attune/issues/171) (Console accessibility alignment), [platform maturity program](./2026-07-05-platform-maturity-program.md) |

---

## Problem

A maturity contract only matters if it is enforced.

Attune already has strong examples of enforced contracts:

- `make ci-check`;
- deployment docs that line up with runtime checks;
- proposal docs that describe accepted shapes and tradeoffs;
- preflight / doctor surfaces for operational readiness.

The missing layer is the contract between those pieces. A world-class platform
does not leave the operator or contributor to infer whether a new feature
respects the maturity bar. It makes the bar visible in CI, docs, and release
workflow.

## Goals

- Make every major track in the maturity program traceable to a proposal and
  an acceptance check.
- Keep docs, runtime behavior, and CI aligned.
- Make release gates explicit for breaking or lifecycle-sensitive changes.
- Keep the contract visible to contributors before code lands.

## Non-goals

- Do not create a second CI platform.
- Do not duplicate every existing test as a new gate.
- Do not turn proposals into purely ceremonial paperwork.
- Do not add enforcement that cannot be run locally or reviewed in the repo.

## Proposal

### 1. Make the maturity contract traceable

Each track proposal should clearly state:

- what it changes;
- what it does not change;
- how it is verified;
- which existing work it depends on.

The umbrella proposal should then link those track proposals directly.

### 2. Add a contract-aware acceptance checklist

For important work, the repo should be able to answer:

- does this change touch governance, lifecycle, observability, semantics, or
  developer parity?
- if yes, which specific subtrack does it belong to?
- what is the acceptance test or review gate?

### 3. Keep docs in sync with runtime behavior

Docs should not describe a stronger or weaker contract than the runtime
actually provides.

That means a change can fail review if the docs, proposal, and implementation
do not say the same thing.

### 4. Preserve release-gate discipline

Breaking or compatibility-sensitive changes should continue to flow through
the same gate discipline already used in the repo:

- proposal first;
- implementation next;
- tests and docs together;
- CI confirmation before merge.

The implemented gate is `scripts/lint-maturity-contract.sh`, wired into
`make ci-check` and CI so the umbrella proposal, child track links, and
verification sections stay in sync.

## Alternatives considered

### Rely on code review alone

Rejected. Review catches a lot, but it is not a durable contract.

### Add a separate maturity-check service

Rejected. The repo already has the right CI and docs surface; the gap is
alignment, not another service.

### Put all enforcement into one giant gate

Rejected. That would become hard to maintain and easy to bypass mentally.

## Risks / Tradeoffs

- Over-enforcement can make the repo feel bureaucratic.
- Under-enforcement makes the maturity contract decorative.
- Too many small gates can become noisy unless they are documented well.

## Implementation Plan

1. Link the umbrella proposal to the child proposals.
2. Add a simple contract checklist to the proposal template or review flow.
3. Require every major track to name its verification path.
4. Add any missing doc or CI links needed to keep the contract visible.
5. Review whether one lightweight maturity gate should aggregate the contract
   checks.

## Verification

- The umbrella proposal links to each track proposal.
- Each track proposal has a verification section that can be run or reviewed.
- CI and docs continue to agree with the implemented runtime behavior.

## References

- [Production readiness preflight](../06/2026-06-23-production-readiness-preflight.md)
- [Backup / restore drill](../06/2026-06-24-backup-restore-drill.md)
- [SLO burn-rate / tenant impact](./2026-07-04-slo-burn-rate-tenant-impact-dashboard.md)
- [Console accessibility industry alignment](./2026-07-01-console-accessibility-industry-alignment.md)
