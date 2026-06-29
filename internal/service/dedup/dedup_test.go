// SPDX-License-Identifier: Apache-2.0

package dedup

import "testing"

func TestFindCandidates(t *testing.T) {
	scores := []Candidate{
		{1, 2, 0.95},
		{1, 3, 0.60},
		{2, 3, 0.85},
	}
	got := FindCandidates(scores, 0.80)
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2", len(got))
	}
}

func TestFindCandidates_NoneAboveThreshold(t *testing.T) {
	scores := []Candidate{{1, 2, 0.5}}
	got := FindCandidates(scores, 0.9)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestPlanMerge(t *testing.T) {
	ids := []int64{10, 20, 30}
	votes := map[int64]int{10: 3, 20: 7, 30: 2}

	result := PlanMerge(ids, votes)
	if result.CanonicalID != 20 {
		t.Errorf("canonical = %d, want 20 (highest votes)", result.CanonicalID)
	}
	if len(result.MergedIDs) != 2 {
		t.Errorf("merged count = %d, want 2", len(result.MergedIDs))
	}
	if result.VoteSum != 12 {
		t.Errorf("vote sum = %d, want 12", result.VoteSum)
	}
}

func TestPlanMerge_Empty(t *testing.T) {
	result := PlanMerge(nil, nil)
	if result.CanonicalID != 0 {
		t.Errorf("expected zero result for empty input")
	}
}
