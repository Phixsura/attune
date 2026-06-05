# Proposal — `docker-compose.obs.yml` Prometheus + Grafana overlay

| | |
|---|---|
| **Issue** | #6 |
| **Status** | Accepted (2026-06-05) |
| **Started** | 2026-06-05 19:26 CST |
| **Rethought** | 2026-06-05 (superpowers:brainstorming + codebase verification — open questions resolved, load-bearing assumptions checked in code) |
| **Related** | #5 (main compose — must exist first; ✅ shipped in v0.2.0) · #7 (docs) · #14 (image) |

## Problem

The v0.2.0 deploy kit (#5) gives operators a running attune + postgres, and
attune already exposes Prometheus metrics at `/metrics` plus a base Grafana
dashboard under `observability/dashboards/`. But there's no turnkey way to
*see* those metrics — operators must stand up and wire Prometheus + Grafana
themselves. This is the "zero-config monitoring" half of the v0.3 milestone.

## Value & audience (honest)

**What it is.** Friction reduction, not new capability — every metric is already
scrapeable today, so this only collapses *an afternoon of Grafana plumbing*
(stand up Prometheus, write a scrape config, provision a datasource, import + map
the dashboard) into one `-f` flag. It's the last mile of already-sunk cost: the
instrumentation and the dashboard already exist; without this they stay invisible.

**Who benefits.** Self-hosters **without** an existing monitoring stack — dev,
evaluation, small private deploys. Operators who already run Prometheus/Grafana
won't use the overlay; they point their stack at `/metrics` (what `targets.yaml`
is for). The audience is narrower than "all operators," and that's fine.

**What it is *not*.** Production-grade monitoring. `:latest` images, a single-node
local TSDB, no alerting/HA/remote-write — serious production runs managed
Prometheus/Datadog. This is a "see it's working" tool for the first five minutes,
not the monitoring strategy.

**Why it's worth it now.** A v0.3 "可装/Deployable" milestone gate (P1,
`pillar/enterprise`): for a self-host/enterprise story, *"you can see it's
working"* is table stakes — an operator won't trust a feedback pipeline in prod
without the outbox-lag / notify-failure / enrich-latency signals. High leverage
(cheap connective tissue on existing investment), tiny risk surface — no
production Go change (just a comment cleanup + a drift-guard test), no new runtime
deps.

**Scope consequence.** Because the value is friction-reduction for the
batteries-included case, the YAGNI cuts (no alerting/HA/remote-write, `:latest`,
single TSDB, no per-backend overlays) are *correct*, not gaps — investing past
"see it working" would be gold-plating a convenience layer.

## Goals

- An **optional overlay** that stacks Prometheus + Grafana on the main compose:
  `docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d`.
- Prometheus scrapes `attune:8090/metrics` over the compose network.
- Grafana auto-provisions the Prometheus datasource **and** auto-loads
  `observability/dashboards/*.json` — the "Attune Overview" dashboard appears
  with no manual import.
- Admin login gated by an env password; persistent data for both.
- Secure-by-default, consistent with #5 — including an explicit posture for
  attune's **unauthenticated** `/metrics`.
- **No internal jargon on the contract surfaces** — clean the "Wave" roadmap leak
  out of the dashboard, `observability/README.md`, and the `metrics.go` package
  doc (which also miscounts "5 core" when there are 7) (CLAUDE.md §1).
- A **CI smoke check** so a broken overlay can't merge (quality-gated repo).
- **Observability as a layered contract, not a bundled monolith** — the overlay
  is *one reference stack*; the stable seam is `/metrics` + the portable
  `observability/` assets, so any Prometheus-compatible backend (VictoriaMetrics,
  OTel Collector, …) plugs in without an attune-specific overlay.

## Non-goals

- **Tracing / OTel collector** (Tempo/Jaeger) — the issue is metrics-only; a
  traces overlay can come later.
