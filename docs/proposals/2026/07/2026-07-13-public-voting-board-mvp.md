<!-- markdownlint-disable MD013 -->

# Public Voting Board

| Field | Value |
|---|---|
| Issue | [#221](https://github.com/Phixsura/attune/issues/221) |
| Status | Implemented |
| Started | 2026-07-13T11:20:23+08:00 |
| Related | [#212](https://github.com/Phixsura/attune/issues/212), [#215](https://github.com/Phixsura/attune/issues/215), [#220](https://github.com/Phixsura/attune/issues/220), [Customer Requests](./2026-07-07-customer-requests.md), [Public Visibility and Moderation Policy](./2026-07-10-public-visibility-moderation-policy.md), [End-User Feedback Submission Portal](./2026-07-11-end-user-feedback-submission-portal.md) |

## Problem

Attune already has the important building blocks for a public feedback
experience: raw feedback ingest, Customer Requests, public request projections,
moderation policy, and a submit-only portal. What is still missing is the
actual public board.

Today, external users can submit feedback through the portal and operators can
curate public request projections in Console, but there is no complete board
experience where visitors can browse requests, vote, and see the resulting
public state in one place. If we build that surface without a clear
product model, we will either leak internal data or create a second, divergent
feedback system.

The strongest products in this category do not expose raw internal records.
They split the problem into a portal family:

- a submit-only intake surface,
- a curated public board,
- a moderation queue,
- a stable identity policy, and
- a public projection layer that can safely show counts, status, and roadmap
  context.

That is the shape we should copy.

## Goals

- Add a public request board with listing and detail views.
- Support vote and unvote actions on public requests.
- Support public comments on request detail pages with moderation-aware
  visibility.
- Keep the submit-only portal separate from the board.
- Reuse `customer_requests` as the canonical planning object.
- Reuse `customer_request_votes` as the canonical vote ledger.
- Reuse `customer_request_comments` as the canonical public comment ledger.
- Keep public fields allowlisted and private fields hidden.
- Preserve tenant isolation, anti-abuse, and auditability.

## Non-goals

- Do not replace the submit-only portal with the board.
- Do not expose internal notes, internal scores, or customer CRM fields on the
  public surface.
- Do not add nested comment threads, attachments, reactions, or direct
  messaging.
- Do not add custom-domain or embed support in this issue.
- Do not add a public changelog editor in this slice.
- Do not require full CRM or support-inbox behavior.

## Current State

Attune already has a meaningful subset of the needed architecture:

- `PortalService` serves the public request read surface and the submit-only
  portal config and submission endpoints.
- `PublicVisibilityService` owns the public visibility policy, moderation
  states, and public request projection rules.
- `public_request_profiles` already stores the public slug, title, summary,
  state, roadmap column, and publication metadata for a request.
- `public_moderation_subjects` already provides a shared moderation queue model
  for `request`, `request_comment`, `roadmap_item`, `changelog_post`, and
  `portal_submission`.
- `customer_request_votes` already stores tenant-scoped vote identities and
  contributes to request prioritization.
- Public request summaries already expose vote and comment count fields, and
  the public comment projection now counts only approved public comments.

The missing pieces are the public write paths, a stable public visitor
identity model, and a board UI that uses the existing public projection instead
of a separate data model.

## Industry Synthesis

Ten top-tier products converge on the same pattern.

| Product | World-class pattern | Decision for Attune |
|---|---|---|
| Aha! Ideas | Full portals and submit-only portals are different modes. | Keep intake separate from the board. |
| Canny | Public, private, and custom boards; identity display can be masked. | Separate access policy from display policy. |
| Featurebase | Moderation, duplicate merge, and private/public boards are first-class. | Make moderation and merge operations explicit. |
| Productboard | Customers see curated portals and published roadmaps, not the raw workspace. | Publish an allowlisted projection only. |
| UserVoice | Public and internal statuses are distinct, and spam moderation is built in. | Keep moderation state separate from delivery state. |
| Pendo Feedback | Visitor hiding, request moderation, and roadmap closure are core. | Allow anonymous-to-peers, visible-to-operators identity. |
| Jira Product Discovery | Published views expose selected fields and remain previewable. | Use field allowlists and preview the board in Console. |
| Linear Customer Requests | Intake and execution are linked, but not conflated. | Keep the board tied to Customer Requests, not raw feedback. |
| Productlane | Multiple portals can serve different audiences while syncing to execution. | Design for multiple audience surfaces from day one. |
| Sleekplan | Privacy knobs, anonymous posting, and a board-plus-changelog family work together. | Keep the portal family cohesive and privacy-aware. |

The common rule is simple: world-class systems never expose the internal object
directly. They publish a curated view, keep a moderation queue behind it, and
preserve a stable link back to the internal planning system.

## Proposal

### 1. Keep the portal family split into intake and board

Attune should have two sibling public surfaces:

- `submission` for submit-only intake, which remains the current portal flow.
- `requests` for the public board, which adds browse and vote.

Both surfaces should share branding and policy, but not behavior. The intake
flow should stay optimized for fast submissions and minimal exposure. The board
should stay optimized for public discovery, discussion, and prioritization.

The public board should keep the existing read routes and add write routes in the
same `PortalService` family. That keeps the public API coherent and avoids
splitting visitor-facing semantics across multiple services.

The public gates should stay unambiguous:

| Flag | Controls | Off behavior |
|---|---|---|
| `portal_access_mode` | all public portal routes | return `404` |
| `requests_enabled` | request list and detail routes | return `404` |
| `comments_enabled` | public comment visibility and count surfacing | hide comment surfaces |
| `submission_write_mode` | submit-only intake writes | hide the form and reject `POST` |
| `vote_write_mode` | vote and unvote actions | hide vote controls and reject `POST` |
| `comment_write_mode` | public comment writes | hide comment composer and reject `POST` |
| `search_indexing_enabled` | indexing hints on public pages | emit `noindex` unless enabled |

### 2. Make identity stable, private, and policy-driven

World-class boards need a stable identity model even when the public display is
anonymous. The board should therefore mint a signed, tenant-scoped visitor
cookie the first time a browser reaches the public surface.

The cookie should be:

- opaque to the browser;
- `HttpOnly`, `Secure`, and `SameSite=Lax`;
- scoped to the portal path;
- refreshed on successful vote writes; and
- treated as an inactivity-bound session, with a long TTL such as 180 days.

The cookie holds an opaque visitor identifier, not the final subject key. The
server derives the stable `subject_key` for anonymous votes from that visitor
identifier plus the tenant context. The system then derives:

- `subject_hash` for tenant-scoped dedupe and audit,
- `subject_display` for public display, and
- private operator-visible identity metadata for moderation and support.

If the portal is in identified mode, the subject key should be bound to the
authenticated or provided identity instead of the anonymous visitor token.
Display rules remain separate from subject rules:

- `PublicIdentityMode_ANONYMOUS` hides the writer from peers.
- `PublicIdentityMode_DISPLAY_NAME` shows a chosen display name.
- `PublicIdentityMode_ORGANIZATION` shows the organization label.

This keeps the public board user-friendly without losing operator traceability.
It also makes vote unvote semantics possible, because the same browser can be
recognized on later visits. Clearing cookies resets the anonymous identity,
which is acceptable for a public board.

### 3. Reuse the canonical vote ledger

The vote model should stay unified. `customer_request_votes` already has the
right shape for public voting because it stores tenant-scoped identities,
subject display values, and account context. Public board votes should write to
that ledger instead of creating a second vote table.

Public votes are identified by a portal-scoped subject key prefix:

- `portal:<visitor-id>` for browser-issued votes.
- `subject_hash` derived from the tenant and subject key for dedupe and audit.
- `subject_display` set to a stable public label such as `Portal visitor`.

Portal votes should always be one-person-one-vote, use weight `1`, and avoid
free-form notes or account context. Console-originated votes can continue to
carry the richer operator context used by the internal Customer Request flow.
Public vote counts should include only portal subject keys, while internal
prioritization can still inspect the full ledger.

### 4. Use moderation subjects as the shared public gate

The shared moderation queue already exists. The public board should use it for
request publication in the same way as other public surfaces:

- `surface = request` for published request profiles,
- `surface = portal_submission` for submit-only intake.

Public writes should create or update moderation subjects, and public reads
should only surface approved content. Rejected, hidden, and spam content should
remain visible only to operators.

This is the same pattern that top products use when they separate public
content from moderation state. It keeps review workflow, audit, and operator
actions centralized.

### 5. Keep the public projection allowlisted

The public board should continue to render from the request profile projection,
not from the internal Customer Request record.

Public request summaries should expose only allowlisted data:

- public slug,
- public title and summary,
- public state,
- roadmap column,
- vote count when allowed,
- comment count when allowed,
- submitter display when allowed,
- timestamps when allowed.

The board detail view should include the viewer vote state, public comment
threads, and moderation-aware author metadata, but it should still avoid any
internal notes, internal scoring signals, or customer CRM data. The comment
count field stays in the response contract and the comment thread renders from
the curated public comment ledger.

Public listing should default to a trust-building order:

- vote count,
- recent activity,
- then stable identifiers as a tiebreaker.

The richer internal prioritization score should remain Console-only. That keeps
the board intuitive for visitors while still letting operators use the full
decision model behind the scenes.

### 6. Preserve the roadmap and lifecycle bridge

The public board should not be a dead-end. It should stay connected to the
existing public state and roadmap fields already carried by the request
profile.

When operators move a request from review into planning or delivery, the public
detail should reflect that lifecycle shift through the public state and roadmap
column. That is how the board closes the loop and becomes trustworthy.

The public board does not need a full changelog editor in this issue. It only
needs to keep the public request state legible and keep the roadmap bridge
intact for later surfaces.

### 7. Build the board UI as a reusable public-safe surface

The board UI should mirror the product patterns above:

- a list view with voting controls and a compact roadmap label,
- a detail view that shows the public-safe request summary and viewer vote
  state,
- a shared component tree for list and detail so the public projection stays
  consistent.

The UI should not try to be a support inbox or a full CRM. It should feel like
a polished feedback board: simple to read and simple to vote on.

### 8. Add the missing public safety rails

Public write endpoints should be hardened from the start:

- tenant slug lookups return `404` when the portal is disabled or not public,
  so the tenant cannot be enumerated.
- anonymous participation requires a signed portal visitor token.
- vote writes use idempotent semantics where possible.
- rate limits apply per tenant and per visitor.
- `search_indexing_enabled` stays `false` by default.
- public write responses avoid leaking private identity details.

These controls make the board safer without turning it into a heavy support
system.

## Alternatives Considered

- **Expose the internal Customer Request object directly.** Rejected because it
  leaks internal notes, internal scores, and operator-only metadata.
- **Create a separate public vote table.** Rejected because it splits the
  demand signal and complicates scoring and moderation.
- **Keep the portal submit-only and stop there.** Rejected because it leaves the
  public board missing the very capabilities that define this issue.
- **Force login for all public interactions.** Rejected because the best
  products support public participation with controlled identity and strong
  moderation.

## Risks / Tradeoffs

- Anonymous participation needs careful token and CSRF handling.
- Votes can overweight noisy popularity if we do not keep internal prioritization
  separate.
- A cached board can drift from moderation state if the cache strategy is too
  aggressive.

## Implementation Plan

1. Extend the proto contract with public vote and unvote RPCs plus viewer vote
   state on public request summaries, plus public comment detail/write support.
2. Add portal visitor-cookie handling for anonymous board interactions.
3. Wire vote, unvote, and comment actions into `PortalService` and keep writes
   on the existing `customer_request_votes` and `customer_request_comments`
   ledgers.
4. Build the public board UI on top of the existing public-safe request
   projection, including the comment thread and composer.
5. Add backend, frontend, and integration tests for tenant isolation, vote
   idempotency, comment moderation, viewer vote state, and abuse controls.

## Verification

- `make proto`
- `go test ./...`
- `go test -tags=integration ./test/integration/postgres/publicvisibility`
- `pnpm tsc -b --noEmit` from `console/`
- `pnpm biome check` from `console/`

## References

- [#221](https://github.com/Phixsura/attune/issues/221)
- [#220](https://github.com/Phixsura/attune/issues/220)
- [#215](https://github.com/Phixsura/attune/issues/215)
- [Aha! Ideas public and submit-only portals](https://support.aha.io/aha-roadmaps/support-articles/ideas/public-ideas-portal~7444636331870503394)
- [Canny public boards](https://help.canny.io/en/articles/3832293-public-boards)
- [Featurebase moderation](https://help.featurebase.app/articles/6982593-post-and-comment-moderation)
- [Productboard portals](https://support.productboard.com/hc/en-us/articles/360056315454-Getting-started-with-portals)
- [UserVoice moderation and statuses](https://help.uservoice.com/hc/en-us/articles/360035481633-Moderate-Ideas-and-Comments)
- [Pendo Feedback moderation](https://support.pendo.io/hc/en-us/articles/360032949332-Moderate-requests)
- [Jira Product Discovery published views](https://support.atlassian.com/jira-product-discovery/docs/share-project-views/)
- [Linear Customer Requests](https://linear.app/customer-requests)
- [Productlane feedback portal](https://productlane.com/feedback)
- [Sleekplan feedback and roadmap](https://sleekplan.com/feedback)
