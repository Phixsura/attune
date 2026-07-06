<!-- markdownlint-disable MD013 -->

# Observability plane: release health, ownership, and saved investigation state

| Field | Value |
| --- | --- |
| **Issue** | N/A (platform maturity subtrack under [#202](https://github.com/Phixsura/attune/issues/202)) |
| **Status** | Implemented |
| **Started** | 2026-07-05 |
| **Related** | [#152](https://github.com/Phixsura/attune/issues/152) (audit evidence export), [#156](https://github.com/Phixsura/attune/issues/156) (SLO burn-rate / tenant impact), [#170](https://github.com/Phixsura/attune/issues/170) (saved views), [platform maturity program](./2026-07-05-platform-maturity-program.md) |

---

## Problem

Attune already has metrics, dashboards, and a growing set of Console workbenches.
That covers signal. It does not yet cover operational memory.

Mature observability products do more than show red or green:

- Sentry ties issues to releases, owners, and investigations;
- Grafana routes alerts and makes ownership and paging explicit;
- PostHog connects runtime behavior to investigation and replay context.

The current gap is that Attune can tell operators that something is happening,
but it does not yet give them enough structured memory to answer:

- what changed?
- who owns it?
- which tenants or workspaces are impacted?
- is this a release issue or a long-running trend?
- have we seen this exact investigation before?

## Goals

- Add release-health reporting that links symptoms to a build or release.
- Add ownership metadata to issues, alerts, and Console workbenches.
- Add saved investigation state so operators can return to a triage context.
- Add impact attribution for tenant or workspace slices.
- Keep runbook links attached to the surfaces that page or triage.
- Keep the Console and Grafana stories semantically aligned.

## Non-goals

- Do not replace Grafana or build a new metrics backend.
- Do not add raw user content to metric labels or saved-view metadata.
- Do not make release health a substitute for the underlying SLOs.
- Do not turn every dashboard into a saved view.

## Proposal

### 1. Add release-health as a first-class operational object

Attune should be able to answer which release or build introduced a visible
change in health.

That means preserving:

- release identifier;
- deployment timestamp;
- owner or owning team;
- affected subsystem;
- health trend or regression summary.

### 2. Add ownership metadata across the triage surface

Alerts, issues, and key workbench views should carry structured ownership
metadata.

The first version can be simple:

- owner team;
- subsystem;
- runbook URL;
- escalation channel or destination.

### 3. Add saved investigation state

Investigations should be storable and reopenable.

That includes the current:

- filters;
- release context;
- owner context;
- impacted tenant slice;
- active alert or issue set.

### 4. Add impact attribution

The observability plane should make it easy to see which tenants or workspaces
account for the majority of the budget burn or symptom load.

This is the bridge between signal and action in a multi-tenant product.

## Alternatives considered

### Leave ownership in runbooks only

Rejected. Ownership that only exists in prose is too easy to drift.

### Build a new triage product outside the Console

Rejected. Attune already has a Console and Grafana; the missing piece is the
contract, not a brand new surface.

### Make saved views the only observability artifact

Rejected. Saved views are useful, but they are not a substitute for release
health or owner context.

## Risks / Tradeoffs

- Ownership metadata can become stale if it is not validated.
- Saved views can create privacy concerns if they capture raw content.
- Release-health data can be misleading if the release boundary is not
  consistently defined.

## Implementation Plan

1. Define release-health and ownership metadata shapes.
2. Add saved-view persistence for investigation state.
3. Link alerts and key Console surfaces to owner and runbook data.
4. Add tenant / workspace impact slices to the triage views.
5. Add tests for saving, restoring, and rendering investigation state.

## Verification

- Tests for saved-view persistence and restore.
- Tests that owner and runbook fields render consistently.
- Metrics and dashboard checks for release-health slices.
- Manual operator review of impact attribution and investigation reopenability.

## References

- [Sentry issues](https://docs.sentry.io/product/issues/)
- [Sentry issue details](https://docs.sentry.io/product/issues/issue-details/)
- [Sentry ownership rules](https://docs.sentry.io/product/issues/ownership-rules/)
- [Sentry release health](https://docs.sentry.io/product/releases/health/)
- [Sentry issue views](https://docs.sentry.io/product/issues/issue-views/)
- [Grafana alerting](https://grafana.com/docs/grafana/latest/alerting/)
- [PostHog session replay](https://posthog.com/docs/session-replay)
