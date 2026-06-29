// SPDX-License-Identifier: Apache-2.0

package trends

import "testing"

func TestBuildTrends(t *testing.T) {
	data := []BucketCount{
		{"2026-W01", "bug", 5},
		{"2026-W01", "feature", 3},
		{"2026-W02", "bug", 8},
		{"2026-W02", "feature", 2},
		{"2026-W03", "bug", 4},
	}

	lines := BuildTrends(data)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	var bugLine TrendLine
	for _, l := range lines {
		if l.Label == "bug" {
			bugLine = l
			break
		}
	}
	if len(bugLine.Buckets) != 3 {
		t.Fatalf("bug has %d buckets, want 3", len(bugLine.Buckets))
	}
	if bugLine.Buckets[0].Count != 5 || bugLine.Buckets[1].Count != 8 {
		t.Errorf("unexpected bug counts: %v", bugLine.Buckets)
	}
}

func TestBuildTrends_Empty(t *testing.T) {
	lines := BuildTrends(nil)
	if lines != nil {
		t.Errorf("expected nil for empty data, got %v", lines)
	}
}
