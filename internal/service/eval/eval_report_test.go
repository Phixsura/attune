// ptrext:file-allow test fixtures construct EvalReports by address.
package eval

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
)

func baseReport() *EvalReport {
	return &EvalReport{
		Mode:        "consistency",
		LabelSource: "ai-rerun",
		GeneratedAt: time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
		Since:       time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
		SampleSize:  10,
		Dims:        map[string]DimScore{},
	}
}

// --- FormatReport: header + empty-sample short-circuit -----------------

func TestFormatReport_EmptySample(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.SampleSize = 0

	out := FormatReport(rep)

	require.Contains(t, out, "# attune eval report")
	require.Contains(t, out, "mode: consistency")
	require.Contains(t, out, "label source: ai-rerun")
	require.Contains(t, out, "sample size: 0")
	require.Contains(t, out, "no rows sampled")
	// agreement table must NOT render for an empty sample.
	require.NotContains(t, out, "agreement (per dimension)")
}

func TestFormatReport_OmitsZeroCostAndZeroSince(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Since = time.Time{} // zero
	rep.LLMCostYuan = 0
	rep.Dims["type"] = DimScore{Name: "type", Kind: domain.DimSingle, Total: 10, Match: 9}

	out := FormatReport(rep)

	require.NotContains(t, out, "since:")
	require.NotContains(t, out, "LLM cost")
}

func TestFormatReport_RendersCostWhenPositive(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.LLMCostYuan = 0.08
	rep.Dims["type"] = DimScore{Name: "type", Kind: domain.DimSingle, Total: 10, Match: 8}

	out := FormatReport(rep)

	require.Contains(t, out, "LLM cost (est): ¥0.08")
	require.Contains(t, out, "since:")
}

// --- FormatReport: per-dim agreement rows ------------------------------

func TestFormatReport_SingleDimMatchRate(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Dims["severity"] = DimScore{Name: "severity", Kind: domain.DimSingle, Total: 10, Match: 7}

	out := FormatReport(rep)

	require.Contains(t, out, "agreement (per dimension)")
	require.Contains(t, out, "| severity | single | match rate 70.0% (7/10) | 80% |")
}

func TestFormatReport_MultiDimAvgIoU(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Dims["modules"] = DimScore{Name: "modules", Kind: domain.DimMulti, Total: 4, SumIoU: 3.0}

	out := FormatReport(rep)

	require.Contains(t, out, "| modules | multi | avg IoU 0.75 | 0.75 |")
}

func TestFormatReport_DimsSortedAlphabetically(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Dims["zeta"] = DimScore{Name: "zeta", Kind: domain.DimSingle, Total: 1, Match: 1}
	rep.Dims["alpha"] = DimScore{Name: "alpha", Kind: domain.DimSingle, Total: 1, Match: 1}

	out := FormatReport(rep)

	require.Less(t, strings.Index(out, "| alpha |"), strings.Index(out, "| zeta |"))
}

// --- FormatReport: mismatch table + top-10 cap -------------------------

func TestFormatReport_MismatchTableRendersSingleAndMulti(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Dims["type"] = DimScore{Name: "type", Kind: domain.DimSingle, Total: 1}
	rep.Mismatches = []Mismatch{{
		FeedbackID: 42,
		Content:    "checkout flow is broken",
		Diffs: map[string]DimDiff{
			"type":    {Kind: domain.DimSingle, Old: "bug", New: "feature"},
			"modules": {Kind: domain.DimMulti, IoU: 0.5},
		},
	}}

	out := FormatReport(rep)

	require.Contains(t, out, "top 10 mismatches")
	require.Contains(t, out, "| 42 |")
	require.Contains(t, out, "type: bug→feature")
	require.Contains(t, out, "modules: IoU 0.50")
	require.Contains(t, out, "checkout flow is broken")
}

func TestFormatReport_MismatchTableCapsAtTen(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.SampleSize = 20
	for i := 0; i < 15; i++ {
		rep.Mismatches = append(rep.Mismatches, Mismatch{
			FeedbackID: int64(i),
			Content:    "row",
			Diffs:      map[string]DimDiff{"type": {Kind: domain.DimSingle, Old: "a", New: "b"}},
		})
	}

	out := FormatReport(rep)

	// IDs 0..9 present, 10..14 dropped by the cap.
	require.Contains(t, out, "| 9 |")
	require.NotContains(t, out, "| 10 |")
	require.NotContains(t, out, "| 14 |")
}

func TestFormatReport_NoMismatchSectionWhenEmpty(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Dims["type"] = DimScore{Name: "type", Kind: domain.DimSingle, Total: 10, Match: 10}

	out := FormatReport(rep)
	require.NotContains(t, out, "mismatches")
}

