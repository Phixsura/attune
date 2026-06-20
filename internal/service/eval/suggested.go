package eval

import (
	"fmt"
	"math"
	"sort"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// SuggestedAttrsReport captures off-list values from classification.
// It surfaces LLM suggestions that were dropped by the taxonomy filter,
// enabling operators to identify systematic gaps in their taxonomy.
type SuggestedAttrsReport struct {
	// ValueFreq is the frequency distribution of off-list values: dim → value → count.
	ValueFreq map[string]map[string]int `json:"value_freq,omitempty"`

	// Coverage is the ratio of kept values to total values per dimension.
	// 1.0 means all LLM outputs were on-list; 0.85 means 15% were dropped.
	Coverage map[string]float64 `json:"coverage,omitempty"`

	// Candidates are the top off-list values with analysis metadata,
	// sorted by confidence descending. Capped at maxCandidatesPerDim per dim.
	Candidates []SuggestedCandidate `json:"candidates,omitempty"`

	// Recommendations are actionable suggestions for the operator.
	Recommendations []SuggestedRecommendation `json:"recommendations,omitempty"`
}

// SuggestedCandidate is one off-list value with analysis metadata.
type SuggestedCandidate struct {
	Dim            string  `json:"dim"`
	Value          string  `json:"value"`
	Count          int     `json:"count"`
	Confidence     float64 `json:"confidence"`              // frequency as 0-1
	CoverageImpact float64 `json:"coverage_impact"`         // predicted Δcoverage if added
	NearestValue   string  `json:"nearest_value,omitempty"` // P3: most similar existing value
}

// SuggestedRecommendation is an actionable suggestion for the operator.
type SuggestedRecommendation struct {
	Action string `json:"action"` // "add" in P0/P1; P3 may add "merge", "investigate"
	Dim    string `json:"dim"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
	Impact string `json:"impact"` // human-readable, e.g., "+12% coverage"
}

const (
	maxCandidatesPerDim  = 20
	minConfidenceForReco = 0.1 // 10% frequency threshold for recommendations
)

// suggestedAccumulator collects drop diagnostics across eval rows and builds
// a SuggestedAttrsReport.
type suggestedAccumulator struct {
	// valueFreq: dim → value → count
	valueFreq map[string]map[string]int
	// keptCount: dim → count of kept values
	keptCount map[string]int
	// droppedCount: dim → count of dropped values
	droppedCount map[string]int
}

func newSuggestedAccumulator() *suggestedAccumulator {
	return ptrext.Of(suggestedAccumulator{
		valueFreq:    make(map[string]map[string]int),
		keptCount:    make(map[string]int),
		droppedCount: make(map[string]int),
	})
}

// Add processes one row's drop diagnostics and kept attrs.
func (a *suggestedAccumulator) Add(
	diagnostics []domain.AttrDropDiagnostic,
	keptAttrs map[string]any,
	dims domain.DimensionSet,
) {
	// Count kept values per dim
	for _, d := range dims {
		v, ok := keptAttrs[d.Name]
		if !ok {
			continue
		}
		switch d.Kind {
		case domain.DimSingle:
			if s, ok := v.(string); ok && s != "" {
				a.keptCount[d.Name]++
			}
		case domain.DimMulti:
			if arr := toStringSlice(v); len(arr) > 0 {
				a.keptCount[d.Name] += len(arr)
			}
		}
	}

	// Count dropped values per dim and accumulate value frequencies
	for _, diag := range diagnostics {
		if diag.Reason != domain.AttrDropOffListValue {
			continue
		}
		a.droppedCount[diag.Dim] += diag.Count

		if a.valueFreq[diag.Dim] == nil {
			a.valueFreq[diag.Dim] = make(map[string]int)
		}
		for _, v := range diag.Values {
			a.valueFreq[diag.Dim][v]++
		}
	}
}

// Build constructs the SuggestedAttrsReport from accumulated data.
func (a *suggestedAccumulator) Build(sampleSize int) SuggestedAttrsReport {
	if len(a.valueFreq) == 0 && len(a.droppedCount) == 0 {
		return SuggestedAttrsReport{}
	}

	report := SuggestedAttrsReport{
		ValueFreq: a.valueFreq,
		Coverage:  make(map[string]float64),
	}

	// Compute coverage per dim
	allDims := make(map[string]bool)
	for dim := range a.keptCount {
		allDims[dim] = true
	}
	for dim := range a.droppedCount {
		allDims[dim] = true
	}
	for dim := range allDims {
		kept := a.keptCount[dim]
		dropped := a.droppedCount[dim]
		total := kept + dropped
		if total == 0 {
			report.Coverage[dim] = 1.0
		} else {
			report.Coverage[dim] = float64(kept) / float64(total)
		}
	}

	// Build candidates from valueFreq
	var candidates []SuggestedCandidate
	for dim, values := range a.valueFreq {
		totalDroppedForDim := a.droppedCount[dim]
		coverage := report.Coverage[dim]

		// Sort values by count descending
		type valCount struct {
			val   string
			count int
		}
		var sorted []valCount
		for v, c := range values {
			sorted = append(sorted, valCount{v, c})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].count > sorted[j].count
		})

		// Take top N
		for i, vc := range sorted {
			if i >= maxCandidatesPerDim {
				break
			}
			confidence := computeConfidence(vc.count, sampleSize)
			impact := predictImpact(vc.count, coverage, totalDroppedForDim)
			candidates = append(candidates, SuggestedCandidate{
				Dim:            dim,
				Value:          vc.val,
				Count:          vc.count,
				Confidence:     confidence,
				CoverageImpact: impact,
			})
		}
	}

	// Sort all candidates by confidence descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Confidence > candidates[j].Confidence
	})
	report.Candidates = candidates

	// Generate recommendations for high-confidence candidates
	for _, c := range candidates {
		if c.Confidence < minConfidenceForReco {
			continue
		}
		report.Recommendations = append(report.Recommendations, SuggestedRecommendation{
			Action: "add",
			Dim:    c.Dim,
			Value:  c.Value,
			Reason: formatReason(c.Count, sampleSize),
			Impact: formatImpact(c.CoverageImpact),
		})
	}

	return report
}

// computeConfidence returns the frequency as the confidence score.
// Simple and interpretable: 0.24 means "appeared in 24% of samples".
func computeConfidence(count int, sampleSize int) float64 {
	if sampleSize == 0 {
		return 0
	}
	return math.Min(1.0, float64(count)/float64(sampleSize))
}

// predictImpact estimates coverage gain if the value were added to taxonomy.
func predictImpact(count int, currentCoverage float64, totalDroppedForDim int) float64 {
	if totalDroppedForDim == 0 {
		return 0
	}
	coverageGap := 1 - currentCoverage
	valueShare := float64(count) / float64(totalDroppedForDim)
	return coverageGap * valueShare
}

func formatReason(count, sampleSize int) string {
	pct := 100.0 * float64(count) / float64(sampleSize)
	return fmt.Sprintf("appeared %d times (%.0f%%)", count, pct)
}

func formatImpact(impact float64) string {
	return fmt.Sprintf("+%.0f%% coverage", impact*100)
}

// toStringSlice converts any to []string (handles []any from JSON decode).
func toStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