- **Alerting** (Alertmanager, alert rules) — dashboards + scrape only.
- **Prometheus HA / remote-write / long-term storage** — single local TSDB,
  15d retention.
- **HA / autoscaling / alerting / remote-write** for prom/grafana — the
  operator's real monitoring stack owns these (consume the contract instead).
  *Memory limits are set* to bound the co-location OOM risk (see Resolved
  decisions) — that's the one "production" guard that belongs in a co-located
  reference stack.
- **Prometheus self-monitoring** — the overlay scrapes attune only, not
  prom/grafana's own `/metrics`.
- **Per-backend overlays** (VictoriaMetrics / Datadog / …) or bundling
  alternative TSDBs — other backends consume the contract (§ "layered contract")
  and bring their own runtime via their own `-f` overlay.
- **A code-level metrics plugin system or jsonnet/mixin tooling** — the standard
  `/metrics` exposition is already the vendor-neutral seam; plain portable JSON
  suits a single service. Adding a plugin layer would be capability-free
  complexity.
- **Docs/tutorial** — #7; this ships a short pointer in `deploy/README.md`.

## Key finding (de-risks the whole thing)

`observability/dashboards/attune-overview.json` has **no `datasource` field on
any panel or target**, an empty `templating.list`, and no `__inputs` export
block (verified by reading the file). So its panels resolve against Grafana's
**default datasource** at query time. Therefore provisioning Prometheus as the
default datasource (`isDefault: true`) makes the dashboard "just work" — **no
datasource-uid injection, no input mapping, no edits to the committed JSON's
panels.**

### Verified against the codebase (not just asserted)

Three load-bearing assumptions, each checked in code rather than assumed:

| Assumption | Evidence | Result |
|---|---|---|
| Prometheus can scrape `/metrics` (no 401) | `cmd/attune/router.go:54-56` — `r.Handle("/metrics", …)` is on the **root** router; api-key auth is scoped to the `/v1` group only | ✅ **`/metrics` has no app-level auth** |
| Scrape target `attune:8090` is right | `internal/infra/config/config.go:154` (Port defaults 8090) + `cmd/attune/server.go:129` (single `:Port` listener) | ✅ `/metrics` shares the API port; target correct |
| Every panel query has a backing metric | `internal/infra/metrics/metrics.go` registered names + label sets vs all 8 panel `expr`s | ✅ the 8 panels use 5 of the 7 registered families, names+labels all match (`attune_ingest_total{tenant,source,result}`, `attune_enrich_duration_seconds_bucket{le,…}`, `attune_notify_failures_total{destination_type,reason}`, `attune_outbox_lag_seconds`, `attune_claim_contention_total`) — see Metrics reference |

## Observability as a layered contract (the abstraction)

The overlay is **one reference stack**, not "attune's observability." Top OSS
converges on separating the *stable contract* from *one runtime that consumes
it*; attune adopts the same shape, scaled to a single Go service (no jsonnet, no
plugin layer). Three layers, with explicit ownership and a stable seam:

**Layer 1 — Exposition contract (vendor-neutral; the real API).** attune exposes
Prometheus/OpenMetrics at `/metrics`. This is the seam every backend already
speaks — Prometheus, VictoriaMetrics (`vmagent`), Grafana Agent, OTel Collector
(`prometheusreceiver`), Datadog OpenMetrics. **We add no per-backend code; the
standard exposition format *is* the abstraction** (OpenTelemetry/OpenMetrics
"instrument once, export anywhere"). To make the contract real rather than
implicit, ship a **metrics reference** — a catalog of every `attune_*` metric
(name · type · labels · meaning), the way HashiCorp Vault ships a "Telemetry
reference: all metrics" page. Consequence: metric names/labels become a
**semver-stable surface** (rename = breaking; follow Prometheus naming —
`attune_<subsystem>_<name>_<unit>`, `_total`, base units).

