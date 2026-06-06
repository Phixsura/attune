# Observability Overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship an optional Prometheus+Grafana docker-compose overlay for attune's `/metrics`, frame `observability/` as a backend-agnostic contract, and fix a metric-cardinality footgun found while doing so.

**Architecture:** Three layers — (1) the `/metrics` OpenMetrics exposition + a documented metrics reference; (2) portable assets in `observability/` (datasource-agnostic dashboards, `targets.yaml`); (3) one reference runtime `deploy/docker-compose.obs.yml` layered via `-f`. No per-backend overlays, no new Go runtime deps.

**Tech Stack:** docker-compose, Prometheus, Grafana file-provisioning, Go 1.25 (`prometheus/client_golang`), GitHub Actions (`dorny/paths-filter`).

**Spec:** `docs/proposals/2026/06/2026-06-05-observability-overlay.md` (Accepted). That proposal is the §10 record; this plan is the execution aid.

**Note:** Every commit ends with the trailer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` (omitted in the commands below for brevity). Conventional-commit types per CLAUDE.md §4.

---

### Task 1: Branch + commit the accepted proposal

**Files:**
- Modify: `docs/proposals/2026/06/2026-06-05-observability-overlay.md` (already Status: Accepted in working tree)

- [ ] **Step 1: Create the feature branch** (we're on `main`)

Run: `git checkout -b feat/observability-overlay`
Expected: `Switched to a new branch 'feat/observability-overlay'`

- [ ] **Step 2: Commit the accepted proposal + this plan**

```bash
git add docs/proposals/2026/06/2026-06-05-observability-overlay.md docs/superpowers/plans/2026-06-05-observability-overlay.md
git commit -m "docs(proposal): accept observability overlay design (#6)"
```

---

### Task 2: Fix the `attune_ingest_total{source}` cardinality footgun (TDD)

`internal/handlers/ingest.go:66` records the raw client `source` on the
validate_err path → unbounded series. Bound it to `domain.ValidSources` ∪ `{invalid}`.

**Files:**
- Create: `internal/handlers/ingest_source_test.go`
- Modify: `internal/handlers/ingest.go`

- [ ] **Step 1: Write the failing test**

Create `internal/handlers/ingest_source_test.go`:

```go
package handlers

import "testing"

