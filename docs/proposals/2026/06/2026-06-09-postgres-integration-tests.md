# PostgreSQL integration tests + CI tier

| Field   | Value |
|---------|-------|
| Issue   | #12 |
| Status  | **Implemented** |
| Started | 2026-06-09 |
| Related | #1 (CI), #5 (docker-compose attune + Postgres), CLAUDE.md §9 (tests for new behavior), CLAUDE.md §10 (one proposal per issue) |

## Problem

The Go unit suite covers pure logic and handler wiring, but the database-facing
paths can still regress in ways only a real PostgreSQL instance catches:
migration drift, JSONB containment syntax, transaction boundaries, row locks,
foreign keys, uniqueness constraints, and retry queue state transitions.

The repository already had a small integration foothold:
`make test-integration`, a real-Postgres CI job, and
`internal/repo/feedback/feedback_io_test.go`. That was useful, but it only
covered feedback repo behavior. Issue #12 asks for smoke coverage across the
main DB-touching paths:

- feedback ingest -> enrich -> outbox drain,
- API key issue / verify / revoke,
- tenant + notify-target CRUD,
- migrations applied before tests run.

After pulling `main` at `938b10d`, the scope has widened slightly: #66 added
admin login, channel-agnostic inbound sources, and a destructive Lark-delete
guard. Those packages now contain skipped placeholders that explicitly say
"requires testdb harness -- tracked in #12":

- `internal/repo/admin/admins_test.go`,
- `internal/repo/inboundsource/inbound_sources_test.go`,
- `internal/infra/database/confirm_lark_test.go`,
- selected console inbound delete branches that need a real `pgxpool`.

So #12 should not only add the harness; it should also replace those skips with
real smoke tests.

The issue body specifically suggested a GitHub Actions `services: postgres:18`
container plus a database URL. This proposal adopts that CI shape directly.
The shared test helper still keeps a local testcontainers fallback so
contributors can run `make test-integration` without provisioning Postgres by
hand, but CI exercises the service-container path.

The earlier local red `cmd/lint-rawptr` test is fixed on current `main`
(`go test ./cmd/lint-rawptr` passes), so it is no longer part of this proposal.

## Goals

- Add real PostgreSQL smoke tests for API keys, tenants, notify targets, and the
  ingest/enrich/outbox flow.
- Replace the new #66 testdb placeholders with real smoke coverage for admin
  repo behavior, inbound source repo behavior, inbound source delete paths, and
  the destructive Lark-delete guard.
- Keep the default unit tier fast and offline.
- Keep the integration tier explicit via the `integration` build tag and
  `make test-integration`.
- Keep PostgreSQL smoke tests discoverable by collecting them under
  integration-suite directories instead of scattering `*_io_test.go` files
  through production packages.
- Enforce the integration-suite layout with a repo lint so future PRs do not
  drift back to package-adjacent integration tests.
- Share one Postgres test helper so new integration tests do not copy container
  startup and migration code.
- Run the CI integration tier against a GitHub Actions `postgres:18` service
  container, with one temporary database per test.
- Keep local integration runs ergonomic with a `postgres:18` testcontainers
  fallback when no service DSN is exported.
- Run migrations twice in at least one smoke test to prove the tracker path is
  idempotent, not just that a fresh database can migrate once.
- Add a deterministic single-batch outbox worker test entry point so the
  integration suite does not rely on ticker sleeps.
- Document the integration tier in `docs/testing.md`.
- Keep the CI aggregator aware of the integration job.

## Non-goals

- No live LLM calls. The end-to-end enrich smoke test uses a deterministic fake
  LLM client.
- No external webhook calls. The outbox drain uses a loopback `httptest.Server`.
- No broad API or schema behavior changes. The only production-code surface
  change is a narrow `OutboxWorker.ProcessOnce(ctx)` wrapper over the existing
  batch-processing logic, added to make tests and operational drains
  deterministic.
- No Playwright or browser E2E coverage.

## Proposal

Add `internal/testdb` behind the `integration` build tag. The helper has two
paths:

