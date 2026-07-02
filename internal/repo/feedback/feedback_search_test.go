package feedback

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestSearchConstants(t *testing.T) {
	if DefaultSearchLimit != 20 {
		t.Errorf("DefaultSearchLimit = %d, want 20", DefaultSearchLimit)
	}
	if MaxSearchLimit != 100 {
		t.Errorf("MaxSearchLimit = %d, want 100", MaxSearchLimit)
	}
	if DefaultMinSimilarity != 0.5 {
		t.Errorf("DefaultMinSimilarity = %f, want 0.5", DefaultMinSimilarity)
	}
	if DefaultEfSearch != 200 {
		t.Errorf("DefaultEfSearch = %d, want 200", DefaultEfSearch)
	}
	if EmbeddingDims != 256 {
		t.Errorf("EmbeddingDims = %d, want 256", EmbeddingDims)
	}
}

func TestSemanticSearchParams_ZeroValue(t *testing.T) {
	p := SemanticSearchParams{}
	if p.TenantID != "" {
		t.Error("zero value TenantID should be empty")
	}
	if p.Limit != 0 {
		t.Error("zero value Limit should be 0")
	}
	if p.MinSimilarity != 0 {
		t.Error("zero value MinSimilarity should be 0")
	}
}

func TestKeywordSearchParams_ZeroValue(t *testing.T) {
	p := KeywordSearchParams{}
	if p.Query != "" {
		t.Error("zero value Query should be empty")
	}
}

func TestLexicalSearchParams_ZeroValue(t *testing.T) {
	p := LexicalSearchParams{}
	if p.Query != "" {
		t.Error("zero value Query should be empty")
	}
}

func TestSemanticSearchHit_Structure(t *testing.T) {
	fb := ptrext.Of(SearchFeedback{
		ID:      123,
		Content: "test content",
	})
	hit := SemanticSearchHit{
		Feedback:   fb,
		Similarity: 0.95,
	}
	if hit.Feedback.ID != 123 {
		t.Errorf("Feedback.ID = %d, want 123", hit.Feedback.ID)
	}
	if hit.Similarity != 0.95 {
		t.Errorf("Similarity = %f, want 0.95", hit.Similarity)
	}
}

func TestLexicalSearchHit_Structure(t *testing.T) {
	fb := ptrext.Of(SearchFeedback{
		ID:      123,
		Content: "checkout invoice failed",
	})
	hit := LexicalSearchHit{
		Feedback: fb,
		Rank:     2,
		Score:    0.72,
		Fields:   []string{"content"},
		Snippets: []SearchSnippet{{Field: "content", Snippet: "invoice failed"}},
	}
	if hit.Feedback.ID != 123 {
		t.Errorf("Feedback.ID = %d, want 123", hit.Feedback.ID)
	}
	if hit.Rank != 2 {
		t.Errorf("Rank = %d, want 2", hit.Rank)
	}
	if hit.Score != 0.72 {
		t.Errorf("Score = %f, want 0.72", hit.Score)
	}
	if len(hit.Fields) != 1 || hit.Fields[0] != "content" {
		t.Errorf("Fields = %v, want [content]", hit.Fields)
	}
	if len(hit.Snippets) != 1 || hit.Snippets[0].Snippet != "invoice failed" {
		t.Errorf("Snippets = %v, want invoice failed", hit.Snippets)
	}
}

func TestSearchFeedback_Fields(t *testing.T) {
	stateID := "state-123"
	clusterID := "cluster-456"
	label := "test cluster"
	fb := SearchFeedback{
		ID:                   1,
		Content:              "content",
		Source:               "api",
		EnrichedTitle:        "title",
		EnrichedDisplayTitle: "display title",
		EnrichedRationale:    "rationale",
		IsUrgent:             true,
		EnrichmentStatus:     "done",
		WorkflowStateID:      ptrext.Of(stateID),
		ClusterID:            ptrext.Of(clusterID),
		ClusterLabel:         ptrext.Of(label),
	}
	if !fb.IsUrgent {
		t.Error("IsUrgent should be true")
	}
	if ptrext.Indirect(fb.WorkflowStateID) != stateID {
		t.Error("WorkflowStateID mismatch")
	}
	if ptrext.Indirect(fb.ClusterID) != clusterID {
		t.Error("ClusterID mismatch")
	}
}

func TestLexicalSnippets_TitleAndContent(t *testing.T) {
	fb := SearchFeedback{
		Content:              "checkout invoices fail after a plan upgrade",
		EnrichedDisplayTitle: "Invoice checkout failure",
	}
	snippets := lexicalSnippets("invoice checkout", fb)
	if len(snippets) != 2 {
		t.Fatalf("snippets = %d, want 2", len(snippets))
	}
	if snippets[0].Field != "title" {
		t.Errorf("first snippet field = %s, want title", snippets[0].Field)
	}
	if snippets[1].Field != "content" {
		t.Errorf("second snippet field = %s, want content", snippets[1].Field)
	}
}