**Layer 2 — Portable asset bundle (the "mixin" idea, scaled down).**
`observability/` holds backend-agnostic assets: datasource-agnostic dashboards
(the no-`datasource`-field finding is exactly what makes them portable across
Prometheus *and* a VictoriaMetrics-as-datasource), `targets.yaml` (the
bring-your-own-scraper seam), and a README that **is** the contract doc. This
mirrors the Prometheus *monitoring-mixins* convention (dashboards + alerts +
rules published alongside the code, selectors not hardcoded) — but we keep plain
JSON and **no jsonnet/mixtool** (that tooling is for multi-team platforms like
kube-prometheus; over-engineering here). Alerts/recording rules are a future
addition to this same bundle (currently a non-goal).

**Layer 3 — Reference runtime (one optional implementation).**
`deploy/docker-compose.obs.yml` (prom+grafana), explicitly labeled
*reference/example*, layered via `-f`. Other backends bring their own runtime
against Layers 1–2; the `-f` overlay mechanism **is** the extension point — an
operator writes `docker-compose.obs-vm.yml` and reuses Layers 1–2 unchanged.

**The directory boundary encodes the abstraction:** `observability/` = portable,
backend-agnostic, ours to keep stable (contract + assets); `deploy/` = runtime
wiring, one reference implementation. That split is the structural statement that
"Prometheus/Grafana is just one component."

### Metrics reference (the Layer 1 contract surface)

The catalog #6 ships in `observability/README.md`, derived from
`internal/infra/metrics/metrics.go` (served as **OpenMetrics** —
`EnableOpenMetrics: true`, the CNCF standard VictoriaMetrics/OTel/Datadog all
consume):

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `attune_ingest_total` | counter | `tenant`,`source`¹,`result`² | ingest API requests |
| `attune_enrich_duration_seconds` | histogram (0.5–64s) | `tenant`,`result`³ | end-to-end enrichment latency |
| `attune_notify_failures_total` | counter | `destination_type`⁴,`reason`⁵ | notifier push failures |
| `attune_outbox_lag_seconds` | gauge | — | age of oldest pending outbox row (0 = empty) |
| `attune_claim_contention_total` | counter | — | enricher `tryClaim` lost to another worker |
| `attune_ingest_rate_limit_total` | counter | `tenant` | ingest requests rejected 429 by the rate limiter |
| `attune_triage_decisions_total` | counter | `tenant`,`decision`⁶ | triage routing decisions |

¹`domain.ValidSources` on success — **but the raw client value on `validate_err` (see Risks)**
²`ok·validate_err·auth_err·internal_err` ³`ok·llm_err·parse_err·other_err·db_err`
⁴`lark-pool·lark-radar·raw-webhook` ⁵`transport·terminal` ⁶`ignore·fast·full`

**Naming/cardinality audit — code-verified (not comment-sourced).** Names all
follow Prometheus conventions (`_total` counters, `_seconds` base unit); none need
renaming before freezing (a later rename is a §3 flagged-breaking change). Label
*values* were read from the call sites — which caught **two wrong code comments**
(`reason` is `{transport,terminal}`, not the comment's "terminal/retryable/
timeout"; enrich `result` includes `other_err`). All labels are bounded enums or
`tenant` **except one footgun**: `attune_ingest_total{source}` on the
`validate_err` path records the **raw client-supplied `source`**
(`internal/handlers/ingest.go:66`) → unbounded cardinality (see Risks).

**Coverage gap (documented, not fixed here):** the committed dashboard charts 5
of the 7 — `attune_ingest_rate_limit_total` and `attune_triage_decisions_total`
have no panel (the triage one even has a stated PM use). Candidate panels for a
follow-up; #6 does **not** expand the dashboard beyond the title/tags jargon fix.

**Drift-guard test (in scope):** a small test that gathers the registry's
metric-family names and asserts they match the documented set, so the reference
can't silently drift from `metrics.go` (there's already `metrics_test.go` + a
`SetRegistry` hook). Cheap; keeps the Layer-1 contract honest.

