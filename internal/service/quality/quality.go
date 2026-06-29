// SPDX-License-Identifier: Apache-2.0

// Package quality implements AI output quality scoring for enrichment
// results. It evaluates classification accuracy, consistency, and
// completeness to produce quality metrics per tenant and model.
package quality

// Metric is a single quality measurement.
type Metric struct {
	Name  string
	Value float64
	Max   float64
}

// Report is the quality assessment for a batch of enrichments.
type Report struct {
	TenantID     string
	ModelID      string
	TotalItems   int
	Metrics      []Metric
	OverallScore float64
}

// Evaluate computes quality metrics for a batch of enrichment results.
// Accuracy = correct / total, Completeness = items with all fields
// filled / total, Consistency = items where re-classification matches
// original / total.
func Evaluate(total, correct, complete, consistent int) Report {
	accuracy := safeDiv(correct, total)
	completeness := safeDiv(complete, total)
	consistency := safeDiv(consistent, total)
	overall := accuracy*0.5 + completeness*0.25 + consistency*0.25

	return Report{
		TotalItems: total,
		Metrics: []Metric{
			{Name: "accuracy", Value: accuracy, Max: 1.0},
			{Name: "completeness", Value: completeness, Max: 1.0},
			{Name: "consistency", Value: consistency, Max: 1.0},
		},
		OverallScore: overall,
	}
}

func safeDiv(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

// Grade maps an overall score to a letter grade.
func Grade(score float64) string {
	switch {
	case score >= 0.9:
		return "A"
	case score >= 0.8:
		return "B"
	case score >= 0.7:
		return "C"
	case score >= 0.6:
		return "D"
	default:
		return "F"
	}
}
