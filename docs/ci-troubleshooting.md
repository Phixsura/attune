# CI Troubleshooting Guide

Quick fixes for common CI failures. Run `make ci-check` locally before pushing to catch most issues.

## Quick Reference

| Check | Command to reproduce | Common fix |
|-------|---------------------|------------|
| pr-title | N/A (GitHub validates) | Use `feat(scope):` not `Feat(scope):` |
| go vet | `go vet ./...` | Fix reported issues |
| go build | `go build ./...` | Fix compile errors |
| golangci-lint | `golangci-lint run` | See [golangci-lint](#golangci-lint) |
| lizard | `lizard . -l go -C 15 -T nloc=100 --warnings_only` | See [lizard](#lizard) |
| lint-slog | `bash scripts/lint-slog.sh --strict` | See [lint-slog](#lint-slog) |
| lint-rawptr | `bash scripts/lint-rawptr.sh` | Use `ptrext.Of()` / `ptrext.Indirect()` |
| lint-errorcode | `bash scripts/lint-errorcode.sh` | Use enum values for ErrorResponse.code |
| lint-integration-layout | `bash scripts/lint-integration-layout.sh` | Move integration tests to `test/integration/` |
| jscpd | `npx -y jscpd . -f go -i '**/*.pb.go' -t 5` | Reduce duplication below 5% |
| trufflehog | `trufflehog git file://. --only-verified` | See [trufflehog](#trufflehog) |
| console (tsc) | `pnpm -C console tsc -b --noEmit` | Fix TypeScript errors |
| console (biome) | `pnpm -C console biome check` | Run `pnpm -C console biome check --write` |
| console (vitest) | `pnpm -C console vitest run` | Fix failing tests |
| console (arch) | `pnpm -C console arch` | Use supported Node and fix dependency-cruiser violations |
| Console Coverage | `pnpm -C console vitest run --coverage` | See [coverage](#coverage) |
| Go Coverage | `go test -race -coverprofile=c.out ./...` | See [coverage](#coverage) |
| integration-postgres | `make test-integration` | See [integration-postgres](#integration-postgres) |
| dependency-review | N/A (GitHub validates) | See [dependency-review](#dependency-review) |
| proto-sync | `make proto && git diff --exit-code` | Commit generated files |

---

## Detailed Solutions

### pr-title

**Error:** PR title doesn't match Conventional Commits format.

**Fix:** Use lowercase type prefix:
```
feat(enricher): add batch processing       ✓
Feat(enricher): add batch processing       ✗
feat: add batch processing                 ✓ (scope optional)
```

Valid types: `feat`, `fix`, `docs`, `chore`, `ci`, `test`, `refactor`, `perf`, `build`, `revert`

---

### golangci-lint

**Error:** Various linter findings.

**Common issues:**

1. **depguard (slog-facade):** Direct `log/slog` import in business code
   ```go
   // ✗ Don't do this
   import "log/slog"
   
   // ✓ Use the facade
   import "github.com/Phixsura/attune/internal/pkg/logext"
   ```

2. **depguard (layering):** Cross-layer imports violating §5 rules
   - `handlers` importing `repo` directly
   - `notify` importing `service`
   - Check `.golangci.yml` for the full rule set

3. **bodyclose:** HTTP response body not closed
   ```go
   resp, err := http.Get(url)
   if err != nil { return err }
   defer resp.Body.Close()  // ← required
   ```

---

### lizard

**Error:** Function exceeds CCN ≤15 or NLOC ≤100.

**Fix:** Refactor into smaller helper functions:

```go
// ✗ Before: CCN=20
func (c *Config) Validate() error {
    if c.A == "" { return errors.New("a required") }
    if c.B == "" { return errors.New("b required") }
    // ... 18 more conditions
}

// ✓ After: CCN=4
func (c *Config) Validate() error {
    if err := c.validateRequired(); err != nil { return err }
    if err := c.validateFormat(); err != nil { return err }
    if err := c.validateSecurity(); err != nil { return err }
    return nil
}
```

---

### console arch

**Error:** dependency-cruiser reports that the current Node version is not
supported.

**Fix:** CI runs Console checks on Node 22. Local `make ci-check` automatically
uses `scripts/with-supported-node.sh` to select Node 20, 22, or 24+ when one is
installed. If Node is installed in a custom location, set:

```bash
ATTUNE_NODE_BIN=/path/to/node make ci-check
```

For ordinary direct Console commands, use Node 22 or run through the wrapper:

```bash
bash scripts/with-supported-node.sh corepack pnpm -C console arch
```

---

### lint-slog

**Error:** Rule-2 or Rule-3 violation.

**Rule-2:** Field name collides with OTel auto-injected key (`trace_id`, `span_id`, etc.)
```go
// ✗ Collides with OTel
logext.Infof(ctx, "msg", "trace_id", val)

// ✓ Use different name
logext.Infof(ctx, "msg", "external_trace_id", val)
```

**Rule-3:** `&http.Client{}` without `otelhttp.NewTransport`
```go
// ✗ No tracing
client := &http.Client{}

// ✓ With tracing
client := &http.Client{
    Transport: otelhttp.NewTransport(http.DefaultTransport),
}
```

---

### trufflehog

**Error:** TruffleHog is required for local CI preflight.

Install TruffleHog or start Docker:
```bash
brew install trufflehog
# or
docker pull ghcr.io/trufflesecurity/trufflehog:3.95.5
```

The local target mirrors the repository Secret Scan workflow by scanning
verified and unknown findings while honoring `.trufflehogignore`.

**Error:** Found secrets in committed files.

**For test credentials:** Add paths to `.trufflehogignore`:
```
deploy/config.oidc-test.yaml
deploy/docker-compose.oidc-test.yml
test/fixtures/fake-credentials.json
```

**For real secrets:** Remove from git history:
```bash
git filter-branch --force --index-filter \
  'git rm --cached --ignore-unmatch <file>' HEAD
```

---

### coverage

**Error:** Coverage decreased.

**Options:**

1. **Add tests** for new code (preferred)

2. **Skip check** with label (reviewer must approve):
   - `skip-go-coverage` for Go
   - `skip-console-coverage` for Console

Labels trigger workflow re-run via `labeled` event.

---

### integration-postgres

**Error:** Wrong number of arguments to `console.NewRouter`.

**Fix:** Check the current signature and update all call sites:
```go
// Find the signature
grep -n "func NewRouter" internal/handlers/console/router.go

// Find all call sites
grep -rn "console.NewRouter" test/
```

Usually happens when a new handler is added to the router.

---

### dependency-review

**Error:** High-severity vulnerability in dependency.

**Fix:**
1. Check the advisory link in CI output
2. Upgrade the vulnerable package:
   ```bash
   go get github.com/example/pkg@latest
   go mod tidy
   ```
3. If no fix available, document exception in PR description

---

## One-liner: Full Local CI

```bash
make ci-check
```

This runs all checks in sequence. If any fails, fix and retry.
