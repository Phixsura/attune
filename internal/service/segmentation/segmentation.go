// SPDX-License-Identifier: Apache-2.0

// Package segmentation implements customer segmentation by plan tier,
// cohort, and revenue weighting for feedback prioritization.
package segmentation

// Segment represents a named customer group.
type Segment struct {
	Name     string
	Tier     string
	Criteria map[string]string
}

// WeightedItem is a feedback item with a revenue weight.
type WeightedItem struct {
	FeedbackID int64
	Revenue    float64
	Segment    string
	Score      float64
}

// ApplyRevenueWeight assigns a score to each item based on its revenue
// relative to the total. Higher-revenue segments get proportionally
// higher scores.
func ApplyRevenueWeight(items []WeightedItem) []WeightedItem {
	var totalRevenue float64
	for _, item := range items {
		totalRevenue += item.Revenue
	}
	if totalRevenue == 0 {
		return items
	}
	result := make([]WeightedItem, len(items))
	for i, item := range items {
		result[i] = item
		result[i].Score = item.Revenue / totalRevenue * 100
	}
	return result
}

// GroupBySegment groups items by their segment name.
func GroupBySegment(items []WeightedItem) map[string][]WeightedItem {
	groups := make(map[string][]WeightedItem)
	for _, item := range items {
		groups[item.Segment] = append(groups[item.Segment], item)
	}
	return groups
}
