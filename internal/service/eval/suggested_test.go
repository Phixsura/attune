package eval

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestSuggestedAccumulator_Empty(t *testing.T) {
	acc := newSuggestedAccumulator()
	report := acc.Build(10)
	if len(report.ValueFreq) != 0 {
		t.Errorf("expected empty ValueFreq, got %v", report.ValueFreq)
	}
	if len(report.Coverage) != 0 {
		t.Errorf("expected empty Coverage, got %v", report.Coverage)
	}
}

func TestSuggestedAccumulator_SingleDimSingleValue(t *testing.T) {
	acc := newSuggestedAccumulator()
	dims := domain.DimensionSet{
		{Name: "modules", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{{Value: "payment"}}},
	}
	diagnostics := []domain.AttrDropDiagnostic{
		{Dim: "modules", Reason: domain.AttrDropOffListValue, Values: []string{"checkout"}, Count: 1},
	}
	keptAttrs := map[string]any{"modules": []string{"payment"}}

	acc.Add(diagnostics, keptAttrs, dims)
	report := acc.Build(10)

	if report.ValueFreq["modules"]["checkout"] != 1 {
		t.Errorf("expected checkout count 1, got %v", report.ValueFreq["modules"]["checkout"])
	}
	if report.Coverage["modules"] != 0.5 {
		t.Errorf("expected coverage 0.5 (1 kept, 1 dropped), got %v", report.Coverage["modules"])
	}
}

func TestSuggestedAccumulator_MultipleRows(t *testing.T) {
	acc := newSuggestedAccumulator()
	dims := domain.DimensionSet{
		{Name: "modules", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{{Value: "payment"}}},
	}

	// Row 1: LLM outputs ["payment", "checkout"]
	acc.Add(
		[]domain.AttrDropDiagnostic{{Dim: "modules", Reason: domain.AttrDropOffListValue, Values: []string{"checkout"}, Count: 1}},
		map[string]any{"modules": []string{"payment"}},
		dims,
	)
	// Row 2: LLM outputs ["payment", "checkout", "billing"]
	acc.Add(
		[]domain.AttrDropDiagnostic{{Dim: "modules", Reason: domain.AttrDropOffListValue, Values: []string{"checkout", "billing"}, Count: 2}},
		map[string]any{"modules": []string{"payment"}},
		dims,
	)
	// Row 3: LLM outputs ["payment"] only
	acc.Add(
		nil,
		map[string]any{"modules": []string{"payment"}},
		dims,
	)

	report := acc.Build(3)

	// checkout: 2 occurrences (rows 1 and 2)
	if report.ValueFreq["modules"]["checkout"] != 2 {
		t.Errorf("expected checkout count 2, got %v", report.ValueFreq["modules"]["checkout"])
	}
	// billing: 1 occurrence (row 2)
	if report.ValueFreq["modules"]["billing"] != 1 {
		t.Errorf("expected billing count 1, got %v", report.ValueFreq["modules"]["billing"])
	}
	// Coverage: 3 kept (payment x3), 3 dropped (checkout x2, billing x1) = 3/6 = 0.5
	if report.Coverage["modules"] != 0.5 {
		t.Errorf("expected coverage 0.5, got %v", report.Coverage["modules"])
	}
}

func TestSuggestedAccumulator_Confidence(t *testing.T) {
	acc := newSuggestedAccumulator()
	dims := domain.DimensionSet{
		{Name: "modules", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{{Value: "payment"}}},
	}

	// 5 rows, each with "checkout" dropped
	for i := 0; i < 5; i++ {
		acc.Add(
			[]domain.AttrDropDiagnostic{{Dim: "modules", Reason: domain.AttrDropOffListValue, Values: []string{"checkout"}, Count: 1}},
			map[string]any{"modules": []string{"payment"}},
			dims,
		)
	}

	report := acc.Build(10)

	// Confidence = count / sampleSize = 5/10 = 0.5
	var checkoutCandidate *SuggestedCandidate
	for i := range report.Candidates {
		if report.Candidates[i].Value == "checkout" {
			checkoutCandidate = &report.Candidates[i]
			break
		}
	}
	if checkoutCandidate == nil {
		t.Fatal("expected checkout candidate")
	}
	if checkoutCandidate.Confidence != 0.5 {
		t.Errorf("expected confidence 0.5, got %v", checkoutCandidate.Confidence)
	}
}

