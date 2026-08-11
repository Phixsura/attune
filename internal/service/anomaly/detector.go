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

// dropBaselineFloor is the minimum baseline median for a drop verdict: a
// series that never carried ~5/day of volume produces no actionable drop.
const dropBaselineFloor = 5

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
//	sigma = max(MAD/0.6745, sqrt(max(med,1)), 1)  — the Poisson floor keeps
//	  a sampled MAD of ~8 points from underestimating count noise; the
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
	case z <= -cfg.ZThreshold && med >= dropBaselineFloor:
		v.Direction = DirectionDrop
	}
	return v
}

// median of int64 values (mean of the middle pair for even lengths).
// Copies the input so callers' slices are never reordered.
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