- In CI, `ATTUNE_TEST_DATABASE_URL` points at the GitHub Actions
  `postgres:18` service container. `testdb.NewPool(t)` connects to that admin
  database, creates a uniquely named temporary database for the test, opens a
  `pgxpool` against the temporary database, runs `database.RunMigrations`, and
  drops the temporary database during cleanup.
- Locally, when `ATTUNE_TEST_DATABASE_URL` is unset, the helper starts
  `postgres:18` with testcontainers-go and runs migrations inside a fresh
  container.

This keeps CI aligned with the issue text while preserving local ergonomics.
Isolation remains per test, so package-level concurrency and `go test ./...`
ordering cannot leak rows across smoke tests. Runtime is controlled by keeping
the integration suite smoke-like; if it grows past an acceptable CI budget, a
future PR can optimize with package-level fixtures or schema-per-test isolation.

Keep test ownership explicit:

- `internal/testdb` owns the reusable Postgres harness only.
- `test/integration/postgres/<area>` contains the regular PostgreSQL smoke
  suites.
- Handler-level suites should use public routers or public constructors rather
  than package-private test seams, so the whole PostgreSQL integration tier can
  remain in the repository-level `test/integration` tree.

Add `scripts/lint-integration-layout.sh` and run it from pre-commit,
`scripts/check.sh`, and CI. The lint rejects integration-tagged Go files outside
`test/integration/**` and `internal/testdb/**`, rejects package-adjacent
`*_io_test.go` files, and requires an untagged `doc.go` in integration-only
packages so default `go vet ./...` can still enumerate them.

Add one migration smoke test, likely in `internal/infra/database`, that runs
`RunMigrations` twice on the same pool and verifies the tracker row count is
stable. Fresh-migration success is already implicit in every `testdb.NewPool`;
the extra test specifically guards idempotency.

Add focused integration test files:

- `test/integration/postgres/apikey`: create a tenant, issue a key, verify it
  resolves to the same tenant/key id, revoke it, and verify lookup fails.
- `test/integration/postgres/tenant`: create and resolve tenants, assert slug
  conflicts, exercise `FirstActiveID` / `GetByID`, and exercise notify-target
  insert/list, conflict, update, failure alert, clear failure, and delete
  behavior.
- `test/integration/postgres/admin`: replace the skipped placeholder with
  create/get round-trips, case-insensitive email lookup, failed-attempt lockout,
  reset, password hash update, and bootstrap idempotency/advisory-lock behavior.
- `test/integration/postgres/inboundsource`: replace the skipped placeholder
  with enabled-list filtering, `GetBySlugs` tenant join, `UpdateState`
  round-trip, and `SetEnabled` clearing/storing `last_error`.
- `test/integration/postgres/consoleinbound`: cover the delete happy path and
  race-lost branch through the public console router, with a real `pgxpool` and
  signed session cookie.
- `test/integration/postgres/database`: cover migration idempotency and
  `ConfirmLarkDelete` with lark rows present, env unset (guard fires), and env
  opted in (guard bypasses).
- `test/integration/postgres/feedback`: keep the existing feedback JSONB
  coverage, moved out of the repo package and onto the shared helper.
- `test/integration/postgres/ingest`: insert through `service.Ingestor`,
  run `Enricher.EnrichOne` with a fake LLM, assert `user_feedback` is done and
  a raw-webhook outbox row is queued in the same flow, then call
  `OutboxWorker.ProcessOnce(ctx)` against a loopback receiver and assert the row
  is delivered.

Add `OutboxWorker.ProcessOnce(ctx)` as a public one-shot wrapper around the
existing private batch-processing body. `Run(ctx)` continues to own the ticker
loop and calls the same method. This avoids sleeping in tests, gives operators a
future-friendly manual drain primitive, and does not change retry semantics.

`make test-integration` runs only the integration-suite directories. Unit tests
continue to run in the unit tier, so the integration target does not blur the
two tiers by sweeping `./...`. The target uses `-p 1` so local testcontainers
fallback runs do not try to start many Postgres containers at once; CI still
uses the service-container DSN path with one temporary database per test.

