# Customer Request Collaboration Notes

| Field | Value |
|---|---|
| Issue | [#212](https://github.com/Phixsura/attune/issues/212) |
| Status | Implemented |
| Started | 2026-07-07T21:28:08+08:00 |
| Related | [customer requests](./2026-07-07-customer-requests.md), [customer request decision intelligence](./2026-07-07-customer-request-decision-intelligence.md), [customer request delivery health rollup](./2026-07-07-customer-request-delivery-health-rollup.md) |

## Problem

Customer Requests now collect evidence, supporters, votes, delivery links, and
decision rollups, but the working context around a request still lives outside
the request itself. Product and support operators need a lightweight internal
place to record decision context, risk notes, and follow-up observations without
changing the request description or leaking private discussion into delivery
systems.

## Goals

- Add tenant-scoped internal notes to each Customer Request.
- Make notes visible on the request detail contract and Console drawer.
- Support adding and deleting notes through generated API contracts.
- Record auditable note lifecycle events without duplicating note bodies in the
  audit log.
- Preserve source request discussion context when a request is merged into a
  target request.

## Non-goals

- Public customer comments or customer portal conversations.
- Mention notifications, threaded comments, reactions, or rich text.
- Editing existing notes.
- Syncing notes to GitHub, Jira, Linear, or another delivery system.

## Proposal

Introduce a `customer_request_notes` table keyed by `id` with `(tenant_id,
request_id)` scoped to `customer_requests(tenant_id, id)`. Each row stores a
plain-text `body`, `created_by`, and `created_at`. The service validates body
length from 1 to 5000 characters after trimming whitespace.

Extend `CustomerRequestService` with:

- `AddCustomerRequestNote`
- `DeleteCustomerRequestNote`

Add `CustomerRequestNote` to `CustomerRequestDetail.notes`. Console displays an
internal notes section in the Customer Request detail drawer, with a compact
textarea for new notes and icon-only delete controls for existing notes.

Audit actions:

- `customer_request.add_note`
- `customer_request.delete_note`

Audit metadata records `request_id`, `note_id`, and `body_length`; it does not
store the note body. Merge copies source notes onto the target request while
preserving the original note author and timestamp, so archived duplicate
requests do not hide team context from the surviving request.

## Alternatives Considered

- Reuse `customer_requests.description` as the collaboration field. This would
  blur stable product framing with ephemeral operator context and make audit
  intent harder to follow.
- Store notes only as audit events. Audit logs are optimized for compliance and
  filtering, not day-to-day collaboration or request detail rendering.
- Add threaded comments immediately. Threads, mentions, permissions, and
  notification fan-out are valuable later but materially increase scope.

## Risks / Tradeoffs

- Plain-text notes are intentionally simple and may be insufficient for rich
  discussion. The schema keeps a clear migration path to add format metadata.
- Deleting a note removes it from the request view. The audit event retains the
  note identity and length, but not the body, to avoid creating a second copy of
  sensitive text.
- Copying notes on merge duplicates context on the target request. This is a
  deliberate tradeoff so the surviving request remains operationally complete.

## Implementation Plan

1. Add migration `103_customer_request_notes.sql` and update migration count
   tests.
2. Extend `customer_request.proto` and regenerate Go, TypeScript, and OpenAPI
   outputs with `make proto`.
3. Add repo note read/write/delete methods and merge note copying.
4. Add service validation and audit events.
5. Add Console handlers, router bindings, and dispatch tests.
6. Add Console API hooks, drawer UI, localized strings, and component tests.
7. Add PostgreSQL integration coverage for note lifecycle, audit metadata, touch
   semantics, and merge preservation.

## Verification

- `make proto`
- Targeted Go tests for repo, service, handlers, router, database migration
  tests, and PostgreSQL Customer Request integration tests.
- Targeted Console TypeScript and Vitest suites for Customer Requests and
  feedback detail fixtures.
- Full `make ci-check` and `make test-integration` before completion.

## References

- [Linear Customer Requests](https://linear.app/docs/customer-requests)
- [Productboard Notes](https://support.productboard.com/hc/en-us/articles/4409826079379)
- [Aha! Ideas](https://www.aha.io/support/ideas/ideas/ideas)
