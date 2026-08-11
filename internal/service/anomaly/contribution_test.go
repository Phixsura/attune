// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"math"
	"testing"
)

func TestTopContributionsSingleDominant(t *testing.T) {
	// Total spiked from expected 10 to observed 30 (+20); zendesk explains
	// +13 of that (65%), api +5 (25%), web +2 (10%, dropped).
	groups := map[string][]GroupCount{
		"source": {
			{Value: "zendesk", Observed: 15, BaselineMed: 2},
			{Value: "api", Observed: 10, BaselineMed: 5},
			{Value: "web", Observed: 5, BaselineMed: 3},
		},
	}
	top, spread := TopContributions(groups, 30, 10)
	if spread {
		t.Fatal("spread must be false with a dominant contributor")
	}
	if len(top) != 2 {
		t.Fatalf("want 2 kept contributions (≥15%%), got %d: %+v", len(top), top)
	}
	if top[0].Value != "zendesk" || math.Abs(top[0].Share-0.65) > 0.001 {
		t.Fatalf("want zendesk 0.65 first, got %+v", top[0])
	}
	if top[0].Dimension != "source" {
		t.Fatalf("want dimension source, got %q", top[0].Dimension)
	}
}

func TestTopContributionsCapsAtThree(t *testing.T) {
	// Four values each ≥15%: only top 3 kept.
	groups := map[string][]GroupCount{
		"source": {
			{Value: "a", Observed: 40, BaselineMed: 10}, // +30
			{Value: "b", Observed: 35, BaselineMed: 10}, // +25
			{Value: "c", Observed: 30, BaselineMed: 10}, // +20
			{Value: "d", Observed: 28, BaselineMed: 10}, // +18
		},
	}
	top, spread := TopContributions(groups, 133, 40)
	if spread || len(top) != 3 {
		t.Fatalf("want top-3 kept, got spread=%v len=%d", spread, len(top))
	}
	if top[0].Value != "a" || top[1].Value != "b" || top[2].Value != "c" {
		t.Fatalf("wrong order: %+v", top)
	}
}

func TestTopContributionsSpreadWhenAllBelowThreshold(t *testing.T) {
	// Ten values each ~10% of the delta: broadly distributed.
	groups := map[string][]GroupCount{"source": {}}
	for _, v := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		groups["source"] = append(groups["source"], GroupCount{Value: v, Observed: 3, BaselineMed: 1})
	}
	top, spread := TopContributions(groups, 30, 10)
	if !spread || len(top) != 0 {
		t.Fatalf("want spread with no contributions, got spread=%v %+v", spread, top)
	}
}

func TestTopContributionsZeroDenominator(t *testing.T) {
	groups := map[string][]GroupCount{
		"source": {{Value: "a", Observed: 5, BaselineMed: 5}},
	}
	top, spread := TopContributions(groups, 10, 10) // observed == expected
	if !spread || len(top) != 0 {
		t.Fatalf("zero denominator must yield spread, got spread=%v %+v", spread, top)
	}
	for _, c := range top {
		if math.IsNaN(c.Share) || math.IsInf(c.Share, 0) {
			t.Fatalf("NaN/Inf leaked: %+v", c)
		}
	}
}

func TestTopContributionsDropDirection(t *testing.T) {
	// Drop: observed 4 vs expected 20 (−16); zendesk lost 12 of it (75%).
	groups := map[string][]GroupCount{
		"source": {
			{Value: "zendesk", Observed: 0, BaselineMed: 12},
			{Value: "api", Observed: 4, BaselineMed: 8},
		},
	}
	top, spread := TopContributions(groups, 4, 20)
	if spread {
		t.Fatal("spread must be false")
	}
	if top[0].Value != "zendesk" || math.Abs(top[0].Share-0.75) > 0.001 {
		t.Fatalf("want zendesk share 0.75 (positive share of the drop), got %+v", top[0])
	}
}

func TestTopContributionsMultipleDimensions(t *testing.T) {
	// Both a source and a severity value qualify; ranking is global.
	groups := map[string][]GroupCount{
		"source":   {{Value: "zendesk", Observed: 15, BaselineMed: 5}},  // +10 = 50%
		"severity": {{Value: "critical", Observed: 18, BaselineMed: 2}}, // +16 = 80%
	}
	top, spread := TopContributions(groups, 30, 10)
	if spread || len(top) != 2 {
		t.Fatalf("want 2 kept, got spread=%v %+v", spread, top)
	}
	if top[0].Dimension != "severity" || top[0].Value != "critical" {
		t.Fatalf("want severity=critical first (80%%), got %+v", top[0])
	}
}
