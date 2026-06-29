// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"math"
	"testing"
)

func TestNPSScore(t *testing.T) {
	tests := []struct {
		name       string
		total      int
		promoters  int
		detractors int
		want       float64
	}{
		{"all promoters", 10, 10, 0, 100},
		{"all detractors", 10, 0, 10, -100},
		{"balanced", 100, 40, 20, 20},
		{"no responses", 0, 0, 0, 0},
		{"mixed", 200, 100, 50, 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NPSScore(tt.total, tt.promoters, tt.detractors)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("NPSScore(%d, %d, %d) = %.2f, want %.2f",
					tt.total, tt.promoters, tt.detractors, got, tt.want)
			}
		})
	}
}

func TestCSATScore(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		positive int
		want     float64
	}{
		{"all satisfied", 10, 10, 100},
		{"half satisfied", 10, 5, 50},
		{"none satisfied", 10, 0, 0},
		{"no responses", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CSATScore(tt.total, tt.positive)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("CSATScore(%d, %d) = %.2f, want %.2f",
					tt.total, tt.positive, got, tt.want)
			}
		})
	}
}

func TestCESScore(t *testing.T) {
	tests := []struct {
		name  string
		total int
		sum   float64
		want  float64
	}{
		{"average 3", 10, 30, 3},
		{"average 5.5", 4, 22, 5.5},
		{"no responses", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CESScore(tt.total, tt.sum)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("CESScore(%d, %.1f) = %.2f, want %.2f",
					tt.total, tt.sum, got, tt.want)
			}
		})
	}
}