No new heavy third-party dependencies are planned. `testcontainers-go` and the
Postgres module are already present in `go.mod`; this change reuses them. The
existing `github.com/google/uuid` dependency generates collision-resistant
temporary database names for the service-container path.

Update `CHANGELOG.md` despite the mostly `test`/`docs` shape because this adds
a named CI/test tier and a small public outbox worker method. The entry should
describe the PostgreSQL integration tier under `### Added`.

## Alternatives considered

### Testcontainers-only CI

The repository already had a partial testcontainers-go direction. Keeping CI on
testcontainers only would have been simpler, but it would not match the
service-container CI acceptance shape in issue #12 and would hide a second
Postgres startup path from CI. The accepted design uses the issue's
`postgres:18` service container in CI and keeps testcontainers as a local
fallback only.

### One global container

A global container would be faster, but Go packages run independently and may
run concurrently. Per-test-container isolation is slower but simpler and more
trustworthy for a first real DB tier. If runtime grows too much, a future PR can
optimize with schema-per-test or a package-level fixture.

### Calling private outbox methods from tests

Keeping `processBatch` private and testing it only from `internal/service/outbox`
would make the outbox test easier but would not cover the ingest/enrich/outbox
chain in one place. A tiny public `ProcessOnce(ctx)` is clearer: tests can call
the same operation `Run(ctx)` uses, and production gains an explicit one-shot
drain primitive.

### Handler-level HTTP test for the ingest path

The highest-risk issue #12 path is the service/repo transaction boundary, not
proto decoding. `service.Ingestor` plus `Enricher` plus `OutboxWorker` covers
that boundary without making the test wait on an async goroutine from the HTTP
handler. Existing handler tests continue to cover HTTP decoding and auth
context behavior.

## Risks / tradeoffs

- Local testcontainers runs require Docker. `docs/testing.md` calls this out,
  and the tier remains opt-in locally.
- CI depends on the Postgres service container being healthy before tests run.
  The workflow uses `pg_isready` health checks; `testdb` fails fast with a clear
  connection error if the service DSN is wrong.
- Per-test isolated databases add runtime. The suite should stay small and
  smoke-like: broad enough to catch DB drift, not exhaustive business-logic
  coverage. If CI runtime becomes a problem, optimize the fixture after
  measuring.
- The loopback outbox receiver must avoid asserting fragile JSON field ordering;
  it should assert semantic fields that matter to customers.
- Exposing `OutboxWorker.ProcessOnce(ctx)` slightly expands public service API.
  The method is deliberately small and delegates to existing logic so the new
  surface stays easy to reason about.

## Implementation plan

1. Add the proposal and shared dual-mode `testdb` helper.
2. Add the migration idempotency smoke test.
3. Move feedback integration tests onto the helper.
4. Add `OutboxWorker.ProcessOnce(ctx)` and wire `Run(ctx)` through it.
5. Add API key, tenant/notify-target, admin, inbound source, inbound delete,
   guard, and ingest/enrich/outbox integration tests.
6. Remove or convert the #12 `t.Skip("requires testdb harness")` placeholders.
7. Move PostgreSQL integration suites under `test/integration/postgres/<area>`.
8. Scope `make test-integration` to the integration-suite directories.
9. Add the GitHub Actions `integration-postgres` job using `postgres:18` and
   `ATTUNE_TEST_DATABASE_URL`.
10. Add an integration-layout lint and wire it into pre-commit,
    `scripts/check.sh`, and CI.
11. Update `AGENTS.md`, `docs/testing.md`, and `CHANGELOG.md`.
12. Run `go test -short ./...`, `make test-integration`, and targeted tests for
   the changed packages.

## Verification

- `go test -short ./...`
- `make test-integration`
- `go test -tags=integration -count=1 ./test/integration/postgres/...`
- `bash scripts/lint-integration-layout.sh`

## References

- Issue #12: PostgreSQL integration tests + service-container CI job
- Issue #1: CI
- Issue #5: docker-compose attune + Postgres
