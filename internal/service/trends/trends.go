// SPDX-License-Identifier: Apache-2.0

// Package trends computes topic/theme trend timelines from feedback data.
package trends

// BucketCount is a time-bucketed count for a given label.
type BucketCount struct {
	Bucket string
	Label  string
	Count  int
}

// TrendLine is a named series of time-bucketed counts.
type TrendLine struct {
	Label   string
	Buckets []DataPoint
}

// DataPoint is one point on a trend line.
type DataPoint struct {
	Bucket string
	Count  int
}

// BuildTrends groups raw bucket counts into trend lines by label.
func BuildTrends(data []BucketCount) []TrendLine {
	idx := map[string]int{}
	var lines []TrendLine

	for _, d := range data {
		i, ok := idx[d.Label]
		if !ok {
			i = len(lines)
			idx[d.Label] = i
			lines = append(lines, TrendLine{Label: d.Label})
		}
		lines[i].Buckets = append(lines[i].Buckets, DataPoint{
			Bucket: d.Bucket,
			Count:  d.Count,
		})
	}
	return lines
}
