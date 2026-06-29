# Proposal: Tenant isolation contract suite

| Field     | Value                                                      |
|-----------|------------------------------------------------------------|
| Issue     | #154                                                       |
| Status    | Accepted                                                   |
| Started   | 2026-06-29                                                 |
| Related   | #84 (hardening batch), #197 (SSO/break-glass)              |

## Problem

attune has three auth surfaces (API Key, Console Session, MCP OAuth) and many
tenant-scoped data domains (feedback, tags, workflow, outbox, audit, GDPR, API
keys, notify targets, MCP clients, guard policies, inbound sources, LLM config,
enrichment config). Existing tenant isolation tests are scattered across
individual integration packages (outbox, breakglass, feedback) and cover only
repo-layer assertions. There is no systematic contract that proves cross-tenant
isolation holds across every auth surface and every data domain.

v1.0 milestone requires a contract test suite that proves a tenant A principal
(session, API key, MCP token) can never read, modify, or enumerate tenant B
resources — at the repo layer, the handler layer, and the full HTTP stack.

## Goals

- Systematic, table-driven contract covering every tenant-scoped domain × CRUD
  operation at the repo layer (Layer A).
- Per-domain boundary tests for edge cases the table misses: pagination,
  batch operations, filtered lists (Layer B).
- Full-stack HTTP black-box tests through all three auth surfaces proving
  middleware → router → handler → repo isolation end-to-end (Layer C).
- Precise failure output: breach reports identify domain, operation, auth
  surface, and leaked object.
- Easy extension: adding a new domain = adding rows to the contract table +
  optional Layer B/C test functions.

## Non-goals

- Performance benchmarking of tenant-scoped queries.
- Testing auth mechanisms themselves (login, token issuance) — those have
  dedicated tests.
- Multi-cluster or cross-region isolation.

## Proposal

### Three-layer architecture

```
Layer C — HTTP black-box   (test/integration/postgres/isolation/http_test.go)
  Real httptest.Server + real auth middleware + real DB
  Three auth surfaces: API Key, Console Session, MCP Bearer

Layer A — Contract table   (test/integration/postgres/isolation/contract_test.go)
  Table-driven repo-layer isolation: every domain × operation
  Two-tenant fixture, cross-tenant access → assert not-found / empty

Layer B — Per-domain edge  (test/integration/postgres/*/isolation_test.go)
  Boundary cases in existing packages: pagination, batch, filtered lists
```

### Shared fixture

`test/integration/postgres/isolation/fixture.go` provides `Fixture` with two
fully-seeded tenants. Each test creates its own `testdb.NewPool(t)` for full
isolation. The fixture seeds:

- Tenant A and B (via `tenant.NewTenant(pool).Create`)
- Feedback rows (with enrichment), tags, workflow states, outbox entries,
  audit log entries, API keys, notify targets, MCP clients, guard policies,
  GDPR data, inbound sources per tenant.

### Layer A contract table

Each `IsolationCase` defines: Domain, Operation, a function that executes the
operation using Tenant A's identity against Tenant B's resource, and expected
error (ErrNotFound or empty list). The test loop:

1. Set up fixture with both tenants' data.
2. For each case, call the repo method with Tenant A's ID but Tenant B's
   resource ID.
3. Assert the result is not-found, empty, or error — never Tenant B's data.
4. On failure: `t.Errorf("ISOLATION BREACH: domain=%s op=%s ...")`.

### Layer B per-domain

Add `isolation_test.go` to existing integration packages that don't have
isolation coverage. Priority additions:

- `feedbacktag/` — batch tag assignment across tenants
- `workflowstate/` — list states filtered by tenant
- `apikey/` — lookup by hash returns wrong tenant's key
- `gdpr/` — export/delete scoped to tenant
- `auditlog/` — list filter by tenant

### Layer C HTTP black-box

Start `httptest.Server` with the real console router + API-key admin routes,
backed by real Postgres. For each auth surface:

- **API Key**: Create real API keys for both tenants. Use Tenant A's key to
  hit Tenant B's resources. Assert 404 (not 200 with B's data).
- **Console Session**: Sign real session cookies for both tenants via
  `session.Signer`. Use Tenant A's cookie to hit endpoints with Tenant B's
  resource IDs. Assert 404.
- **MCP OAuth**: Exercise the MCP tool endpoints with JWT bearing Tenant A's
  claims against Tenant B's data.

### CI integration

All three layers live under `test/integration/postgres/...` and run via the
existing `make test-integration` target. No new CI job needed.

## Alternatives considered

1. **Repo-only contract (Layer A only)**: Misses middleware bypass bugs.
2. **HTTP-only (Layer C only)**: Slow startup, hard to debug, misses
   repo-layer boundary cases.
3. **Property-based fuzzing**: Overkill for v1.0; isolation is a binary
   property per endpoint.

## Risks / tradeoffs

- Fixture complexity grows with each new domain — mitigated by centralizing
  seed helpers in `fixture.go`.
- Layer C tests add ~2-3s per auth surface to CI — acceptable for a P0
  security contract.
- Handler nil-service panics in Layer C if handler dependencies not wired —
  mitigated by using `middleware.Recoverer` and wiring only the handlers we
  test.

## Implementation plan

See `docs/superpowers/plans/2026-06-29-tenant-isolation-contract-suite.md`.

## Verification

- `make test-integration` passes with all three layers.
- Mutation test: temporarily remove a `WHERE tenant_id = $N` clause from one
  repo method and confirm the contract catches it.
- CI `go-checks` workflow includes the new tests.

## References

- Existing isolation tests: `test/integration/postgres/outbox/outbox_test.go`,
  `test/integration/postgres/breakglass/breakglass_test.go`,
  `test/integration/postgres/feedback/sample_enriched_test.go`
- Auth surfaces: `internal/infra/apikey/middleware.go`,
  `internal/handlers/console/internal/session/session.go`,
  `internal/mcp/server/middleware.go`
