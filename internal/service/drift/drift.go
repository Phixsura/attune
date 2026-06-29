// SPDX-License-Identifier: Apache-2.0

// Package drift detects classification quality drift by comparing
// recent enrichment distributions against a baseline. When the
// distribution shifts beyond the configured threshold, it signals
// that the model or prompt may need recalibration.
package drift

import "math"

// Distribution is a label → proportion map (values sum to ~1.0).
type Distribution map[string]float64

// DriftResult describes the comparison between baseline and current.
type DriftResult struct {
	Score     float64
	Drifted   bool
	Threshold float64
	Details   map[string]float64
}

// Detect computes the Jensen-Shannon divergence between baseline and
// current distributions. Returns drifted=true when the divergence
// exceeds the given threshold.
func Detect(baseline, current Distribution, threshold float64) DriftResult {
	details := make(map[string]float64)
	allKeys := make(map[string]bool)
	for k := range baseline {
		allKeys[k] = true
	}
	for k := range current {
		allKeys[k] = true
	}

	m := make(Distribution, len(allKeys))
	for k := range allKeys {
		m[k] = (baseline[k] + current[k]) / 2
	}

	var jsd float64
	for k := range allKeys {
		p := baseline[k]
		q := current[k]
		mid := m[k]
		if mid == 0 {
			continue
		}
		if p > 0 {
			jsd += p * math.Log2(p/mid) / 2
		}
		if q > 0 {
			jsd += q * math.Log2(q/mid) / 2
		}
		details[k] = current[k] - baseline[k]
	}

	return DriftResult{
		Score:     jsd,
		Drifted:   jsd > threshold,
		Threshold: threshold,
		Details:   details,
	}
}