## Proposal

### Overlay mechanism — separate `-f` file (not compose `profiles:`)

Ship a standalone `deploy/docker-compose.obs.yml`, layered with
`-f docker-compose.yml -f docker-compose.obs.yml`. This matches the issue spec,
is what #7's docs reference, keeps the obs services out of the default deploy
(monitoring isn't forced on the minimal "5-minute setup"), and is self-contained
/ discoverable. (Profiles considered and rejected — see Alternatives.)

### Files (all under `deploy/`)

- **`docker-compose.obs.yml`** — `prometheus` + `grafana` services + two named
  volumes (`attune-prom`, `attune-grafana`).
- **`prometheus.yml`** — one static scrape job, target `attune:8090`.
- **`grafana-datasource.yml`** — `apiVersion: 1`; provisions Prometheus as the
  **default** datasource, fixed `uid: prometheus`, url `http://prometheus:9090`.
- **`grafana-dashboards.yml`** — `apiVersion: 1`; a file provider pointing at
  `/etc/grafana/provisioning/dashboards/attune/`.

> Both Grafana provisioning YAMLs **require the top-level `apiVersion: 1`** key —
> omitting it makes Grafana skip/err the provisioning file at boot.

### `prometheus` service

- `image: prom/prometheus:latest`
- `./prometheus.yml:/etc/prometheus/prometheus.yml:ro`
- `command: --config.file=… --storage.tsdb.retention.time=15d` (no
  `--web.enable-admin-api` / `--web.enable-lifecycle` — keep the surface minimal)
- volume `attune-prom:/prometheus`; `restart: unless-stopped`
- published to **loopback** by default (see Security posture)
- HTTP healthcheck against `/-/healthy`

`prometheus.yml` — `scrape_interval: 15s`, one job:
```yaml
scrape_configs:
  - job_name: attune
    static_configs:
      - targets: ["attune:8090"]
```
(`attune` resolves on the compose network; the scrape is **independent of the
host port bind** — it works even if attune isn't published to the host. Default
scrape path `/metrics` needs no `metrics_path` override.)

### `grafana` service

- `image: grafana/grafana:latest`
- mounts: `../observability/dashboards/:/etc/grafana/provisioning/dashboards/attune/:ro`,
  `./grafana-datasource.yml:/etc/grafana/provisioning/datasources/prometheus.yml:ro`,
  `./grafana-dashboards.yml:/etc/grafana/provisioning/dashboards/attune.yml:ro`
- volume `attune-grafana:/var/lib/grafana`; `restart: unless-stopped`
- admin password + sign-up off via `environment:` (see Security posture)
- `depends_on: prometheus` (datasource is up before first query) — soft ordering
- published to **loopback** by default
- HTTP healthcheck against `/api/health`

`grafana-datasource.yml`: Prometheus, `url: http://prometheus:9090`,
`uid: prometheus`, `isDefault: true`. `grafana-dashboards.yml`: a provider with
`options.path: /etc/grafana/provisioning/dashboards/attune`, a folder name, and
`foldersFromFilesStructure: false`.

### Security posture (adopted from #5 — not optional deviations)

- **attune `/metrics` is unauthenticated by design.** `cmd/attune/router.go:54`
  says it outright: *"Restrict to internal CIDR via nginx in production — no auth
  at the Go level."* In-compose the `prometheus → attune:8090` scrape is exactly
  that internal path, so the overlay is fine. But #6 operationalizes scraping, so
  the docs must state it: under #5's loopback default any host-local process can
  `curl 127.0.0.1:8090/metrics`; if the operator sets `ATTUNE_BIND=0.0.0.0`,
  `/metrics` (all metrics) is exposed publicly with no auth → **front it with
  your proxy / firewall.** `deploy/README.md` carries this warning.
- **Loopback bind by default** for the obs UIs too. Grafana (admin UI) and
  Prometheus (all your metrics) are sensitive; reuse #5's `ATTUNE_BIND` (default
  `127.0.0.1`) → `${ATTUNE_BIND:-127.0.0.1}:${GRAFANA_PORT:-3000}:3000` (and
  likewise for Prometheus). Front with a proxy or set `ATTUNE_BIND=0.0.0.0`.
