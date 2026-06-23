# Production readiness preflight and Console system report

| Field | Value |
| --- | --- |
| **Issue** | [#149](https://github.com/Phixsura/attune/issues/149) |
| **Status** | Implemented |
| **Started** | 2026-06-23 |
| **Related** | [#66](https://github.com/Phixsura/attune/issues/66) (inbound framework), [#93](https://github.com/Phixsura/attune/issues/93) (MCP server), [2026-06-11-config-first-runtime.md](./2026-06-11-config-first-runtime.md) |

## Problem

Attune's deployment docs define production non-negotiables (TLS, Tink keyset,
OIDC, migration state, pgvector, etc.) but operators have no automated way to
verify them. The startup path in `cmd/attune/server.go` catches some fatal
misconfigurations, but only as hard panics deep in the boot sequence — there is
no structured "did I set everything up correctly?" check that an operator can run
*before* routing traffic.

Consequences:

- **Silent misconfiguration.** A missing Tink keyset, stale migrations, or
  unreachable OIDC issuer surface only when a user hits the affected path —
  sometimes hours after deploy.
- **No pre-production acceptance gate.** Operators manually walk through the
  deploy docs with no machine-checkable list. This makes the v1.0 enterprise
  promise ("install, verify, go live") hollow.
- **No Console visibility.** Self-hosted operators have no dashboard view of
  system health beyond `/healthz` (always-200 liveness) and `/readyz`
  (DB-ping-only readiness).

## Goals

- Expose a machine-checkable readiness report via **`attune doctor`** CLI
  (human-readable + JSON) and **`/fb/v1/console/system/preflight`** HTTP
  endpoint.
- Cover the production-critical checks: config validity, DB connectivity,
  pgvector, migration state, Tink keyset, OIDC/base-URL/session settings,
  metrics endpoint, and worker supervision status.
- Include per-check remediation text so operators know exactly what to fix.
- CLI and HTTP share a single check library — no duplicated logic.
- Add a Console system readiness page that renders the preflight results.
- Unit-test each check for both pass and fail paths.
- Document `attune doctor` as the pre-production acceptance gate.

## Non-goals

- Runtime continuous health monitoring (that is `/healthz` + `/readyz` +
  Prometheus; this is a point-in-time diagnostic).
- Auto-remediation (`--fix` mode) — the checks are read-only.
- Plugin/extension API for third-party checks — start with a closed set,
  extensibility can follow.
- Replacing the existing `/healthz` and `/readyz` probes — those serve
  Kubernetes; `doctor`/`preflight` serves operators.
- Checking LLM provider reachability (provider keys are tenant-level; the
  preflight scope is infrastructure, not per-tenant state).

## Prior art

Surveyed 20+ implementations across CLI tools, health-check standards, enterprise
preflight systems, and Go health-check libraries. Key influences on this design:

| Source | Pattern adopted |
|---|---|
| **IETF draft-inadarei-api-health-check-06** | Three-level `pass`/`warn`/`fail` status model; `application/health+json` response shape; `{component}:{measurement}` check key format |
| **Vault `operator diagnose`** | Hierarchical check structure with child→parent status bubbling; `--skip` flag; offline-first design for stopped systems; remediation as inline advice |
| **Replicated Troubleshoot** | `fail`/`warn`/`pass` outcome with `message` + `uri` remediation; `strict: true` blocking pattern |
| **heptiolabs/healthcheck** (Go) | Single registry → dual HTTP+CLI consumer; `?full=1` verbosity gating; `Async()` wrapper for expensive checks |
| **dotse/go-health** (Go) | IETF-conformant three-level status; `WorstStatus()` aggregation; `health.Main(ctx)` CLI mode + `HandleHTTP` dual consumption |
| **Grafana Advisor** | Per-category collapsible UI with inline remediation; "Action needed" / "Investigation needed" / "No action needed" severity labels |
| **PostHog `/_preflight/`** | Flat per-component boolean → UI tile mapping (adopted cautiously: their Redis heartbeat drift is a documented anti-pattern) |
| **GitLab Admin health** | 4-tier endpoint taxonomy (`/-/health`, `/health_check`, `/-/readiness`, `/-/liveness`); schema drift detection |

Full research notes available in the session transcript.

## Proposal

### Check model

A check is a named, categorized function that probes one aspect of production
readiness and returns a structured result with remediation guidance.

```go
// internal/preflight/check.go

package preflight

type Status string

const (
    StatusPass    Status = "pass"
    StatusWarn    Status = "warn"
    StatusFail    Status = "fail"
    StatusSkipped Status = "skipped"
)

type Category string

const (
    CategoryConfig     Category = "config"
    CategoryDatabase   Category = "database"
    CategoryMigration  Category = "migration"
    CategoryEncryption Category = "encryption"
    CategoryAuth       Category = "auth"
    CategoryMetrics    Category = "metrics"
    CategoryWorker     Category = "worker"
)

type Result struct {
    Name        string   `json:"name"`
    Category    Category `json:"category"`
    Status      Status   `json:"status"`
    Message     string   `json:"message"`
    Remediation string   `json:"remediation,omitempty"`
}

type CheckFunc func(ctx context.Context, env *Environment) Result
```

The `Environment` struct is the check's dependency bag — it carries everything a
check might need without giving checks access to the full server wiring:

```go
type Environment struct {
    Cfg  *config.Config
    Pool *pgxpool.Pool   // nil when DB is unreachable (checks must handle)
}
```

### Registry — single source, dual consumption

```go
// internal/preflight/registry.go

type Check struct {
    Name        string
    Category    Category
    Run         CheckFunc
}

var registry []Check

func Register(c Check) { registry = append(registry, c) }

func RunAll(ctx context.Context, env *Environment) Report { ... }
```

All checks register via `Register()` in their respective `init()` functions
inside `internal/preflight/checks/`. `RunAll` iterates, collects results, and
computes the aggregate status via `WorstStatus()`.

```go
type Report struct {
    Status  Status   `json:"status"`
    Checks  []Result `json:"checks"`
}

func (r Report) WorstStatus() Status { ... }
```

Both CLI and HTTP consume the same `RunAll`:

- **CLI** (`cmd/attune/doctor.go`): calls `preflight.RunAll`, renders
  human-readable table or `--format=json`.
- **HTTP** (`handlers/console/system/preflight.go`): calls `preflight.RunAll`,
  returns JSON with `Content-Type: application/health+json`.

### Check inventory

| # | Name | Category | What it checks | Pass | Warn | Fail |
|---|---|---|---|---|---|---|
| 1 | `config:parse` | config | YAML loads without error | loads cleanly | — | parse error |
| 2 | `config:base_url` | config | `console.base_url` is non-empty and is a valid URL | set + valid | — | empty or invalid |
| 3 | `config:tls_consistency` | config | `dev_login`/`insecure_cookies` are off when `base_url` is HTTPS | both off | — | either on with HTTPS |
| 4 | `database:connectivity` | database | `pool.Ping(ctx)` succeeds within 5s | responds | — | timeout or error |
| 5 | `database:pgvector` | database | pgvector extension installed, version ≥ 0.5.0 | present + version ok | clustering disabled (skipped) | missing or version too old |
| 6 | `migration:pending` | migration | No pending migrations (all applied) | 0 pending | — | ≥ 1 pending |
| 7 | `encryption:tink_keyset` | encryption | Tink keyset loads + encrypt/decrypt round-trip succeeds | round-trip ok | — | missing or broken |
| 8 | `auth:oidc_reachable` | auth | OIDC issuer `/.well-known/openid-configuration` responds | 200 | OIDC disabled (skipped) | unreachable or non-200 |
| 9 | `auth:session_key` | auth | `console.session_key` ≥ 32 bytes | ≥ 32 bytes | — | too short or empty |
| 10 | `metrics:endpoint` | metrics | `/metrics` serves Prometheus text | responds with metrics | — | error or empty |
| 11 | `worker:enricher` | worker | Enricher goroutine count > 0 (via config) | configured workers > 0 | — | workers = 0 |

Checks 5 and 8 use `StatusSkipped` when the feature is disabled (clustering
off / OIDC not configured) — skipped checks do not affect the aggregate status.

### CLI: `attune doctor`

```
$ attune doctor

attune preflight — production readiness report

 Config
  ✓ config:parse            Config loaded successfully
  ✓ config:base_url         https://feedback.example.com
  ✓ config:tls_consistency  No insecure flags with HTTPS

 Database
  ✓ database:connectivity   Connected (4ms)
  ✓ database:pgvector       pgvector 0.7.0

 Migration
  ✓ migration:pending       All migrations applied (22/22)

 Encryption
  ✓ encryption:tink_keyset  Encrypt/decrypt round-trip OK (key 123456789)

 Auth
  ✓ auth:oidc_reachable     https://accounts.google.com responded OK
  ✓ auth:session_key        Session key configured (48 bytes)

 Metrics
  ✓ metrics:endpoint        /metrics serving 42 metric families

 Worker
  ✓ worker:enricher         3 enricher workers configured

Overall: PASS (11 checks, 0 warnings, 0 failures)
```

Failure example:

```
 Encryption
  ✗ encryption:tink_keyset  Tink keyset file not found
    → Set secrets.tink_keyset to a valid keyset JSON file path.
      Generate one with: attune secrets generate-keyset
```

Flags:

- `--format=json` — machine-readable JSON output (same shape as HTTP response)
- `--format=text` — human-readable table (default)
- `--category=<cat>` — run only checks in a category
- `--warn-exit` — exit code 1 on any warn (default: exit 1 only on fail)

Exit codes: 0 = all pass/warn, 1 = any fail (or any warn with `--warn-exit`),
2 = doctor itself errored.

### HTTP: `/fb/v1/console/system/preflight`

```
GET /fb/v1/console/system/preflight
```

Requires Console session (same auth as other `/fb/v1/console/` routes). Returns
`application/health+json`:

```json
{
  "status": "warn",
  "checks": [
    {
      "name": "database:connectivity",
      "category": "database",
      "status": "pass",
      "message": "Connected (4ms)"
    },
    {
      "name": "auth:oidc_reachable",
      "category": "auth",
      "status": "skipped",
      "message": "OIDC not configured"
    },
    {
      "name": "encryption:tink_keyset",
      "category": "encryption",
      "status": "fail",
      "message": "Tink keyset file not found",
      "remediation": "Set secrets.tink_keyset to a valid keyset JSON file path. Generate one with: attune secrets generate-keyset"
    }
  ]
}
```

HTTP status: 200 for pass/warn, 503 for fail (per IETF draft convention).

### Console UI: System readiness page

Route: `/system` in the Console SPA (new top-level nav item under the admin
section).

Layout (Grafana Advisor-inspired):

1. **Banner** at top: overall status with color (green/amber/red) and count
   summary ("11 checks: 9 passed, 1 warning, 1 failure").
2. **Category sections**: collapsible groups (Config, Database, Migration,
   Encryption, Auth, Metrics, Worker). Each shows a group status icon.
3. **Per-check rows**: status icon + check name + message. Failed/warned checks
   expand to show remediation text.
4. **Refresh button**: re-fetches `/fb/v1/console/system/preflight`.

The page calls the preflight endpoint on mount and renders the JSON response. No
additional frontend logic beyond presentation.

### Package layout

```
internal/
  preflight/
    check.go          # Status, Category, Result, Report, Environment types
    registry.go       # Register(), RunAll(), WorstStatus()
    format.go         # CLI table formatter
    checks/
      config.go       # config:parse, config:base_url, config:tls_consistency
      database.go     # database:connectivity, database:pgvector
      migration.go    # migration:pending
      encryption.go   # encryption:tink_keyset
      auth.go         # auth:oidc_reachable, auth:session_key
      metrics.go      # metrics:endpoint
      worker.go       # worker:enricher
cmd/attune/
  doctor.go           # attune doctor subcommand
internal/handlers/console/system/
  preflight.go        # GET /fb/v1/console/system/preflight handler
console/src/pages/system/
  SystemReadiness.tsx  # Console UI page
```

### Wire-up

**CLI** (`cmd/attune/main.go`):

```go
var subcommands = map[string]func([]string) error{
    "server": ...,
    "doctor": runDoctor,  // new
    ...
}
```

`runDoctor` loads config, optionally opens a DB pool (tolerates failure — the
connectivity check reports it), builds `preflight.Environment`, calls
`preflight.RunAll`, and formats output.

**HTTP** (`cmd/attune/router.go`):

The preflight handler is mounted inside the existing Console route group:

```go
r.Route("/fb/v1/console", func(r chi.Router) {
    r.Mount("/", consoleRouter)
})
```

The handler is added to the console router under `GET /system/preflight`.

**Console**: new route `/system` in the SPA router, new nav item in the admin
section sidebar.

## Alternatives considered

### A. Extend `/readyz` with detailed checks

Rejected. `/readyz` is a Kubernetes probe consumed by load balancers at high
frequency. Adding expensive checks (OIDC discovery, Tink round-trip, migration
scan) would violate the Kubernetes best practice of keeping readiness probes
lightweight. The existing `/readyz` stays as-is.

### B. Third-party health-check library (alexliesenfeld/health, heptiolabs/healthcheck)

Considered. These libraries solve HTTP handler + check registry well, but:

- They impose binary up/down status (no warn/skipped).
- They own the HTTP handler shape — we need to integrate with chi and Console
  session auth.
- The check functions need access to `*config.Config` and `*pgxpool.Pool`, not
  just `context.Context`.
- The overhead of a dependency is not justified for ~100 lines of registry code.

A small internal registry that borrows the `Checker` interface pattern and IETF
output format gives us the right trade-off.

### C. YAML-driven check definitions (WP-CLI doctor pattern)

Rejected. The YAML-driven pattern (check name → class mapping) is powerful for
plugin ecosystems with external contributors. Attune's checks are all internal,
tightly coupled to config/DB/Tink internals, and benefit from compile-time type
safety. A Go registry with `init()` registration is simpler and equally
extensible for our needs.

### D. Collect → Analyze two-phase pipeline (Replicated Troubleshoot)

Rejected for now. The collector/analyzer separation enables offline analysis and
redaction, which matters for support bundles sent to vendors. Attune's preflight
is operator-local — the checks run where the data lives. If we later add
`attune support-bundle`, the Replicated pattern becomes relevant.

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| OIDC discovery check adds outbound HTTP call | 5s timeout; `StatusSkipped` when OIDC disabled |
| Tink round-trip test creates ephemeral ciphertext | Uses a fixed test plaintext; no side effects on keyset state |
| Migration check requires DB access | Gracefully reports `fail` if pool is nil (DB unreachable) |
| New `internal/preflight` package adds to import graph | Zero dependencies on `service`/`repo`/`handlers`; only imports `config` + `database` from infra |
| CLI `doctor` needs DB but is not the server | Reuses `database.NewPool` with short connect timeout (5s); reports failure, does not panic |

## Implementation plan

### PR 1: Check library + CLI (`internal/preflight` + `attune doctor`)

1. Create `internal/preflight/` with types, registry, and `RunAll`.
2. Implement all 11 checks in `internal/preflight/checks/`.
3. Add `cmd/attune/doctor.go` with human-readable + JSON output.
4. Unit-test each check for pass, fail, and (where applicable) warn/skipped.
5. Add `doctor` to `printUsage()` and the subcommand dispatch table.

### PR 2: Console API + UI

1. Add `GET /fb/v1/console/system/preflight` handler.
2. Define proto message (or hand-craft JSON — TBD based on proto migration
   status for Console system routes).
3. Add Console `SystemReadiness` page with category-grouped check display.
4. Add nav item in admin section.
5. Integration test: start server with known-good config, hit preflight
   endpoint, assert all-pass.

### PR 3: Docs + changelog

1. Add `attune doctor` to the deploy docs as the pre-production acceptance gate.
2. Update `docs/private-deploy.md` with a "verify your installation" section.
3. Changelog entry under `### Added`.

## Adding a new check (SOP)

Adding a preflight check touches exactly four files, plus one test file. The
registry is `init()`-driven, so no wiring changes are needed in `cmd/attune` or
the HTTP handler.

### Step-by-step

1. **Pick a category.** Reuse an existing `Category` constant from
   `internal/preflight/check.go` if the check fits. If none fits, add a new
   constant — then also update `CATEGORY_ORDER` in the Console page,
   `categoryTitle()` in `format.go`, and `system_readiness.category` in
   `zh-CN.json`.

2. **Write the check function** in the appropriate file under
   `internal/preflight/checks/`. Convention: one file per category
   (`config.go`, `database.go`, …). The function signature is always:

   ```go
   func checkSomething(ctx context.Context, env *preflight.Environment) preflight.Result {
       r := preflight.Result{
           Name:     "category:measurement",
           Category: preflight.CategoryXxx,
       }
       // ... probe logic ...
       return r
   }
   ```

   Rules:
   - **Name format**: `category:measurement` (lowercase, colon-separated).
   - **Handle nil deps gracefully**: `env.Cfg` or `env.Pool` may be nil. Return
     `StatusFail` with remediation, never panic.
   - **Never leak secrets or raw errors** into `Message` — sanitize before
     returning. Internal details go to `logext`, not the check result.
   - **Provide `Remediation`** on every non-pass result. Tell the operator
     exactly what to change and where.
   - **Use `StatusSkipped`** when a feature is disabled (e.g., OIDC not
     configured). Skipped does not affect aggregate status.
   - **Outbound HTTP** must use `otelhttp.NewTransport` and a ≤ 5 s timeout.

3. **Register in `init()`** in the same file:

   ```go
   func init() {
       preflight.Register(preflight.Check{
           Name:     "category:measurement",
           Category: preflight.CategoryXxx,
           Run:      checkSomething,
       })
   }
   ```

   Registration order within a category determines display order.

4. **Add unit tests** in `internal/preflight/checks/<category>_test.go`.
   Minimum: one test per status the check can return (pass, fail, and
   warn/skipped if applicable). Use `// ptrext:file-allow test fixtures…` at
   the top of new test files.

5. **Add the check to the proposal table** (Check inventory section above) and
   update `CHANGELOG.md`.

### Checklist

- [ ] Check function in `internal/preflight/checks/<category>.go`
- [ ] `init()` registration with correct name/category
- [ ] Nil-safety for `env.Cfg` and `env.Pool`
- [ ] Remediation text on every non-pass path
- [ ] No `err.Error()` in `Result.Message`
- [ ] Unit tests covering all return statuses
- [ ] `CATEGORY_ORDER` updated in Console page if new category
- [ ] `categoryTitle()` case added in `format.go` if new category
- [ ] `Category` constant added in `check.go` if new category
- [ ] i18n key in `zh-CN.json` under `system_readiness.category` if new category
- [ ] `CHANGELOG.md` entry

### What you do NOT need to touch

- `cmd/attune/doctor.go` — discovers checks via the registry automatically.
- `internal/handlers/console/system/preflight.go` — runs `RunAll`, no per-check
  awareness.
- Console `system-readiness-page.tsx` — renders whatever the API returns. New
  categories need a `CATEGORY_ORDER` entry; new checks in existing categories
  appear automatically.

## Verification

- `go vet ./...` + `golangci-lint` clean.
- Unit tests for each check: pass path (correct config/DB/keyset) and fail path
  (missing/broken dependency). Use injected interfaces / test doubles — no real
  DB needed for unit tests.
- Integration test (PR 2): `make test-integration` with a real Postgres instance
  verifies `database:connectivity`, `database:pgvector`, and
  `migration:pending` checks end-to-end.
- Manual verification: run `attune doctor` against a deliberately misconfigured
  local instance and confirm each failure produces actionable remediation text.
- Console UI: start dev server, navigate to `/system`, verify all checks render
  with correct status colors and remediation text.

## References

- IETF Health Check Response Format: https://datatracker.ietf.org/doc/html/draft-inadarei-api-health-check-06
- Kubernetes probes: https://kubernetes.io/docs/concepts/configuration/liveness-readiness-startup-probes/
- Vault operator diagnose: https://developer.hashicorp.com/vault/docs/commands/operator/diagnose
- Replicated Troubleshoot: https://troubleshoot.sh/docs/
- Grafana Advisor: https://grafana.com/docs/grafana/latest/administration/grafana-advisor/
- GitLab health checks: https://docs.gitlab.com/ee/administration/monitoring/health_check.html
- PostHog preflight: https://github.com/PostHog/posthog/issues/1808
- heptiolabs/healthcheck: https://github.com/heptiolabs/healthcheck
- alexliesenfeld/health: https://github.com/alexliesenfeld/health
- dotse/go-health: https://pkg.go.dev/github.com/dotse/go-health
- OpenTelemetry Collector health check v2: https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/extension/healthcheckv2extension/README.md
- Docker Distribution health: https://pkg.go.dev/github.com/docker/distribution/health
- React Native CLI doctor: https://github.com/react-native-community/cli/blob/main/docs/healthChecks.md
- WP-CLI doctor: https://github.com/wp-cli/doctor-command
- Salesforce CLI doctor: https://developer.salesforce.com/docs/platform/salesforce-cli-plugin/guide/integrate-doctor.html
- Consul health checks: https://developer.hashicorp.com/consul/docs/register/health-check/vm
