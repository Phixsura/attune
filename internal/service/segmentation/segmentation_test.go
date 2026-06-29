// SPDX-License-Identifier: Apache-2.0

package segmentation

import (
	"math"
	"testing"
)

func TestApplyRevenueWeight(t *testing.T) {
	items := []WeightedItem{
		{FeedbackID: 1, Revenue: 1000, Segment: "enterprise"},
		{FeedbackID: 2, Revenue: 500, Segment: "pro"},
		{FeedbackID: 3, Revenue: 500, Segment: "pro"},
	}

	weighted := ApplyRevenueWeight(items)
	if math.Abs(weighted[0].Score-50) > 0.01 {
		t.Errorf("enterprise score = %.2f, want 50", weighted[0].Score)
	}
	if math.Abs(weighted[1].Score-25) > 0.01 {
		t.Errorf("pro score = %.2f, want 25", weighted[1].Score)
	}
}

func TestApplyRevenueWeight_ZeroRevenue(t *testing.T) {
	items := []WeightedItem{
		{FeedbackID: 1, Revenue: 0},
	}
	result := ApplyRevenueWeight(items)
	if result[0].Score != 0 {
		t.Errorf("score = %.2f, want 0 for zero revenue", result[0].Score)
	}
}

func TestGroupBySegment(t *testing.T) {
	items := []WeightedItem{
		{FeedbackID: 1, Segment: "enterprise"},
		{FeedbackID: 2, Segment: "pro"},
		{FeedbackID: 3, Segment: "enterprise"},
	}

	groups := GroupBySegment(items)
	if len(groups["enterprise"]) != 2 {
		t.Errorf("enterprise count = %d, want 2", len(groups["enterprise"]))
	}
	if len(groups["pro"]) != 1 {
		t.Errorf("pro count = %d, want 1", len(groups["pro"]))
	}
}
