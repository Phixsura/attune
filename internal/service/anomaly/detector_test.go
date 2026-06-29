// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"math"
	"testing"
)

func TestDetect_Spike(t *testing.T) {
	points := []Point{
		{"d1", 10},
		{"d2", 12},
		{"d3", 11},
		{"d4", 10},
		{"d5", 13},
		{"d6", 11},
		{"d7", 50},
	}
	anomalies := Detect(points, 2.0)
	if len(anomalies) != 1 {
		t.Fatalf("got %d anomalies, want 1", len(anomalies))
	}
	a := anomalies[0]
	if a.Bucket != "d7" {
		t.Errorf("bucket = %q, want d7", a.Bucket)
	}
	if a.Count != 50 {
		t.Errorf("count = %.0f, want 50", a.Count)
	}
	if a.Deviation < 2.0 {
		t.Errorf("deviation = %.2f, want >= 2.0", a.Deviation)
	}
}

func TestDetect_NoSpike(t *testing.T) {
	points := []Point{
		{"d1", 10}, {"d2", 11}, {"d3", 10}, {"d4", 11},
	}
	anomalies := Detect(points, 2.0)
	if len(anomalies) != 0 {
		t.Fatalf("got %d anomalies, want 0", len(anomalies))
	}
}

func TestDetect_TooFewPoints(t *testing.T) {
	anomalies := Detect([]Point{{"d1", 10}}, 2.0)
	if anomalies != nil {
		t.Fatal("expected nil for < 3 points")
	}
}

func TestDetect_ZeroStdDev(t *testing.T) {
	points := []Point{{"d1", 5}, {"d2", 5}, {"d3", 5}}
	anomalies := Detect(points, 1.0)
	if anomalies != nil {
		t.Fatal("expected nil when stddev is 0")
	}
}

func TestStats(t *testing.T) {
	points := []Point{{"a", 2}, {"b", 4}, {"c", 4}, {"d", 4}, {"e", 5}, {"f", 5}, {"g", 7}, {"h", 9}}
	mean, stddev := stats(points)
	if math.Abs(mean-5.0) > 0.01 {
		t.Errorf("mean = %.2f, want 5.0", mean)
	}
	if stddev < 1.0 || stddev > 3.0 {
		t.Errorf("stddev = %.2f, expected between 1 and 3", stddev)
	}
}
