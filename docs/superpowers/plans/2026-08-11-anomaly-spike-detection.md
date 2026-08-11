# Anomaly & Spike Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect spikes/drops in feedback volume per slice (total/source/dimension/cluster/cohort/custom), persist them as `anomaly_events` linked to quality actions, notify via the notify adapter framework, and expose a Console analytics page with drilldown.

**Architecture:** A dedicated hourly worker (digest-worker skeleton) recomputes a 3-day rollup window into `feedback_volume_buckets`, then runs a pure-function detector (same-weekday median/MAD baseline with Poisson floor) over settled daily buckets, applying an open/resolved/retracted event state machine. Console reads through a new `AnomalyService` proto surface.

**Tech Stack:** Go 1.x + pgx/PostgreSQL, buf/proto + google.api.http annotations, React/TanStack (console), vitest, testcontainer-style pg integration tests under `test/integration/postgres/`.

**Spec:** `docs/superpowers/specs/2026-08-11-anomaly-spike-detection-design.md` (committed on this branch).

## Global Constraints

- Branch: `feat/237-anomaly-spike-detection`; issue #237.
- Quality gates (CLAUDE.md): `go vet ./...`, `go build ./...`, `go test -race` on changed packages, lizard CCN ≤ 15 / NLOC ≤ 100, jscpd < 5%, no direct `log/slog` (use `logext`), no bare `*p`/`&x` on flagged patterns (use `internal/pkg/ptrext`), error codes from the `attunev1.ErrorCode` enum, `buf lint` + `buf breaking` + generated output committed (`make proto`), console `pnpm tsc -b --noEmit` + `biome check` + `vitest` coverage thresholds + `pnpm arch`.
- Every code PR updates `CHANGELOG.md` `[Unreleased]` → `### Added`.
- All Go/proto comments and identifiers in English only (lint-artifacts Check B gates CJK).
- Migration number: **146** (next free after `145_webhook_subscriptions.sql`).
- New audit action constants pattern: see migration `116_inbound_source_update_audit.sql`.
- Timezone authority: `tenants.timezone` (IANA string, default `Asia/Shanghai`, migration 008). Do NOT add a timezone column to anomaly config.
- Sensitivity → z-threshold map: high=2.0, medium=2.5 (default), low=3.0.
- Defaults: `min_count=10`, `settle_delay_hours=3`, drop guard `med ≥ 5`, baseline = 8 same-weekday points, `MinBaselinePoints=4`, rollup recompute window = 3 days, bucket retention 400 days, runs retention 90 days, backfill 90 days, ≤5 sample feedback ids per bucket, ≤20 custom slices, ≤500 configured series (validation) / ≤1000 per tick (runtime truncate), notification fuse: >20 NEW events per tenant-tick → top 20 by |z| + one summary.

---

### Task 1: Detector core (pure functions)

**Files:**
- Create: `internal/service/anomaly/detector.go`
- Create: `internal/service/anomaly/detector_test.go`
- Modify: `CHANGELOG.md` (Unreleased → Added)

**Interfaces:**
- Produces (later tasks call these exactly):
  ```go
  package anomaly

  type DetectorConfig struct {
      ZThreshold        float64 // 2.0 / 2.5 / 3.0
      MinCount          int64   // spike observed floor
      MinBaselinePoints int     // default 4
  }

  type Verdict struct {
      Direction    string  // "" | "spike" | "drop"
      Z            float64
      ExpectedMed  float64
      ExpectedLow  float64 // clipped at 0
      ExpectedHigh float64
      Insufficient bool
  }

  func Detect(baseline []int64, observed int64, cfg DetectorConfig) Verdict
  func ZThresholdFor(sensitivity string) float64 // "high"→2.0, "medium"→2.5, "low"→3.0, else 2.5
  ```

- [ ] **Step 1: Write the failing tests**

`internal/service/anomaly/detector_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"math"
	"testing"
)

func defCfg() DetectorConfig {
	return DetectorConfig{ZThreshold: 2.5, MinCount: 10, MinBaselinePoints: 4}
}

func TestDetectSpikeOnQuietBaseline(t *testing.T) {
	// All-zero baseline: med=0, mad=0, sigma floors at 1.0 → z = 15.
	v := Detect([]int64{0, 0, 0, 0, 0, 0, 0, 0}, 15, defCfg())
	if v.Direction != "spike" {
		t.Fatalf("want spike, got %q (z=%f)", v.Direction, v.Z)
	}
	if v.Z != 15 {
		t.Fatalf("want z=15, got %f", v.Z)
	}
}

func TestDetectLowCountDoublingIsQuiet(t *testing.T) {
	// med=3; observed 6 passes the 2*med multiplier but z=(6-3)/sigma stays
	// under 2.5 with sigma=max(mad/0.6745, sqrt(3), 1)=sqrt(3)≈1.73 → z≈1.73.
	v := Detect([]int64{3, 2, 4, 3}, 6, defCfg())
	if v.Direction != "" {
		t.Fatalf("low-count doubling must not fire, got %q z=%f", v.Direction, v.Z)
	}
}

func TestDetectMinCountGuardBlocksTinySpike(t *testing.T) {
	// z is enormous but observed=8 < MinCount=10 → no verdict.
	v := Detect([]int64{0, 0, 0, 0}, 8, defCfg())
	if v.Direction != "" {
		t.Fatalf("min-count guard failed, got %q", v.Direction)
	}
}

func TestDetectMultiplierGuardBlocksTrendGrowth(t *testing.T) {
	// Steady growth: baseline med=100, observed 150 → z=(150-100)/10=5 ≥ 2.5
	// but 150 < 2*100 → multiplier guard blocks (sigma=sqrt(100)=10; MAD small).
	v := Detect([]int64{90, 95, 100, 105, 100, 98, 102, 100}, 150, defCfg())
	if v.Direction != "" {
		t.Fatalf("multiplier guard failed, got %q z=%f", v.Direction, v.Z)
	}
}

func TestDetectSpikeOnConstantBaseline(t *testing.T) {
	// MAD=0 → sigma=sqrt(10)≈3.162; z=(25-10)/3.162≈4.74 ≥ 2.5; 25 ≥ 2*10.
	v := Detect([]int64{10, 10, 10, 10}, 25, defCfg())
	if v.Direction != "spike" {
		t.Fatalf("want spike, got %q z=%f", v.Direction, v.Z)
	}
	if math.Abs(v.Z-4.743) > 0.01 {
		t.Fatalf("want z≈4.743, got %f", v.Z)
	}
}

func TestDetectDropToZero(t *testing.T) {
	// med=12 ≥ 5, observed 0 → z=(0-12)/sqrt(12)≈-3.46 ≤ -2.5 → drop.
	v := Detect([]int64{12, 11, 13, 12, 12, 11, 12, 13}, 0, defCfg())
	if v.Direction != "drop" {
		t.Fatalf("want drop, got %q z=%f", v.Direction, v.Z)
	}
}

func TestDetectDropGuardOnLowBaseline(t *testing.T) {
	// med=3 < 5 → drop never fires even at observed 0.
	v := Detect([]int64{3, 3, 4, 3}, 0, defCfg())
	if v.Direction != "" {
		t.Fatalf("drop guard failed, got %q", v.Direction)
	}
}

func TestDetectInsufficientBaseline(t *testing.T) {
	v := Detect([]int64{5, 6, 7}, 100, defCfg())
	if !v.Insufficient || v.Direction != "" {
		t.Fatalf("want insufficient+quiet, got insufficient=%v direction=%q", v.Insufficient, v.Direction)
	}
}

func TestDetectExpectedBand(t *testing.T) {
	// baseline [10,10,10,10]: med=10, sigma=sqrt(10). Band = med ± Z*sigma.
	v := Detect([]int64{10, 10, 10, 10}, 25, defCfg())
	wantHigh := 10 + 2.5*math.Sqrt(10)
	if math.Abs(v.ExpectedHigh-wantHigh) > 0.001 || v.ExpectedMed != 10 {
		t.Fatalf("band wrong: med=%f high=%f (want high %f)", v.ExpectedMed, v.ExpectedHigh, wantHigh)
	}
	// Low clipped at 0 when Z*sigma > med.
	v2 := Detect([]int64{1, 1, 1, 1}, 30, defCfg())
	if v2.ExpectedLow != 0 {
		t.Fatalf("expected low clipped to 0, got %f", v2.ExpectedLow)
	}
}

func TestDetectDeterminism(t *testing.T) {
	b := []int64{7, 3, 9, 5, 6, 4, 8, 5}
	first := Detect(b, 40, defCfg())
	for i := 0; i < 10; i++ {
		if got := Detect(b, 40, defCfg()); got != first {
			t.Fatalf("nondeterministic: %+v vs %+v", got, first)
		}
	}
}

func TestZThresholdFor(t *testing.T) {
	cases := map[string]float64{"high": 2.0, "medium": 2.5, "low": 3.0, "": 2.5, "bogus": 2.5}
	for in, want := range cases {
		if got := ZThresholdFor(in); got != want {
			t.Fatalf("ZThresholdFor(%q)=%f want %f", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/phj/Develop/attune && go test ./internal/service/anomaly/ -run TestDetect -v 2>&1 | head -20`
