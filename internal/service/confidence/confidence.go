// SPDX-License-Identifier: Apache-2.0

// Package confidence provides scoring and calibration for AI enrichment
// outputs. It computes reliability scores based on multiple signals
// (LLM self-reported confidence, cross-model agreement, historical
// accuracy) and flags items that fall below the configured threshold
// for human review.
package confidence

// Score represents an enrichment confidence assessment.
type Score struct {
	Overall     float64
	LLMSelf     float64
	Agreement   float64
	Calibration float64
	NeedsReview bool
}

// Compute calculates a composite confidence score from individual signals.
// The overall score is a weighted average of the three sub-scores:
// 50% LLM self-reported, 30% cross-model agreement, 20% calibration.
func Compute(llmSelf, agreement, calibration, threshold float64) Score {
	overall := llmSelf*0.5 + agreement*0.3 + calibration*0.2
	return Score{
		Overall:     overall,
		LLMSelf:     llmSelf,
		Agreement:   agreement,
		Calibration: calibration,
		NeedsReview: overall < threshold,
	}
}

// Bucket maps a confidence score to a human-readable tier.
func Bucket(overall float64) string {
	switch {
	case overall >= 0.9:
		return "high"
	case overall >= 0.7:
		return "medium"
	default:
		return "low"
	}
}

// CalibrationScore computes how well past predictions matched actual
// outcomes. correctPredictions / totalPredictions gives the accuracy
// rate which serves as the calibration input.
func CalibrationScore(correct, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(correct) / float64(total)
}
