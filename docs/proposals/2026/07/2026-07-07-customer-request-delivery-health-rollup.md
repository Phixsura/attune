<!-- markdownlint-disable MD013 -->

# Customer Request Delivery Health Rollup

| Field | Value |
|---|---|
| Issue | [#212](https://github.com/Phixsura/attune/issues/212) |
| Status | Implemented |
| Started | 2026-07-07T20:36:29+08:00 |
| Related | [customer requests](./2026-07-07-customer-requests.md), [customer request decision intelligence](./2026-07-07-customer-request-decision-intelligence.md) |

## Problem

Customer Requests can now link delivery issues and record per-link sync state.
That is enough for detail-level inspection, but it still makes backlog review
too slow: operators must open each request to learn whether a delivery reference
is synced, stale, pending, failed, or only manually tracked.

World-class product feedback workflows surface delivery confidence at the
request level. Productboard, Linear, Jira Product Discovery, Aha!, GitLab, and
GitHub Projects all help operators scan whether an idea is connected to active
delivery work and whether that delivery state is trustworthy. Attune should do
the same without adding provider polling or new credentials in this slice.

## Goals

- Derive a request-level delivery health value from linked issue sync states.
- Expose synced, stale, failed, pending, and manual issue counts on request
  summaries.
- Add a delivery-health sort so failed and stale delivery references can be
  reviewed first.
- Show the delivery health signal in the Console list and detail views.
- Keep the implementation generated-contract compatible and backed by
  PostgreSQL integration coverage.

## Non-goals

- Poll GitHub, Jira, Linear, or any other provider automatically.
- Change Customer Request lifecycle status based on delivery health.
- Add provider credentials, webhook registration, or retry workers.
- Notify customers or publish roadmap state externally.

## Proposal

Keep delivery health as a derived summary value. The repository already stores
per-link `sync_state` in `customer_request_issue_links`; the summary query
counts each state and computes the request-level health using deterministic
severity order:

1. `failed` when any linked issue failed sync.
2. `stale` when there are stale links and no failed links.
3. `pending` when there are pending links and no failed or stale links.
4. `manual` when links exist but are not all synced.
5. `synced` when every linked issue is synced.
6. `no_links` when the request has no delivery references.

The proto contract adds `CustomerRequestDeliveryHealth`, per-state issue counts,
and `CUSTOMER_REQUEST_SORT_DELIVERY_HEALTH`. The Console renders a compact
health badge in request rows, adds detail metrics, and exposes the sort option.

## Alternatives considered

- **Persist a rollup column.** Rejected because sync state already lives on issue
  links and the summary query is the source of truth for other counts.
- **Treat manual links as worse than pending links.** Rejected because pending
  usually means automation is in-flight; manual is less urgent unless stale or
  failed state is known.
- **Add external polling now.** Rejected because provider credentials, rate
  limits, webhooks, retries, and audit semantics need a separate proposal.

## Risks / tradeoffs

- A derived rollup can only reflect the latest recorded sync state, not a live
  provider status.
- Mixed synced and manual links collapse to `manual`, which is conservative but
  may hide partial success unless operators inspect the counts.
- Sorting by delivery health is severity-oriented rather than lifecycle-oriented.

## Implementation plan

1. Extend the Customer Request proto contract and regenerate artifacts.
2. Add repository summary fields, SQL rollup counts, delivery health rank, and
   sort support.
3. Map the fields through the Console handler.
4. Render health badges, detail metrics, and sort controls in the Console.
5. Add frontend fixtures and PostgreSQL integration coverage.
6. Update the changelog and run the relevant quality gates.

## Verification

- `make proto`
- `go test ./internal/repo/customerrequest ./internal/service/customerrequest ./internal/handlers/console/customerrequest ./internal/handlers/console`
- `go test -tags=integration ./test/integration/postgres/customerrequest`
- `pnpm tsc -b --noEmit`
- `pnpm vitest run src/features/customer-requests/components/customer-requests-page.test.tsx src/features/feedback/components/detail-sheet.test.tsx`

## References

- [Linear Customer Requests](https://linear.app/docs/customer-requests)
- [Jira Product Discovery](https://www.atlassian.com/software/jira/product-discovery)
- [Productboard](https://www.productboard.com/)
- [Aha! Roadmaps](https://www.aha.io/roadmaps/overview)