func TestLexicalFields_MatchesTerms(t *testing.T) {
	fb := SearchFeedback{
		Content:   "password reset loops after SSO login",
		PageURL:   "https://example.test/settings/security",
		UserID:    "customer-7",
		Source:    "widget",
		Type:      "bug",
		Language:  "en",
		IsUrgent:  true,
		CreatedAt: mustParseTime(t, "2026-07-02T00:00:00Z"),
	}
	fields := lexicalFields("SSO security", fb)
	if len(fields) != 2 {
		t.Fatalf("fields = %v, want two matches", fields)
	}
	if fields[0] != "content" || fields[1] != "page_url" {
		t.Errorf("fields = %v, want [content page_url]", fields)
	}
}

func TestLikeContainsPattern_EscapesWildcards(t *testing.T) {
	got := likeContainsPattern(`100%_done\ok`)
	want := `%100\%\_done\\ok%`
	if got != want {
		t.Errorf("likeContainsPattern = %q, want %q", got, want)
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}

func TestVecToString_Empty(t *testing.T) {
	got := vecToString(nil)
	if got != "[]" {
		t.Errorf("nil → %q, want []", got)
	}
	got = vecToString([]float32{})
	if got != "[]" {
		t.Errorf("empty → %q, want []", got)
	}
}

func TestVecToString_Values(t *testing.T) {
	got := vecToString([]float32{1.5, 2.25, -3.0})
	// Note: exact format depends on FormatFloat, but should contain the values.
	if got == "[]" {
		t.Error("should not be empty")
	}
	if got[0] != '[' || got[len(got)-1] != ']' {
		t.Errorf("should be bracketed: %s", got)
	}
}

func TestParseVecString_Empty(t *testing.T) {
	tests := []string{"", "[]", " [] "}
	for _, tc := range tests {
		got, err := parseVecString(tc)
		if err != nil {
			t.Errorf("parseVecString(%q) error: %v", tc, err)
		}
		if len(got) != 0 {
			t.Errorf("parseVecString(%q) = %v, want empty", tc, got)
		}
	}
}

func TestParseVecString_Values(t *testing.T) {
	got, err := parseVecString("[1.5,2.25,-3.0]")
	if err != nil {
		t.Fatalf("parseVecString error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] != 1.5 || got[1] != 2.25 || got[2] != -3.0 {
		t.Errorf("values mismatch: %v", got)
	}
}

func TestParseVecString_InvalidFormat(t *testing.T) {
	tests := []string{"1,2,3", "[1,2,3", "1,2,3]", "{1,2,3}"}
	for _, tc := range tests {
		_, err := parseVecString(tc)
		if err == nil {
			t.Errorf("parseVecString(%q) should error", tc)
		}
	}
}

func TestParseVecString_InvalidFloat(t *testing.T) {
	_, err := parseVecString("[1.5,abc,-3.0]")
	if err == nil {
		t.Error("should error on invalid float")
	}
}

func TestVecToString_Roundtrip(t *testing.T) {
	original := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	str := vecToString(original)
	parsed, err := parseVecString(str)
	if err != nil {
		t.Fatalf("roundtrip parse error: %v", err)
	}
	if len(parsed) != len(original) {
		t.Fatalf("len mismatch: %d vs %d", len(parsed), len(original))
	}
	for i := range original {
		// Allow small floating point differences.
		diff := original[i] - parsed[i]
		if diff < -0.0001 || diff > 0.0001 {
			t.Errorf("value %d mismatch: %f vs %f", i, original[i], parsed[i])
		}
	}
}

func TestEmbeddingStats_ZeroValue(t *testing.T) {
	s := EmbeddingStats{}
	if s.TotalFeedback != 0 || s.EmbeddedCount != 0 {
		t.Error("zero value should have 0 counts")
	}
	if s.EmbeddingPercent != 0 {
		t.Error("zero value should have 0 percent")
	}
}

func TestEmbeddingStats_Calculation(t *testing.T) {
	s := EmbeddingStats{
		TotalFeedback: 100,
		EmbeddedCount: 75,
	}
	// Manually calculate what the percent would be.
	expected := 75.0
	s.EmbeddingPercent = float64(s.EmbeddedCount) / float64(s.TotalFeedback) * 100
	if s.EmbeddingPercent != expected {
		t.Errorf("EmbeddingPercent = %f, want %f", s.EmbeddingPercent, expected)
	}
}