Expected: FAIL — package does not compile (`Detect` undefined).

- [ ] **Step 3: Write the implementation**

`internal/service/anomaly/detector.go`:

```go
// SPDX-License-Identifier: Apache-2.0

// Package anomaly implements spike/drop detection over daily feedback
// volume buckets (#237). The detector is a pure function over a
// same-weekday baseline: robust location/scale via median and MAD with a
// Poisson noise floor, a z-score decision, and two false-positive guards
// (an absolute observed floor for spikes and a minimum-baseline floor for
// drops). No clock, no IO — deterministic by construction.
package anomaly

import (
	"math"
	"sort"
)

// Directions reported by Detect.
const (
	DirectionSpike = "spike"
	DirectionDrop  = "drop"
)

// Sensitivity tiers exposed to operators; each maps to a z threshold.
const (
	SensitivityHigh   = "high"
	SensitivityMedium = "medium"
	SensitivityLow    = "low"
)

// madToSigma converts a median absolute deviation to a normal-consistent
// standard deviation estimate (1/Φ⁻¹(3/4)).
const madToSigma = 1 / 0.6745

// DetectorConfig carries the tunable knobs. Zero values are NOT defaulted
// here — the caller (worker/config layer) resolves tenant config first.
type DetectorConfig struct {
	// ZThreshold is the |z| needed to fire (2.0 / 2.5 / 3.0).
	ZThreshold float64
	// MinCount is the absolute observed floor for spikes.
	MinCount int64
	// MinBaselinePoints is the minimum baseline size to judge at all.
	MinBaselinePoints int
}

// Verdict is the outcome for one (baseline, observed) pair.
type Verdict struct {
	Direction    string // "", DirectionSpike, or DirectionDrop
	Z            float64
	ExpectedMed  float64
	ExpectedLow  float64 // med − Z·sigma, clipped at 0
	ExpectedHigh float64 // med + Z·sigma
	Insufficient bool    // baseline shorter than MinBaselinePoints
}

// ZThresholdFor maps a sensitivity tier to its z threshold, defaulting to
// medium for unknown input so a corrupt config row degrades safely.
func ZThresholdFor(sensitivity string) float64 {
	switch sensitivity {
	case SensitivityHigh:
		return 2.0
	case SensitivityLow:
		return 3.0
	default:
		return 2.5
	}
}

// Detect judges one observation against its same-weekday baseline.
//
//	sigma = max(MAD/0.6745, sqrt(max(med,1)), 1)  — Poisson floor keeps a
//	  sampled MAD of ~8 points from underestimating count noise; the
//	  absolute floor of 1 avoids z blowups on all-zero baselines.
//	spike ⇐ z ≥ ZThreshold AND observed ≥ max(MinCount, 2·med) — the
//	  multiplier guard absorbs steady growth without a trend fit.
//	drop  ⇐ z ≤ −ZThreshold AND med ≥ 5 — a dead stream (observed 0)
//	  still fires when the baseline was alive.
func Detect(baseline []int64, observed int64, cfg DetectorConfig) Verdict {
	if len(baseline) < cfg.MinBaselinePoints {
		return Verdict{Insufficient: true}
	}
	med := median(baseline)
	sigma := math.Max(mad(baseline, med)*madToSigma, math.Sqrt(math.Max(med, 1)))
	sigma = math.Max(sigma, 1)
	z := (float64(observed) - med) / sigma

	v := Verdict{
		Z:            z,
		ExpectedMed:  med,
		ExpectedLow:  math.Max(0, med-cfg.ZThreshold*sigma),
		ExpectedHigh: med + cfg.ZThreshold*sigma,
	}
	switch {
	case z >= cfg.ZThreshold && float64(observed) >= math.Max(float64(cfg.MinCount), 2*med):
		v.Direction = DirectionSpike
	case z <= -cfg.ZThreshold && med >= 5:
		v.Direction = DirectionDrop
	}
	return v
}

// median of int64 values (mean of the middle pair for even lengths).
func median(xs []int64) float64 {
	s := make([]int64, len(xs))
	copy(s, xs)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return float64(s[n/2])
	}
	return (float64(s[n/2-1]) + float64(s[n/2])) / 2
}

// mad is the median absolute deviation around a precomputed median.
func mad(xs []int64, med float64) float64 {
	dev := make([]float64, len(xs))
	for i, x := range xs {
		dev[i] = math.Abs(float64(x) - med)
	}
	sort.Float64s(dev)
	n := len(dev)
	if n%2 == 1 {
		return dev[n/2]
	}
	return (dev[n/2-1] + dev[n/2]) / 2
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/phj/Develop/attune && go test -race ./internal/service/anomaly/ -v 2>&1 | tail -20`
Expected: all PASS.

- [ ] **Step 5: Lint gates**

Run: `cd /Users/phj/Develop/attune && go vet ./internal/service/anomaly/ && lizard internal/service/anomaly -l go -C 15 -T nloc=100 --warnings_only`
Expected: no output (clean).

- [ ] **Step 6: CHANGELOG + commit**

Add under `[Unreleased]` → `### Added` in `CHANGELOG.md`:

```markdown
- Anomaly & spike detection for product signals (#237): robust same-weekday
  z-score detector over daily feedback-volume rollups, anomaly event records
  linked to control-tower quality actions, operator notifications, and a
  Console analytics page with evidence drilldown.
```

(One entry for the whole feature; later tasks do not add more entries.)

```bash
cd /Users/phj/Develop/attune
git add internal/service/anomaly/ CHANGELOG.md
git commit -m "feat(anomaly): add pure spike/drop detector core (#237)"
```

---

### Task 2: Migration 146 — schema for buckets, events, config, runs

**Files:**
- Create: `internal/infra/database/migrations/146_anomaly_detection.sql`
- Test: `test/integration/postgres/anomaly/schema_test.go` (new dir)

**Interfaces:**
- Produces: the five tables exactly as below; audit actions `anomaly_config.update`, `anomaly_custom_slice.create`, `anomaly_custom_slice.delete` registered in the `audit_actions` seeding pattern used by migration 116.

