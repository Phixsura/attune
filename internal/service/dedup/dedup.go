// SPDX-License-Identifier: Apache-2.0

// Package dedup implements feedback deduplication and merge logic.
// It identifies near-duplicate feedback items and provides a merge
// operation that consolidates duplicates into a canonical item.
package dedup

// Candidate is a potential duplicate pair.
type Candidate struct {
	SourceID int64
	TargetID int64
	Score    float64
}

// MergeResult describes the outcome of merging duplicates.
type MergeResult struct {
	CanonicalID int64
	MergedIDs   []int64
	VoteSum     int
}

// FindCandidates returns pairs of feedback items whose similarity score
// exceeds the given threshold. In a real implementation this would use
// pgvector cosine similarity; here we define the interface.
func FindCandidates(scores []Candidate, threshold float64) []Candidate {
	var out []Candidate
	for _, c := range scores {
		if c.Score >= threshold {
			out = append(out, c)
		}
	}
	return out
}

// PlanMerge selects the canonical item (highest vote count or first by ID)
// and lists the items to be merged into it.
func PlanMerge(ids []int64, votes map[int64]int) MergeResult {
	if len(ids) == 0 {
		return MergeResult{}
	}

	canonicalID := ids[0]
	maxVotes := votes[ids[0]]
	for _, id := range ids[1:] {
		if votes[id] > maxVotes {
			canonicalID = id
			maxVotes = votes[id]
		}
	}

	var merged []int64
	totalVotes := 0
	for _, id := range ids {
		totalVotes += votes[id]
		if id != canonicalID {
			merged = append(merged, id)
		}
	}

	return MergeResult{
		CanonicalID: canonicalID,
		MergedIDs:   merged,
		VoteSum:     totalVotes,
	}
}
