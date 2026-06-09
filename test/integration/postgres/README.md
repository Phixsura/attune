# PostgreSQL Integration Suites

These suites exercise attune against real PostgreSQL and run only with
`//go:build integration`.

Rules:

- Put regular PostgreSQL smoke tests in `test/integration/postgres/<area>`.
- Keep reusable database setup in `internal/testdb`; do not copy container or
  migration setup into individual suites.
- Use public package APIs, public routers, or public constructors. Do not import
  package-private test seams just to make a PostgreSQL smoke test easier.
- Do not add package-adjacent `*_io_test.go` files or integration-tagged tests
  under business packages.
- Keep an untagged `doc.go` in each integration-suite package so
  `go vet ./...` works without the `integration` tag.

Run:

```bash
make test-integration
```

The layout is enforced by `scripts/lint-integration-layout.sh`.