- [ ] **Step 1: Inspect the audit-action seeding pattern**

Run: `sed -n 1,40p /Users/phj/Develop/attune/internal/infra/database/migrations/116_inbound_source_update_audit.sql`
Copy its INSERT/enum-extension shape exactly for the three new actions.

- [ ] **Step 2: Write the migration**

`internal/infra/database/migrations/146_anomaly_detection.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
--
-- Anomaly & spike detection (#237): daily feedback-volume rollup buckets,
-- anomaly event records, per-tenant detection config, custom slice
-- definitions, and per-day detection run claims.

CREATE TABLE IF NOT EXISTS feedback_volume_buckets (
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    bucket_date     DATE NOT NULL,
    slice_type      TEXT NOT NULL,
    slice_key       TEXT NOT NULL,
    slice_display   TEXT NOT NULL DEFAULT '',
    config_version  INT  NOT NULL DEFAULT 1,
    feedback_count  BIGINT NOT NULL DEFAULT 0
        CONSTRAINT chk_volume_buckets_count_nonnegative CHECK (feedback_count >= 0),
    sample_feedback_ids BIGINT[] NOT NULL DEFAULT '{}',
    computed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, bucket_date, slice_type, slice_key),
    CONSTRAINT chk_volume_buckets_slice_type CHECK (
        slice_type IN ('total','source','dimension','cluster','cohort','custom')),
    CONSTRAINT chk_volume_buckets_slice_key_len CHECK (length(slice_key) BETWEEN 1 AND 120)
);

CREATE INDEX IF NOT EXISTS idx_volume_buckets_series
    ON feedback_volume_buckets (tenant_id, slice_type, slice_key, bucket_date DESC);
CREATE INDEX IF NOT EXISTS idx_volume_buckets_retention
    ON feedback_volume_buckets (bucket_date);

CREATE TABLE IF NOT EXISTS anomaly_events (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    slice_type        TEXT NOT NULL,
    slice_key         TEXT NOT NULL,
    slice_display     TEXT NOT NULL DEFAULT '',
    direction         TEXT NOT NULL
        CONSTRAINT chk_anomaly_events_direction CHECK (direction IN ('spike','drop')),
    first_bucket_date DATE NOT NULL,
    last_bucket_date  DATE NOT NULL,
    observed          BIGINT NOT NULL,
    expected_med      DOUBLE PRECISION NOT NULL,
    expected_low      DOUBLE PRECISION NOT NULL,
    expected_high     DOUBLE PRECISION NOT NULL,
    z_score           DOUBLE PRECISION NOT NULL,
    status            TEXT NOT NULL DEFAULT 'open'
        CONSTRAINT chk_anomaly_events_status CHECK (status IN ('open','resolved','retracted')),
    quality_action_id UUID REFERENCES feedback_quality_actions(id) ON DELETE SET NULL,
    evidence          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at       TIMESTAMPTZ,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_anomaly_events_open
    ON anomaly_events (tenant_id, slice_type, slice_key, direction)
    WHERE status = 'open';
CREATE INDEX IF NOT EXISTS idx_anomaly_events_tenant_status
    ON anomaly_events (tenant_id, status, last_bucket_date DESC);

CREATE TABLE IF NOT EXISTS tenant_anomaly_configs (
    tenant_id           TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    sensitivity         TEXT NOT NULL DEFAULT 'medium'
        CONSTRAINT chk_anomaly_configs_sensitivity CHECK (sensitivity IN ('high','medium','low')),
    min_count           INT  NOT NULL DEFAULT 10
        CONSTRAINT chk_anomaly_configs_min_count CHECK (min_count BETWEEN 0 AND 10000),
    settle_delay_hours  INT  NOT NULL DEFAULT 3
        CONSTRAINT chk_anomaly_configs_settle_delay CHECK (settle_delay_hours BETWEEN 0 AND 48),
    enabled_slice_types TEXT[] NOT NULL
        DEFAULT '{total,source,dimension,cluster,cohort,custom}',
    drop_enabled_slice_types TEXT[] NOT NULL
        DEFAULT '{total,source,dimension,cohort,custom}',
    notify_mode         TEXT NOT NULL DEFAULT 'immediate'
        CONSTRAINT chk_anomaly_configs_notify_mode CHECK (notify_mode IN ('immediate','digest','off')),
    detection_enabled   BOOLEAN NOT NULL DEFAULT TRUE,
    config_version      INT NOT NULL DEFAULT 1,
    backfilled_at       TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by          TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS tenant_anomaly_custom_slices (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL
        CONSTRAINT chk_anomaly_custom_slices_name CHECK (length(name) BETWEEN 1 AND 80),
    definition  JSONB NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    last_error  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_anomaly_custom_slices_name UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS anomaly_detection_runs (
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    bucket_date  DATE NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending'
        CONSTRAINT chk_anomaly_runs_status CHECK (status IN ('pending','running','done','failed')),
    claimed_by   TEXT NOT NULL DEFAULT '',
    claimed_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    error        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, bucket_date)
);
```

Then append the three audit-action registrations following the exact
pattern found in Step 1 (migration 116).

- [ ] **Step 3: Write the failing schema test**

`test/integration/postgres/anomaly/schema_test.go` — follow the build-tag +
setup helper conventions of `test/integration/postgres/feedback` (copy its
`TestMain`/db bootstrap). Assertions:

```go
//go:build integration

package anomaly_test

// TestAnomalySchemaConstraints exercises migration 146 invariants:
//  1. duplicate open events for one (tenant,slice,direction) are rejected
//     by uq_anomaly_events_open, but a second row inserts fine once the
//     first is resolved;
//  2. invalid slice_type / direction / status / sensitivity values are
//     rejected by CHECK constraints;
//  3. tenant_anomaly_configs defaults land as designed (medium/10/3h/
//     immediate/TRUE, cluster excluded from drop_enabled_slice_types).
func TestAnomalySchemaConstraints(t *testing.T) {
	db := setupDB(t) // per-package bootstrap copied from feedback suite
	tenantID := createTenant(t, db)

	// 1. partial unique index
	mustExec(t, db, `INSERT INTO anomaly_events
	  (tenant_id, slice_type, slice_key, direction, first_bucket_date,
	   last_bucket_date, observed, expected_med, expected_low, expected_high, z_score)
	  VALUES ($1,'total','total','spike','2026-08-01','2026-08-01',31,12,6,21,3.8)`, tenantID)
	err := exec(db, `INSERT INTO anomaly_events
	  (tenant_id, slice_type, slice_key, direction, first_bucket_date,
	   last_bucket_date, observed, expected_med, expected_low, expected_high, z_score)
	  VALUES ($1,'total','total','spike','2026-08-02','2026-08-02',40,12,6,21,4.0)`, tenantID)
	requireUniqueViolation(t, err)
	mustExec(t, db, `UPDATE anomaly_events SET status='resolved' WHERE tenant_id=$1`, tenantID)
	mustExec(t, db, `INSERT INTO anomaly_events (...) VALUES ($1,'total','total','spike',...)`, tenantID) // now OK

	// 2. CHECKs
	requireCheckViolation(t, exec(db, `INSERT INTO feedback_volume_buckets
	  (tenant_id,bucket_date,slice_type,slice_key) VALUES ($1,'2026-08-01','bogus','x')`, tenantID))

	// 3. config defaults
	mustExec(t, db, `INSERT INTO tenant_anomaly_configs (tenant_id) VALUES ($1)`, tenantID)
	row := queryRow(db, `SELECT sensitivity, min_count, settle_delay_hours, notify_mode,
	  'cluster' = ANY(drop_enabled_slice_types) FROM tenant_anomaly_configs WHERE tenant_id=$1`, tenantID)
	// assert medium / 10 / 3 / immediate / false
}
```