func TestSuggestedAccumulator_Recommendations(t *testing.T) {
	acc := newSuggestedAccumulator()
	dims := domain.DimensionSet{
		{Name: "modules", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{{Value: "payment"}}},
	}

	// 3 rows with "checkout" dropped (30% frequency > 10% threshold)
	for i := 0; i < 3; i++ {
		acc.Add(
			[]domain.AttrDropDiagnostic{{Dim: "modules", Reason: domain.AttrDropOffListValue, Values: []string{"checkout"}, Count: 1}},
			map[string]any{"modules": []string{"payment"}},
			dims,
		)
	}

	report := acc.Build(10)

	if len(report.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(report.Recommendations))
	}
	rec := report.Recommendations[0]
	if rec.Action != "add" {
		t.Errorf("expected action 'add', got %q", rec.Action)
	}
	if rec.Dim != "modules" {
		t.Errorf("expected dim 'modules', got %q", rec.Dim)
	}
	if rec.Value != "checkout" {
		t.Errorf("expected value 'checkout', got %q", rec.Value)
	}
}

func TestSuggestedAccumulator_NoRecommendationBelowThreshold(t *testing.T) {
	acc := newSuggestedAccumulator()
	dims := domain.DimensionSet{
		{Name: "modules", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{{Value: "payment"}}},
	}

	// 1/11 ≈ 9% < 10% threshold
	acc.Add(
		[]domain.AttrDropDiagnostic{{Dim: "modules", Reason: domain.AttrDropOffListValue, Values: []string{"checkout"}, Count: 1}},
		map[string]any{"modules": []string{"payment"}},
		dims,
	)
	report := acc.Build(11)

	if len(report.Recommendations) != 0 {
		t.Errorf("expected no recommendations below threshold, got %d", len(report.Recommendations))
	}
}

func TestSuggestedAccumulator_IgnoresNonOffListReasons(t *testing.T) {
	acc := newSuggestedAccumulator()
	dims := domain.DimensionSet{
		{Name: "modules", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{{Value: "payment"}}},
	}

	// This diagnostic is for unknown dimension, not off-list value
	acc.Add(
		[]domain.AttrDropDiagnostic{{Dim: "unknown_dim", Reason: domain.AttrDropUnknownDimension, Values: []string{"foo"}, Count: 1}},
		map[string]any{"modules": []string{"payment"}},
		dims,
	)

	report := acc.Build(10)

	// Should not capture this as a suggested value
	if len(report.ValueFreq) != 0 {
		t.Errorf("expected empty ValueFreq for non-off-list diagnostics, got %v", report.ValueFreq)
	}
}

func TestSuggestedAccumulator_SingleKindDim(t *testing.T) {
	acc := newSuggestedAccumulator()
	dims := domain.DimensionSet{
		{Name: "sentiment", Kind: domain.DimSingle, Taxonomy: []domain.Taxonomy{{Value: "positive"}, {Value: "negative"}}},
	}

	acc.Add(
		[]domain.AttrDropDiagnostic{{Dim: "sentiment", Reason: domain.AttrDropOffListValue, Values: []string{"mixed"}, Count: 1}},
		map[string]any{"sentiment": "positive"},
		dims,
	)

	report := acc.Build(10)

	if report.ValueFreq["sentiment"]["mixed"] != 1 {
		t.Errorf("expected mixed count 1, got %v", report.ValueFreq["sentiment"]["mixed"])
	}
	// 1 kept, 1 dropped = 50% coverage
	if report.Coverage["sentiment"] != 0.5 {
		t.Errorf("expected coverage 0.5, got %v", report.Coverage["sentiment"])
	}
}