// TestBoundedSource verifies attune_ingest_total's `source` label can't be
// driven to unbounded cardinality by an arbitrary client-supplied source on the
// validate_err path (proposal #6).
func TestBoundedSource(t *testing.T) {
	// Known-valid sources pass through unchanged.
	for _, valid := range []string{"api", "lark-group", "other"} {
		if got := boundedSource(valid); got != valid {
			t.Errorf("boundedSource(%q) = %q, want %q", valid, got, valid)
		}
	}
	// Unknown / arbitrary sources collapse to a single bounded value.
	for _, unknown := range []string{"req-id-7f3a9c", "", "../../etc"} {
		if got := boundedSource(unknown); got != "invalid" {
			t.Errorf("boundedSource(%q) = %q, want %q", unknown, got, "invalid")
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/handlers/ -run TestBoundedSource`
Expected: FAIL — `undefined: boundedSource`

- [ ] **Step 3: Implement `boundedSource` and use it at the error path**

In `internal/handlers/ingest.go`, change the validate_err recording line (currently
`metrics.IngestTotal.WithLabelValues(tenantID, in.Source, result).Inc()` inside the
`if err != nil` block):

```go
		metrics.IngestTotal.WithLabelValues(tenantID, boundedSource(in.Source), result).Inc()
```

Then add the helper at the end of the file:

```go
// boundedSource keeps attune_ingest_total's `source` label bounded to known
// sources. On the validate_err path we'd otherwise record the raw client value,
// an unbounded-cardinality vector (proposal #6). Mirrors how the JSON-decode and
// auth paths record "unknown".
func boundedSource(s string) string {
	if domain.ValidSources[s] {
		return s
	}
	return "invalid"
}
```

(The `ok` path at the end of `Ingest` keeps `in.Source` — validation has passed,
so it is always a `ValidSources` member.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/handlers/ -run TestBoundedSource -v`
Expected: PASS

- [ ] **Step 5: Build + vet**

Run: `go build ./... && go vet ./internal/handlers/`
Expected: no output (success)

- [ ] **Step 6: Commit**

```bash
git add internal/handlers/ingest.go internal/handlers/ingest_source_test.go
git commit -m "fix(handlers): bound ingest source label cardinality"
```

---

### Task 3: metrics.go doc accuracy + drift-guard test (TDD)

Fix the stale/jargon package doc and the two wrong label-value comments; extract
an `allMetrics` slice; replace the stale `TestRegistryHasAllFiveCoreMetrics` (it
checks only 5 of 7 and cites "design doc §3.7") with an exact-match drift-guard.

**Files:**
- Modify: `internal/infra/metrics/metrics.go`
- Modify: `internal/infra/metrics/metrics_test.go`

- [ ] **Step 1: Replace the package doc comment (lines 1-10)**

Replace the top block (`// Package metrics exposes attune's 5 core …` through the
`// recorders themselves.` line) with:

```go
// Package metrics exposes attune's 7 Prometheus metrics — the telemetry contract
// documented in observability/README.md. Any Prometheus-compatible backend can
// scrape them at /metrics (OpenMetrics).
//
// All metrics use the "attune_" prefix to namespace them on a shared scrape.
//
// One Registry singleton — no per-package globals, no init() side effects beyond
// registration. Handler() is the only public hook outside the metric recorders.
```

- [ ] **Step 2: Fix the two wrong label-value comments + drop design-doc/sprint refs**

In `EnrichDuration`'s comment, change `result ∈ {ok, llm_err, parse_err, db_err}`
→ `result ∈ {ok, llm_err, parse_err, other_err, db_err}` and replace
`p95 ≤ 30s per design doc §5.3.2` → `p95 SLO tracking (target p95 ≤ 30s)`.

In `NotifyFailuresTotal`'s comment, change `reason is the error class (terminal,
retryable, timeout, etc)` → `reason is the error class (transport | terminal)`.

In `TriageDecisionsTotal`'s comment, drop the `Sprint 1.3 (Y1 工程, 2026-05-18)
introduced the triage stage …` sentence and the `(v1 feature)` / `(Sprint 1.3
default)` parentheticals; keep the `ignore | fast | full` explanation.

- [ ] **Step 3: Extract `allMetrics` and use it in `init()`**

Replace the `func init() { Registry.MustRegister( … ) }` block with:

```go
// allMetrics is the registered set — the single source of truth that init()
// registers and the drift-guard test checks against the documented reference
// (observability/README.md). Add a metric here AND to that reference together.
var allMetrics = []prometheus.Collector{
	IngestTotal,
	EnrichDuration,
	NotifyFailuresTotal,
	OutboxLagSeconds,
	ClaimContentionTotal,
	IngestRateLimitTotal,
	TriageDecisionsTotal,
}

func init() {
	Registry.MustRegister(allMetrics...)
}
```

- [ ] **Step 4: Replace the stale test + add the enumeration helper**

In `internal/infra/metrics/metrics_test.go`, replace the import block with:

```go
import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)
```

Replace the whole `TestRegistryHasAllFiveCoreMetrics` function (its doc comment +
body) with:

```go
// TestRegisteredMetricsMatchDocumentedReference is the drift-guard: the metrics
// registered in metrics.go must exactly equal the catalog documented in
// observability/README.md. Add or rename a metric without updating the docs and
// this fails. (Names are a semver-stable contract — proposal #6.)
func TestRegisteredMetricsMatchDocumentedReference(t *testing.T) {
	// Mirror of observability/README.md's metrics reference (the 7 families).
	documented := map[string]bool{
		"attune_ingest_total":            true,
		"attune_enrich_duration_seconds": true,
		"attune_notify_failures_total":   true,
		"attune_outbox_lag_seconds":      true,
		"attune_claim_contention_total":  true,
		"attune_ingest_rate_limit_total": true,
		"attune_triage_decisions_total":  true,
	}

	got := registeredMetricNames(t)
	if len(got) != len(documented) {
		t.Fatalf("registered %d metrics, documented %d: %v", len(got), len(documented), got)
	}
	for _, name := range got {
		if !documented[name] {
			t.Errorf("metric %q is registered but missing from observability/README.md's reference", name)
		}
	}
}

// registeredMetricNames extracts the fully-qualified name of every collector in
// allMetrics via Describe (each emits one Desc), so it sees label-vec metrics
// that Gather() omits until first observation.
func registeredMetricNames(t *testing.T) []string {
	t.Helper()
	ch := make(chan *prometheus.Desc)
	go func() {
		defer close(ch)
		for _, c := range allMetrics {
			c.Describe(ch)
		}
	}()
	fqName := regexp.MustCompile(`fqName: "([^"]+)"`)
	var names []string
	for d := range ch {
		m := fqName.FindStringSubmatch(d.String())
		if m == nil {
			t.Fatalf("could not parse fqName from Desc: %s", d.String())
		}
		names = append(names, m[1])
	}
	return names
}
```

- [ ] **Step 5: Run the metrics tests**

Run: `go test ./internal/infra/metrics/ -v`
Expected: PASS (`TestRegisteredMetricsMatchDocumentedReference`, `TestHandlerServesPrometheusFormat`)

- [ ] **Step 6: Sanity-check the guard actually catches drift (manual, revert after)**

Temporarily append `, IngestTotal` a second time? No — instead temporarily add a
fake name to `documented` (e.g. `"attune_fake": true`) and run: the test must FAIL
with a count mismatch. Then remove the fake line. (Confirms the guard isn't a
no-op.)

Run: `go test ./internal/infra/metrics/ -run TestRegisteredMetricsMatchDocumentedReference`
Expected after temp edit: FAIL (registered 7, documented 8). Then revert.

- [ ] **Step 7: Build + vet + commit**

```bash
go build ./... && go vet ./internal/infra/metrics/
git add internal/infra/metrics/metrics.go internal/infra/metrics/metrics_test.go
git commit -m "fix(metrics): correct stale package doc + add drift-guard test"
```

---

### Task 4: Overlay files (`deploy/`)

**Files:**
- Create: `deploy/docker-compose.obs.yml`
- Create: `deploy/prometheus.yml`
- Create: `deploy/grafana-datasource.yml`
- Create: `deploy/grafana-dashboards.yml`

- [ ] **Step 1: Create `deploy/prometheus.yml`**

```yaml
# Prometheus scrape config for the attune observability overlay. Scrapes attune's
# /metrics over the compose network (service `attune`, port 8090) — independent of
# the host port bind. Default scrape path /metrics needs no metrics_path override.
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: attune
    static_configs:
      - targets: ["attune:8090"]
```

- [ ] **Step 2: Create `deploy/grafana-datasource.yml`**

```yaml
# Grafana datasource provisioning — Prometheus as the default datasource. The
# committed dashboard has no hardcoded datasource, so isDefault routes its panels
# here. uid is pinned so a future second datasource can't steal the default.
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    uid: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false
```

- [ ] **Step 3: Create `deploy/grafana-dashboards.yml`**

```yaml
# Grafana dashboard provider — loads every *.json mounted under the attune/
# provisioning dir (../observability/dashboards is bind-mounted there, read-only).
apiVersion: 1

providers:
  - name: attune
    orgId: 1
    folder: attune
    type: file
    disableDeletion: false
    allowUiUpdates: false
    updateIntervalSeconds: 30
    options:
      path: /etc/grafana/provisioning/dashboards/attune
      foldersFromFilesStructure: false
```

- [ ] **Step 4: Create `deploy/docker-compose.obs.yml`**

```yaml
# Optional observability overlay — Prometheus + Grafana for attune's /metrics.
#
# Usage (layer on top of the base stack):
#   docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d
#   # Grafana    → http://127.0.0.1:3000  (admin / $GF_SECURITY_ADMIN_PASSWORD)
#   # Prometheus → http://127.0.0.1:9090
#
# Tear down with the SAME -f flags (or add --remove-orphans) so the obs
# containers don't orphan:
#   docker compose -f docker-compose.yml -f docker-compose.obs.yml down
#
# This is ONE reference stack. attune exposes standard Prometheus/OpenMetrics at
# /metrics — point any compatible backend (VictoriaMetrics, OTel Collector, …) at
# it instead if you prefer. See ../observability/README.md.

services:
  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - attune-prom:/prometheus
    command:
      - --config.file=/etc/prometheus/prometheus.yml
      - --storage.tsdb.retention.time=15d
    # Loopback by default — Prometheus exposes ALL your metrics, unauthenticated.
    # Front with a proxy or set ATTUNE_BIND=0.0.0.0 to publish.
    ports:
      - "${ATTUNE_BIND:-127.0.0.1}:${PROMETHEUS_PORT:-9090}:9090"
    restart: unless-stopped
    security_opt:
      - no-new-privileges:true
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:9090/-/healthy"]
      interval: 15s
      timeout: 5s
      retries: 5
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

  grafana:
    image: grafana/grafana:latest
    depends_on:
      - prometheus
    volumes:
      - ../observability/dashboards/:/etc/grafana/provisioning/dashboards/attune/:ro
      - ./grafana-datasource.yml:/etc/grafana/provisioning/datasources/prometheus.yml:ro
      - ./grafana-dashboards.yml:/etc/grafana/provisioning/dashboards/attune.yml:ro
      - attune-grafana:/var/lib/grafana
    # Only Grafana's own vars — NOT env_file: .env (that would inject attune's
    # DB/LLM secrets into this container).
    environment:
      GF_SECURITY_ADMIN_PASSWORD: ${GF_SECURITY_ADMIN_PASSWORD:-admin}
      GF_USERS_ALLOW_SIGN_UP: "false"
    ports:
      - "${ATTUNE_BIND:-127.0.0.1}:${GRAFANA_PORT:-3000}:3000"
    restart: unless-stopped
    security_opt:
      - no-new-privileges:true
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:3000/api/health"]
      interval: 15s
      timeout: 5s
      retries: 5
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

volumes:
  attune-prom:
  attune-grafana:
```

- [ ] **Step 5: Verify base + overlay merge & parse**

Run: `cd deploy && docker compose -f docker-compose.yml -f docker-compose.obs.yml config -q; cd ..`
Expected: no output, exit 0. (If Docker isn't available locally, the CI job in
Task 7 covers this — note it in the PR.)

- [ ] **Step 6: Commit**

```bash
git add deploy/docker-compose.obs.yml deploy/prometheus.yml deploy/grafana-datasource.yml deploy/grafana-dashboards.yml
git commit -m "feat(deploy): add prometheus + grafana observability overlay (#6)"
```

---

### Task 5: Jargon cleanup — dashboard + observability/ contract doc

**Files:**
- Modify: `observability/dashboards/attune-overview.json`
- Modify: `observability/README.md`

- [ ] **Step 1: De-jargon the dashboard (title + tags only; uid + panels untouched)**

In `observability/dashboards/attune-overview.json`:
- `"title": "Attune Overview (Wave 1.2)",` → `"title": "Attune Overview",`
- `"tags": ["attune", "wave1.2"],` → `"tags": ["attune"],`

- [ ] **Step 2: Verify the JSON still parses + uid intact**

Run: `jq -e '.uid == "attune-overview" and .title == "Attune Overview"' observability/dashboards/attune-overview.json`
Expected: `true`

- [ ] **Step 3: Rewrite `observability/README.md` as the contract doc**

Replace the entire file with:

```markdown
# attune observability

attune ships its own observability **contract** so it can deploy self-contained.
Prometheus/Grafana is just one reference stack — the stable surface is the metrics
exposition plus the portable assets in this directory.

## Layers

- **Exposition contract** — attune serves Prometheus/OpenMetrics at
  `:8090/metrics` (no app-level auth; restrict via your proxy / internal network).
  Any compatible backend scrapes it: Prometheus, VictoriaMetrics (`vmagent`),
  Grafana Agent, the OpenTelemetry Collector (`prometheusreceiver`), Datadog's
  OpenMetrics check. Metric names + labels are a stable contract — renaming one is
  a breaking change.
- **Portable assets (this dir)** — backend-agnostic:
  - `dashboards/*.json` — Grafana dashboards with **no hardcoded datasource**, so
    they render against whatever default Prometheus-compatible datasource you
    provision (our bundled Prometheus, your VictoriaMetrics, …).
  - `targets.yaml` — a `file_sd_configs` target list for an **external**
    Prometheus/VictoriaMetrics reading `127.0.0.1:8090` (the standalone-host case).
- **Reference runtime** — `../deploy/docker-compose.obs.yml` bundles Prometheus +
  Grafana and auto-provisions the datasource + dashboards. Its `prometheus.yml`
  targets the compose service `attune:8090` (not `targets.yaml`). Bring your own
  backend instead by pointing it at `/metrics`.

## Metrics reference

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `attune_ingest_total` | counter | `tenant`, `source`, `result` | ingest API requests |
| `attune_enrich_duration_seconds` | histogram | `tenant`, `result` | end-to-end AI enrichment latency |
| `attune_notify_failures_total` | counter | `destination_type`, `reason` | notifier push failures |
| `attune_outbox_lag_seconds` | gauge | — | age of the oldest pending outbox row (0 = empty) |
| `attune_claim_contention_total` | counter | — | enricher `tryClaim` lost to another worker |
| `attune_ingest_rate_limit_total` | counter | `tenant` | ingest requests rejected (429) by the rate limiter |
| `attune_triage_decisions_total` | counter | `tenant`, `decision` | triage-stage routing decisions |

Label values:

- `source` — one of `api`, `lark-group`, `lark-bitable`, `lark-approval`,
  `lark-helpdesk`, `lark-form`, `email`, `web`, `other`; or `invalid` when a
  request's source failed validation.
- ingest `result` — `ok` · `validate_err` · `auth_err` · `internal_err`.
- enrich `result` — `ok` · `llm_err` · `parse_err` · `other_err` · `db_err`.
- `destination_type` — `lark-pool` · `lark-radar` · `raw-webhook`.
- `reason` — `transport` · `terminal`.
- `decision` — `ignore` · `fast` · `full`.

The registered set is drift-guarded by `internal/infra/metrics/metrics_test.go` —
it must match this table.

## Add a dashboard

Drop `dashboards/<name>.json` here. Prefix the name with the service to avoid
clashing on a shared Grafana. Keep panels datasource-less so they stay portable.

## Add a scrape target

For external multi-instance setups, add entries to `targets.yaml` and point your
Prometheus/VictoriaMetrics `file_sd_configs` at it.
```

- [ ] **Step 4: Verify no roadmap jargon remains on the contract surfaces**

Run: `grep -rin wave observability/ deploy/docker-compose.obs.yml internal/infra/metrics/metrics.go`
Expected: no matches (exit 1).

- [ ] **Step 5: Commit**

```bash
git add observability/dashboards/attune-overview.json observability/README.md
git commit -m "docs(observability): contract doc + metrics reference, drop Wave jargon"
```

---

### Task 6: `.env.example` + deploy/README overlay section

**Files:**
- Modify: `deploy/.env.example`
- Modify: `deploy/README.md`

- [ ] **Step 1: Add obs vars to `deploy/.env.example`**

Insert after the `# ── Image / port ──` block (after the
`# ATTUNE_IMAGE=ghcr.io/phixsura/attune:v0.2.0` line):

