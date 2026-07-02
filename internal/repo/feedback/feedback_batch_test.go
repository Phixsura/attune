package feedback

import (
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestBatchResult_Empty(t *testing.T) {
	r := ptrext.Of(BatchResult{})
	if r.Succeeded != 0 || r.Skipped != 0 || len(r.Failed) != 0 {
		t.Errorf("empty BatchResult should have zero values")
	}
}

func TestItemFailure_Codes(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{BatchErrNotFound, "not_found"},
		{BatchErrVersionConflict, "version_conflict"},
		{BatchErrDeleted, "deleted"},
		{BatchErrInvalidState, "invalid_state"},
	}
	for _, tc := range tests {
		if tc.code != tc.want {
			t.Errorf("code %q != want %q", tc.code, tc.want)
		}
	}
}

func TestBatchTagUpdate_ZeroValue(t *testing.T) {
	u := BatchTagUpdate{}
	if u.FeedbackID != 0 {
		t.Error("zero value should have 0 FeedbackID")
	}
	if len(u.AddTags) != 0 || len(u.RemoveTags) != 0 {
		t.Error("zero value should have nil slices")
	}
}

func TestFeedbackFilter_Defaults(t *testing.T) {
	f := ptrext.Of(FeedbackFilter{})
	if f.IncludeDeleted {
		t.Error("IncludeDeleted should default to false")
	}
	if f.Urgent != nil {
		t.Error("Urgent should default to nil")
	}
	if f.EnrichmentStatus != nil {
		t.Error("EnrichmentStatus should default to nil")
	}
	if f.TerminalFailedOnly != nil {
		t.Error("TerminalFailedOnly should default to nil")
	}
}

func TestFeedbackFilter_WithTimeRange(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	f := ptrext.Of(FeedbackFilter{
		CreatedAfter:  ptrext.Of(yesterday),
		CreatedBefore: ptrext.Of(now),
	})
	if f.CreatedAfter == nil || f.CreatedBefore == nil {
		t.Error("time range should be set")
	}
	if !ptrext.Indirect(f.CreatedAfter).Before(ptrext.Indirect(f.CreatedBefore)) {
		t.Error("CreatedAfter should be before CreatedBefore")
	}
}

func TestMaxBatchSize(t *testing.T) {
	if MaxBatchSize != 500 {
		t.Errorf("MaxBatchSize = %d, want 500", MaxBatchSize)
	}
}

func TestMaxFilterIDsLimit(t *testing.T) {
	if MaxFilterIDsLimit != 10000 {
		t.Errorf("MaxFilterIDsLimit = %d, want 10000", MaxFilterIDsLimit)
	}
}

func TestItemFailure_WithUpdatedAt(t *testing.T) {
	now := time.Now()
	f := ItemFailure{
		FeedbackID: 123,
		Code:       BatchErrVersionConflict,
		Message:    "modified",
		UpdatedAt:  ptrext.Of(now),
	}
	if f.UpdatedAt == nil {
		t.Error("UpdatedAt should be set")
	}
	if !ptrext.Indirect(f.UpdatedAt).Equal(now) {
		t.Error("UpdatedAt should equal the set time")
	}
}

func TestBatchTagUpdate_WithTags(t *testing.T) {
	u := BatchTagUpdate{
		FeedbackID: 456,
		AddTags:    []string{"tag-1", "tag-2"},
		RemoveTags: []string{"tag-3"},
	}
	if len(u.AddTags) != 2 {
		t.Errorf("AddTags len = %d, want 2", len(u.AddTags))
	}
	if len(u.RemoveTags) != 1 {
		t.Errorf("RemoveTags len = %d, want 1", len(u.RemoveTags))
	}
}

func TestApplyBatchFilters_EnrichmentStatusAndTerminalFailure(t *testing.T) {
	qb := newQueryBuilder("tenant-1")
	repo := ptrext.Of(FeedbackRepo{})
	err := repo.applyBatchFilters(qb, ptrext.Of(FeedbackFilter{
		EnrichmentStatus:   ptrext.Of("failed"),
		TerminalFailedOnly: ptrext.Of(true),
	}))
	if err != nil {
		t.Fatalf("applyBatchFilters returned error: %v", err)
	}

	if !strings.Contains(qb.where, "deleted_at IS NULL") {
		t.Fatalf("where missing deleted filter: %s", qb.where)
	}
	if !strings.Contains(qb.where, "enrichment_status = $2") {
		t.Fatalf("where missing enrichment status arg filter: %s", qb.where)
	}
	if !strings.Contains(qb.where, "enrichment_status = 'failed'") {
		t.Fatalf("where missing terminal failed status guard: %s", qb.where)
	}
	if !strings.Contains(qb.where, "enrichment_attempts >= $3") {
		t.Fatalf("where missing terminal attempt guard: %s", qb.where)
	}
	if !strings.Contains(qb.where, "enrichment_next_retry_at IS NULL") {
		t.Fatalf("where missing terminal retry guard: %s", qb.where)
	}
	if got, want := len(qb.args), 3; got != want {
		t.Fatalf("args len = %d, want %d", got, want)
	}
	if got, want := qb.args[1], "failed"; got != want {
		t.Fatalf("status arg = %v, want %v", got, want)
	}
	if got, want := qb.args[2], maxEnrichmentAttempts; got != want {
		t.Fatalf("attempt arg = %v, want %v", got, want)
	}
}