// --- FormatReport: suggested-values section (#83) ----------------------

func TestFormatReport_SuggestedSectionAndRecommendations(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Dims["modules"] = DimScore{Name: "modules", Kind: domain.DimMulti, Total: 10, SumIoU: 8}
	rep.SuggestedAttrs = SuggestedAttrsReport{
		Coverage: map[string]float64{"modules": 0.85},
		Candidates: []SuggestedCandidate{
			{Dim: "modules", Value: "checkout", Count: 12},
		},
		Recommendations: []SuggestedRecommendation{
			{Action: "add", Dim: "modules", Value: "checkout", Reason: "appeared 12 times", Impact: "+12% coverage"},
		},
	}

	out := FormatReport(rep)

	require.Contains(t, out, "suggested values (off-list)")
	require.Contains(t, out, "| modules | 85% | checkout (12) |")
	require.Contains(t, out, "Recommendations:")
	require.Contains(t, out, "• ADD modules.checkout — appeared 12 times, +12% coverage")
}

func TestFormatReport_SuggestedCoverageDimWithNoCandidatesShowsDash(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Dims["type"] = DimScore{Name: "type", Kind: domain.DimSingle, Total: 10, Match: 10}
	// coverage present but no candidates for that dim → "—".
	rep.SuggestedAttrs = SuggestedAttrsReport{
		Coverage: map[string]float64{"type": 1.0},
	}

	out := FormatReport(rep)
	require.Contains(t, out, "| type | 100% | — |")
}

func TestFormatReport_NoSuggestedSectionWhenEmpty(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Dims["type"] = DimScore{Name: "type", Kind: domain.DimSingle, Total: 10, Match: 10}

	out := FormatReport(rep)
	require.NotContains(t, out, "suggested values")
}

// --- formatTopSuggestions ----------------------------------------------

func TestFormatTopSuggestions_CapsAtN(t *testing.T) {
	t.Parallel()
	cands := []SuggestedCandidate{
		{Dim: "modules", Value: "a", Count: 5},
		{Dim: "modules", Value: "b", Count: 4},
		{Dim: "modules", Value: "c", Count: 3},
		{Dim: "modules", Value: "d", Count: 2}, // beyond n=3
		{Dim: "other", Value: "x", Count: 9},   // wrong dim, skipped
	}
	got := formatTopSuggestions(cands, "modules", 3)
	require.Equal(t, "a (5), b (4), c (3)", got)
}

func TestFormatTopSuggestions_NoMatchReturnsEmpty(t *testing.T) {
	t.Parallel()
	cands := []SuggestedCandidate{{Dim: "other", Value: "x", Count: 1}}
	require.Equal(t, "", formatTopSuggestions(cands, "modules", 3))
}

// --- summarizeDiffs / truncateForReport edge cases ---------------------

func TestSummarizeDiffs_Empty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", summarizeDiffs(nil))
	require.Equal(t, "", summarizeDiffs(map[string]DimDiff{}))
}

func TestSummarizeDiffs_SortedByName(t *testing.T) {
	t.Parallel()
	diffs := map[string]DimDiff{
		"zeta":  {Kind: domain.DimSingle, Old: "1", New: "2"},
		"alpha": {Kind: domain.DimMulti, IoU: 0.3},
	}
	got := summarizeDiffs(diffs)
	require.Less(t, strings.Index(got, "alpha"), strings.Index(got, "zeta"))
	require.Contains(t, got, "alpha: IoU 0.30")
	require.Contains(t, got, "zeta: 1→2")
}

func TestTruncateForReport(t *testing.T) {
	t.Parallel()
	require.Equal(t, "short", truncateForReport("short", 60))
	require.Equal(t, "a b c", truncateForReport("a\nb\nc", 60), "newlines collapsed to spaces")

	long := strings.Repeat("x", 100)
	got := truncateForReport(long, 60)
	require.Equal(t, 60+len("…"), len(got))
	require.True(t, strings.HasSuffix(got, "…"))
}

// --- FormatJSON --------------------------------------------------------

func TestFormatJSON_RoundTrips(t *testing.T) {
	t.Parallel()
	rep := baseReport()
	rep.Dims["type"] = DimScore{Name: "type", Kind: domain.DimSingle, Total: 10, Match: 9}
	rep.SuggestedAttrs = SuggestedAttrsReport{
		Coverage: map[string]float64{"modules": 0.9},
	}

	raw, err := rep.FormatJSON()
	require.NoError(t, err)

	var back EvalReport
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Equal(t, "consistency", back.Mode)
	require.Equal(t, 10, back.SampleSize)
	require.Equal(t, 0.9, back.SuggestedAttrs.Coverage["modules"])
	// pretty-printed (indented) output.
	require.Contains(t, string(raw), "\n ")
}
