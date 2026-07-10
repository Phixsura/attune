<!-- markdownlint-disable MD013 -->

# Public Visibility and Moderation Policy

| Field | Value |
|---|---|
| Issue | [#215](https://github.com/Phixsura/attune/issues/215) |
| Status | Implemented |
| Started | 2026-07-10T11:00:39+08:00 |
| Related | [Customer Requests](./2026-07-07-customer-requests.md) |

## Problem

Attune now has an internal Customer Requests model that aggregates raw feedback,
customer evidence, votes, delivery links, scoring signals, notes, audit entries,
and external sync state. Public-facing surfaces need to publish a curated subset
of that product demand without exposing the internal operating record.

The current codebase has no public portal surface yet. Console Customer Request
routes live under `/fb/v1/console/customer-requests`, and the current generated
contract exposes rich internal DTOs for authenticated operators. Those DTOs
include fields such as raw feedback content, source metadata, user identifiers,
customer identity keys and hashes, revenue and CRM attributes, internal notes,
delivery sync errors, audit entries, owner metadata, hidden feedback counts, and
decision-scoring inputs. The `/v1` public API surface currently contains inbound
webhooks, feedback ingest, auth verification, and selected operational APIs; it
does not define `/v1/portal`, `/v1/roadmap`, `/v1/changelog`, or moderation
routes.

If each public surface decides visibility independently, Attune risks leaking
private fields, creating divergent moderation behavior, and making it hard for
operators to reason about which customer-facing surfaces are safe. Issue #215
establishes one tenant-scoped visibility and moderation contract.

## Goals

- Add a tenant-scoped public visibility policy for requests, comments, roadmap
  items, changelog posts, and submitter identity.
- Define canonical moderation states:
  `pending`, `approved`, `rejected`, `hidden`, and `spam`.
- Provide public-safe DTO and query helpers that use an explicit allowlist
  instead of redacting the existing Console DTOs.
- Add one minimal public request read endpoint that exercises the shared policy,
  moderation, and projection path without becoming an interactive request list.
- Add Console policy settings and a moderation queue that can review public
  submissions and comments across surfaces.
- Audit policy updates and moderation decisions.
- Prove tenant isolation, field-boundary enforcement, and moderation-state
  behavior with tests.
- Give future public-facing surfaces one shared visibility and moderation
  contract.

## Non-goals

- Build visitor-initiated feedback intake.
- Build interactive public request listing or voting.
- Build a full public roadmap.
- Build a full public changelog and feed system.
- Replace the internal Customer Request DTOs used by authenticated Console
  operators.
- Add a full customer account or CRM model.
- Require email verification, SSO, or account membership for every public
  visitor.
- Add an external machine-learning moderation provider.
- Add custom-domain routing, customer SSO, or organization-scoped portal access.

## Current State

The repository already has most of the authenticated operator workflow that a
public projection would draw from:

- `proto/attune/v1/customer_request.proto` defines internal Customer Request
  list and detail contracts under `/fb/v1/console/customer-requests`.
- `internal/handlers/console/customerrequest/handler.go` maps full internal
  request detail, feedback evidence, issue links, customers, votes, account
  profiles, and notes into Console DTOs.
- `internal/repo/customerrequest/customerrequest.go` has an internal
  `visibility` filter for active, archived, and merged requests. That value is
  operational lifecycle state, not customer-facing public visibility.
- Migrations `101_customer_requests.sql` through
  `104_customer_request_scoring_settings.sql` add request evidence, explicit
  customer links, vote links, delivery links, account intelligence, internal
  notes, and scoring settings.
- `internal/service/auditlog/actions.go` keeps an allowlist of audit actions,
  and `internal/handlers/console/router_audit_inventory_test.go` checks mutating
  Console routes against audit decisions.
- `console/src/lib/permissions.ts` has customer-request permissions, but no
  public-policy or moderation-review permissions.

The public policy work should therefore add a new publication and moderation
layer. It should not overload internal Customer Request lifecycle filters, and
it should not publish Console DTOs.

## Industry Scan

Ten leading feedback and product-discovery systems converge on the same design
shape: public pages are curated projections, not raw internal records.

| Product | Observed pattern | Implication for Attune |
|---|---|---|
| Canny | Boards can be public, private, or custom-access; public boards can be indexed; users are identified before posting, voting, or commenting; anonymous boards can mask identity. Canny also connects boards to public roadmaps and changelogs. | Separate access, indexing, write permission, and identity display policy. |
| Featurebase | Post and comment moderation can hold content before it goes live; roles distinguish moderation settings, post moderation, comment moderation, private content visibility, changelog management, and roadmap management. | Treat moderation as a first-class cross-surface permission and queue. |
| Productboard | Customers see public-facing portals, updates, and shared roadmaps, not the internal workspace. Portal users can see aggregate signals such as vote counts without seeing other customers' feedback details. | Publish an allowlisted projection and keep evidence private by default. |
| Aha! Ideas | Portals support full and submit-only modes, public and private access, default idea visibility, private comments, organization-specific visibility, voting, subscriptions, and custom public pages. | Policy needs surface, audience, default moderation, comment, and identity settings. |
| UserVoice | New ideas can enter Needs Review; admins approve, reply, edit, delete, merge, mark spam, and separate public status from internal status. Users can report inappropriate content. | Model review state separately from delivery state and keep spam/reporting paths extensible. |
| Pendo Feedback | Request moderation hides unreviewed requests from customers; requests can be private; visitor timestamps and submission ability can be disabled. | Policy should be able to disable writes and hide metadata, not only hide whole records. |
| Jira Product Discovery | Published views are disabled by default, require admin configuration, expose selected fields, support preview and unpublish, and omit unsupported internal fields such as votes, reactions, and insights. | Default public publishing off, use field allowlists, and include preview checks. |
| Linear Customer Requests | Customer requests and attributes are internal planning inputs linked to projects and issues. Release communication is a separate surface. | Keep the internal demand model private and project only selected public fields outward. |
| Productlane | Feedback portals, public roadmaps, and changelogs sync with Linear while customer portals can show only the requesting user's or team's requests behind auth. | The same policy layer should support anonymous public and authenticated customer-scoped projections. |
| Sleekplan | Feedback boards, public roadmaps, changelogs, widgets, and CSAT share one portal family; public usernames can be anonymized while admins keep backend identity. | Identity display must be configurable without losing operator traceability. |

## Proposal

Introduce a typed public visibility policy and a generic moderation subject
model. Public surfaces consult the policy and moderation state before returning
or accepting customer-facing content. Console operators manage the policy and
queue through authenticated routes. Public DTOs are built from allowlisted
projection helpers instead of existing internal DTOs.

### Contract

Add a generated contract for the shared policy and moderation model. The exact
file can be `proto/attune/v1/public_visibility.proto` or a broader
`portal.proto`, but the concepts should be stable across all public surfaces:

- `PublicSurface`
  - `PUBLIC_SURFACE_REQUEST`
  - `PUBLIC_SURFACE_REQUEST_COMMENT`
  - `PUBLIC_SURFACE_ROADMAP_ITEM`
  - `PUBLIC_SURFACE_CHANGELOG_POST`
  - `PUBLIC_SURFACE_PORTAL_SUBMISSION`
- `ModerationState`
  - `MODERATION_STATE_PENDING`
  - `MODERATION_STATE_APPROVED`
  - `MODERATION_STATE_REJECTED`
  - `MODERATION_STATE_HIDDEN`
  - `MODERATION_STATE_SPAM`
- `PublicIdentityMode`
  - `PUBLIC_IDENTITY_MODE_ANONYMOUS`
  - `PUBLIC_IDENTITY_MODE_DISPLAY_NAME`
  - `PUBLIC_IDENTITY_MODE_ORGANIZATION`
- `PublicAccessMode`
  - `PUBLIC_ACCESS_MODE_DISABLED`
  - `PUBLIC_ACCESS_MODE_PUBLIC`
  - `PUBLIC_ACCESS_MODE_AUTHENTICATED`
  - `PUBLIC_ACCESS_MODE_INVITE_ONLY`
- `PublicWriteMode`
  - `PUBLIC_WRITE_MODE_DISABLED`
  - `PUBLIC_WRITE_MODE_ANONYMOUS`
  - `PUBLIC_WRITE_MODE_IDENTIFIED`
- `PublicVisibilityPolicy`
- `ModerationSubject`
- `PublicRequestProfile`
- `PublicCustomerRequestSummary`
- `PublicCustomerRequestDetail`

The generated OpenAPI should make the public/private boundary visible in the
schema. Public DTOs must not reuse fields from the Console request detail type
unless the field has been deliberately included in the public contract.

### Data Model

Add a migration with strongly typed policy and moderation tables.

`public_visibility_policies`

- `tenant_id TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE`
- `portal_access_mode TEXT NOT NULL DEFAULT 'disabled'`
- `search_indexing_enabled BOOLEAN NOT NULL DEFAULT false`
- `requests_enabled BOOLEAN NOT NULL DEFAULT false`
- `comments_enabled BOOLEAN NOT NULL DEFAULT false`
- `roadmap_enabled BOOLEAN NOT NULL DEFAULT false`
- `changelog_enabled BOOLEAN NOT NULL DEFAULT false`
- `submission_write_mode TEXT NOT NULL DEFAULT 'disabled'`
- `comment_write_mode TEXT NOT NULL DEFAULT 'disabled'`
- `vote_write_mode TEXT NOT NULL DEFAULT 'disabled'`
- `default_request_state TEXT NOT NULL DEFAULT 'pending'`
- `default_comment_state TEXT NOT NULL DEFAULT 'pending'`
- `submitter_identity_mode TEXT NOT NULL DEFAULT 'anonymous'`
- `show_vote_count BOOLEAN NOT NULL DEFAULT true`
- `show_comment_count BOOLEAN NOT NULL DEFAULT true`
- `show_submitter_display BOOLEAN NOT NULL DEFAULT false`
- `hide_public_timestamps BOOLEAN NOT NULL DEFAULT false`
- `updated_by TEXT NOT NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`

`public_moderation_subjects`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE`
- `surface TEXT NOT NULL`
- `subject_id TEXT NOT NULL`
- `state TEXT NOT NULL DEFAULT 'pending'`
- `reason_code TEXT NOT NULL DEFAULT ''`
- `reason_note TEXT NOT NULL DEFAULT ''`
- `submitted_by_display TEXT NOT NULL DEFAULT ''`
- `submitted_by_fingerprint TEXT NOT NULL DEFAULT ''`
- `reviewed_by TEXT NOT NULL DEFAULT ''`
- `reviewed_at TIMESTAMPTZ`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `UNIQUE (tenant_id, surface, subject_id)`

Add indexes for moderation queue access by
`(tenant_id, state, surface, created_at DESC)` and direct subject lookups by
`(tenant_id, surface, subject_id)`.

Add check constraints for valid surfaces, moderation states, identity modes, and
bounded text lengths. `portal_access_mode` should allow `disabled`, `public`,
`authenticated`, and `invite_only`; #215 implements disabled and public
behavior and defines the other values as invalid for runtime policy updates
until customer-authenticated portal access exists.
`default_request_state` and `default_comment_state` should allow only `pending`
and `approved`, because rejected, hidden, and spam are moderation outcomes rather
than safe defaults for newly published subjects.
`submission_write_mode`, `comment_write_mode`, and `vote_write_mode` should
allow `disabled`, `anonymous`, and `identified`. `search_indexing_enabled`
defaults to false so newly enabled portals are unindexed until an operator opts
in.

`reason_code` is optional for approval and restore actions, but required for
reject, hide, and spam actions. It must be a stable lower-case code matching
`^[a-z0-9][a-z0-9_.-]{0,79}$`, so audit trails can be filtered without storing
free-form content in the primary reason field. `reason_note` should be small
enough for review context and must not store raw submitted content.
`submitted_by_fingerprint` is an operator-only,
tenant-scoped keyed digest over a normalized external identity. It must not be
reversible or comparable across tenants. Raw identity belongs in the private
surface-owned submission table, not in public moderation subjects, and public
DTOs must never read the fingerprint.

Add a request-specific publication table for the minimal public read endpoint
and future request-facing public surfaces:

`public_request_profiles`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `tenant_id TEXT NOT NULL`
- `request_id UUID NOT NULL`
- `public_slug TEXT NOT NULL`
- `public_title TEXT NOT NULL`
- `public_summary TEXT NOT NULL DEFAULT ''`
- `public_state TEXT NOT NULL DEFAULT ''`
- `roadmap_column TEXT NOT NULL DEFAULT ''`
- `included_in_portal BOOLEAN NOT NULL DEFAULT false`
- `included_in_roadmap BOOLEAN NOT NULL DEFAULT false`
- `published_at TIMESTAMPTZ`
- `updated_by TEXT NOT NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`

This keeps public copy and placement independent from the internal planning
record while preserving referential integrity. The profile should have unique
constraints on `(tenant_id, request_id)` and `(tenant_id, public_slug)`, and a
foreign key to `(tenant_id, request_id)` on `customer_requests`. The moderation
subject for a request profile uses `surface = 'request'` and
`subject_id = public_request_profiles.id`, so public slugs can change without
breaking moderation history.

### Moderation State Machine

Moderation state is separate from internal delivery status. Public routes only
return subjects in `approved` state and only when the relevant policy surface is
enabled.

| State | Public visible | Semantics | Allowed transitions |
|---|---:|---|---|
| `pending` | No | Awaiting operator review. | `approve`, `reject`, `mark_spam` |
| `approved` | Yes | Safe for the configured public surface. | `hide`, `mark_spam` |
| `rejected` | No | Reviewed and intentionally not published. | `restore`, `mark_spam` |
| `hidden` | No | Previously approved content removed from public view. | `restore`, `mark_spam` |
| `spam` | No | Abuse or junk content removed from ordinary review. | `restore` |

Action behavior:

- `approve` moves `pending` or `rejected` to `approved`.
- `reject` moves only `pending` to `rejected`.
- `hide` moves only `approved` to `hidden`.
- `mark_spam` moves any non-spam state to `spam`.
- `restore` moves `hidden` back to `approved` and moves `rejected` or `spam`
  back to `pending`.

Illegal transitions and missing required reason codes should return validation
errors and should not emit audit events.

### Services and Repositories

Add small, layered packages that follow the repository's existing architecture:

- `internal/repo/publicvisibility`
- `internal/service/publicvisibility`
- `internal/handlers/console/publicvisibility`

The service owns policy defaults, moderation transitions, audit emission, and
tenant isolation. The repository owns typed persistence and queue queries. The
Console handler owns RBAC, request validation, and generated DTO mapping.

Public query helpers must query approved public subjects and public profiles
directly. They should not call the existing internal Customer Request detail
loader and then remove fields. This prevents accidental leakage when internal
DTOs gain new fields.

### Public-Safe Projection Rules

Public DTO helpers should be allowlist-only. A public request summary or detail
may include:

- Public id or slug.
- Public title and public summary.
- Public status or roadmap column.
- Aggregate vote and comment counts when policy allows them.
- Public submitter display when policy allows it.
- Public timestamps when policy allows them.
- Public changelog or roadmap links when the linked surface is enabled and
  approved.

Public DTOs must not include:

- Raw feedback content or private feedback notes.
- Source payload metadata, source names that reveal private systems, or external
  raw identifiers.
- User ids, identity keys, identity hashes, or account identifiers.
- Customer revenue, tier, CRM attributes, or account profile details.
- Internal notes.
- Audit entries.
- Owner member id, owner email, reviewer ids, or operator identities.
- Hidden feedback counts.
- Delivery sync state, sync errors, external assignee metadata, or private
  issue fields.
- Decision score explanations that mention private evidence or revenue inputs.

Tests should construct a request fixture containing every sensitive field above,
marshal the public DTO to JSON, and assert the forbidden field names and values
are absent.

### Console Routes and Permissions

Add generated Console routes under `/fb/v1/console/public-visibility`:

- `GET /policy`
- `PUT /policy`
- `GET /moderation`
- `GET /requests/{request_id}/profile`
- `PUT /requests/{request_id}/profile`
- `POST /moderation/{id}:approve`
- `POST /moderation/{id}:reject`
- `POST /moderation/{id}:hide`
- `POST /moderation/{id}:mark-spam`
- `POST /moderation/{id}:restore`

Add permissions in backend and Console:

- `public_policy:view`
- `public_policy:edit`
- `moderation:view`
- `moderation:triage`
- `moderation:enforce`

Suggested grants:

- `admin`: all five permissions.
- `delegated_admin`: all five permissions.
- `member`: `moderation:view` and `moderation:triage`.
- `viewer`: none by default.

Policy edits should require delegated-admin level access. `approve` and
`reject` should require `moderation:triage`. `hide`, `mark-spam`, and `restore`
should require `moderation:enforce` because they can remove already published
content or change abuse classification. Every moderation decision must be
audited. Console moderation actions should collect a stable reason code from a
bounded option set and may collect a short operator note that excludes raw
customer content. When an operator moderates a subject that is currently loaded
in the request-profile panel, the panel should update its displayed moderation
state from the mutation response so the queue and profile detail do not show
conflicting states after approve, reject, hide, spam, or restore.

### Public Routes

Issue #215 adds a narrow public read surface so the visibility contract is
executable and testable across both request-board and roadmap views:

- `GET /v1/portal/{tenant_slug}/requests`
- `GET /v1/portal/{tenant_slug}/requests/{public_slug}`
- `GET /v1/portal/{tenant_slug}/roadmap`

The request detail endpoint returns `PublicCustomerRequestDetail` only when:

- The tenant slug resolves to a tenant.
- The public API version middleware accepts the request.
- `portal_access_mode` is `public` and the request surface is enabled.
- The request profile exists for the tenant and slug.
- The matching moderation subject is `approved`.

It returns `404` for disabled, missing, unapproved, rejected, hidden, spam, and
cross-tenant records so callers cannot infer private moderation state. It must
apply anonymous rate limiting and should set noindex metadata when
`search_indexing_enabled` is false. Public request endpoint responses use
`Cache-Control: no-store`, including not-found responses for moderation-blocked
content, so hide and restore actions are not masked by browser or intermediary
caches.

The request list and roadmap list endpoints return the same
`PublicCustomerRequestSummary` projection. Request lists require the request
surface to be enabled and `included_in_portal` to be true. Roadmap lists require
the roadmap surface to be enabled and `included_in_roadmap` to be true. Both
lists require approved moderation subjects and live Customer Requests, hide
pending, rejected, hidden, spam, excluded, archived, merged, and cross-tenant
records, and use the same count, submitter identity, timestamp, no-store, and
noindex controls as request detail responses.

### Audit

Register new audit actions in the audit service allowlist, database constraint,
and Console audit taxonomy:

- `public_policy.update`
- `public_request_profile.upsert`
- `moderation.approve`
- `moderation.reject`
- `moderation.hide`
- `moderation.mark_spam`
- `moderation.restore`

Audit target types should be `public_visibility_policy`,
`public_request_profile`, and `public_moderation_subject`. Audit metadata should
include tenant id, surface, subject id, public profile identifiers, previous
state, next state, and bounded reason code. It should not include raw submission
content, raw comment bodies, customer identity hashes, or private notes.

## Alternatives Considered

### Add public columns directly to customer_requests

This is simple for public request listings, but it mixes public copy,
moderation, roadmap placement, and internal planning state in one table. It also
does not cover comments, portal submissions, changelog posts, or roadmap items.
A separate policy plus generic moderation subject keeps the model reusable.

### Store policy in system_settings JSON

`system_settings` works well for small preference blobs and saved views, but a
public visibility policy should be typed, constrained, easy to query, and easy
to migrate. A typed policy table gives better validation and clearer schema
ownership.

### Redact Console DTOs for public responses

Redaction starts convenient and becomes fragile as internal DTOs grow. The
Customer Request detail DTO already includes enough private evidence that a
missed field would be a security defect. Public endpoints should use separate
allowlisted DTOs and query paths.

### Implement the public board before the policy

Building public listing, voting, and comments first would force those handlers
to invent visibility and moderation rules locally. The dedicated policy work
makes the board, portal, roadmap, and changelog features safer and more
consistent.

### Create one moderation table per surface

Per-surface tables can be stricter, but the initial state machine and operator
queue are the same across requests, comments, roadmap items, changelog posts,
and submissions. A generic moderation subject avoids duplicated queues and
permission models while still preserving surface-specific subject ids.

## Risks / Tradeoffs

- Scope creep: #215 can grow into the whole portal family. Keep this issue to
  policy, moderation, public projections, audit, and Console review.
- Manual moderation volume: a queue is necessary but not sufficient for large
  tenants. Default-pending policy, rate limits, and spam state provide the
  baseline without committing to an external classifier.
- Identity privacy: operators need traceability, but public visitors should see
  only the configured identity mode. Store backend identity separately from
  public display.
- Projection drift: public copy and request state can become stale if it is
  copied too aggressively. Prefer deterministic query projections, and update
  public profile rows in the same transaction when copied fields are needed.
- Audit leakage: moderation decisions are sensitive. Audit only bounded metadata
  and never raw submitted content.
- Backward compatibility: public endpoints should use the date-pinned API
  version contract introduced for `/v1`, while Console routes remain under the
  authenticated Console API.

## Implementation Plan

1. Add the proposal and generated proto contract for public policy, moderation
   states, access modes, write modes, identity modes, Console policy settings,
   moderation queue actions, request profiles, and public-safe request DTOs.
2. Update `CHANGELOG.md` under `[Unreleased]` when the implementation PR adds
   code.
3. Add migrations for `public_visibility_policies` and
   `public_moderation_subjects`, including constraints and indexes. Add
   `public_request_profiles` in the same change because the minimal public read
   endpoint depends on it.
4. Implement `repo/publicvisibility` with policy upsert/read, queue list,
   subject lookup, request-profile upsert/read, and state-transition
   persistence.
5. Implement `service/publicvisibility` with policy defaults, RBAC-facing
   commands, moderation transition validation, audit writes, tenant-scoped
   submitter fingerprints, and public-safe projection helpers.
6. Add Console handlers, generated routes, router registration, permission
   checks, audit inventory entries, and request validation.
7. Add public read endpoints for request lists, request detail, and roadmap
   lists with API version enforcement, tenant slug resolution, rate limiting,
   and `404` behavior for disabled or unapproved records.
8. Add Console settings and moderation queue views with permission-gated actions.
9. Add public projection tests and route tests that prove forbidden internal
   fields never appear in JSON, illegal moderation transitions fail without
   audit events, and disabled/unapproved records return `404`.
10. Update audit action allowlists, frontend audit action labels, generated
   OpenAPI output, and proposal status as implementation lands.

## Verification

- `make proto`
- `go test ./internal/repo/publicvisibility ./internal/service/publicvisibility`
- `go test ./internal/handlers/console/publicvisibility`
- `go test ./internal/handlers/console -run 'Audit|Router|PublicVisibility|Moderation'`
- `go test ./internal/service/customerrequest ./internal/handlers/console/customerrequest`
- `go test -tags=integration ./test/integration/postgres/publicvisibility`
- `go vet ./...`
- `go build ./...`
- `go test -race ./...`
- `pnpm tsc -b --noEmit`
- `pnpm biome check`
- `pnpm vitest run --coverage`
- `pnpm test:e2e:a11y`
- Browser behavior E2E: open public visibility in the real Console router, save
  policy, load and save a request profile, approve, reject, hide, mark spam, and
  restore moderation subjects, and verify member users can triage without seeing
  policy/profile editing or enforcement controls. The run covers desktop and
  mobile Chromium, asserts API request payloads, rejects unmocked Console API
  calls, checks console diagnostics, and verifies the page has no horizontal
  overflow.
- `scripts/lint-artifacts.sh --strict`
- `make ci-check`

## References

- [Issue #215: public visibility and moderation policy](https://github.com/Phixsura/attune/issues/215)
- [Customer Requests proposal](./2026-07-07-customer-requests.md)
- [Public API version contract proposal](../06/2026-06-28-public-api-version-contract.md)
- [Canny board access settings](https://help.canny.io/en/articles/3831745-choosing-the-right-board-access-public-private-or-custom)
- [Canny public boards](https://help.canny.io/en/articles/3832293-public-boards)
- [Canny anonymous boards](https://help.canny.io/en/articles/8932303-anonymous-boards)
- [Featurebase feedback moderation](https://help.featurebase.app/articles/6728409-collect-and-manage-feedback)
- [Featurebase post and comment moderation](https://help.featurebase.app/articles/6982593-post-and-comment-moderation)
- [Featurebase admin roles](https://help.featurebase.app/articles/3863653-admin-roles)
- [Productboard customer visibility](https://support.productboard.com/hc/en-us/articles/6507036754195-What-visibility-of-Productboard-do-our-customers-have)
- [Productboard portal overview](https://support.productboard.com/hc/en-us/articles/360056315454-Getting-started-with-portals)
- [Productboard external roadmaps](https://support.productboard.com/hc/en-us/articles/4417696648339-Share-your-roadmaps-with-external-stakeholders-and-customers)
- [Aha! Ideas portal introduction](https://support.aha.io/aha-ideas/support-articles/ideas-portals/ideas-portals-introduction)
- [Aha! public ideas portal](https://support.aha.io/aha-ideas/support-articles/ideas-portals/public-ideas-portal~7444660621182363968)
- [Aha! idea visibility](https://support.aha.io/aha-ideas/support-articles/ideas-management/idea-visibility~7444662676457322380)
- [UserVoice moderation](https://help.uservoice.com/hc/en-us/articles/360035481633-Moderate-Ideas-and-Comments)
- [UserVoice spam filters](https://help.uservoice.com/hc/en-us/articles/360035478833-Spam-Filters-for-Ideas-and-Comments)
- [UserVoice public and internal status](https://help.uservoice.com/hc/en-us/articles/360034982174-Customize-Public-and-Internal-Status)
- [Pendo Feedback moderation](https://support.pendo.io/hc/en-us/articles/360032949332-Moderate-requests)
- [Pendo Feedback settings](https://support.pendo.io/hc/en-us/articles/360032380631-Settings-for-Pendo-Feedback)
- [Jira Product Discovery published views](https://support.atlassian.com/jira-product-discovery/docs/share-project-views/)
- [Jira Product Discovery roadmaps](https://support.atlassian.com/jira-product-discovery/docs/create-and-manage-roadmaps/)
- [Linear Customer Requests](https://linear.app/docs/customer-requests)
- [Linear releases](https://linear.app/docs/releases)
- [Productlane feedback portal](https://productlane.com/feedback)
- [Productlane support portal](https://productlane.com/support-portal)
- [Productlane Linear integration](https://productlane.com/docs/integrations/linear)
- [Sleekplan docs](https://sleekplan.com/docs)
- [Sleekplan public username anonymization](https://help.sleekplan.com/en/articles/12885546-how-to-anonymize-usernames-for-public-feedback)
- [Sleekplan roadmap tool](https://sleekplan.com/use-cases/roadmap-tool)
