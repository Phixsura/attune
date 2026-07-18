package feedback

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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

func TestSearchRowScannersHydrateNullableFields(t *testing.T) {
	t.Parallel()

	now := mustParseTime(t, "2026-07-02T00:00:00Z")
	nextRetry := now.Add(time.Hour)
	values := append(searchFeedbackValues(now, nextRetry), 0.92)

	semanticRows := ptrext.Of(fakeSearchRows{values: values})
	fb, similarity, err := scanSemanticSearchRow(semanticRows)
	if err != nil {
		t.Fatalf("scanSemanticSearchRow() error = %v", err)
	}
	if similarity != 0.92 || fb.ID != 123 || fb.Content != "checkout fails" {
		t.Fatalf("scanSemanticSearchRow() = (%#v, %v), want search feedback", fb, similarity)
	}
	if ptrext.Indirect(fb.ClassificationConfidence) != 0.87 ||
		ptrext.Indirect(fb.WorkflowStateID) != "state-1" ||
		ptrext.Indirect(fb.ClusterID) != "cluster-1" ||
		ptrext.Indirect(fb.ClusterLabel) != "Checkout" ||
		ptrext.Indirect(fb.EnrichmentNextRetryAt) != nextRetry {
		t.Fatalf("scanSemanticSearchRow() nullable fields = %#v, want hydrated pointers", fb)
	}

	lexicalRows := ptrext.Of(fakeSearchRows{values: append(searchFeedbackValues(now, nextRetry), 1.5)})
	fb, score, err := scanLexicalSearchRow(lexicalRows)
	if err != nil {
		t.Fatalf("scanLexicalSearchRow() error = %v", err)
	}
	if score != 1.5 || fb.EnrichedDisplayTitle != "Checkout failure" {
		t.Fatalf("scanLexicalSearchRow() = (%#v, %v), want lexical feedback", fb, score)
	}
}

func TestSearchRowScannersPropagateScanErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("scan failed")
	if _, _, err := scanSemanticSearchRow(ptrext.Of(fakeSearchRows{err: boom})); !errors.Is(err, boom) {
		t.Fatalf("scanSemanticSearchRow(error) = %v, want %v", err, boom)
	}
	if _, _, err := scanLexicalSearchRow(ptrext.Of(fakeSearchRows{err: boom})); !errors.Is(err, boom) {
		t.Fatalf("scanLexicalSearchRow(error) = %v, want %v", err, boom)
	}
}

func TestHydrateSearchFeedbackNullsLeavesInvalidValuesNil(t *testing.T) {
	t.Parallel()

	fb := hydrateSearchFeedbackNulls(
		SearchFeedback{ID: 123},
		sql.NullFloat64{},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{},
		sql.NullTime{},
	)
	if fb.ClassificationConfidence != nil || fb.WorkflowStateID != nil || fb.ClusterID != nil ||
		fb.ClusterLabel != nil || fb.EnrichmentNextRetryAt != nil {
		t.Fatalf("hydrateSearchFeedbackNulls() = %#v, want nil nullable pointers", fb)
	}
}

func TestNormalizeLexicalScore_ClampsToDisplayRange(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		want  float64
	}{
		{"zero gets fallback relevance", 0, 0.1},
		{"negative gets fallback relevance", -2, 0.1},
		{"in range preserved", 0.42, 0.42},
		{"above one capped", 1.7, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLexicalScore(tt.score); got != tt.want {
				t.Errorf("normalizeLexicalScore(%v) = %v, want %v", tt.score, got, tt.want)
			}
		})
	}
}

func TestSearchTerms_TrimsAndDropsOneRuneSplitTerms(t *testing.T) {
	got := searchTerms("  a checkout b flow  ")
	want := []string{"a checkout b flow", "checkout", "flow"}
	if len(got) != len(want) {
		t.Fatalf("searchTerms length = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("searchTerms[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSnippetAroundQuery_FallsBackWhenQueryDoesNotMatch(t *testing.T) {
	got := snippetAroundQuery("checkout invoice flow is still loading", "missing", 12)
	if got != "checkout ..." {
		t.Errorf("snippetAroundQuery fallback = %q, want %q", got, "checkout ...")
	}
}

func TestTruncateRunes_Boundaries(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxRunes int
		want     string
	}{
		{"non-positive max", "abcdef", 0, ""},
		{"short text unchanged", "abc", 5, "abc"},
		{"tiny max has no ellipsis", "abcdef", 3, "abc"},
		{"long text gets ellipsis", "abcdef", 4, "a..."},
		{"unicode counts runes", "你好世界abc", 5, "你好..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateRunes(tt.text, tt.maxRunes); got != tt.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.text, tt.maxRunes, got, tt.want)
			}
		})
	}
}

func TestLikeContainsPattern_EscapesWildcards(t *testing.T) {
	got := likeContainsPattern(`100%_done\ok`)
	want := `%100\%\_done\\ok%`
	if got != want {
		t.Errorf("likeContainsPattern = %q, want %q", got, want)
	}
}

func TestNormalizeLexicalLimit_Bounds(t *testing.T) {
	t.Parallel()

	if got := normalizeLexicalLimit(-1); got != DefaultSearchLimit {
		t.Fatalf("normalizeLexicalLimit(-1) = %d, want default", got)
	}
	if got := normalizeLexicalLimit(MaxSearchLimit + 1); got != MaxSearchLimit {
		t.Fatalf("normalizeLexicalLimit(max+1) = %d, want max", got)
	}
	if got := normalizeLexicalLimit(7); got != 7 {
		t.Fatalf("normalizeLexicalLimit(7) = %d, want 7", got)
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

func searchFeedbackValues(now time.Time, nextRetry time.Time) []any {
	return []any{
		int64(123),
		"checkout fails",
		"widget",
		"bug",
		"user-1",
		"en",
		"https://example.test/checkout",
		"Checkout",
		"Checkout failure",
		"en-US",
		[]byte(`{"severity":"high"}`),
		"The checkout flow fails after payment",
		true,
		sql.NullFloat64{Float64: 0.87, Valid: true},
		"done",
		now,
		sql.NullString{String: "state-1", Valid: true},
		sql.NullString{String: "cluster-1", Valid: true},
		sql.NullString{String: "Checkout", Valid: true},
		2,
		sql.NullTime{Time: nextRetry, Valid: true},
		"provider_error",
		"gpt-4.1-mini",
		"channel-1",
		"Support",
		"fingerprint-1",
		"reply-v1",
	}
}

type fakeSearchRows struct {
	values []any
	err    error
}

func (r fakeSearchRows) Close() {}
func (r fakeSearchRows) Err() error {
	return nil
}

func (r fakeSearchRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r fakeSearchRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r fakeSearchRows) Next() bool {
	return false
}

func (r fakeSearchRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if err := assignFeedbackBatchScanValue(dest[i], r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r fakeSearchRows) Values() ([]any, error) {
	return r.values, nil
}

func (r fakeSearchRows) RawValues() [][]byte {
	return nil
}

func (r fakeSearchRows) Conn() *pgx.Conn {
	return nil
}
