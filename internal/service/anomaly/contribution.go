// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"math"
	"sort"
)

// GroupCount is one grouping value's observed count on the anomalous day
// and its same-weekday baseline median.
type GroupCount struct {
	Value       string
	Observed    int64
	BaselineMed float64
}

// Contribution attributes a share of the total deviation to one grouping
// value: share = (obs_v − exp_v) / (obs_total − exp_total). Shares are
// positive when the value moved in the anomaly's direction.
type Contribution struct {
	Dimension string  `json:"dim"`
	Value     string  `json:"value"`
	Share     float64 `json:"share"`
}

// contributionShareFloor is the minimum |share| to surface; below it the
// deviation is considered broadly distributed.
const contributionShareFloor = 0.15

// contributionTopN caps how many contributions are surfaced.
const contributionTopN = 3

// TopContributions ranks grouping values by their share of the total
// deviation, keeping |share| ≥ 15% up to the top 3. It returns
// (nil, spread=true) when nothing qualifies or the denominator is ~0 —
// "broadly distributed, no concentrated origin".
func TopContributions(
	groups map[string][]GroupCount, obsTotal int64, expTotal float64,
) ([]Contribution, bool) {
	denom := float64(obsTotal) - expTotal
	if math.Abs(denom) < 1 {
		return nil, true
	}
	var all []Contribution
	for dim, counts := range groups {
		for _, g := range counts {
			share := (float64(g.Observed) - g.BaselineMed) / denom
			if share >= contributionShareFloor {
				all = append(all, Contribution{Dimension: dim, Value: g.Value, Share: share})
			}
		}
	}
	if len(all) == 0 {
		return nil, true
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Share != all[j].Share {
			return all[i].Share > all[j].Share
		}
		if all[i].Dimension != all[j].Dimension {
			return all[i].Dimension < all[j].Dimension
		}
		return all[i].Value < all[j].Value
	})
	if len(all) > contributionTopN {
		all = all[:contributionTopN]
	}
	return all, false
}
