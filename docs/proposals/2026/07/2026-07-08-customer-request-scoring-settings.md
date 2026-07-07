<!-- markdownlint-disable MD013 -->

# Customer Request Scoring Settings

| Field | Value |
|---|---|
| Issue | [#212](https://github.com/Phixsura/attune/issues/212) |
| Status | Implemented |
| Started | 2026-07-08T00:00:00+08:00 |
| Related | [customer requests](./2026-07-07-customer-requests.md), [customer request decision intelligence](./2026-07-07-customer-request-decision-intelligence.md) |

## Problem

Customer Requests now expose a deterministic decision score that combines
priority, feedback breadth, customer breadth, account breadth, votes, and
revenue impact. That makes backlog sorting inspectable, but it is still a
single product-opinionated formula. Mature discovery tools such as Jira Product
Discovery, Productboard, Aha!, Savio, and GitHub Projects let teams adapt
prioritization fields and scoring views to their operating model.

Attune needs tenant-level scoring configuration so different teams can tune the
relative importance of evidence, account value, and urgency without changing
code or forking request data.

## Goals

- Add tenant-scoped Customer Request scoring settings with defaults that preserve
  the current formula.
- Compute request `decision_score` from the tenant's scoring settings in list
  and detail queries.
- Expose generated Console API methods to read and update scoring settings.
- Audit scoring-settings changes without logging raw customer request contents.
- Add a Console settings dialog so delegated operators can tune weights and
  caps from the Customer Requests page.
- Add backend and frontend tests for default compatibility, custom scoring, and
  validation.

## Non-goals

- Add arbitrary formula expressions or scripting.
- Add custom request fields beyond the scoring weights in this slice.
- Add saved prioritization views, roadmap boards, or public roadmaps.
- Let scoring changes mutate Customer Request priority or status.

## Proposal

Create `customer_request_scoring_settings` keyed by tenant. The table stores
priority weights for each priority value, per-signal weights and caps for
feedback, customers, accounts, and votes, plus revenue conversion settings.

The default row is optional. If a tenant has no row, SQL uses constants that
match the existing formula:

- low / medium / high / urgent priority: `20 / 40 / 60 / 80`
- feedback: `2` points each, capped at `80`
- customers: `5` points each, capped at `100`
- accounts: `8` points each, capped at `120`
- votes: `4` points each, capped at `80`
- revenue: `1` point per `100000` cents, capped at `100`

The Console API adds:

- `GetCustomerRequestScoringSettings`
- `UpdateCustomerRequestScoringSettings`

Updates are restricted to delegated-admin-or-higher Console sessions, validated
server-side, written atomically, and audited with action
`customer_request.update_scoring_settings`.

The Customer Requests page adds a compact scoring-settings dialog. It reads the
current settings, lets authorized operators adjust numeric weights and caps, and
invalidates request lists after save so visible decision scores reflect the new
formula.

## Alternatives considered

- **Keep the fixed formula.** Rejected because it keeps attune below the
  configurable prioritization baseline set by top discovery tools.
- **Expose arbitrary formulas.** Rejected because expression parsing, sandboxing,
  type checking, and explainability are materially broader than weighted
  settings.
- **Persist a score snapshot on every request.** Rejected because score changes
  should re-rank the backlog immediately without rewriting request rows.

## Risks / tradeoffs

- Tuning weights can make scores less comparable across tenants. Scores are
  tenant-scoped, so this is acceptable.
- Revenue-heavy formulas can over-prioritize enterprise accounts. Caps remain
  required and visible to keep the bias bounded.
- A settings dialog is less powerful than saved prioritization views, but it
  creates the foundation for later views without expanding this PR too far.

## Implementation plan

1. Add migration 104 with scoring settings and audit action support.
2. Extend the Customer Request proto contract and regenerate Go, TypeScript, and
   OpenAPI artifacts.
3. Add repo methods for default/read/update settings and wire the SQL decision
   score to tenant settings.
4. Add service validation and audit recording.
5. Add Console handlers, routes, API hooks, and a scoring-settings dialog.
6. Add focused backend/frontend tests and update changelog.
7. Run targeted quality gates before pushing.

## Verification

- `make proto`
- `make ci-check`
- `go test ./internal/repo/customerrequest ./internal/service/customerrequest ./internal/handlers/console/customerrequest ./internal/handlers/console ./internal/infra/database ./internal/service/auditlog`
- `go test -tags=integration ./test/integration/postgres/database`
- `go test -tags=integration ./test/integration/postgres/customerrequest`
- `pnpm tsc -b --noEmit`
- `pnpm biome check src/features/customer-requests/components/customer-requests-page.tsx src/features/customer-requests/components/customer-requests-page.test.tsx src/lib/customer-request-api.ts src/lib/customer-request-api.test.tsx src/lib/permissions.ts src/lib/permissions.test.ts src/features/audit-log/actions.ts src/features/audit-log/actions.test.ts src/i18n/zh-CN.json`
- `pnpm vitest run src/features/customer-requests/components/customer-requests-page.test.tsx src/lib/customer-request-api.test.tsx src/lib/permissions.test.ts src/features/audit-log/actions.test.ts`

## References

- [Jira Product Discovery](https://www.atlassian.com/software/jira/product-discovery)
- [Productboard](https://www.productboard.com/)
- [Aha! Ideas](https://www.aha.io/product/ideas)
- [Savio feature request prioritization](https://www.savio.io/use-cases/analyze-and-prioritize-feature-requests/)
- [GitHub Projects](https://docs.github.com/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects)
