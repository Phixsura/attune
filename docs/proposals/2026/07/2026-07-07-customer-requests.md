<!-- markdownlint-disable MD013 -->

# Customer Requests

| Field | Value |
|---|---|
| Issue | [#212](https://github.com/Phixsura/attune/issues/212) |
| Status | Implemented |
| Started | 2026-07-07T10:38:32+08:00 |
| Related | [#202](https://github.com/Phixsura/attune/issues/202), [feedback intelligence control tower](./2026-07-03-feedback-intelligence-control-tower.md), [semantic search operator workflow](./2026-07-02-semantic-search-operator-workflow.md), [feedback manual tags](../06/2026-06-14-feedback-manual-tags.md), [feedback workflow status](../06/2026-06-14-feedback-workflow-status.md) |

## Problem

Attune stores and enriches raw feedback well, and the Console already supports
triage, manual tags, workflow states, semantic search, clusters, outbound
delivery, and audit timelines. Product operators still lack a first-class object
that represents the product request behind one or more feedback rows.

Without a Customer Request layer, teams must infer demand by reading individual
feedback rows, tags, and clusters. That makes it hard to answer the questions
that product, customer success, and engineering teams ask during planning:

1. Which customers asked for this capability?
2. How many distinct feedback events support it?
3. Which delivery issues or projects are meant to satisfy it?
4. Did we merge a duplicate request without losing backlinks?
5. Who linked, unlinked, merged, or reprioritized the request?

Top product feedback systems converge on the same operating model. Linear
Customer Requests links customer feedback and attributes to issues and projects.
Productboard treats feedback as evidence that is linked to feature ideas.
Jira Product Discovery separates ideas, insights, prioritization fields, and
delivery work. Canny, Aha!, UserVoice, Pendo Listen, Productlane, Savio,
Dovetail, FeatureOS, LaunchNotes, GitHub Projects, and GitLab all reinforce the
same principle: raw feedback is evidence, while the product request is the
curated decision object.

## Goals

- Add a tenant-scoped `customer_requests` object with stable identifiers, title,
  description, status, owner, priority, timestamps, and merge state.
- Link one or more `user_feedback` rows to a Customer Request without moving or
  rewriting the raw feedback.
- Show request-level evidence counts: supporting feedback count, distinct
  customer count, distinct account count, vote count, duplicate count, and
  linked delivery issue reference count.
- Support create, update, link feedback, unlink feedback, link explicit
  customer/account references, add and remove votes, merge, link delivery issue,
  and unlink delivery issue operations through the Console API.
- Preserve backlinks when requests are merged and make merged requests readable
  as aliases of the surviving request.
- Reject cross-tenant links, unlinks, issue references, and merges.
- Record audit events for every sensitive request mutation.
- Add a Console list/detail workflow, owner filtering and assignment, a
  feedback-list promotion path, feedback-scoped deep links, and a
  feedback-detail panel for viewing, linking, and unlinking related requests.
- Keep the implementation compatible with the proto/OpenAPI generated contract
  model.

## Non-goals

- Add a public voting portal.
- Add public roadmap publishing.
- Add customer-facing notification email or changelog automation.
- Add a full customer/account CRM model.
- Add automatic bidirectional sync with Jira, Linear, GitHub, or Salesforce.
- Replace feedback tags, workflow states, semantic clusters, or search.
- Let model-generated suggestions mutate requests without an explicit operator
  action.

## Proposal

Introduce Customer Requests as a new product-operation object between raw
feedback and delivery work. The implementation is internal to the authenticated
Console and backed by the same tenant isolation, generated API contract, audit,
and repository patterns already used by feedback operations.

### Data model

Add a migration with the request table, feedback evidence links, explicit
customer/account links, vote links, delivery issue links, and a small
per-tenant counter table. All UUID primary keys use
`DEFAULT gen_random_uuid()` so the database is safe for direct inserts and
tests.

`customer_request_counters`

- `tenant_id TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE`
- `next_number BIGINT NOT NULL DEFAULT 1`

The service allocates display numbers inside the same transaction as request
creation by locking the tenant counter row, reading `next_number`, incrementing
it, and writing the request with `display_number = previous_next_number` and
`display_id = 'CR-' || display_number`. On insert conflict, the service retries
allocation in a fresh transaction. This keeps `display_id` stable, short, and
tenant-local without making it the primary key.

`customer_requests`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT`
- `display_number BIGINT NOT NULL`
- `display_id TEXT NOT NULL`
- `title TEXT NOT NULL`
- `description TEXT NOT NULL DEFAULT ''`
- `status TEXT NOT NULL`
- `priority TEXT NOT NULL`
- `owner_member_id UUID REFERENCES tenant_members(id) ON DELETE SET NULL`
- `created_by TEXT NOT NULL`
- `updated_by TEXT NOT NULL`
- `merged_into_request_id UUID REFERENCES customer_requests(id)`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `archived_at TIMESTAMPTZ`
- `UNIQUE (tenant_id, display_id)`
- `UNIQUE (tenant_id, display_number)`
- `UNIQUE (tenant_id, id)`
- `CHECK (length(title) BETWEEN 1 AND 200)`
- `CHECK (length(description) <= 10000)`
- `CHECK (status IN ('open', 'planned', 'in_progress', 'shipped', 'cancelled'))`
- `CHECK (priority IN ('none', 'low', 'medium', 'high', 'urgent'))`
- `CHECK (merged_into_request_id IS NULL OR merged_into_request_id <> id)`

The status set stays small and explicit:

- `open`
- `planned`
- `in_progress`
- `shipped`
- `cancelled`

The proto contract exposes these values as a `CustomerRequestStatus` enum.
Create requests treat an unspecified status as `open`; unsupported values return
`BAD_REQUEST`.

The priority set mirrors product-planning vocabulary rather than engineering
incident severity:

- `none`
- `low`
- `medium`
- `high`
- `urgent`

The proto contract exposes these values as a `CustomerRequestPriority` enum.
Create requests treat an unspecified priority as `none`; unsupported values
return `BAD_REQUEST`.

`owner_member_id` is the nullable tenant-member UUID for the operator who owns
the request. Create and update calls must verify that the referenced member
belongs to the authenticated tenant. List and detail responses include an owner
display object with member ID, role, and best-effort email/name fields so the
Console can filter and render owners without trusting free-form text.

`customer_request_feedback_links`

- `tenant_id TEXT NOT NULL`
- `request_id UUID NOT NULL`
- `feedback_id BIGINT NOT NULL`
- `importance TEXT NOT NULL DEFAULT 'normal'`
- `note TEXT NOT NULL DEFAULT ''`
- `created_by TEXT NOT NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `PRIMARY KEY (request_id, feedback_id)`
- `UNIQUE (tenant_id, request_id, feedback_id)`
- `CHECK (importance IN ('normal', 'important', 'critical'))`
- `CHECK (length(note) <= 5000)`
- `FOREIGN KEY (tenant_id, request_id) REFERENCES customer_requests(tenant_id, id) ON DELETE CASCADE`
- `FOREIGN KEY (tenant_id, feedback_id) REFERENCES user_feedback(tenant_id, id) ON DELETE CASCADE`

The migration adds a supporting unique index on `user_feedback(tenant_id, id)`
before creating the composite feedback foreign key. The link write path also
verifies that the feedback row is not soft-deleted.

`customer_request_customer_links`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `tenant_id TEXT NOT NULL`
- `request_id UUID NOT NULL`
- `subject_key TEXT NOT NULL DEFAULT ''`
- `subject_hash TEXT NOT NULL DEFAULT ''`
- `subject_display TEXT NOT NULL DEFAULT ''`
- `account_key TEXT NOT NULL DEFAULT ''`
- `account_display TEXT NOT NULL DEFAULT ''`
- `note TEXT NOT NULL DEFAULT ''`
- `created_by TEXT NOT NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `UNIQUE (tenant_id, request_id, subject_hash, subject_key, account_key)`
- `CHECK (subject_key <> '' OR subject_hash <> '' OR account_key <> '')`
- `CHECK (length(subject_key) <= 512)`
- `CHECK (length(subject_hash) <= 128)`
- `CHECK (length(subject_display) <= 500)`
- `CHECK (length(account_key) <= 512)`
- `CHECK (length(account_display) <= 500)`
- `CHECK (length(note) <= 5000)`
- `FOREIGN KEY (tenant_id, request_id) REFERENCES customer_requests(tenant_id, id) ON DELETE CASCADE`

Explicit customer links cover proxy capture cases where an operator knows the
customer or account that needs the request even when there is no raw feedback
row to attach. The unique identity constraint makes repeated link attempts
idempotent at the request level while allowing the display names and note to be
refreshed.

`customer_request_votes`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `tenant_id TEXT NOT NULL`
- `request_id UUID NOT NULL`
- `subject_key TEXT NOT NULL DEFAULT ''`
- `subject_hash TEXT NOT NULL DEFAULT ''`
- `subject_display TEXT NOT NULL DEFAULT ''`
- `account_key TEXT NOT NULL DEFAULT ''`
- `account_display TEXT NOT NULL DEFAULT ''`
- `weight INT NOT NULL DEFAULT 1`
- `note TEXT NOT NULL DEFAULT ''`
- `created_by TEXT NOT NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `UNIQUE (tenant_id, request_id, subject_hash, subject_key, account_key)`
- `CHECK (subject_key <> '' OR subject_hash <> '' OR account_key <> '')`
- `CHECK (weight BETWEEN 1 AND 100)`
- `CHECK (length(subject_key) <= 512)`
- `CHECK (length(subject_hash) <= 128)`
- `CHECK (length(subject_display) <= 500)`
- `CHECK (length(account_key) <= 512)`
- `CHECK (length(account_display) <= 500)`
- `CHECK (length(note) <= 5000)`
- `FOREIGN KEY (tenant_id, request_id) REFERENCES customer_requests(tenant_id, id) ON DELETE CASCADE`

Votes are internal demand signals, not a public voting portal. They let
operators record customer/account support with a bounded weight while keeping
the canonical request lifecycle private to the Console.

`customer_request_issue_links`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT`
- `request_id UUID NOT NULL`
- `provider TEXT NOT NULL`
- `external_key TEXT NOT NULL`
- `external_url TEXT NOT NULL`
- `title TEXT NOT NULL DEFAULT ''`
- `status TEXT NOT NULL DEFAULT ''`
- `created_by TEXT NOT NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `last_synced_at TIMESTAMPTZ`
- `UNIQUE (tenant_id, request_id, provider, external_key)`
- `CHECK (provider IN ('github', 'jira', 'linear', 'other'))`
- `CHECK (length(external_key) BETWEEN 1 AND 512)`
- `CHECK (length(external_url) BETWEEN 1 AND 2048)`
- `FOREIGN KEY (tenant_id, request_id) REFERENCES customer_requests(tenant_id, id) ON DELETE CASCADE`

The implementation stores explicit delivery references, not sync state.
`provider` can be `github`, `jira`, `linear`, or `other`. `external_key` is
normalized from the URL when the provider is recognized, and falls back to the
full URL for `other`. The same external issue may be linked to multiple
requests; the uniqueness constraint only prevents duplicate links on the same
request.

### Derived counts

List and detail reads compute counts from the link tables. All feedback
evidence joins include `user_feedback.deleted_at IS NULL` unless the
request explicitly asks for hidden evidence metadata.

- `supporting_feedback_count`: `COUNT(*)` from feedback links joined to
  non-deleted feedback.
- `customer_count`: distinct supporter identity across visible linked feedback,
  explicit customer links, and votes.
- `account_count`: distinct non-empty `account_key` across explicit customer
  links and votes.
- `vote_count`: `COUNT(*)` from vote links.
- `linked_issue_count`: `COUNT(*)` from issue links.
- `duplicate_request_count`: requests in the same tenant whose
  `merged_into_request_id` points at this request.
- `first_feedback_at`: earliest non-deleted linked feedback timestamp.
- `latest_feedback_at`: latest non-deleted linked feedback timestamp.
- `hidden_feedback_count`: linked feedback rows excluded from visible evidence
  because they are soft-deleted.

This reuses existing feedback subject identity and augments it with explicit
operator-captured customer/account references. A customer/account model can
replace the derived count source while keeping the request API stable.

Promotion and explicit link operations must reject soft-deleted feedback. If a
feedback row is soft-deleted after it has been linked, list and detail responses
keep the request link but exclude that row from visible evidence and customer
counts. Operators can still unlink the hidden feedback from the request.

### Merge semantics

Merging request `source` into request `target` is a single transaction:

1. Lock both request rows by tenant and ID.
2. Reject self-merge, missing requests, cross-tenant requests, archived targets,
   and already-merged sources unless the source already points at the same
   target.
3. Move missing feedback links from source to target with conflict-safe inserts.
4. Move missing customer links from source to target with conflict-safe inserts.
5. Move missing vote links from source to target with conflict-safe inserts.
6. Move missing delivery issue links from source to target with conflict-safe
   inserts.
7. Mark source as `merged_into_request_id = target.id` and `archived_at = now()`.
8. Record an audit event containing source ID, target ID, moved feedback count,
   moved customer count, moved vote count, moved issue count, and skipped
   duplicate counts.

When the target already has the same feedback link, merge skips the source link
and increments `skipped_duplicate_feedback_count`. When the target already has
the same provider/external key issue link, merge skips the source issue link,
keeps the target link metadata, and increments
`skipped_duplicate_issue_count`. Merge must not overwrite target issue titles,
statuses, URLs, or notes from source duplicates.

Customer and vote links use the same duplicate policy as feedback links: when
the target already has the same `(subject_hash, subject_key, account_key)`
identity, merge keeps the target row and records the source row as skipped.

Reads for a merged request return the surviving request ID in a
`merged_into_request_id` field so the Console can redirect the operator without
losing context.

### Backend shape

Add feature packages:

- `internal/repo/customerrequest`
- `internal/service/customerrequest`
- `internal/handlers/console/customerrequest`

The repository owns SQL and tenant-scoped locking. The service owns validation,
merge orchestration, and audit recording. The handler owns the generated proto
HTTP surface and RBAC checks.

The service uses `auditlog.Service.RecordTx` so mutations and audit rows commit
atomically. New audit actions:

- `customer_request.create`
- `customer_request.update`
- `customer_request.promote_feedback`
- `customer_request.link_feedback`
- `customer_request.unlink_feedback`
- `customer_request.link_customer`
- `customer_request.unlink_customer`
- `customer_request.add_vote`
- `customer_request.remove_vote`
- `customer_request.merge`
- `customer_request.link_issue`
- `customer_request.unlink_issue`

The audit action list and the database check constraint must be updated in the
same change. Console route inventory tests must classify every mutating route as
audited.

Audit metadata must stay bounded and deterministic:

- create: request ID, display ID, title length, status, priority, owner member
  ID.
- update: request ID plus old/new status, old/new priority, old/new owner member
  ID, title changed, description changed.
- promote feedback: request ID, display ID, feedback IDs, feedback count, and
  idempotency key hash.
- link/unlink feedback: request ID, feedback ID, importance, and note length.
- link/unlink customer: request ID, customer link ID, subject identity presence,
  account identity presence, and note length.
- add/remove vote: request ID, vote ID, subject identity presence, account
  identity presence, vote weight, and note length.
- merge: source request ID, target request ID, moved feedback count, moved
  customer count, moved vote count, moved issue count, and skipped duplicate
  counts for each link type.
- link/unlink issue: request ID, issue link ID, provider, external key, and URL
  host.

Promotion records a single `customer_request.promote_feedback` event instead of
separate create and link events. Plain request creation still records
`customer_request.create`.

### API contract

Add `proto/attune/v1/customer_request.proto` with a
`CustomerRequestService`. Generated Go, TypeScript, OpenAPI, and SDK-facing
artifacts come from `make proto`.

Initial RPCs and HTTP paths:

- `ListCustomerRequests`: `GET /fb/v1/console/customer-requests`
- `GetCustomerRequest`: `GET /fb/v1/console/customer-requests/{id}`
- `CreateCustomerRequest`: `POST /fb/v1/console/customer-requests`
- `UpdateCustomerRequest`: `PATCH /fb/v1/console/customer-requests/{id}`
- `PromoteFeedbackToCustomerRequest`:
  `POST /fb/v1/console/customer-requests:promote-feedback`
- `LinkFeedbackToCustomerRequest`:
  `POST /fb/v1/console/customer-requests/{id}/feedback`
- `UnlinkFeedbackFromCustomerRequest`:
  `DELETE /fb/v1/console/customer-requests/{id}/feedback/{feedback_id}`
- `LinkCustomerToCustomerRequest`:
  `POST /fb/v1/console/customer-requests/{id}/customers`
- `UnlinkCustomerFromCustomerRequest`:
  `DELETE /fb/v1/console/customer-requests/{id}/customers/{customer_link_id}`
- `AddCustomerRequestVote`:
  `POST /fb/v1/console/customer-requests/{id}/votes`
- `RemoveCustomerRequestVote`:
  `DELETE /fb/v1/console/customer-requests/{id}/votes/{vote_id}`
- `MergeCustomerRequests`:
  `POST /fb/v1/console/customer-requests/{source_id}:merge`
- `LinkCustomerRequestIssue`:
  `POST /fb/v1/console/customer-requests/{id}/issue-links`
- `UnlinkCustomerRequestIssue`:
  `DELETE /fb/v1/console/customer-requests/{id}/issue-links/{issue_link_id}`

`ListCustomerRequestsRequest` includes:

- `q`
- `status`
- `priority`
- `owner_member_id`
- `visibility`: `active`, `merged`, `archived`, or `all`; default `active`
- `sort`: `updated_at`, `customer_count`, `supporting_feedback_count`,
  `latest_feedback_at`, or `priority`; default `updated_at`
- `direction`: `asc` or `desc`; default `desc`
- `limit`
- `cursor`
- `feedback_id`: optional feedback-row filter used by feedback detail to list
  requests already linked to that feedback row

List responses return a bounded summary object with request ID, display
ID, title, status, priority, owner display object, counts, first/latest feedback
timestamps, hidden feedback count, merged target ID, archived timestamp,
created timestamp, updated timestamp, and next cursor.

Detail responses include the same summary plus description, visible linked
feedback evidence, explicit customer/account links, votes, linked issue
references, duplicate requests, merge alias information, and recent audit
entries. Evidence pagination stays separate from request list pagination so
large request histories do not make the detail payload unbounded.

Create, promote, and merge requests must include an `idempotency_key` field with
the same character and length rules as the existing idempotency table. The
service stores the request hash under the authenticated tenant. Reusing the same
key with the same payload returns the original response; reusing it with a
different payload returns `IDEMPOTENCY_CONFLICT`.

`PromoteFeedbackToCustomerRequest` accepts one or more feedback IDs,
validate all rows in the tenant, create the request, link the feedback rows, and
return the created detail object. This directly satisfies the acceptance case
where an operator promotes one or more feedback rows into a request. Replaying a
successful promote with the same idempotency key returns the created request
detail without creating another request.

Writes that change request backlinks, votes, customer links, issue references,
or merge state also touch the request row so the default `updated_at` sort
reflects fresh demand signals.

`MergeCustomerRequests` is retry-safe. If the source request is already
merged into the same target and the idempotency key/payload match, the service
returns the target detail. If the source is already merged into a different
target, the service returns a conflict.

### Console workflow

Add a Customer Requests navigation item under the Feedback group. The Console
surface includes:

- List page with search, status filter, priority filter, owner filter, customer
  count, account count, vote count, feedback count, duplicate count, linked
  issue count, first/latest feedback timestamps, updated time,
  active/merged/archived visibility filter, and sort controls.
- Detail page or detail drawer with overview, linked feedback evidence,
  explicit customer/account links, votes, delivery issue references, duplicate
  request links, merge status, and audit timeline.
- Feedback list promotion action from the existing selected-row action bar.
- Feedback detail request panel showing linked requests via `feedback_id`,
  search-based selection for linking an existing active request, and unlink
  actions guarded by Customer Request edit permission.
- Customer Request route search accepts `feedback_id` so a feedback-scoped URL
  can open the request list already filtered to the related demand objects.

The UI stays internal and operational. It avoids public roadmap language and
keeps votes framed as internal demand signals.

### Permissions

Add explicit Customer Request permissions to the Console permission matrix:

- `customer_request:view`
- `customer_request:edit`
- `customer_request:merge`

Default grants:

- admin: view, edit, merge
- delegated admin: view, edit, merge
- member: view, edit
- viewer: view

Backend route guards map request reads to viewer access, create/update
and link/unlink operations to member access, and merge to strict delegated-admin
access. Console route guards, permission tests, and user-facing role
descriptions must use the same matrix.

### Industry alignment

The design intentionally borrows the strongest patterns from the research:

- Linear: customer attributes and request links attached to delivery work.
- Productboard: feedback remains evidence and can be linked or re-linked to
  curated product ideas.
- Jira Product Discovery: request ideas carry prioritization fields and delivery
  links.
- Canny and Aha!: duplicate handling, proxy capture, status communication, and
  subscriber preservation.
- UserVoice and Productlane: engineering work links back to the customer
  feedback system.
- Savio: request detail exposes feedback count, people count, company
  count, revenue context, and first/last feedback dates when data exists.
- Dovetail: AI-assisted grouping must keep traceable source evidence.
- GitHub Projects and GitLab: custom fields, boards, and delivery tracking work
  best when the request object remains simple and adaptable.

## Alternatives considered

### Use tags as requests

Rejected. Tags are useful for classification and filtering, but they cannot
carry owner, status, priority, delivery references, merge state, or audit
semantics without becoming an overloaded request object.

### Use semantic clusters as requests

Rejected. Clusters are model-assisted grouping hints. A product request needs
operator-owned lifecycle semantics, stable IDs, delivery references, and audit.
Clusters can suggest likely links, but they do not become the canonical
request record.

### Create delivery issues directly from feedback

Rejected. Raw feedback often describes a symptom, not a scoped product change.
Creating engineering issues directly from every demand signal would skip product
curation and recreate the duplicate/noise problem in the delivery tracker.

### Build a customer/account model first

Rejected for this issue. Customer/account modeling is valuable, but Attune
already stores tenant-scoped subject identity on feedback rows. The request
layer can deliver the core workflow while preserving a clean path to a richer
customer model.

### Build public voting first

Rejected. Public voting is useful only after the request object, status model,
duplicate handling, and close-loop mechanics exist. Starting with internal
request operations keeps the data model durable and reduces public API risk.

## Risks / tradeoffs

- Derived customer counts may undercount or overcount when subject identity is
  incomplete. The API exposes both `customer_count` and `account_count`, derived
  from feedback subjects plus explicit customer and vote links.
- Merge operations can hide source requests from normal lists. The detail API
  must expose `merged_into_request_id` so operators can trace aliases.
- Delivery issue links are explicit references, not authoritative sync state.
  Status values may become stale until provider sync is added.
- Request statuses and priorities add product vocabulary. Keeping the first
  enum small reduces migration churn.
- Explicit customer-request permissions add setup work across Console, route
  guards, and tests, but avoid giving merge access to every feedback editor.
- Required idempotency adds request bookkeeping, but prevents duplicate request
  creation when a browser or proxy retries a mutation after a timeout.
- Link tables can grow quickly. The migration includes tenant/request,
  tenant/feedback, and identity indexes for list, detail, and unlink paths.

## Implementation plan

1. Add the proposal and keep it linked from the implementing PR with
   `Closes #212`.
2. Add database migrations for request counter, request, feedback-link,
   customer-link, vote, and issue-link tables, including composite tenant
   foreign keys, tenant-scoped indexes, status/priority checks, and the
   `user_feedback(tenant_id, id)` supporting unique index.
3. Add repository methods for list, detail, create, update, link, unlink,
   issue-link management, idempotency lookup, display-ID allocation, and merge.
4. Add service validation for soft-deleted feedback, owner membership, enum
   values, idempotency keys, retry-safe create/promote/merge, and atomic audit
   recording for every mutation.
5. Add audit actions to the Go allow-list and the database check constraint.
6. Add `customer_request.proto` with explicit enums, HTTP annotations,
   pagination, filters, sort keys, visibility filters, idempotency fields, and
   bounded detail/evidence messages. Run `make proto` and commit generated Go,
   TypeScript, SDK, and OpenAPI output.
7. Add Console handlers, router wiring, explicit Customer Request permissions,
   RBAC checks, and mutating-route audit inventory coverage.
8. Add Console API clients, query keys, list/detail routes, promotion dialog,
   linked feedback evidence, delivery issue references, merged/archived filters,
   owner display, first/latest feedback timestamps, and audit timeline.
9. Add unit tests, handler tests, PostgreSQL integration tests, Console tests,
   and tenant-isolation cases.
10. Update `CHANGELOG.md` under `[Unreleased]` because the implementation will
    ship product behavior.

## Verification

Implementation verification cites the relevant subset first and ends with
`make ci-check` before the PR is marked complete.

Backend verification:

- `go test ./internal/repo/customerrequest ./internal/service/customerrequest ./internal/handlers/console/customerrequest`
- `go test ./internal/handlers/console`
- `go test -tags=integration ./test/integration/postgres/customerrequest`
- `go test ./internal/service/auditlog`
- `go mod tidy && git diff --exit-code go.mod go.sum`
- `go vet ./...`
- `go build ./...`

Contract verification:

- `make proto`
- `buf lint`
- `buf generate && git diff --exit-code internal/proto console/src/proto docs/openapi sdk`
- `scripts/lint-errorcode.sh`

Console verification:

- `cd console && pnpm --ignore-workspace tsc -b --noEmit`
- `cd console && pnpm --ignore-workspace biome check`
- `cd console && pnpm --ignore-workspace vitest run`
- `cd console && pnpm --ignore-workspace vitest run src/features/customer-requests/components/customer-requests-page.test.tsx`
- `cd console && pnpm --ignore-workspace vitest run src/features/feedback/components/detail-sheet.test.tsx`
- Browser smoke test against a migrated local Postgres database and real Console
  API: login, create request, promote feedback `1, 2`, open request detail,
  link feedback `3`, unlink feedback `3`, merge a duplicate request, verify the
  drawer switches to the surviving target request, and verify detail evidence
  and duplicate counts update.

Data and security verification:

- Cross-tenant feedback link attempts return a not-found or forbidden response
  without creating a link.
- Create, promote, and update owner assignment reject `owner_member_id` values
  that belong to another tenant and leave existing request ownership unchanged.
- Cross-tenant request merge attempts are rejected.
- Cross-tenant issue-link updates are rejected.
- List filtering by `feedback_id` returns only Customer Requests linked to the
  requested feedback row in the authenticated tenant.
- Service-level feedback promotion writes the request, feedback evidence, audit
  row, and completed idempotency record atomically.
- Soft-deleted feedback cannot be promoted or linked, and linked feedback that
  is later soft-deleted is excluded from visible evidence counts, increments the
  hidden-feedback count, and remains unlinkable by operators.
- Replayed create, promote, and merge requests with the same idempotency key
  return the original response; mismatched payloads return
  `IDEMPOTENCY_CONFLICT`.
- Service-level promote replay returns the original Customer Request without
  creating a second request or audit row; a same-key promote with changed
  payload returns an idempotency conflict.
- Merge preserves feedback backlinks, explicit customer links, votes, and issue
  references on the target request.
- Backlink writes update the request `updated_at` and `updated_by` fields so
  list ordering reflects new evidence.
- Audit timeline shows create, update, link, unlink, issue-link, and merge
  events with bounded metadata.
- `scripts/lint-integration-layout.sh`
- `scripts/lint-artifacts.sh --strict`

Final verification:

- `make ci-check`
- `make test-integration`

## References

- [Issue #212](https://github.com/Phixsura/attune/issues/212)
- [Issue #202](https://github.com/Phixsura/attune/issues/202)
- [Linear Customer Requests](https://linear.app/docs/customer-requests)
- [Productboard Feedback quick start](https://support.productboard.com/hc/en-us/articles/26907498937235-Quick-start-guide-Feedback)
- [Jira Product Discovery quick start](https://www.atlassian.com/software/jira/product-discovery/guides/getting-started/quick-start)
- [Canny feature request management](https://canny.io/use-cases/feature-request-management)
- [Aha! Ideas introduction](https://support.aha.io/aha-roadmaps/support-articles/ideas/ideas-introduction~7444635822038636020)
- [UserVoice feedback manager guide](https://help.uservoice.com/hc/en-us/articles/360034983414-Feedback-Manager-s-Guide-to-UserVoice)
- [Pendo Listen overview](https://support.pendo.io/hc/en-us/articles/18159674293531-Overview-of-Pendo-Listen)
- [Productlane Linear integration](https://productlane.com/docs/integrations/linear)
- [Savio feature request page](https://www.savio.io/feature-request/)
- [Dovetail Channels](https://docs.dovetail.com/help/channels)
- [GitHub Projects](https://docs.github.com/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects)
- [GitLab Issues](https://docs.gitlab.com/user/project/issues/)
- [Mattermost feature requests](https://handbook.mattermost.com/operations/research-and-development/product/product-planning/feature-requests)