```bash

# ── Observability overlay (docker-compose.obs.yml — optional) ────────────
# Only used when you layer the obs overlay:
#   docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d
# Grafana admin password. Empty ⇒ Grafana falls back to admin/admin and forces a
# change on first login. SET THIS for any shared/persistent deploy.
GF_SECURITY_ADMIN_PASSWORD=
# Host ports for the obs UIs (bound to ATTUNE_BIND, loopback by default).
# GRAFANA_PORT=3000
# PROMETHEUS_PORT=9090
```

- [ ] **Step 2: Add an Observability section to `deploy/README.md`**

Insert between `## 3. Create the first tenant + API key` (end of its curl block)
and `## Operations`:

```markdown
## 4. Observability (optional overlay)

Layer Prometheus + Grafana on top to see attune's metrics — zero manual setup:

```bash
docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d
```

- **Grafana** → http://127.0.0.1:3000 — log in as `admin` with
  `GF_SECURITY_ADMIN_PASSWORD` (set it in `.env`). The "Attune Overview" dashboard
  is auto-loaded.
- **Prometheus** → http://127.0.0.1:9090 — `Status → Targets` shows the `attune`
  job UP.

Tear down with the **same** `-f` flags (or add `--remove-orphans`) so the obs
containers don't orphan:

```bash
docker compose -f docker-compose.yml -f docker-compose.obs.yml down
```

> **Exposure:** Grafana and Prometheus bind `127.0.0.1` by default; Prometheus
> exposes *all* your metrics unauthenticated. Front them with your proxy (or set
> `ATTUNE_BIND=0.0.0.0` only behind one). attune's own `/metrics` is likewise
> unauthenticated — keep `:8090` off the public internet.
>
> The `attune_enrich_duration_seconds` panels stay empty until enrichment runs
> against a real (or mock/ollama) LLM — see `FEEDBACK_API_LLM_OPENAI_BASE_URL` in
> `.env.example`.

attune exposes standard Prometheus/OpenMetrics — to use VictoriaMetrics, the
OpenTelemetry Collector, or another backend instead, point it at `/metrics` (see
[`../observability/README.md`](../observability/README.md)).
```

