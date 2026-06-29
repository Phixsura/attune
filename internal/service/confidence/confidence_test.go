// SPDX-License-Identifier: Apache-2.0

package confidence

import (
	"math"
	"testing"
)

func TestCompute(t *testing.T) {
	s := Compute(0.9, 0.8, 0.7, 0.75)
	want := 0.9*0.5 + 0.8*0.3 + 0.7*0.2
	if math.Abs(s.Overall-want) > 0.001 {
		t.Errorf("overall = %.3f, want %.3f", s.Overall, want)
	}
	if s.NeedsReview {
		t.Error("should not need review when above threshold")
	}
}

func TestCompute_BelowThreshold(t *testing.T) {
	s := Compute(0.3, 0.2, 0.1, 0.5)
	if !s.NeedsReview {
		t.Error("should need review when below threshold")
	}
}

func TestBucket(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{0.95, "high"},
		{0.9, "high"},
		{0.8, "medium"},
		{0.7, "medium"},
		{0.5, "low"},
		{0.0, "low"},
	}
	for _, tt := range tests {
		got := Bucket(tt.score)
		if got != tt.want {
			t.Errorf("Bucket(%.1f) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestCalibrationScore(t *testing.T) {
	got := CalibrationScore(85, 100)
	if math.Abs(got-0.85) > 0.001 {
		t.Errorf("got %.3f, want 0.85", got)
	}
}

func TestCalibrationScore_Zero(t *testing.T) {
	got := CalibrationScore(0, 0)
	if got != 0 {
		t.Errorf("got %.3f, want 0 for zero total", got)
	}
}
