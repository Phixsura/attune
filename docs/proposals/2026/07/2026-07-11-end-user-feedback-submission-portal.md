<!-- markdownlint-disable MD013 -->

# End-User Feedback Submission Portal

| Field | Value |
|---|---|
| Issue | [#220](https://github.com/Phixsura/attune/issues/220) |
| Status | Implemented |
| Started | 2026-07-11T12:38:04+08:00 |
| Related | [#202](https://github.com/Phixsura/attune/issues/202), [#212](https://github.com/Phixsura/attune/issues/212), [#215](https://github.com/Phixsura/attune/issues/215), [Customer Requests](./2026-07-07-customer-requests.md), [Public Visibility and Moderation Policy](./2026-07-10-public-visibility-moderation-policy.md) |

## Problem

Attune now has three strong pieces:

- a canonical raw feedback ingest path for authenticated API-key senders,
- a public read-only portal for requests and roadmap items, and
- a public visibility and moderation layer that already knows about portal
  submissions.

What is missing is the actual end-user submission surface. External users still
cannot submit product feedback through a tenant-branded public page, so teams
fall back to support tickets, ad hoc forms, or manual re-entry. The current
`/v1/feedback/ingest` route is not a substitute; it requires API keys, is meant
for trusted integrations, and has different abuse assumptions. The current
portal routes are read-only. If we bolt browser submissions onto the ingest
path, we risk exposing internal semantics, duplicating validation logic, and
making the portal impossible to configure safely.

Industry leaders have converged on a different model: public or submit-only
portals, clear write modes, configurable identity display, moderation queues,
duplicate handling, and a closed-loop handoff back into internal planning
objects. Attune should follow that shape while keeping raw feedback canonical
and private.

## Goals

- Add a tenant-branded public feedback submission page.
- Support request, bug, and general submission kinds.
- Support anonymous or identified submissions according to tenant policy.
- Capture page URL and tenant-configured custom fields.
- Store submissions as raw feedback evidence with portal metadata and
  submission kind, not as public board entries.
- Expose a public config endpoint for the portal page and a public create
  endpoint for submissions.
- Reuse the existing public visibility and moderation contract, including
  `portal_submission`.
- Extend the source vocabulary with a dedicated `portal` source token rather
  than overloading the existing `web` widget channel.
- Persist the portal submission kind through the existing `user_feedback.type`
  field so Console filters and search can query it without a second schema.
- Add moderation and anti-spam controls with readable public failures.
- Add Console settings and a live preview for the portal form.
- Give operators an explicit path from submission to Customer Request linkage.
- Keep public routes safe-by-default and test-covered.

## Non-goals

- Do not build a public voting board, roadmap board, or changelog board in this
  issue.
- Do not expose other users' submissions to end users.
- Do not require login, SSO, or customer accounts for v1.
- Do not add a custom-domain routing system or embed SDK in this issue.
- Do not replace the API-key ingest path.
- Do not add an external CAPTCHA or ML moderation provider in v1.
- Do not turn the portal into a full CRM or support inbox replacement.

## Current State

- `proto/attune/v1/public_visibility.proto` already defines
  `PUBLIC_SURFACE_PORTAL_SUBMISSION`, but there is no submit route yet.
- `internal/service/publicvisibility/publicvisibility.go` already models access
  mode, write mode, identity mode, and moderation states.
- `internal/handlers/portal/handler.go` and `cmd/attune/router.go` expose only
  GET portal routes today.
- `internal/handlers/ingest.go` and `proto/attune/v1/ingest.proto` serve
  API-key-authenticated feedback ingest, not public portal traffic.
- `internal/repo/feedback/feedback.go` already has the raw feedback store,
  including `source` and `source_meta`, which is the right place for portal
  submissions to land.
- `internal/repo/publicvisibility/publicvisibility.go` and the
  `106_public_visibility_moderation.sql` migration already have the moderation
  subject table that can track `portal_submission` review state.
- `internal/handlers/console/publicvisibility/handler.go` already manages
  policy and moderation settings, so the missing piece is the public intake
  flow and its preview.

## Industry Alignment Findings

| Source | Observed pattern | Decision for Attune |
|---|---|---|
| Canny | Public, private, and custom boards; identity display can be anonymized. | Separate write permission from identity display. |
| Featurebase | Post and comment moderation, duplicate merging, private/company/read-only boards. | Make moderation first-class and keep a merge path. |
| Productboard | Public/private links, multiple portals, configurable forms, portal card updates. | Make the portal configurable and support closure back to planning. |
| Aha! Ideas | Public and submit-only portals, private comments, public visibility controls. | Ship a submit-only portal first, not a public board. |
| UserVoice | Spam filters and inappropriate-content reporting. | Add spam-safe public failures and operator review actions. |
| Pendo Feedback | Request moderation and visitor request controls. | Gate writes with explicit policy and safe defaults. |
| Jira Product Discovery | Published views are curated by field allowlists. | Render only allowlisted public-safe fields. |
| Linear | Requests become internal issues and planning objects. | Treat portal intake as evidence that can promote into Customer Requests. |
| Productlane | Multiple portals and multi-source intake. | Keep the design extensible to later audience splits. |
| Sleekplan | Anonymous posting, privacy controls, and merging. | Preserve anonymous mode while keeping operator traceability. |

## Proposal

### 1. Keep the portal submit-only in v1

The portal is a submission surface, not a public board. Users submit one
feedback item at a time and receive a confirmation. They do not browse other
submissions. That matches Aha's submit-only model and keeps the product aligned
with the issue scope.

Public readers will still have the existing request and roadmap read surfaces,
but the submission route itself will not expose a public list or comment thread.

### 2. Add a public portal config endpoint and a submission endpoint

Extend `PortalService` with two new public routes:

- `GET /v1/portal/{tenant_slug}/submission-config`
- `POST /v1/portal/{tenant_slug}/submissions`

`submission-config` returns only allowlisted, public-safe data:

- tenant display name and branding
- portal headline and description
- enabled submission kinds
- visible and required field definitions
- identity mode
- submit button and acknowledgement copy

`POST /submissions` accepts:

- submission kind: `request`, `bug`, or `general`
- title and details
- optional page URL
- optional contact identity, depending on portal mode
- tenant-configured custom fields
- an idempotency token

Both routes return `404` when the portal is disabled or not public, to avoid
tenant enumeration. The read route should continue to use `Cache-Control:
no-store`, and both the public config page and submission page should remain
`noindex`.

### 3. Persist portal submissions as raw feedback evidence

The public submission should land in `user_feedback`, not in a separate public
board table. That keeps Attune's raw-feedback model canonical and lets the rest
of the product reuse the same evidence pipeline.

Implementation details:

- set `source = 'portal'` after extending the source vocabulary and its tests
- set `type = 'request' | 'bug' | 'general'` for portal submissions so the
  public kind is queryable through the existing Console/search shape
- store portal metadata and custom fields in `source_meta.portal_submission`
- store operator-visible contact identity in
  `source_meta.portal_submission.private_contact` only when the portal mode
  allows identification
- use a synthetic tenant-scoped user principal for anonymous submits
- keep the private contact payload out of all public routes and projections
- keep raw identity private and operator-visible only

Add a partial `user_feedback` index for the portal inbox path, optimized for
tenant-scoped newest-first queries on portal submissions. The portal inbox
should never need to full-scan the tenant's entire feedback history.

This keeps the public portal from becoming a second feedback system.

### 4. Reuse the existing moderation subject model

Create a `public_moderation_subjects` row for each submission with
`surface = 'portal_submission'` and `subject_id = user_feedback.id`.

That gives us:

- one moderation queue across surfaces
- shared state transitions
- audit-friendly reviewer actions
- no duplication of raw submission payload in moderation tables

Default moderation state should follow the existing policy:

- `pending` for review-first portals
- `approved` for auto-approve portals where the operator explicitly opts in

### 5. Make portal form configuration a small, versioned schema

Add a single tenant-scoped form config blob to `public_visibility_policies`
instead of building a full form-builder subsystem for v1.

The config should be a typed, validated JSON object with:

- version
- portal copy
- ordered field definitions
- required and visible flags
- custom field definitions with bounded types
- button and acknowledgement copy

Supported field kinds for v1:

- text
- textarea
- select
- multiselect
- boolean

This keeps the feature shippable without sacrificing future extensibility. The
Console preview can render the same schema directly, so the preview and public
page stay aligned.

### 6. Keep anti-spam controls practical and observable

Add layered abuse controls:

- per-tenant rate limiting
- per-IP rate limiting
- idempotency for browser retries
- max-length and max-count validation on all inputs
- a hidden honeypot field in the form payload
- safe error messages that do not reveal rule thresholds or spam heuristics

Validation failures should use clear, user-readable copy. Spam or rate-limit
failures should not leak internal scoring, raw fingerprints, or operator notes.

### 7. Give operators a clean Console workflow

The Console should gain a portal settings and preview workflow plus a
submission inbox.

Settings should control:

- portal enabled or disabled
- write mode
- identity mode
- visible fields
- required fields
- custom field schema
- acknowledgement copy
- moderation default

Inbox and detail views should show:

- submission kind
- title and details
- page URL
- contact identity, when allowed
- custom fields
- moderation state
- spam or review metadata
- source and timestamp

Operators should be able to:

- approve
- reject
- hide
- mark spam
- restore
- promote to Customer Request
- link to an existing Customer Request

Promotion should preserve backlinks by linking the raw feedback row into
`customer_request_feedback_links` rather than copying content into another
place.

All portal settings, moderation, and promotion actions must emit audit events
through the existing allowlisted actions:

- `public_policy.update`
- `public_request_profile.upsert`
- `moderation.approve`
- `moderation.reject`
- `moderation.hide`
- `moderation.mark_spam`
- `moderation.restore`
- `customer_request.promote_feedback`
- `customer_request.link_feedback`

### 8. Keep the implementation layered

Add a slim `internal/service/portal` orchestration layer for public submission
intake and config rendering.

- `publicvisibility` remains the owner of policy, moderation state, and public
  projections.
- `portal` owns public submission orchestration and public config assembly.
- `feedback` remains the raw data store for `user_feedback`.

That split keeps the visibility rules shared and prevents the portal intake
from becoming a second giant service.

## Alternatives Considered

### Reuse `/v1/feedback/ingest` directly

Rejected. It would force a public browser flow through an API-key contract,
couple the portal to trusted-integrator semantics, and make the public surface
harder to harden and explain.

### Build a separate portal submission table

Rejected. Attune already has a canonical raw feedback table. A separate portal
table would create another intake lane and make triage, search, enrichment, and
request promotion harder to unify.

### Build a full public board with voting and comments

Rejected for #220. That is a materially larger product surface and belongs in a
later issue. The submit-only portal is the smallest useful product that matches
the issue and the competitive baseline.

## Risks / Tradeoffs

- Reusing `user_feedback` means `source_meta` must stay disciplined, or
  portal-specific metadata could become messy.
- Anonymous submission increases abuse pressure, so rate limiting and
  idempotency need to be real, not aspirational.
- A JSON-based form schema is faster to ship than a form-builder table, but it
  shifts correctness burden to service-layer validation.
- Promotion to Customer Request should stay an operator action so the portal
  does not accidentally auto-clutter the planning layer.
- Public endpoints must remain no-store and noindex so the portal does not
  leak into search engines or caches.

## Implementation Plan

1. Extend `proto/attune/v1/public_visibility.proto` with public portal config
   and submission messages, then regenerate contracts.
2. Extend the source vocabulary, add the portal inbox index, and wire validation
   so `portal` and the portal kind values are accepted end to end.
3. Add the portal form schema column or equivalent policy storage and service
   validation.
4. Implement `GET /v1/portal/{tenant_slug}/submission-config`.
5. Implement `POST /v1/portal/{tenant_slug}/submissions` with validation, rate
   limiting, idempotency, and `user_feedback` insertion.
6. Create moderation subjects for submissions and wire the Console
   inbox/detail views.
7. Add operator actions for moderation and promotion to Customer Request.
8. Build the public portal page and Console preview from the same typed config
   model.
9. Add end-to-end tests for the public route, moderation transitions, portal
   settings preview, and request promotion.

## Verification

- Unit tests for portal config normalization, identity gating, field
  allowlists, and error mapping.
- Unit tests for source vocabulary extension, kind validation, and portal inbox
  ordering.
- Unit tests for rate limiting, idempotency, and spam-safe failure paths.
- Handler tests for public config and submission routes.
- Repository and service tests that prove submissions land in `user_feedback`
  and moderation subjects carry `portal_submission`.
- Console tests for settings, preview, and inbox rendering.
- Audit inventory tests that cover portal settings, moderation, and promotion
  routes with allowlisted actions.
- Generated contract checks: `make proto` plus clean diff.
- Go checks on affected packages, plus the normal repository CI gates for the
  touched code paths.

## References

### Internal

- [Customer Requests](./2026-07-07-customer-requests.md)
- [Public Visibility and Moderation Policy](./2026-07-10-public-visibility-moderation-policy.md)
- [#212](https://github.com/Phixsura/attune/issues/212)
- [#215](https://github.com/Phixsura/attune/issues/215)

### External

- [Canny board settings](https://help.canny.io/en/articles/4968514-board-settings)
- [Canny anonymous boards](https://help.canny.io/en/articles/8932303-anonymous-boards)
- [Featurebase collect and manage feedback](https://help.featurebase.app/articles/6728409-collect-and-manage-feedback)
- [Featurebase moderation](https://help.featurebase.app/articles/6982593-post-and-comment-moderation)
- [Aha! submit-only portal](https://support.aha.io/aha-roadmaps/support-articles/ideas/submit-only-ideas-portal~7444636482802978917)
- [Aha! public ideas portal](https://support.aha.io/aha-roadmaps/support-articles/ideas/public-ideas-portal~7444636331870503394)
- [UserVoice spam filters](https://help.uservoice.com/hc/en-us/articles/360035478833-Spam-Filters-for-Ideas-and-Comments)
- [Productboard portals](https://support.productboard.com/hc/en-us/articles/360056315454-Getting-started-with-portals)
- [Pendo request moderation](https://support.pendo.io/hc/en-us/articles/360032949332-Moderate-requests)
- [Jira Product Discovery published views](https://support.atlassian.com/jira-product-discovery/docs/share-project-views/)
- [Linear Asks](https://linear.app/docs/linear-asks)
- [Productlane multiple portals](https://hello.productlane.com/docs/feedback-portal/multiple-portals)
- [Sleekplan privacy controls](https://help.sleekplan.com/en/articles/5260805-configuring-privacy-settings-and-end-user-access-controls)
