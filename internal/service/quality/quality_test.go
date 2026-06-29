// SPDX-License-Identifier: Apache-2.0

package quality

import (
	"math"
	"testing"
)

func TestEvaluate(t *testing.T) {
	r := Evaluate(100, 90, 85, 80)
	wantAccuracy := 0.9
	wantCompleteness := 0.85
	wantConsistency := 0.8
	wantOverall := wantAccuracy*0.5 + wantCompleteness*0.25 + wantConsistency*0.25

	if len(r.Metrics) != 3 {
		t.Fatalf("got %d metrics, want 3", len(r.Metrics))
	}
	if math.Abs(r.Metrics[0].Value-wantAccuracy) > 0.001 {
		t.Errorf("accuracy = %.3f, want %.3f", r.Metrics[0].Value, wantAccuracy)
	}
	if math.Abs(r.OverallScore-wantOverall) > 0.001 {
		t.Errorf("overall = %.3f, want %.3f", r.OverallScore, wantOverall)
	}
}

func TestEvaluate_ZeroItems(t *testing.T) {
	r := Evaluate(0, 0, 0, 0)
	if r.OverallScore != 0 {
		t.Errorf("overall = %.3f, want 0 for empty batch", r.OverallScore)
	}
}

func TestGrade(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{0.95, "A"},
		{0.85, "B"},
		{0.75, "C"},
		{0.65, "D"},
		{0.50, "F"},
	}
	for _, tt := range tests {
		got := Grade(tt.score)
		if got != tt.want {
			t.Errorf("Grade(%.2f) = %q, want %q", tt.score, got, tt.want)
		}
	}
}
