<!-- markdownlint-disable MD013 -->

# Customer Request Saved Views

| Field | Value |
|---|---|
| Issue | [#212](https://github.com/Phixsura/attune/issues/212) |
| Status | Implemented |
| Started | 2026-07-08T00:00:00+08:00 |
| Related | [customer requests](./2026-07-07-customer-requests.md), [customer request decision intelligence](./2026-07-07-customer-request-decision-intelligence.md), [customer request scoring settings](./2026-07-08-customer-request-scoring-settings.md) |

## Problem

Customer Requests now support request evidence, account value, delivery health,
notes, and tenant-tunable scoring. Operators can filter and sort the backlog,
but their planning views are still ephemeral. Mature planning tools such as
Jira Product Discovery, Productboard, Aha!, GitHub Projects, Linear, and Canny
let teams return to named prioritization views instead of rebuilding the same
status, owner, score, and delivery-health filters each time.

Attune needs saved Customer Request views so operators can preserve recurring
planning lenses without introducing a full roadmap board or public portal.

## Goals

- Let each Console user save, update, apply, and delete Customer Request list
  views.
- Persist the same list state the page already supports: search query, status,
  priority, owner, visibility, sort, direction, and feedback deep-link scope.
- Keep views tenant-scoped and user-scoped.
- Reuse the existing `system_settings` saved-view storage pattern.
- Add generated API contracts, Console controls, and focused tests.

## Non-goals

- Add shared team views or view-level permissions.
- Add board columns, drag ordering, roadmap publishing, or public voting.
- Audit per-user view preference changes in the unified audit stream.
- Add arbitrary scoring expressions.

## Proposal

Add saved-view messages and CRUD endpoints to the Customer Request proto:

- `ListCustomerRequestSavedViews`
- `CreateCustomerRequestSavedView`
- `UpdateCustomerRequestSavedView`
- `DeleteCustomerRequestSavedView`

Saved views are stored in a JSON envelope under `system_settings` with a
`customer_request.saved_views.user:<user_id>` key. This matches the existing
Audit Log saved-view pattern, avoids adding a table for per-user UI state, and
keeps all data tenant-scoped through `system_settings.tenant_id`.

The service validates names and filter state before writing:

- non-empty name, capped to a compact display length;
- valid status, priority, visibility, sort, and direction values;
- owner ID must be a UUID when present;
- feedback ID must be positive when present.

The Customer Requests page adds a small saved-views control near the toolbar.
Operators can apply a saved view, save the current list state as a new view,
overwrite the selected view, and delete obsolete views.

## Alternatives considered

- **Do nothing.** Rejected because repeatedly rebuilding priority filters keeps
  Attune behind standard planning workflows in top request tools.
- **Add shared team views now.** Rejected because ownership, role rules, and
  naming collisions are broader than the current per-user workflow.
- **Create a dedicated table.** Rejected because the existing saved-view
  envelope pattern already fits small per-user UI state.

## Risks / tradeoffs

- Per-user views do not create a shared team taxonomy. This is acceptable for
  the first slice and keeps scope low-risk.
- A saved owner filter can point at a deleted member later. Applying the view
  still sends the owner ID and naturally returns no matching rows.
- Stored JSON is less queryable than a table, but these views are read by user
  and do not need analytics queries.

## Implementation plan

1. Extend the Customer Request proto contract and regenerate Go, TypeScript, SDK,
   and OpenAPI artifacts.
2. Add `customerrequestview` repo and service packages using `system_settings`.
3. Wire saved-view handlers and routes under `/customer-requests/saved-views`.
4. Add Console API hooks and a compact saved-view control on the Customer
   Requests page.
5. Add backend and frontend tests, changelog entry, and route audit-inventory
   exemption.
6. Run targeted quality gates before updating the PR.

## Verification

- `make proto`
- `go test ./internal/repo/customerrequestview ./internal/service/customerrequestview ./internal/handlers/console/customerrequest ./internal/handlers/console`
- `pnpm vitest run src/features/customer-requests/components/customer-requests-page.test.tsx src/lib/customer-request-api.test.tsx`
- `pnpm tsc -b --noEmit`

## References

- [Jira Product Discovery](https://www.atlassian.com/software/jira/product-discovery)
- [Productboard](https://www.productboard.com/)
- [Aha! Ideas](https://www.aha.io/product/ideas)
- [GitHub Projects](https://docs.github.com/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects)
- [Linear Customer Requests](https://linear.app/docs/customer-requests)