- **Grafana gets an `environment:` block, not `env_file: .env`.** `env_file`
  would inject attune's DB/LLM secrets into the Grafana container. Instead pass
  only `GF_SECURITY_ADMIN_PASSWORD` (from `.env` via `${…}` interpolation) +
  `GF_USERS_ALLOW_SIGN_UP=false`. Prometheus needs no env.
- **`security_opt: [no-new-privileges:true]` + log-size caps** on both (as with
  #5's postgres). No `read_only`/`cap_drop:ALL` — both write to their data dirs.
- **`:latest` as the issue specifies, but `.env.example` documents digest /
  version pinning** (same guidance as #5's images).
- **Datasource `uid: prometheus` pinned** (still `isDefault: true`) so a future
  second datasource can't silently steal the default.
- **HTTP healthchecks** on prom (`/-/healthy`) and grafana (`/api/health`) so
  `docker compose ps` reflects real readiness.

### CI smoke check

`.github/workflows/ci.yml` gates jobs via a `changes` (dorny/paths-filter) job
with `go`/`console`/`code`/`changelog` outputs — **no `deploy` filter today**, and
no compose validation. Two small additions:

1. add a `deploy` filter+output to the `changes` gate (`deploy/**` +
   `.github/workflows/ci.yml`, matching the existing pattern);
2. a new `compose-config` job — `needs: changes`, `if: …outputs.deploy == 'true'`
   — running:
   ```bash
   docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml config -q
   ```

This proves base + overlay **merge and parse** on every PR that touches `deploy/`
— cheap, and it can't go stale because it lives with the files it guards.

### Operator docs (`deploy/README.md` section)

A short "Observability overlay" section covering: the `-f … -f …` up command,
Grafana URL + first-login (env password), where the auto-loaded dashboard lives,
the **`/metrics` exposure warning** above, **tear-down with the same `-f … -f …`
(or `--remove-orphans`)** so prom/grafana don't orphan, and the **enrich-panel
runbook** (below).

### New `.env` vars (added to `.env.example`)

`GF_SECURITY_ADMIN_PASSWORD` (admin login), `GRAFANA_PORT` (3000),
`PROMETHEUS_PORT` (9090). `GF_USERS_ALLOW_SIGN_UP=false` is set in-compose.

Grafana behavior note: if `GF_SECURITY_ADMIN_PASSWORD` is empty, Grafana falls
back to `admin/admin` and forces a change on first login. `.env.example` ships
it with a clear "set me" note.

### Jargon cleanup (CLAUDE.md §1)

- `observability/dashboards/attune-overview.json` *(operator-visible)*: `title` →
  `"Attune Overview"`; drop `"wave1.2"` from `tags`. **`uid` stays
  `attune-overview`** — provisioning and the verification check
  (`GET /api/dashboards/uid/attune-overview`) depend on it.
- `observability/README.md` *(operator-visible)*: reword the "Wave 3+ 多 attune
  实例" line to a neutral "future multi-instance" phrasing, **and add one line
  distinguishing the two scrape sources** — `targets.yaml` is for an external
  VictoriaMetrics/Prometheus reading `127.0.0.1:8090` (standalone host), while the
  overlay's `prometheus.yml` targets the compose service `attune:8090`.
- `internal/infra/metrics/metrics.go` package doc *(contributor-visible; confirmed
  in scope)*: it is **stale and jargon-laden** — claims "**5 core**
  metrics" (there are **7**), says dashboards "land in **Wave 2**" (already
  shipped — self-contradictory), and cites an internal "v0.4 design doc §3.7" /
  "the main backend". Fix the count, drop the roadmap/design-doc/"main backend"
  refs, de-jargon per-metric comments ("Sprint 1.3 (Y1 工程…)", "design doc
  §5.3.2"), **and correct the two wrong label-value comments** (`reason` →
  `{transport,terminal}`; enrich `result` add `other_err`). **The exposed `Help`
  strings are already clean — leave them.**
- Grep `Wave`/roadmap jargon over `observability/`, `deploy/`, and
  `internal/infra/metrics/metrics.go` during implementation.

## Resolved decisions (rethink traceability)

| Item | Decision | Rationale |
|---|---|---|
| D1 bind | **loopback by default** | obs UIs are sensitive; match #5 |
| D2 Grafana env | **`environment:` (GF_* only), no `env_file`** | avoid DB/LLM secret sprawl into Grafana |
| D3 hardening | **`no-new-privileges` + log caps, no `read_only`** | match #5; both need writable data dirs |
| D4 image tags | **pin versions in-file** (prometheus v3.12.0, grafana 13.0.2) | revisited under the production lens — top reference stacks (dockprom, otel-demo) pin; `:latest` is testing-only. Pinned to the live-verified versions |
| resource limits | **memory caps (512M) on prom/grafana + `retention.size=2GB`** | otel-demo sets limits "for stability on local machines" — same co-location risk; stops a runaway Prometheus from OOMing attune/postgres |
| prod framing | **README: this is a reference/dev stack; prod scrapes `/metrics` from a *separate* backend** | monitoring shouldn't co-locate with the app it watches; benchmarked vs top compose stacks |
| Q1 decision surface | **adopt secure defaults, don't manufacture forks** | decisiveness over choice theater |
| Q2 jargon | **clean dashboard + `observability/README.md` in #6** | §1 leak operators would see |
| Q3 mechanism | **separate `-f obs.yml`** | issue spec; #7 docs reference it; self-contained |
| Q4 datasource uid | **pin `uid: prometheus`** | cheap robustness vs future default-steal |
| Q5 all-in-one | **explicit prom+grafana, not `otel-lgtm`** | transparent/customizable for "scrape my /metrics" |
| healthchecks | **add HTTP healthchecks to both** | readiness visible in `compose ps` |
| `/metrics` posture | **document unauthenticated `/metrics` + proxy guidance** | code says "no auth at the Go level"; §8 baseline |
| `apiVersion: 1` | **required in both provisioning YAMLs** | Grafana skips/errs provisioning without it |
| CI check | **add `compose config` smoke job (filtered on `deploy/**`)** | quality-gated repo; broken overlay can't merge |
| acceptance #4 | **add an enrich-panel runbook (ollama/mock)** | turn "can't fully verify" into reproducible |
| issue close | **implementing PR uses `Closes #6`** | align to §10 (vs #5's hand-close) |
| extensibility | **document the contract as a layered abstraction; no per-backend overlays** | seam is `/metrics` + portable assets; `-f` is the extension point |
| metrics reference | **ship an `attune_*` metrics catalog (Layer 1)** | makes the contract real (Vault-style telemetry reference) |
| jargon scope+ | **extend cleanup to `metrics.go` package doc** *(confirmed)* | stale "5 core" (really 7) + Wave/design-doc/"main backend" leaks in the contract's canonical doc |
| drift-guard | **test: registry families == documented set** *(confirmed)* | keeps the reference honest vs `metrics.go` |
| source cardinality | **fix in #6: record `"invalid"` for non-`ValidSources` on the error path** | unbounded `attune_ingest_total{source}` series via authenticated input (`ingest.go:66`) |

## Alternatives considered

- **compose `profiles:`** (`docker compose --profile obs up`, single file).
  *Rejected* — deviates from the issue spec and #7's documented `-f` usage, and
  folds the obs service definitions into the main compose file. The separate
  overlay is more discoverable and keeps the two concerns cleanly split.
- **Inject a datasource uid into the dashboard JSON** (edit the committed file or
  template it). *Rejected* — unnecessary given the no-datasource finding;
  default-datasource provisioning is simpler and keeps the JSON's panels
  untouched (we only touch title/tags for the jargon fix).
- **Reuse `observability/targets.yaml` via `file_sd_configs`** instead of a
  static target. *Rejected* — that file targets `127.0.0.1:8090` (the standalone
  host case); in-compose the target is the service name `attune:8090`. A static
  one-line scrape config is clearer for the overlay.
- **Fold prom+grafana into the main `docker-compose.yml`.** *Rejected* — the
  issue wants an optional overlay; monitoring shouldn't be forced on every deploy
  or count against the minimal "5-minute setup."
- **All-in-one `grafana/otel-lgtm`.** *Rejected* — elegant for demos but opaque;
  explicit prom+grafana is more transparent/customizable for "scrape my
  /metrics."

## Risks / tradeoffs

- **Image version drift** — prom/grafana are pinned in-file (v3.12.0 / 13.0.2)
  for reproducibility; bump deliberately. A `@sha256:` digest is the strongest pin.
- **attune `/metrics` is unauthenticated** — by existing design (router.go:54);
  the overlay doesn't change it, but the docs now make the exposure + proxy
  guidance explicit so nobody publishes it unknowingly.
- **Metrics carry a `tenant` label** — exposing Prometheus exposes per-tenant
  request volumes and tenant IDs. Acceptable in a private single-org deploy
  (operator's own data), but called out; loopback bind keeps it host-local.
- **Promoting `/metrics` to a contract makes metric names/labels a semver
  surface** — renaming `attune_ingest_total` would break every consumer's
  dashboards/alerts. The jargon cleanup deliberately touches only the dashboard
  title/tags (cosmetic), never metric names.
- **Cardinality footgun: `attune_ingest_total{source}` is unbounded on
  `validate_err`** (found while building the metrics reference).
  `internal/handlers/ingest.go:66` records the **raw client-supplied `source`**
  when validation rejects it — so an authenticated client sending arbitrary
  `source` values mints a new series each, a slow Prometheus cardinality/memory
  bleed. (The JSON-decode :46 and auth :38 paths already record `"unknown"`; only
  this path leaks.) Realistic trigger for the private-deploy audience is a *buggy
  client* (e.g. a per-request id in `source`), not external attack. **Fixed in
  #6** — record `in.Source` only when `domain.ValidSources[in.Source]`, else
  `"invalid"` (mirrors what the JSON-decode/auth paths already do), with a
  regression test.
- **Acceptance #4 (`enrich_duration_seconds` has data)** needs enrichment to
  actually run; addressed by the runbook (real key, or the ollama/mock path the
  `.env.example` already documents) rather than faked (CLAUDE.md §9).
- **No `read_only` on prom/grafana** (they need writable data dirs) — documented
  asymmetry vs the hardened attune container.

## Implementation plan

1. `deploy/docker-compose.obs.yml` (+ `prometheus.yml`, `grafana-datasource.yml`,
   `grafana-dashboards.yml`) — services, `apiVersion: 1`, loopback binds,
   hardening, healthchecks, `depends_on`.
2. Reframe `observability/README.md` as the **contract doc** (Layers 1–3 +
   bring-your-own-backend), add the `attune_*` **metrics reference** table (the 7
   families), reword "Wave 3+", clean the dashboard title/tags
   (`observability/dashboards/attune-overview.json`), and de-jargon/de-stale the
   `internal/infra/metrics/metrics.go` package doc ("5 core"→7, drop Wave/
   design-doc/"main backend" refs), plus a **drift-guard test** (registry
   metric-families == documented set). (#7 expands the prose tutorial; #6 ships
   the contract surface.)
3. `.github/workflows/ci.yml`: add a `deploy` filter+output to the `changes` gate
   + a `compose-config` job gated on it (`compose … config -q`).
4. `deploy/.env.example`: add `GF_SECURITY_ADMIN_PASSWORD` / `GRAFANA_PORT` /
   `PROMETHEUS_PORT`. `deploy/README.md`: add the "Observability overlay" section
   (access, `/metrics` warning, tear-down note, enrich runbook).
5. **Cardinality fix** (own commit `fix(handlers): bound ingest source label`):
   `internal/handlers/ingest.go:66` — record `in.Source` only when
   `domain.ValidSources[in.Source]`, else `"invalid"`; add a regression test (TDD).
6. `CHANGELOG.md` `[Unreleased]`: `Added` (overlay + CI check) + `Changed`
   (dashboard title/tags jargon cleanup) + `Security` (ingest `source`-label
   cardinality fix). **MINOR bump** — backwards-compatible feature on v0.3.
7. Verify (below). PR `feat(deploy): …`, **`Closes #6`** (§10).

## Verification

- CI: the new `compose config -q` job passes (base + overlay merge & parse).
- `docker compose -f docker-compose.yml -f docker-compose.obs.yml config` parses
  locally too.
- Bring the stack up; then:
  - Prometheus `/api/v1/targets` shows job `attune` (`attune:8090`) **UP**
    (confirms the unauthenticated-`/metrics` scrape path end-to-end).
  - Grafana reachable on the bound port; admin login with the env password works;
    `GET /api/dashboards/uid/attune-overview` returns the dashboard (auto-loaded);
    datasource health check passes; datasource uid is `prometheus`.
  - Dashboard title reads "Attune Overview" (no "Wave 1.2"); `grep -rin wave`
    over `observability/`, `deploy/`, and `internal/infra/metrics/metrics.go`
    returns nothing (roadmap "Wave" jargon gone from the contract surfaces); the
    metrics-reference table matches the 7 registered families.
  - Ingest a test row (tenant + key + `POST /v1/feedback/ingest`) → confirm
    `attune_ingest_total` appears in Prometheus / the "Ingest rate" panel.
  - **Cardinality fix:** an ingest with an invalid `source` (valid key) records
    `attune_ingest_total{source="invalid"}`, not the raw value (regression test).
  - **Enrich-panel runbook:** point `FEEDBACK_API_LLM_OPENAI_BASE_URL` at the
    documented ollama/mock backend, let one enrichment run, confirm
    `attune_enrich_duration_seconds_bucket` fills the p50/p95/p99 panel. (If no
    backend is available, the panel staying empty is expected and documented —
    not a failure.)
  - Tear down with the same `-f … -f …` (or `--remove-orphans`); confirm no
    orphaned prom/grafana containers.
- Results recorded in the PR.

## References

- #5 deploy kit (v0.2.0) · #7 docs · `observability/{README.md,targets.yaml,dashboards/attune-overview.json}`.
- Code checked: `cmd/attune/router.go:54-56` (`/metrics` no-auth), `internal/infra/config/config.go:154` + `cmd/attune/server.go:129` (port 8090), `internal/infra/metrics/metrics.go` (metric names/labels), `.github/workflows/ci.yml` (paths-filter CI).
- Grafana provisioning: datasources + dashboard file provider (`apiVersion: 1`).
- Top-repo patterns referenced for the layered-contract abstraction: Prometheus
  *monitoring-mixins* (`monitoring.mixins.dev` — portable dashboards/alerts/rules
  bundle), HashiCorp Vault *Telemetry reference: all metrics* (metrics catalog),
  OpenMetrics spec / OpenTelemetry (vendor-neutral exposition), Docker Compose
  profiles/overlays (optional reference stack).
