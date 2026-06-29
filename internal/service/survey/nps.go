// SPDX-License-Identifier: Apache-2.0

// Package survey implements survey scoring logic (NPS/CSAT/CES).
package survey

// NPSScore calculates the Net Promoter Score from promoter and detractor
// counts. NPS = %promoters - %detractors, range [-100, 100].
func NPSScore(total, promoters, detractors int) float64 {
	if total == 0 {
		return 0
	}
	return (float64(promoters) - float64(detractors)) / float64(total) * 100
}

// CSATScore calculates the Customer Satisfaction Score.
// CSAT = (positive responses / total) * 100, where positive = score >= threshold.
func CSATScore(total, positive int) float64 {
	if total == 0 {
		return 0
	}
	return float64(positive) / float64(total) * 100
}

// CESScore calculates the Customer Effort Score average.
// CES = average of all scores (typically 1-7 scale).
func CESScore(total int, sumScores float64) float64 {
	if total == 0 {
		return 0
	}
	return sumScores / float64(total)
}
