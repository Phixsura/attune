// SPDX-License-Identifier: Apache-2.0

package abtest

import (
	"math"
	"testing"
)

func TestAssign_Deterministic(t *testing.T) {
	exp := Experiment{
		Variants: []Variant{
			{ID: "control", Weight: 50},
			{ID: "treatment", Weight: 50},
		},
	}
	v1 := Assign(exp, 42)
	v2 := Assign(exp, 42)
	if v1 != v2 {
		t.Errorf("same feedbackID should produce same variant: %q vs %q", v1, v2)
	}
}

func TestAssign_Distribution(t *testing.T) {
	exp := Experiment{
		Variants: []Variant{
			{ID: "control", Weight: 50},
			{ID: "treatment", Weight: 50},
		},
	}
	counts := map[string]int{}
	n := 10000
	for i := 0; i < n; i++ {
		v := Assign(exp, int64(i))
		counts[v]++
	}
	controlPct := float64(counts["control"]) / float64(n)
	if math.Abs(controlPct-0.5) > 0.1 {
		t.Errorf("control = %.2f%%, expected ~50%%", controlPct*100)
	}
}

func TestAssign_EmptyVariants(t *testing.T) {
	exp := Experiment{}
	got := Assign(exp, 1)
	if got != "" {
		t.Errorf("empty variants should return empty string, got %q", got)
	}
}

func TestAssign_SingleVariant(t *testing.T) {
	exp := Experiment{
		Variants: []Variant{{ID: "only", Weight: 100}},
	}
	got := Assign(exp, 999)
	if got != "only" {
		t.Errorf("got %q, want 'only'", got)
	}
}

func TestWinRate(t *testing.T) {
	v := Variant{SampleSize: 200, Successes: 150}
	got := WinRate(v)
	if math.Abs(got-0.75) > 0.001 {
		t.Errorf("win rate = %.3f, want 0.75", got)
	}
}

func TestWinRate_Zero(t *testing.T) {
	v := Variant{SampleSize: 0}
	if WinRate(v) != 0 {
		t.Error("zero sample size should return 0")
	}
}
