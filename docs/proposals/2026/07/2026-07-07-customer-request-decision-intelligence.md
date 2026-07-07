<!-- markdownlint-disable MD013 -->

# Customer Request Decision Intelligence

| Field | Value |
|---|---|
| Issue | [#212](https://github.com/Phixsura/attune/issues/212) |
| Status | Implemented |
| Started | 2026-07-07T19:43:27+08:00 |
| Related | [customer requests](./2026-07-07-customer-requests.md), [feedback intelligence control tower](./2026-07-03-feedback-intelligence-control-tower.md) |

## Problem

The initial Customer Requests workflow gives product operators a curated object
for feedback evidence, explicit demand, owners, duplicates, and delivery issue
references. That closes the loop from feedback to request, but it still leaves
several planning decisions outside attune:

1. Which requests are backed by the highest-value accounts?
2. Which demand links are attached to known account context instead of free-form
   customer text only?
3. Which delivery issue references are current, stale, or failing to sync?
4. How should operators sort the request backlog when priority, evidence, votes,
   and account value disagree?

Top product-feedback systems solve this by separating raw evidence from decision
fields. Jira Product Discovery, Linear Customer Requests, Productboard, Aha!,
Savio, Productlane, Canny, UserVoice, Pendo Listen, Dovetail, GitHub Projects,
and GitLab all expose some combination of account value, prioritization formulae,
delivery status, and synced issue metadata. Attune should add that decision
layer without pretending to be a full CRM or a two-way delivery-sync engine.

## Goals

- Store tenant-scoped account profiles that can be attached through explicit
  customer links and internal votes.
- Summarize revenue impact at the request level without double-counting the
  same account.
- Add a deterministic decision score that combines priority, evidence count,
  customer breadth, account breadth, vote weight, and revenue impact.
- Let operators sort requests by revenue impact and decision score.
- Persist delivery issue sync state and lightweight external metadata so stale
  or failed issue references are visible in the request detail view.
- Keep all new API fields in the generated proto/OpenAPI contract.
- Add backend, frontend, and PostgreSQL integration coverage for the new
  decision metadata.

## Non-goals

- Replace a CRM system or model full opportunity/account ownership state.
- Add automatic outbound polling or webhook-based delivery issue sync.
- Change Customer Request status semantics based on external delivery status.
- Introduce tenant-configurable scoring formulae in this slice.
- Expose customer account revenue in a public or anonymous portal.

## Proposal

Add a small `customer_request_accounts` table keyed by `(tenant_id, account_key)`.
Explicit customer links and internal votes may include optional account profile
fields: display name, revenue in cents, currency, tier, size segment, lifecycle
status, CRM provider, and CRM external ID. The service validates shape and
lengths, normalizes currency to uppercase ISO-style three-letter values, and
only writes an account profile when an account key is present.

The repository upserts account profiles conservatively. Empty profile fields do
not erase existing values, and revenue only changes when a positive value is
provided. This keeps manual corrections stable while still allowing operators to
enrich demand records from later customer links or votes.

Request summaries compute revenue impact as the sum of distinct account revenue
for accounts connected through either customer links or votes. The decision score
is deterministic and intentionally simple:

- priority contributes the base urgency weight;
- supporting feedback, distinct customers, distinct accounts, and votes add
  capped demand breadth;
- revenue contributes a capped value signal.

The response includes both `decision_score` and a short explanation string so
operators can inspect why a request appears above another one. This first score
is product-opinionated but not tenant-configurable; once real usage shows which
signals matter most, a later proposal can add configurable score weights.

Delivery issue links gain sync metadata: sync state, external status category,
external assignee, external updated timestamp, sync error, and last synced
timestamp. The new API operation records observed external issue state without
attempting to own external delivery systems.

The Console shows revenue impact and decision score in the request list, adds
sort options for both fields, surfaces account profiles in detail, extends
customer/vote forms with optional account profile fields, and allows operators
to record a successful issue sync from the issue-link row.

## Alternatives considered

- **Keep account value as free-form link notes.** Rejected because it cannot
  support sorting, deduplication, or request-level aggregation.
- **Add a full tenant CRM/account model first.** Rejected because Customer
  Requests need a lightweight prioritization layer now, and the broader CRM
  domain has more ownership, security, and import concerns.
- **Make scoring fully configurable immediately.** Rejected because configurable
  formulae need UI, validation, migration, and documentation. A deterministic
  first score is easier to test and calibrate.
- **Automatically sync delivery issue status.** Rejected for this slice because
  provider credentials, rate limits, retry policies, and webhook security need a
  separate integration proposal.

## Risks / tradeoffs

- Revenue impact can bias prioritization toward known enterprise accounts. The
  score therefore still includes feedback, customer, account, and vote breadth.
- Manual account profile entry may drift from source CRM data. The schema keeps
  CRM identifiers so future imports can reconcile records.
- A hard-coded score may need tuning. The implementation keeps the explanation
  visible and testable, making later formula changes easier to review.
- Issue sync metadata is operator-recorded in this slice, so it indicates the
  latest known state rather than a guaranteed live external status.

## Implementation plan

1. Add migration 102 with account profiles and issue-link sync metadata.
2. Extend the Customer Request proto contract, generated Go/TypeScript/OpenAPI
   artifacts, repository model, service validation, handler mapping, and audit
   action enum.
3. Compute revenue impact and decision score in request summary queries.
4. Add Console sort options, request-list/detail metrics, account profile rows,
   account profile form inputs, and issue-sync recording.
5. Add focused backend, frontend, and PostgreSQL integration tests.
6. Update the changelog and verify the relevant quality gates.

## Verification

- `make proto`
- `go test ./internal/repo/customerrequest ./internal/service/customerrequest ./internal/handlers/console/customerrequest ./internal/handlers/console ./internal/infra/database`
- `go test -tags=integration ./test/integration/postgres/customerrequest`
- `pnpm tsc -b --noEmit`
- `pnpm biome check src/features/customer-requests/components/customer-requests-page.tsx src/features/customer-requests/components/customer-requests-page.test.tsx src/features/feedback/components/detail-sheet.test.tsx src/lib/customer-request-api.ts src/i18n/zh-CN.json`
- `pnpm vitest run src/features/customer-requests/components/customer-requests-page.test.tsx src/features/feedback/components/detail-sheet.test.tsx`

## References

- [Linear Customer Requests](https://linear.app/docs/customer-requests)
- [Jira Product Discovery](https://www.atlassian.com/software/jira/product-discovery)
- [Productboard](https://www.productboard.com/)
- [Aha! Ideas](https://www.aha.io/product/ideas)
- [Canny](https://canny.io/)
