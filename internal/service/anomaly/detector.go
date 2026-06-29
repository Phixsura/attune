// SPDX-License-Identifier: Apache-2.0

// Package anomaly implements feedback volume spike detection using
// a simple z-score based approach against a sliding window baseline.
package anomaly

import "math"

// Point is a time-bucketed count.
type Point struct {
	Bucket string
	Count  float64
}

// Anomaly describes a detected spike.
type Anomaly struct {
	Bucket    string
	Count     float64
	Baseline  float64
	Deviation float64
}

// Detect finds anomalies in a time series. A point is anomalous when
// its z-score exceeds the given threshold (typically 2.0–3.0).
func Detect(points []Point, threshold float64) []Anomaly {
	if len(points) < 3 {
		return nil
	}
	mean, stddev := stats(points)
	if stddev == 0 {
		return nil
	}
	var out []Anomaly
	for _, p := range points {
		z := (p.Count - mean) / stddev
		if z >= threshold {
			out = append(out, Anomaly{
				Bucket:    p.Bucket,
				Count:     p.Count,
				Baseline:  mean,
				Deviation: z,
			})
		}
	}
	return out
}

func stats(points []Point) (mean, stddev float64) {
	n := float64(len(points))
	var sum float64
	for _, p := range points {
		sum += p.Count
	}
	mean = sum / n
	var variance float64
	for _, p := range points {
		d := p.Count - mean
		variance += d * d
	}
	stddev = math.Sqrt(variance / n)
	return mean, stddev
}
