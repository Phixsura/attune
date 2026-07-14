<!-- markdownlint-disable MD013 -->

# Public Roadmap From Workflow States

| Field | Value |
|---|---|
| Issue | [#222](https://github.com/Phixsura/attune/issues/222) |
| Status | Implemented |
| Started | 2026-07-14T11:13:30+08:00 |
| Related | [#202](https://github.com/Phixsura/attune/issues/202), [#215](https://github.com/Phixsura/attune/issues/215), [#220](https://github.com/Phixsura/attune/issues/220), [#221](https://github.com/Phixsura/attune/issues/221), [Customer Requests](./2026-07-07-customer-requests.md), [Public Visibility and Moderation Policy](./2026-07-10-public-visibility-moderation-policy.md), [End-User Feedback Submission Portal](./2026-07-11-end-user-feedback-submission-portal.md), [Public Voting Board](./2026-07-13-public-voting-board-mvp.md) |

## Problem

Attune already has the public read surface, public request projections, a
tenant-scoped public visibility policy, and a separate `roadmap` list route. The
missing piece in #222 is the source of truth for the public roadmap columns.

Today, the public projection still lets Console operators type `roadmap_column`
as a free-form string. That works for manual curation, but it creates a drift
risk: when a request moves through its workflow, the internal request state and
the public roadmap label can get out of sync unless someone updates both by
hand.

Issue #222 is asking for a stronger model: the customer request workflow state
should drive the public roadmap output, and the public roadmap should remain a
curated projection with explicit inclusion controls, not a second planning
system.

Terminology matters here: in this proposal, "workflow state" means
`customer_requests.status`. It does not mean the separate moderation workflow
under `internal/service/workflow`.

## Goals

- Publish a public roadmap page that is derived from customer request workflow
  state.
- Keep `included_in_portal` and `included_in_roadmap` as explicit per-request
  publication controls.
- Expose request counts, vote counts, and safe summaries on the public roadmap.
- Keep internal notes, CRM data, and moderation internals off the public
  surface.
- Make workflow status changes update the public roadmap predictably.
- Provide a Console configuration and preview flow that uses the same
  projection as the public page.
- Keep the public API shape stable while changing the source of truth behind
  it.
- Preserve tenant branding, empty states, and existing public policy gates.

## Non-goals

- Do not build a separate roadmap planning system.
- Do not add public changelog editing, release-note authoring, or notification
  subscriptions in this slice.
- Do not add custom-domain routing, embed SDKs, SSO access control, or segment
  gating here.
- Do not expose internal workflow notes, scores, or customer CRM fields.
- Do not replace the existing public board or submit-only portal.

## Current State

Attune already has the main primitives needed for a world-class public roadmap:

- `customer_requests.status` is the canonical request lifecycle state and
  currently uses the small vocabulary `open`, `planned`, `in_progress`,
  `shipped`, and `cancelled`.
- `PublicVisibilityService` already owns the public request projection and the
  roadmap read path.
- `public_request_profiles` already stores `public_state`, `roadmap_column`,
  `included_in_portal`, and `included_in_roadmap`.
- `GET /v1/portal/{tenant_slug}/roadmap` already exists and already shares the
  public-safe projection rules.
- Console already has a public-visibility editor and preview path.

The gap is semantic, not structural: the roadmap is still driven by manual
strings instead of a workflow-derived mapping.

## Current Fields

| Field | Current behavior | Proposal |
|---|---|---|
| `customer_requests.status` | Canonical request lifecycle state | Source of truth for roadmap mapping |
| `roadmap_status_mapping` | N/A | Tenant-scoped ordered mapping stored in public visibility policy JSONB |
| `roadmap_column` | Free-form roadmap label | Derived cache from status and tenant mapping |
| `public_state` | Public board label | Separate surface, unchanged in this issue |
| `included_in_portal` | Board inclusion toggle | Unchanged |
| `included_in_roadmap` | Roadmap inclusion toggle | Unchanged |

The key change is that `roadmap_column` stops being hand-authored copy in the
default flow. It becomes a projection of the canonical request workflow state
and the tenant roadmap mapping, with the Console preview showing exactly what
the public roadmap will render. The board-facing `public_state` remains out of
scope here so the roadmap projection can stay focused and deterministic.

## Industry Synthesis

Ten top-tier products converge on the same pattern: public roadmap surfaces are
curated projections, not raw planning records.

| Product | Observed pattern | Decision for Attune |
|---|---|---|
| Aha! Ideas | Intake portals and browseable boards are distinct modes. | Keep submission separate from roadmap browsing. |
| Canny | Public/private/custom boards and roadmaps are first-class. | Make policy and publication state explicit. |
| Featurebase | Moderation, merge, roadmap, and changelog live together. | Keep roadmap tied to moderation and safe copy. |
| Productboard | Published views expose curated fields, not the workspace. | Publish allowlisted projections only. |
| UserVoice | Public and internal statuses are distinct. | Keep workflow status separate from public presentation. |
| Pendo Feedback | Moderation hides unreviewed items from customers. | Only approved, live requests reach the public roadmap. |
| Jira Product Discovery | Published views are previewable and field-curated. | Drive Console preview from the same projection as public. |
| Linear Customer Requests | Internal execution remains canonical. | Derive public roadmap state from the canonical request record. |
| Productlane | Multiple portals can sync to execution. | Keep the model extensible to future audience splits. |
| Sleekplan | Privacy controls, board, roadmap, and changelog coexist. | Keep branding and access policy cohesive across the portal family. |

The common rule is simple: world-class systems never expose the internal object
directly. They publish a curated view, keep a moderation queue behind it, and
preserve a stable link back to the internal planning system.

## World-class Delta

#222 should land the public projection layer first. A world-class roadmap suite
also includes the capabilities below, but they are separate from this issue:

| Gap | Why it matters | Scope |
|---|---|---|
| Status-change notifications and changelog posts | Closes the loop so customers see progress, not just state. | Out of scope for #222 |
| Custom domain, embed, SSO, multiple portals, localization | Makes the board feel native inside each customer environment. | Out of scope for #222 |
| Audit, publish snapshots, and rollback | Turns the roadmap into a governed public asset rather than an ad hoc page. | Out of scope for #222 |
| Duplicate merge, saved views, segmentation, and analytics | Helps operators scale triage and understand demand patterns. | Out of scope for #222 |
| Column recency ordering | Makes the roadmap feel alive when status changes land. | In scope |

The point of #222 is to land the trust contract first: a stable, previewable,
tenant-aware public roadmap that can be derived from execution without manual
per-request relabeling. Everything above still matters, but it should not blur
the boundary of this issue.

## Proposal

### 1. Make workflow state the source of truth for roadmap columns

Introduce a tenant-scoped roadmap mapping that converts
`customer_requests.status` into a public roadmap column label and order. Store
that mapping as `public_visibility_policies.roadmap_status_mapping` JSONB, the
same way `portal_submission_form` already lives on the policy record. The
mapping is applied uniformly to every request in the tenant and surfaced
through the `PublicVisibilityPolicy` contract and Console editor.

The default mapping should be:

- `open` -> `under consideration`
- `planned` -> `planned`
- `in_progress` -> `in progress`
- `shipped` -> `shipped`
- `cancelled` -> excluded from the public roadmap

The mapping should store at least:

- internal request status
- public roadmap label
- column order
- visibility flag

This keeps the public roadmap deterministic while still allowing a tenant to
rename the public label if its audience prefers different wording. If we need
different wording, we edit the tenant mapping, not individual request rows.
`roadmap_column` remains a derived cache for query, sort, and render purposes,
not the authoritative source of truth.

The public roadmap contract shape stays stable. `ListPublicRoadmapResponse`
still returns columns and request summaries; only the label source and ordering
become mapping-driven.

### 2. Keep inclusion controls explicit

`included_in_roadmap` should remain the per-request gate for whether a request
appears on the public roadmap at all. A request can have a mapped roadmap stage
and still be excluded if the operator has not opted it in.

`included_in_portal` should continue to govern the general public board surface.
The roadmap page and the board page answer different questions, so their
publication toggles should stay separate.

Workflow status mapping must not override visibility policy. A request still has
to pass moderation and live-record checks before it becomes public.

### 3. Derive the public roadmap projection during write paths

The roadmap column should be derived whenever we persist a request profile and
whenever the underlying `customer_requests.status` changes. When the tenant
mapping itself changes, the cached public labels should be recomputed too.

That gives us two important properties:

- the public roadmap stays in sync with the canonical request record;
- Console preview can show the exact same result that the public page will
  render.

If the workflow status changes from `planned` to `in_progress`, the roadmap
column should move with it automatically. Operators should not need to edit a
second free-form string to keep the public page current.
Per-request manual overrides are not part of this issue; the source of truth is
the tenant mapping plus the underlying request status.

### 4. Keep the public roadmap page as a curated projection

`GET /v1/portal/{tenant_slug}/roadmap` should continue to serve the public-safe
projection, but the rendered page should group items by the derived roadmap
column and order those columns by the configured workflow mapping.

Each column card should show only allowlisted public data:

- request title
- safe summary
- vote count
- comment count when the policy allows it
- public status / roadmap label

The page should keep the existing tenant branding, empty-state handling,
`no-store` behavior, and `noindex` behavior.
Within each column, the default order should be newest-updated first so status
changes surface immediately. If we later add a dedicated `status_changed_at`
field, the public contract should switch to that signal without changing the
public API shape.
The `roadmap` filter should keep matching the public column label shown on the
page, not a private execution enum.

### 5. Make Console preview read from the same projection

Console should stop behaving like a separate editor for raw roadmap strings and
instead act as a preview and policy surface for the public projection.

The operator experience should let the user:

- inspect which workflow status maps to which public roadmap column;
- see how many requests land in each column before saving;
- edit the status-to-column mapping;
- toggle `included_in_roadmap` per request;
- preview the live public page and the roadmap page from the same data.

## Alternatives Considered

- Keep the current free-form `roadmap_column` text box and treat the public
  roadmap as a manual curation surface.
  - Rejected because it keeps the drift problem alive and makes workflow changes
    unreliable.
- Create a separate roadmap model detached from `customer_requests`.
  - Rejected because it duplicates the source of truth and makes closure harder.
- Build the roadmap entirely in the browser by grouping request-board data.
  - Rejected because the public shape would become fragile, hard to preview, and
    harder to test.
- Add changelog publishing in the same slice.
  - Rejected because #222 is already large enough and the roadmap should land
    before the release-note loop.

## Risks / Tradeoffs

- Tenants with bespoke vocabularies may want custom public labels, which means
  the mapping needs to be configurable rather than hard-coded forever.
- Derived data must be recomputed on `customer_requests.status` changes and on
  mapping changes, or the public roadmap will drift from the canonical request
  record.
- Existing published rows may already carry bespoke `roadmap_column` values, so
  cutover needs a backfill plan that rewrites them from the new mapping without
  surprising operators.
- A more opinionated roadmap is easier to trust, but less flexible than a fully
  manual editor.

## Implementation Plan

1. Extend the public visibility contract with a roadmap-stage mapping structure
   stored in policy JSON and exposed through the Console editor.
2. Add a migration/backfill that seeds the mapping from the canonical status
   set, then rewrites the cached public projection from
   `customer_requests.status`.
3. Update the Console public-visibility page to edit the mapping and preview the
   public roadmap result.
4. Refresh the public roadmap page so it groups by the derived stage and shows
   empty states, counts, and safe summaries.
5. Add tests for visibility, ordering, mapping, migration, and Console preview
   parity.

## Verification

The implementation should be proven with:

- Go unit tests for mapping, projection, and visibility gates.
- Go integration tests for the public roadmap route and tenant isolation.
- Console component tests for the preview and mapping editor.
- `buf lint` and `buf generate && git diff --exit-code` for the policy contract
  update.
- `go test ./...` or the changed-package subset, depending on the final scope.
- `pnpm vitest run --coverage` for the Console surface.
- `pnpm exec vite build` for the final UI bundle smoke check.

## References

- [#222](https://github.com/Phixsura/attune/issues/222)
- [Public Visibility and Moderation Policy](./2026-07-10-public-visibility-moderation-policy.md)
- [End-User Feedback Submission Portal](./2026-07-11-end-user-feedback-submission-portal.md)
- [Public Voting Board](./2026-07-13-public-voting-board-mvp.md)
- [Customer Requests](./2026-07-07-customer-requests.md)
- `internal/infra/database/migrations/107_portal_submission_form_and_inbox.sql`
- `internal/repo/customerrequest/customerrequest.go`
- `internal/service/publicvisibility/publicvisibility.go`
- `internal/handlers/portal/board.go`
- `internal/handlers/console/publicvisibility/handler.go`