(Write the full helpers by copying the neighboring suite; the test body
above is the required coverage, not pseudocode to skip.)

- [ ] **Step 4: Run migration + test**

Run: `cd /Users/phj/Develop/attune && make test-integration 2>&1 | grep -E "anomaly|FAIL|ok" | head -10`
Expected: `ok .../test/integration/postgres/anomaly`.

- [ ] **Step 5: Commit**

```bash
git add internal/infra/database/migrations/146_anomaly_detection.sql test/integration/postgres/anomaly/
git commit -m "feat(anomaly): add rollup/event/config/runs schema (#237)"
```

---

### Task 3: Rollup repo — recompute window + baseline reads

**Files:**
- Create: `internal/repo/anomaly/rollup.go`
- Create: `internal/repo/anomaly/repo.go` (pool holder + constructor `New(pool *pgxpool.Pool) *Repo`)
- Test: `test/integration/postgres/anomaly/rollup_test.go`

**Interfaces:**
- Consumes: `tenants.timezone` via `tenant.TenantRepo.GetByID` (`internal/repo/tenant/tenants.go:118`), dimension list via `internal/repo/tenant/enrich_config.go` (`GetEnrichConfig` → `DimensionSet`).
- Produces:
  ```go
  package anomaly // import "github.com/Phixsura/attune/internal/repo/anomaly"

  type Repo struct{ pool *pgxpool.Pool }
  func New(pool *pgxpool.Pool) *Repo

  // RecomputeWindow rebuilds buckets for [fromDate, toDate] (tenant-local
  // civil dates, inclusive) inside one transaction: upsert current counts,
  // anti-join-delete vanished buckets in the window.
  type RecomputeOpts struct {
      TenantID      string
      Location      *time.Location // tenant tz
      FromDate      time.Time      // civil date (midnight in Location)
      ToDate        time.Time
      ConfigVersion int
      MinCount      int64          // cluster HAVING floor
      Dimensions    domain.DimensionSet
      CustomSlices  []CustomSlice  // pre-validated, compiled by caller
  }
  func (r *Repo) RecomputeWindow(ctx context.Context, opts RecomputeOpts) error

  // BaselineCounts returns feedback_count for the given slice on each of
  // the requested dates (missing bucket rows come back as 0, in order).
  func (r *Repo) BaselineCounts(ctx context.Context, tenantID, sliceType, sliceKey string, dates []time.Time) ([]int64, error)

  // SlicesForDetection returns the union of slices present on detectDate
  // or any baseline date (drop candidates included).
  type SliceRef struct{ Type, Key, Display string }
  func (r *Repo) SlicesForDetection(ctx context.Context, tenantID string, enabled []string, detectDate time.Time, baselineDates []time.Time) ([]SliceRef, error)

  // CountOn returns feedback_count (0 when absent) and sample ids for one
  // slice on one date.
  func (r *Repo) CountOn(ctx context.Context, tenantID, sliceType, sliceKey string, date time.Time) (int64, []int64, error)

  // CleanupRetention deletes buckets older than bucketDays and runs older
  // than runDays; batched, safe to call every tick.
  func (r *Repo) CleanupRetention(ctx context.Context, bucketDays, runDays int) error

  type CustomSlice struct {
      ID         uuid.UUID
      Display    string
      Conditions []CustomCondition
  }
  type CustomCondition struct {
      Field  string   // "source" | "dimension" | "cohort"
      Name   string   // dimension name when Field=="dimension"
      Multi  bool     // dimension kind
      Values []string
  }
  ```
- slice_key formats (fixed, tested): `total`, `source:<source>`,
  `dim:<name>=<sha256(value)[:8]>`, `cluster:<uuid>`, `cohort:<uuid>`,
  `custom:<uuid>`. Export `func DimensionSliceKey(name, value string) string`
  and `func SliceDisplay(...)` helpers from this package — the worker,
  contribution computation, and handlers all reuse them.

- [ ] **Step 1: Write failing integration tests**

`rollup_test.go` scenarios (each a subtest; seed via direct INSERTs into
`user_feedback` with controlled `created_at`, `source`, `enriched_attrs`,
`enrichment_status`, `cluster_id`, `subject_key`):

```go
//go:build integration

// TestRecomputeWindow covers:
//  total_and_source: 3 feedback rows over 2 days, 2 sources → assert exact
//    bucket rows (dates in tenant tz Asia/Shanghai — pick created_at values
//    that straddle a UTC midnight to prove civil-date bucketing).
//  dimension_single_and_multi: enriched_attrs {"severity":"critical",
//    "labels":["a","b"]} with a DimensionSet containing single 'severity'
//    (taxonomy) and multi 'labels' (taxonomy) → severity bucket count 1,
//    label buckets a=1 AND b=1 (multi expansion); a row with
//    enrichment_status='pending' is EXCLUDED from dimension buckets but
//    INCLUDED in total.
//  cluster_min_count: two clusters, counts 12 and 3, MinCount 10 → only
//    the 12-count cluster gets a bucket.
//  cohort_join: membership rows (left_at NULL vs set) → only active
//    membership counts.
//  custom_conjunction: slice source IN ('api') AND severity='critical' →
//    only rows matching both.
//  idempotent_and_zeroing: run RecomputeWindow twice → identical rows;
//    DELETE the feedback and recompute → bucket rows for the window are gone.
//  sample_ids_capped: 7 matching rows → sample_feedback_ids has exactly 5,
//    newest first.
// TestBaselineCounts: seed buckets on 3 of 8 requested dates → returns 8
//    values with zeros for the missing 5, order matching input.
// TestSlicesForDetection: slice present only in baseline dates (vanished
//    today) still returned — drop candidates visible.
// TestCleanupRetention: bucket older than 400 days deleted, newer kept.
```

Write these fully (assert on SELECTed rows, not just "no error").

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/phj/Develop/attune && go build ./internal/repo/anomaly/ 2>&1 | head -3`
Expected: build failure (package missing).

- [ ] **Step 3: Implement the repo**

`RecomputeWindow` core (one transaction; UTC window derived from civil
dates: `fromUTC = time.Date(y,m,d,0,0,0,0,loc).UTC()`, `toUTC` = day after
ToDate):

```sql
-- ① total + source (GROUPING SETS over one scan)
INSERT INTO feedback_volume_buckets
  (tenant_id, bucket_date, slice_type, slice_key, slice_display, config_version,
   feedback_count, sample_feedback_ids, computed_at)
SELECT $1,
       (f.created_at AT TIME ZONE $4)::date,
       CASE WHEN f.source IS NULL THEN 'total' ELSE 'source' END, -- grouping-set discriminator, see Go note
       ...
```

Implementation note: GROUPING SETS with a discriminator is fiddly in pgx —
prefer five plain aggregate queries (total, source, dimension, cluster,
cohort) + one per custom slice, each `INSERT ... SELECT ... ON CONFLICT
(tenant_id,bucket_date,slice_type,slice_key) DO UPDATE SET feedback_count =
EXCLUDED.feedback_count, sample_feedback_ids = EXCLUDED.sample_feedback_ids,
slice_display = EXCLUDED.slice_display, config_version = EXCLUDED.config_version,
computed_at = NOW()`. They all share the window predicate
`f.tenant_id=$1 AND f.created_at >= $2 AND f.created_at < $3`.

Exact per-family shapes:

```sql
-- total
SELECT (f.created_at AT TIME ZONE $4)::date AS d, COUNT(*),
       (array_agg(f.id ORDER BY f.id DESC))[1:5]
FROM user_feedback f WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
GROUP BY d;

