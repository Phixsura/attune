// SPDX-License-Identifier: Apache-2.0

// Package abtest implements A/B testing for enrichment prompts and
// models. Traffic is split by a deterministic hash of the feedback ID
// so assignments are stable and reproducible.
package abtest

import "hash/fnv"

// Variant represents one arm of an A/B test.
type Variant struct {
	ID         string
	Name       string
	Weight     int
	ModelID    string
	PromptID   string
	SampleSize int
	Successes  int
}

// Experiment defines an A/B test with two or more variants.
type Experiment struct {
	ID       string
	TenantID string
	Name     string
	Variants []Variant
	Active   bool
}

// Assign deterministically assigns a feedback item to a variant based
// on its ID. Uses FNV-1a hash for uniform distribution across the
// weighted variants.
func Assign(exp Experiment, feedbackID int64) string {
	if len(exp.Variants) == 0 {
		return ""
	}
	totalWeight := 0
	for _, v := range exp.Variants {
		totalWeight += v.Weight
	}
	if totalWeight == 0 {
		return exp.Variants[0].ID
	}

	h := fnv.New32a()
	buf := [8]byte{}
	for i := 0; i < 8; i++ {
		buf[i] = byte(feedbackID >> (i * 8))
	}
	_, _ = h.Write(buf[:])
	bucket := int(h.Sum32()) % totalWeight
	if bucket < 0 {
		bucket = -bucket
	}

	cumulative := 0
	for _, v := range exp.Variants {
		cumulative += v.Weight
		if bucket < cumulative {
			return v.ID
		}
	}
	return exp.Variants[len(exp.Variants)-1].ID
}

// WinRate computes the success rate for a variant.
func WinRate(v Variant) float64 {
	if v.SampleSize == 0 {
		return 0
	}
	return float64(v.Successes) / float64(v.SampleSize)
}