func TestFormatSuggestedAttrs_Empty(t *testing.T) {
	b := ptrext.Of(strings.Builder{})
	formatSuggestedAttrs(b, ptrext.Of(SuggestedAttrsReport{}), 10)
	if b.Len() != 0 {
		t.Errorf("expected empty output for empty report, got %q", b.String())
	}
}

func TestFormatSuggestedAttrs_WithCoverage(t *testing.T) {
	b := ptrext.Of(strings.Builder{})
	report := ptrext.Of(SuggestedAttrsReport{
		Coverage: map[string]float64{"modules": 0.85},
		Candidates: []SuggestedCandidate{
			{Dim: "modules", Value: "checkout", Count: 12, Confidence: 0.24},
			{Dim: "modules", Value: "billing", Count: 5, Confidence: 0.10},
		},
	})
	formatSuggestedAttrs(b, report, 50)

	output := b.String()
	if !strings.Contains(output, "suggested values") {
		t.Errorf("expected 'suggested values' header, got %q", output)
	}
	if !strings.Contains(output, "modules") {
		t.Errorf("expected 'modules' in output, got %q", output)
	}
	if !strings.Contains(output, "85%") {
		t.Errorf("expected '85%%' coverage, got %q", output)
	}
	if !strings.Contains(output, "checkout (12)") {
		t.Errorf("expected 'checkout (12)' suggestion, got %q", output)
	}
}

func TestFormatSuggestedAttrs_WithRecommendations(t *testing.T) {
	b := ptrext.Of(strings.Builder{})
	report := ptrext.Of(SuggestedAttrsReport{
		Coverage: map[string]float64{"modules": 0.85},
		Recommendations: []SuggestedRecommendation{
			{Action: "add", Dim: "modules", Value: "checkout", Reason: "appeared 12 times (24%)", Impact: "+12% coverage"},
		},
	})
	formatSuggestedAttrs(b, report, 50)

	output := b.String()
	if !strings.Contains(output, "Recommendations:") {
		t.Errorf("expected 'Recommendations:' header, got %q", output)
	}
	if !strings.Contains(output, "ADD modules.checkout") {
		t.Errorf("expected 'ADD modules.checkout' in output, got %q", output)
	}
}

func TestComputeConfidence(t *testing.T) {
	tests := []struct {
		count      int
		sampleSize int
		want       float64
	}{
		{0, 10, 0.0},
		{5, 10, 0.5},
		{10, 10, 1.0},
		{15, 10, 1.0}, // capped at 1.0
		{1, 0, 0.0},   // edge case: zero sample size
	}
	for _, tc := range tests {
		got := computeConfidence(tc.count, tc.sampleSize)
		if got != tc.want {
			t.Errorf("computeConfidence(%d, %d) = %v, want %v", tc.count, tc.sampleSize, got, tc.want)
		}
	}
}

func TestPredictImpact(t *testing.T) {
	tests := []struct {
		count           int
		currentCoverage float64
		totalDroppedDim int
		want            float64
	}{
		{0, 0.85, 10, 0.0},
		{10, 0.85, 10, 0.15}, // 100% of drops, 15% gap
		{5, 0.85, 10, 0.075}, // 50% of drops, 15% gap
		{5, 0.5, 10, 0.25},   // 50% of drops, 50% gap
		{5, 0.85, 0, 0.0},    // no drops
	}
	for _, tc := range tests {
		got := predictImpact(tc.count, tc.currentCoverage, tc.totalDroppedDim)
		if !floatClose(got, tc.want, 1e-9) {
			t.Errorf("predictImpact(%d, %v, %d) = %v, want %v",
				tc.count, tc.currentCoverage, tc.totalDroppedDim, got, tc.want)
		}
	}
}

func floatClose(a, b, epsilon float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}