-- source: add f.source to GROUP BY; slice_key='source:'||f.source.

-- dimension single (one query per configured dim, name as parameter):
SELECT (f.created_at AT TIME ZONE $4)::date AS d,
       f.enriched_attrs ->> $5 AS val, COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
FROM user_feedback f
WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
  AND f.enrichment_status='enriched' AND f.enriched_attrs ? $5
GROUP BY d, val HAVING f.enriched_attrs ->> $5 IS NOT NULL;
-- slice_key computed in Go: DimensionSliceKey($5, val); display $5||'='||val.
-- Per-dimension value guard: if a dim yields >50 distinct values in the
-- window read, keep top 50 by count and increment
-- metrics.AnomalySlicesTruncated (label "dimension").

-- dimension multi: CROSS JOIN LATERAL jsonb_array_elements_text(
--   COALESCE(f.enriched_attrs -> $5, '[]'::jsonb)) AS v(val), same guards.

-- cluster
SELECT (f.created_at AT TIME ZONE $4)::date AS d, f.cluster_id, f.cluster_label,
       COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
FROM user_feedback f
WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3 AND f.cluster_id IS NOT NULL
GROUP BY d, f.cluster_id, f.cluster_label
HAVING COUNT(*) >= $5;

-- cohort
SELECT (f.created_at AT TIME ZONE $4)::date AS d, cm.cohort_id, co.name,
       COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
FROM user_feedback f
JOIN cohort_memberships cm ON cm.tenant_id = f.tenant_id
     AND cm.external_user_id = f.subject_key AND cm.left_at IS NULL
JOIN cohorts co ON co.id = cm.cohort_id
WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3 AND f.subject_key <> ''
GROUP BY d, cm.cohort_id, co.name;

-- custom slice (per slice; conditions compiled to AND-ed predicates):
--   source  → f.source = ANY($n)
--   dim single → f.enrichment_status='enriched' AND f.enriched_attrs ->> $name = ANY($n)
--   dim multi  → f.enrichment_status='enriched' AND f.enriched_attrs -> $name ?| $n
--   cohort  → EXISTS (SELECT 1 FROM cohort_memberships cm WHERE cm.tenant_id=f.tenant_id
--                AND cm.cohort_id = ANY($n::uuid[]) AND cm.external_user_id=f.subject_key
--                AND cm.left_at IS NULL)
-- All parameterized; the compiler lives in this package as
-- func compileCustomConditions(cs []CustomCondition) (where string, args []any).
```

Zeroing pass (same transaction, after all upserts):

```sql
DELETE FROM feedback_volume_buckets b
WHERE b.tenant_id = $1 AND b.bucket_date BETWEEN $from AND $to
  AND b.computed_at < $txStart;  -- rows not touched by this recompute
```

(`$txStart` = a `time.Time` captured before the first upsert — simpler and
race-free versus an anti-join against a values list.)

- [ ] **Step 4: Run integration tests**

Run: `cd /Users/phj/Develop/attune && make test-integration 2>&1 | grep -E "anomaly|FAIL" | head`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
cd /Users/phj/Develop/attune && go vet ./internal/repo/anomaly/ && \
  lizard internal/repo/anomaly -l go -C 15 -T nloc=100 --warnings_only
git add internal/repo/anomaly/ test/integration/postgres/anomaly/
git commit -m "feat(anomaly): rollup repo with recompute window and baseline reads (#237)"
```

---

### Task 4: Event/config repo — state machine rows, claims, config CRUD

**Files:**
- Create: `internal/repo/anomaly/events.go`
- Create: `internal/repo/anomaly/config.go`
- Create: `internal/repo/anomaly/runs.go`
- Test: `test/integration/postgres/anomaly/events_test.go`

**Interfaces:**
- Produces:
  ```go
  type Event struct {
      ID              uuid.UUID
      TenantID        string
      SliceType, SliceKey, SliceDisplay string
      Direction       string
      FirstBucketDate, LastBucketDate time.Time
      Observed        int64
      ExpectedMed, ExpectedLow, ExpectedHigh, ZScore float64
      Status          string // open|resolved|retracted
      QualityActionID *string
      EvidenceJSON    string
      CreatedAt, UpdatedAt time.Time
      ResolvedAt      *time.Time
  }

  // UpsertHit implements the open-row state machine:
  //   no open row  → INSERT, returns (event, isNew=true)
  //   open row     → UPDATE last_bucket_date/observed/z/updated_at,
  //                  returns (event, isNew=false)
  type HitInput struct {
      TenantID, SliceType, SliceKey, SliceDisplay, Direction string
      BucketDate time.Time
      Observed   int64
      ExpectedMed, ExpectedLow, ExpectedHigh, Z float64
      EvidenceJSON string // only stored on INSERT
  }
  func (r *Repo) UpsertHit(ctx context.Context, in HitInput) (Event, bool, error)

  func (r *Repo) SetQualityAction(ctx context.Context, eventID uuid.UUID, actionID string) error
  func (r *Repo) ListOpenEvents(ctx context.Context, tenantID string) ([]Event, error)
  func (r *Repo) ListEvents(ctx context.Context, tenantID string, status string, limit int) ([]Event, error)
  func (r *Repo) GetEvent(ctx context.Context, tenantID string, id uuid.UUID) (*Event, error)
  func (r *Repo) ResolveEvent(ctx context.Context, tenantID string, id uuid.UUID) error   // status open→resolved, resolved_at=NOW()
  func (r *Repo) RetractEvent(ctx context.Context, tenantID string, id uuid.UUID) error   // open|resolved→retracted

  type Config struct {
      TenantID          string
      Sensitivity       string
      MinCount          int
      SettleDelayHours  int
      EnabledSliceTypes []string
      DropEnabledSliceTypes []string
      NotifyMode        string
      DetectionEnabled  bool
      ConfigVersion     int
      BackfilledAt      *time.Time
  }
  func (r *Repo) GetConfig(ctx context.Context, tenantID string) (Config, error) // defaults when no row
  func (r *Repo) UpsertConfig(ctx context.Context, cfg Config, updatedBy string) error // bumps config_version
  func (r *Repo) MarkBackfilled(ctx context.Context, tenantID string, version int) error
  func (r *Repo) ListCustomSlices(ctx context.Context, tenantID string) ([]StoredCustomSlice, error)
  func (r *Repo) ReplaceCustomSlices(ctx context.Context, tenantID string, slices []StoredCustomSlice) error
  func (r *Repo) DisableCustomSlice(ctx context.Context, tenantID string, id uuid.UUID, lastError string) error

  type StoredCustomSlice struct {
      ID uuid.UUID; Name string; DefinitionJSON string; Enabled bool; LastError string
  }

  // Runs: digest_runs claim pattern scoped to (tenant, date).
  func (r *Repo) ClaimRun(ctx context.Context, tenantID string, date time.Time, owner string, stale time.Duration) (bool, error)
  func (r *Repo) MarkRunDone(ctx context.Context, tenantID string, date time.Time, owner string) error
  func (r *Repo) MarkRunFailed(ctx context.Context, tenantID string, date time.Time, owner string, runErr error) error
  func (r *Repo) UnclaimedSettledDates(ctx context.Context, tenantID string, candidates []time.Time) ([]time.Time, error)
  ```

- [ ] **Step 1: Write failing tests** covering: UpsertHit new→ongoing
  (same direction advances `last_bucket_date`, `isNew=false`, evidence NOT
  overwritten); Resolve → a later hit INSERTs a fresh row (partial index
  allows it); Retract from open and from resolved; GetConfig with no row
  returns exact defaults (medium/10/3/immediate/enabled, cluster absent
  from drop list); UpsertConfig bumps version; ClaimRun: second claim for
  same (tenant,date) returns false, stale claim (claimed_at older than
  stale) is re-claimable; UnclaimedSettledDates filters done rows.