- [ ] **Step 3: Commit**

```bash
git add deploy/.env.example deploy/README.md
git commit -m "docs(deploy): document the observability overlay + obs env vars"
```

---

### Task 7: CI compose-config smoke check

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add a `deploy` output to the `changes` job**

In the `changes` job's `outputs:` map, after
`changelog: ${{ steps.filter.outputs.changelog }}`, add:

```yaml
      deploy: ${{ steps.filter.outputs.deploy }}
```

- [ ] **Step 2: Add a `deploy` filter to the `dorny/paths-filter` `filters:` block**

After the `changelog:` filter entry, add:

```yaml
            deploy:
              - 'deploy/**'
              - '.github/workflows/ci.yml'
```

- [ ] **Step 3: Add the `compose-config` job**

Append a new job to `jobs:` (after the last existing job):

```yaml
  # ── Deploy: docker-compose base + obs overlay parse ────────────────────────
  compose-config:
    needs: changes
    if: needs.changes.outputs.deploy == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
        with:
          persist-credentials: false
      - name: Validate base + obs overlay merge & parse
        working-directory: deploy
        run: docker compose -f docker-compose.yml -f docker-compose.obs.yml config -q
```

- [ ] **Step 4: Lint the workflow YAML**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo OK`
Expected: `OK` (valid YAML). If `actionlint` is installed, run it too.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: validate docker-compose base + obs overlay parse on deploy changes"
```