- [ ] **Step 2: Run to verify failure** (build error), **Step 3: implement**
  (ClaimRun = `INSERT ... ON CONFLICT (tenant_id,bucket_date) DO UPDATE SET
  claimed_by=$owner, claimed_at=NOW(), status='running' WHERE
  anomaly_detection_runs.status IN ('pending','failed') OR
  (anomaly_detection_runs.status='running' AND
  anomaly_detection_runs.claimed_at < NOW() - $stale::interval) RETURNING
  ...` — claim succeeded iff a row returned), **Step 4: run
  `make test-integration`**, **Step 5: lint + commit**

```bash
git add internal/repo/anomaly/ test/integration/postgres/anomaly/
git commit -m "feat(anomaly): event state machine, config, and run-claim repos (#237)"
```

---

### Task 5: Contribution breakdown (pure + one query)

**Files:**
- Create: `internal/service/anomaly/contribution.go`
- Create: `internal/service/anomaly/contribution_test.go` (pure part)
- Modify: `internal/repo/anomaly/rollup.go` (add `GroupCounts`)
- Test: append to `test/integration/postgres/anomaly/rollup_test.go`

**Interfaces:**
- Produces:
  ```go
  // repo side: day-of counts for the anomalous slice grouped by another
  // axis (source or a single dimension), plus the same-weekday baseline
  // medians per group value.
  type GroupCount struct{ Value string; Observed int64; BaselineMed float64 }
  func (r *Repo) GroupCounts(ctx context.Context, tenantID string, slice SliceRef, groupBy CustomCondition /* Field+Name only */, date time.Time, baselineDates []time.Time) ([]GroupCount, error)

  // service side, pure:
  type Contribution struct {
      Dimension string  `json:"dim"`   // "source" or dimension name
      Value     string  `json:"value"`
      Share     float64 `json:"share"` // signed, (obs_v-exp_v)/(obs_tot-exp_tot)
  }
  // TopContributions keeps |share| ≥ 0.15, top 3 by |share|; returns
  // (nil, spread=true) when nothing qualifies or the denominator is ~0.
  func TopContributions(groups map[string][]GroupCount, obsTotal int64, expTotal float64) (top []Contribution, spread bool)
  ```
- Evidence JSON shape (stored on event INSERT, read by handlers):
  `{"sample_ids":[...], "contribution":[{"dim":"source","value":"zendesk","share":0.62}], "spread":false}`

- [ ] **Step 1: pure tests** — hand-built GroupCounts where one value explains
  62% (kept), three values at 10% each (dropped, spread=false because one
  ≥15% exists elsewhere), all-below-15% (spread=true), denominator zero
  (spread=true, no NaN/Inf), share sign for drops (negative denominator).
- [ ] **Step 2: fail, Step 3: implement, Step 4: pass** (`go test -race ./internal/service/anomaly/`)
- [ ] **Step 5: repo `GroupCounts` + integration subtest** (seed a spike
  concentrated in one source; assert share ≈ expected within 0.01)
- [ ] **Step 6: lint + commit** — `git commit -m "feat(anomaly): contribution breakdown (#237)"`

---### Task 6: Worker — orchestration, quality actions, notifications

**Files:**
- Create: `internal/service/anomaly/worker.go`
- Create: `internal/service/anomaly/worker_notify.go` (payload build + card render)
- Create: `internal/service/anomaly/worker_test.go` (unit: fakes for repos/transport)
- Test: `test/integration/postgres/anomaly/worker_test.go` (end-to-end)
- Modify: `cmd/attune/server.go` (add `startAnomalyWorker`, called where `startDigestWorker` is)
- Modify: `internal/infra/config/config.go` + `defaults.go` (add `AnomalyInterval time.Duration`, `AnomalyBackfillTenantsPerTick int`; yaml keys `anomaly.interval`, `anomaly.backfill_tenants_per_tick`)
- Modify: `config.example.yaml` (`anomaly: {interval: "1h", backfill_tenants_per_tick: 10}`)
- Modify: `internal/infra/metrics/metrics.go` (register the 7 metrics from the spec §11, following `EnrichAttrsDroppedTotal` CounterVec style)

**Interfaces:**
- Consumes: Task 1 `Detect`/`ZThresholdFor`; Task 3/4/5 repo methods;
  `feedback.FeedbackRepo.UpsertQualityActionStatus(ctx, feedback.QualityActionUpsert)`
  (`internal/repo/feedback/quality_actions.go:115` — fields: TenantID,
  ActionKey, Signal, Status, Severity, TargetPath, MetricLabel, MetricValue,
  RecommendationKey, EvidenceJSON, ActorUserID);
  `notify.Transport.Send` via a sender modeled on `internal/service/digest/sender.go`;
  `notifytarget` reader `ListActiveByTenantAudience(ctx, tenantID, "radar")`;
  tenant enumeration: add to `internal/repo/anomaly/repo.go`
  `ActiveTenantsWithFeedback(ctx, sinceDays int) ([]TenantRef, error)` where
  `TenantRef{ID, Timezone string}` (SQL: `SELECT t.id, t.timezone FROM tenants t
  WHERE t.is_active AND EXISTS (SELECT 1 FROM user_feedback f WHERE
  f.tenant_id=t.id AND f.created_at > NOW() - ($1||' days')::interval)`).
- Produces:
  ```go
  func NewWorker(repo *anomalyrepo.Repo, feedbackRepo qualityActionUpserter,
      targets targetReader, transport *notify.Transport,
      tenantCfg enrichConfigReader, deepLinkBase string) *Worker
  func (w *Worker) Configure(interval time.Duration, backfillPerTick int)
  func (w *Worker) Run(ctx context.Context)                 // digest Run loop shape
  func (w *Worker) ProcessOnce(ctx context.Context, now time.Time)
  ```
  Interface slices (defined worker-side): `qualityActionUpserter`,
  `targetReader`, `enrichConfigReader` — keep the worker testable with fakes.

**Per-tenant ProcessOnce sequence (implement as small methods, CCN ≤ 15 each):**

1. `cfg := repo.GetConfig(tenant)`; skip when `!cfg.DetectionEnabled`.
2. Backfill gate: if `cfg.BackfilledAt == nil` or config_version/tz changed
   (compare a `(tz, version)` pair cached on the config row via
   `backfilled_at IS NULL OR config_version != version-at-backfill` — store
   `backfill_version INT` alongside; simplest: `MarkBackfilled` records the
   version, and the gate re-runs when `ConfigVersion != backfillVersion`) →
   recompute 90 days, `MarkBackfilled`, count against
   `backfillPerTick` budget, return (detection starts next tick).
3. `RecomputeWindow` last 3 civil days.
4. Candidate dates: civil dates D where `now ≥ (D+1 day midnight in loc) +
   settle_delay` and D within the last 3 days → `UnclaimedSettledDates` →
   for each: `ClaimRun`; on claim: detect (below); `MarkRunDone/Failed`.
5. Detect one date: `baselineDates(d) = d-7, d-14, ..., d-56` (8 same-weekday
   dates); `SlicesForDetection(enabled, d, baselineDates)`; runtime cap 1000
   (truncate + metric). For each slice: `BaselineCounts`, `CountOn(d)`,
   `Detect`. Drop verdicts only applied when `slice.Type ∈
   cfg.DropEnabledSliceTypes`.
6. On verdict: compute contribution (source + each single taxonomy dim via
   `GroupCounts` + `TopContributions`), build evidence JSON, `UpsertHit`.
   When `isNew`: `UpsertQualityActionStatus` with
   `ActionKey="anomaly:"+sliceKey`, `Signal="anomaly_detection"`,
   `Status="open"`, `Severity=` `alert` if `|z| ≥ 2*threshold` else `watch`,
   `TargetPath="/analytics/anomalies?event="+eventID`,
   `MetricLabel=sliceDisplay`,
   `MetricValue=fmt.Sprintf("%+.0f%% vs expected", pctDelta)`,
   `EvidenceJSON={"anomaly_event_id":...}`, `ActorUserID="anomaly-worker"`;
   then `SetQualityAction(eventID, actionID)`; then notify (step 7).
7. Notify per `cfg.NotifyMode`: `immediate` → render payload (spec §8 JSON
   for raw-webhook; lark/slack via card builder mirroring digest render) and
   `transport.Send` to each `radar` target; failures log+metric only.
   Fuse: collect NEW events per tenant-tick; if >20, send top 20 by |z| and
   one summary line. `digest`/`off` → skip immediate send.
8. Reconcile: for each `ListOpenEvents`: (a) resolve when the 2 most recent
   settled dates after `last_bucket_date` both judge quiet; (b) retract when
   re-running Detect on `last_bucket_date` with current buckets no longer
   fires (data corrected).
9. `CleanupRetention(400, 90)`.

- [ ] **Step 1: unit tests with fakes** — table-driven over ProcessOnce:
  detection_disabled skips; backfill gate defers detection; settle delay
  respected (bucket for "yesterday" not judged when now < midnight+3h);
  NEW → action upserted + notify sent once; ONGOING → no action, no notify;
  notify_mode=off → no send; fuse at >20; drop suppressed for cluster
  by default. Use a fixed `now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)`
  and fake repos recording calls.
- [ ] **Step 2: fail → Step 3: implement → Step 4: pass** (`go test -race ./internal/service/anomaly/`)
- [ ] **Step 5: pg end-to-end test** — seed 9 weeks of steady feedback
  (12/day) + a 40-count spike on the target date concentrated in source
  `zendesk`; run `ProcessOnce` with `now` = target date +1d +4h; assert:
  one `anomaly_events` row (spike, z>2.5, evidence contribution names
  zendesk with share ≥ 0.5), linked `feedback_quality_actions` row
  (`action_key='anomaly:total'`, severity per rule), run marked done;
  second `ProcessOnce` same now → no duplicate event (ONGOING path), no
  second action. Then: two quiet settled days later → event auto-resolved.
  Two workers racing: run two `ProcessOnce` concurrently → exactly one
  detection (claim).
- [ ] **Step 6: server + config wiring** — `startAnomalyWorker` mirroring
  `startDigestWorker` (`cmd/attune/server.go:725-736`), `safego(ctx,
  "anomaly", ...)`; config keys + defaults + `config.example.yaml`.
  Run: `go build ./... && go vet ./...`
- [ ] **Step 7: lint + commit**

```bash
git add internal/service/anomaly/ internal/repo/anomaly/ cmd/attune/ internal/infra/config/ internal/infra/metrics/ config.example.yaml test/integration/postgres/anomaly/
git commit -m "feat(anomaly): detection worker with quality actions and notifications (#237)"
```

---

### Task 7: Proto surface + generated artifacts

**Files:**
- Create: `proto/attune/v1/anomaly.proto`
- Generated (committed): `internal/proto/attune/v1/anomaly.pb.go`, `console/src/proto/attune/v1/anomaly.ts`, OpenAPI outputs

**Interfaces:**
- Produces the five RPCs (spec §9). Message essentials:

```proto
syntax = "proto3";
package attune.v1;
import "google/api/annotations.proto";

// AnomalyService exposes detected feedback-volume anomalies, their series
// context and evidence, and the per-tenant detection configuration (#237).
service AnomalyService {
  rpc ListAnomalies(ListAnomaliesRequest) returns (ListAnomaliesResponse) {
    option (google.api.http) = {get: "/fb/v1/console/anomalies"};
  }
  rpc GetAnomalySeries(GetAnomalySeriesRequest) returns (GetAnomalySeriesResponse) {
    option (google.api.http) = {get: "/fb/v1/console/anomalies/series"};
  }
  rpc GetAnomalyEvidence(GetAnomalyEvidenceRequest) returns (GetAnomalyEvidenceResponse) {
    option (google.api.http) = {get: "/fb/v1/console/anomalies/{event_id}/evidence"};
  }
  rpc GetAnomalyConfig(GetAnomalyConfigRequest) returns (GetAnomalyConfigResponse) {
    option (google.api.http) = {get: "/fb/v1/console/anomaly-config"};
  }
  rpc UpdateAnomalyConfig(UpdateAnomalyConfigRequest) returns (UpdateAnomalyConfigResponse) {
    option (google.api.http) = {post: "/fb/v1/console/anomaly-config" body: "*"};
  }
}

message AnomalyEvent {
  string event_id = 1;
  string slice_type = 2;
  string slice_key = 3;
  string slice_display = 4;
  string direction = 5;            // spike | drop
  string first_bucket_date = 6;    // YYYY-MM-DD
  string last_bucket_date = 7;
  int64  observed = 8;
  double expected_med = 9;
  double expected_low = 10;
  double expected_high = 11;
  double z_score = 12;
  string status = 13;              // open | resolved | retracted
  string created_at = 14;          // RFC3339
  string resolved_at = 15;
}
message ListAnomaliesRequest { string status = 1; string slice_type = 2; optional int32 limit = 3; }
message ListAnomaliesResponse { repeated AnomalyEvent events = 1; }

message SeriesPoint {
  string date = 1;
  int64  count = 2;
  double expected_med = 3;
  double expected_low = 4;
  double expected_high = 5;
  bool   is_anomalous = 6;
  bool   insufficient = 7;
}
message GetAnomalySeriesRequest { string slice_type = 1; string slice_key = 2; optional int32 days = 3; }
message GetAnomalySeriesResponse { repeated SeriesPoint points = 1; string slice_display = 2; }

message ContributionEntry { string dim = 1; string value = 2; double share = 3; }
message GetAnomalyEvidenceRequest { string event_id = 1; }
message GetAnomalyEvidenceResponse {
  repeated ContributionEntry contributions = 1;
  bool spread = 2;
  repeated int64 feedback_ids = 3;   // live sample ids for drilldown links
}

message AnomalyCustomSlice { string id = 1; string name = 2; string definition_json = 3; bool enabled = 4; string last_error = 5; }
message AnomalyConfig {
  string sensitivity = 1;
  int32  min_count = 2;
  int32  settle_delay_hours = 3;
  repeated string enabled_slice_types = 4;
  repeated string drop_enabled_slice_types = 5;
  string notify_mode = 6;
  bool   detection_enabled = 7;
  repeated AnomalyCustomSlice custom_slices = 8;
}
message GetAnomalyConfigRequest {}
message GetAnomalyConfigResponse { AnomalyConfig config = 1; }
message UpdateAnomalyConfigRequest { AnomalyConfig config = 1; }
message UpdateAnomalyConfigResponse { AnomalyConfig config = 1; string warning = 2; }
```

- [ ] **Step 1: write the proto**, **Step 2: generate + lint**

Run: `cd /Users/phj/Develop/attune && make proto 2>&1 | tail -5`
Expected: buf lint clean, no breaking, artifacts regenerated.

- [ ] **Step 3: commit**

```bash
git add proto/ internal/proto/ console/src/proto/ docs/openapi/ 2>/dev/null; git add -A proto internal/proto console/src/proto
git commit -m "feat(anomaly): AnomalyService proto surface (#237)"
```

---