---

### Task 8: CHANGELOG + full verification + PR

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add entries under `## [Unreleased]`**

Insert between `## [Unreleased]` and `## [0.2.0] - 2026-06-05`:

```markdown

### Added

- **Observability overlay** (`deploy/docker-compose.obs.yml`) — optional
  Prometheus + Grafana stack layered with
  `-f docker-compose.yml -f docker-compose.obs.yml`. Auto-provisions the
  Prometheus datasource and the "Attune Overview" dashboard, and documents the
  `attune_*` metrics as a backend-agnostic contract in `observability/README.md`.
- CI: a `deploy/**`-filtered `docker compose config` smoke check.

### Changed

- Renamed the bundled Grafana dashboard "Attune Overview (Wave 1.2)" → "Attune
  Overview" and removed internal roadmap jargon from `observability/` and the
  `metrics` package doc (no metric names changed).

### Security

- Bounded the `source` label on `attune_ingest_total`: a rejected (invalid)
  client-supplied `source` is now recorded as `invalid` instead of the raw value,
  closing an unbounded metric-cardinality vector on the ingest validation-error
  path.
```

- [ ] **Step 2: Full quality-gate verification**

Run:
```bash
go build ./... && go vet ./... && go test -short ./... && \
( cd deploy && docker compose -f docker-compose.yml -f docker-compose.obs.yml config -q )
```
Expected: all pass / no output. (Skip the compose line if Docker is unavailable
locally; CI covers it.)

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): observability overlay + cardinality fix (#6)"
```

- [ ] **Step 4: Push + open the PR**

```bash
git push -u origin feat/observability-overlay
gh pr create --title "feat(deploy): observability overlay (prometheus + grafana) (#6)" \
  --body "Closes #6. See docs/proposals/2026/06/2026-06-05-observability-overlay.md. <fill: verification results>"
```

- [ ] **Step 5: Manual stack verification (record results in the PR)**

With Docker running, from `deploy/`:
```bash
docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d
```
Then confirm:
- `curl -s localhost:9090/api/v1/targets | jq '.data.activeTargets[].health'` → `"up"` for the `attune` job.
- Grafana at http://127.0.0.1:3000 — admin login works; `GET /api/dashboards/uid/attune-overview` returns the dashboard; datasource health OK; uid is `prometheus`.
- Ingest a row with a valid key + **invalid** `source` → `attune_ingest_total{source="invalid"}` (not the raw value).
- Tear down with the same `-f … -f …`; no orphaned containers.

---

## Self-Review

**Spec coverage (vs proposal):** overlay files ✓ (T4) · loopback/hardening/healthchecks/`apiVersion: 1`/uid pin/`depends_on` ✓ (T4) · jargon cleanup dashboard+README+metrics.go ✓ (T3,T5) · metrics reference + drift-guard ✓ (T3,T5) · `/metrics` exposure docs ✓ (T6) · CI smoke check ✓ (T7) · enrich runbook ✓ (T6) · `.env` vars ✓ (T6) · cardinality fix + test ✓ (T2) · CHANGELOG Added/Changed/Security + MINOR ✓ (T8) · `Closes #6` ✓ (T8). No gaps.

**Placeholder scan:** the only `<fill: …>` is the PR body verification results, filled at T8/Step 5 from real output. No TBD/TODO in code or config.

**Type/name consistency:** `boundedSource` (T2) used + defined together; `allMetrics` defined in metrics.go (T3 Step 3) and referenced by `registeredMetricNames` (T3 Step 4); documented metric set in the drift-guard (T3) == the `observability/README.md` table (T5) == the CHANGELOG (no name changes). uid `attune-overview` preserved (T5) and asserted (T8). Datasource `uid: prometheus` consistent across grafana-datasource.yml (T4) and verification (T8).