### Task 8: Console handlers + router wiring

**Files:**
- Create: `internal/handlers/console/anomaly/handler.go`
- Create: `internal/handlers/console/anomaly/handler_test.go`
- Modify: `internal/handlers/console/router.go` (route table comment + registration, viewer/admin middleware — follow the quality-actions rows at `router.go:144-145`)

**Interfaces:**
- Consumes: Task 4 repo methods via interface slices; Task 1 `Detect` (series replay); `enrichconfig`-style `SetAuditLogger(auditRecorder)` for `anomaly_config.update`.
- Produces handler methods bound with `dispatcher.Bind` exactly like
  `feedback.QualityActionHandler` (`internal/handlers/console/feedback/quality_actions.go`):
  `ListAnomalies`, `GetAnomalySeries`, `GetAnomalyEvidence`, `GetAnomalyConfig`, `UpdateAnomalyConfig`.

**Validation rules (each is a test):**
- `days` ∈ [1,180] default 90; `limit` ∈ [1,200] default 50.
- `slice_type` when present must be one of the six; `slice_key` ≤ 120 chars.
- `event_id` must parse as UUID → else `VALIDATION`.
- Unknown event → `NOT_FOUND`. Cross-tenant access impossible (queries all tenant-scoped).
- UpdateAnomalyConfig validation (spec §9 table): sensitivity/notify_mode
  enums, min_count 0–10000, settle 0–48, drop ⊆ enabled ⊆ full set, ≤20
  custom slices each with 1–3 conditions over whitelisted fields with values
  validated against SourceSet (`domain.DefaultSourceSet` injected set),
  DimensionSet taxonomy, and tenant cohorts; series estimate ≤500 (repo
  count of distinct slice_key over 30 days) else `VALIDATION` with count in
  message; digest mode without digest subscription → 200 + `warning`.
- Series replay: for each date in the window, baseline = 8 prior same-weekday
  buckets, call `Detect` with tenant config — response points carry the same
  verdicts the worker produced.

- [ ] **Step 1: failing handler tests** (httptest through dispatcher.Bind,
  fake repos; assert status codes, error `code` fields, payload shapes)
- [ ] **Step 2: fail → Step 3: implement → Step 4: pass** (`go test -race ./internal/handlers/console/anomaly/`)
- [ ] **Step 5: router wiring + router_http_test.go additions** (follow the
  existing route-table test for quality-actions), `go build ./...`
- [ ] **Step 6: lint-errorcode + commit**

```bash
bash scripts/lint-errorcode.sh && git add internal/handlers/console/ && \
git commit -m "feat(anomaly): console handlers and routes (#237)"
```

---

### Task 9: Console UI — anomalies page, control-tower lane, feature API

**Files:**
- Create: `console/src/features/anomalies/api/anomalies.ts` (query defs + `useUpdateAnomalyConfig` mutation, modeled on `features/quality-actions/api/quality-actions.ts`)
- Create: `console/src/features/anomalies/components/anomaly-card.tsx`
- Create: `console/src/features/anomalies/components/anomaly-series-chart.tsx`
- Create: `console/src/features/anomalies/components/contribution-bars.tsx`
- Create: `console/src/routes/_authed.analytics.anomalies.tsx`
- Create: `console/src/routes/_authed.analytics.anomalies.test.tsx`
- Modify: `console/src/routes/-control-tower-page.tsx` (hero metric: open anomaly count via `ListAnomalies(status=open, limit=1)` total; anomaly quality actions already render through the existing lane — verify signal `anomaly_detection` maps to a titleKey)
- Modify: `console/src/i18n/zh-CN.json` (+ en inline defaults) — `anomalies.*` keys from spec §22

**Interfaces:**
- Consumes: generated `console/src/proto/attune/v1/anomaly.ts` types; `api` client from `@/lib/api-client`; severity palette from control-tower `SignalSeverity`.
- Card copy (i18n default English): `"observed {{observed}}, expected {{med}} ({{low}}–{{high}})"`, ongoing badge `"ongoing {{days}}d"`, retracted badge `"retracted after data correction"`.

**Chart contract (`anomaly-series-chart.tsx`):** props
`{points: SeriesPoint[], height?: number}`; renders SVG — expected band as
translucent area (`expected_low→expected_high`), count line, red dots where
`is_anomalous`; skips band for `insufficient` points. No external chart lib
(match existing hand-rolled SVG stats charts in the codebase).

- [ ] **Step 1: failing vitest** — route wiring test (page renders, tabs
  filter by status, card shows magnitude sentence from fixture event);
  chart component test (band + dot presence via querySelector on SVG);
  contribution bars test (3 bars + spread empty-state).
- [ ] **Step 2: fail → Step 3: implement → Step 4: pass**

Run: `cd /Users/phj/Develop/attune/console && pnpm vitest run src/features/anomalies src/routes/_authed.analytics.anomalies.test.tsx 2>&1 | tail -5`

- [ ] **Step 5: full console gates**

Run: `cd /Users/phj/Develop/attune/console && pnpm tsc -b --noEmit && pnpm biome check src/features/anomalies src/routes && pnpm arch`

- [ ] **Step 6: commit**

```bash
git add console/src/
git commit -m "feat(anomaly): console anomalies page and control-tower integration (#237)"
```

---

### Task 10: Config UI + digest section + docs

**Files:**
- Create: `console/src/routes/_authed.configuration.anomaly-detection.tsx` (+ test)
- Create: `console/src/features/anomalies/components/custom-slice-table.tsx`
- Modify: `internal/service/digest/aggregator.go` + `render.go` — optional `anomalyReader` interface `OpenEventsInWindow(ctx, tenantID string, from, to time.Time) ([]anomalyrepo.Event, error)`; render an "Anomalies" section when non-empty; wire in `cmd/attune/server.go` digest construction
- Create: `docs/anomaly-detection.md` (operator doc: algorithm explanation, config guide, false-positive triage — guardrails.md tone)
- Test: digest render unit test extension (`internal/service/digest/render_test.go` pattern)

- [ ] **Step 1: failing tests** — config form renders defaults, saving posts
  UpdateAnomalyConfig, custom-slice add/remove, error badge on
  `last_error != ''` rows; digest render includes anomaly lines when reader
  returns events and omits the section when empty.
- [ ] **Step 2: fail → Step 3: implement → Step 4: pass**
- [ ] **Step 5: full gates**

Run backend + console suites:
```bash
cd /Users/phj/Develop/attune && go build ./... && go vet ./... && go test -race ./internal/... && make test-integration
cd console && pnpm tsc -b --noEmit && pnpm biome check && pnpm vitest run --coverage && pnpm arch && pnpm exec vite build
```

- [ ] **Step 6: commit**

```bash
git add console/src/ internal/service/digest/ cmd/attune/ docs/anomaly-detection.md
git commit -m "feat(anomaly): config UI, digest anomaly section, operator docs (#237)"
```

---

## Final verification (before PR)

- [ ] `go mod tidy && git diff --exit-code go.mod go.sum`
- [ ] `lizard . -l go -C 15 -T nloc=100 --warnings_only` (repo root) — no new findings
- [ ] `npx -y jscpd . --silent` — under threshold
- [ ] `bash scripts/lint-slog.sh --strict && bash scripts/lint-rawptr.sh && bash scripts/lint-errorcode.sh && bash scripts/lint-integration-layout.sh && bash scripts/lint-artifacts.sh --strict`
- [ ] `buf lint && buf breaking --against '.git#branch=main' && buf generate && git diff --exit-code`
- [ ] CHANGELOG entry present (from Task 1)
- [ ] PR title: `feat(analytics): add anomaly and spike detection for product signals (#237)`
