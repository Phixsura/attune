// ptrext:file-allow test-fixtures
// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// ---------------------------------------------------------------------------
// nullableString
// ---------------------------------------------------------------------------

func TestNullableString_Empty(t *testing.T) {
	t.Parallel()
	require.Nil(t, nullableString(""))
}

func TestNullableString_NonEmpty(t *testing.T) {
	t.Parallel()
	got := nullableString("hello")
	require.NotNil(t, got)
	require.Equal(t, "hello", *got)
}

// ---------------------------------------------------------------------------
// toProtoFeedback
// ---------------------------------------------------------------------------

func TestToProtoFeedback_AllFields(t *testing.T) {
	t.Parallel()

	retryAt := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	conf := 0.95
	row := feedback.ConsoleListRow{
		ID:                       42,
		Content:                  "app crashes on save",
		Source:                   "api",
		Type:                     "bug",
		UserID:                   "u-1",
		Language:                 "en",
		PageURL:                  "https://example.com/page",
		EnrichedTitle:            "Crash on save",
		EnrichedDisplayTitle:     "Save Crash",
		EnrichedDisplayLocale:    "en-US",
		EnrichedAttrs:            []byte(`{"severity":"high"}`),
		IsUrgent:                 true,
		ClassificationConfidence: &conf,
		EnrichmentStatus:         "completed",
		CreatedAt:                time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		EnrichmentAttempts:       3,
		EnrichmentNextRetryAt:    &retryAt,
	}

	got := toProtoFeedback(row)

	require.Equal(t, int64(42), got.GetId())
	require.Equal(t, "app crashes on save", got.GetContent())
	require.Equal(t, "api", got.GetSource())
	require.Equal(t, "bug", got.GetType())
	require.Equal(t, "u-1", got.GetUserId())
	require.Equal(t, "en", got.GetLanguage())
	require.Equal(t, "https://example.com/page", got.GetPageUrl())
	require.Equal(t, "Crash on save", got.GetEnrichedTitle())
	require.Equal(t, "Save Crash", got.GetEnrichedDisplayTitle())
	require.Equal(t, "en-US", got.GetEnrichedDisplayLocale())
	require.True(t, got.GetIsUrgent())
	require.InDelta(t, 0.95, got.GetClassificationConfidence(), 1e-9)
	require.Equal(t, "completed", got.GetEnrichmentStatus())
	require.Equal(t, "2026-01-01T12:00:00Z", got.GetCreatedAt())
	require.Equal(t, int32(3), got.GetEnrichmentAttempts())
	require.Equal(t, "2026-06-15T10:00:00Z", got.GetEnrichmentNextRetryAt())

	// EnrichedAttrs decoded into structpb
	attrs := got.GetEnrichedAttrs()
	require.NotNil(t, attrs)
	require.Equal(t, "high", attrs.GetFields()["severity"].GetStringValue())

	// Optional string fields are populated as pointers
	require.NotNil(t, got.Language)
	require.NotNil(t, got.EnrichedTitle)
	require.NotNil(t, got.EnrichedDisplayTitle)
	require.NotNil(t, got.EnrichedDisplayLocale)
	require.NotNil(t, got.EnrichmentNextRetryAt)
}

func TestToProtoFeedback_MinimalFields(t *testing.T) {
	t.Parallel()

	row := feedback.ConsoleListRow{
		ID:               1,
		Content:          "minimal",
		Source:           "web",
		EnrichmentStatus: "pending",
		CreatedAt:        time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}

	got := toProtoFeedback(row)

	require.Equal(t, int64(1), got.GetId())
	require.Equal(t, "minimal", got.GetContent())
	require.Equal(t, "web", got.GetSource())
	require.Equal(t, "pending", got.GetEnrichmentStatus())
	require.Equal(t, "2026-03-01T00:00:00Z", got.GetCreatedAt())

	// Optional string fields should be nil when source strings are empty.
	require.Nil(t, got.Language)
	require.Nil(t, got.EnrichedTitle)
	require.Nil(t, got.EnrichedDisplayTitle)
	require.Nil(t, got.EnrichedDisplayLocale)

	// No retry time set.
	require.Nil(t, got.EnrichmentNextRetryAt)

	// EnrichedAttrs still returns an empty struct (never nil).
	require.NotNil(t, got.GetEnrichedAttrs())

	// Zero enrichment attempts wraps to int32(0).
	require.Equal(t, int32(0), got.GetEnrichmentAttempts())
}

// ---------------------------------------------------------------------------
// tagInfoToProto
// ---------------------------------------------------------------------------

func TestTagInfoToProto(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	info := feedbacktagassignment.TagInfo{
		TagID:          id,
		Name:           "priority",
		Color:          "#ff0000",
		Description:    "High priority",
		ExclusiveScope: ptrext.Of("priority-group"),
		UsageCount:     7,
		Archived:       false,
		CreatedBy:      "admin@example.com",
		TagCreatedAt:   time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC),
		TagUpdatedAt:   time.Date(2026, 4, 1, 16, 30, 0, 0, time.UTC),
	}

	got := tagInfoToProto(info)

	require.Equal(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", got.GetId())
	require.Equal(t, "priority", got.GetName())
	require.Equal(t, "#ff0000", got.GetColor())
	require.Equal(t, "High priority", got.GetDescription())
	require.Equal(t, "priority-group", got.GetExclusiveScope())
	require.Equal(t, int32(7), got.GetUsageCount())
	require.False(t, got.GetArchived())
	require.Equal(t, "admin@example.com", got.GetCreatedBy())
	require.Equal(t, "2026-02-01T08:00:00Z", got.GetCreatedAt())
	require.Equal(t, "2026-04-01T16:30:00Z", got.GetUpdatedAt())
}

// ---------------------------------------------------------------------------
// attrFiltersFromProto
// ---------------------------------------------------------------------------

func TestAttrFiltersFromProto_Empty(t *testing.T) {
	t.Parallel()

	dims := []domain.Dimension{{Name: "severity", Kind: domain.DimSingle}}

	require.Nil(t, attrFiltersFromProto(nil, dims))
	require.Nil(t, attrFiltersFromProto([]*attunev1.AttrFilter{}, dims))
}

func TestAttrFiltersFromProto_WithDims(t *testing.T) {
	t.Parallel()

	dims := []domain.Dimension{
		{Name: "severity", Kind: domain.DimSingle},
		{Name: "labels", Kind: domain.DimMulti},
	}
	filters := []*attunev1.AttrFilter{
		{Dim: "severity", Value: "critical"},
		{Dim: "labels", Value: "payment"},
	}

	got := attrFiltersFromProto(filters, dims)
	require.Len(t, got, 2)

	byDim := map[string]feedback.AttrFilter{}
	for _, f := range got {
		byDim[f.Dim] = f
	}
	require.Equal(t, "critical", byDim["severity"].Value)
	require.False(t, byDim["severity"].Multi)
	require.Equal(t, "payment", byDim["labels"].Value)
	require.True(t, byDim["labels"].Multi)
}

func TestAttrFiltersFromProto_UnknownDimIgnored(t *testing.T) {
	t.Parallel()

	dims := []domain.Dimension{{Name: "severity", Kind: domain.DimSingle}}
	filters := []*attunev1.AttrFilter{
		{Dim: "nonexistent", Value: "val"},
	}

	got := attrFiltersFromProto(filters, dims)
	require.Nil(t, got)
}

// ---------------------------------------------------------------------------
// Setter methods
// ---------------------------------------------------------------------------

// stubDrafter satisfies the Drafter interface for setter verification.
type stubDrafter struct{}

func (stubDrafter) Precheck(context.Context, int64, string) (string, bool, bool, *time.Time, error) {
	return "", false, false, nil, nil
}

func (stubDrafter) Generate(context.Context, int64, string) (string, time.Time, error) {
	return "", time.Time{}, nil
}

// stubTagReader satisfies the tagAssignmentReader interface.
type stubTagReader struct{}

func (stubTagReader) ListByFeedback(context.Context, string, int64) ([]feedbacktagassignment.TagInfo, error) {
	return nil, nil
}

func (stubTagReader) ListByFeedbackBatch(context.Context, string, []int64) (map[int64][]feedbacktagassignment.TagInfo, error) {
	return nil, nil
}

// stubAuditRecorder satisfies the auditRecorder interface.
type stubAuditRecorder struct{}

func (stubAuditRecorder) Record(context.Context, auditlogsvc.Event) error { return nil }

func TestSetDrafter_WiresDrafter(t *testing.T) {
	t.Parallel()

	h := &FeedbackHandler{}
	require.Nil(t, h.drafter)

	d := stubDrafter{}
	h.SetDrafter(d)
	require.NotNil(t, h.drafter)
}

func TestNewFeedbackHandler_AllowsNilReposForWiring(t *testing.T) {
	t.Parallel()

	h := NewFeedbackHandler(nil, nil)

	require.NotNil(t, h)
	require.Nil(t, h.repo)
	require.Nil(t, h.tenants)
}

func TestSetReplyDraftWorkflow_WiresWorkflow(t *testing.T) {
	t.Parallel()

	h := &FeedbackHandler{}
	h.SetReplyDraftWorkflow(nil)

	require.Nil(t, h.replyWorkflow)
}

func TestSetRegenLimiter_WiresLimiter(t *testing.T) {
	t.Parallel()

	h := &FeedbackHandler{}
	require.Nil(t, h.regenLimiter)

	l := ratelimit.New(10, 5, false, nil)
	h.SetRegenLimiter(l)
	require.Equal(t, l, h.regenLimiter)
}

func TestSetTagAssignments_WiresReader(t *testing.T) {
	t.Parallel()

	h := &FeedbackHandler{}
	require.Nil(t, h.tagAssignments)

	r := stubTagReader{}
	h.SetTagAssignments(r)
	require.NotNil(t, h.tagAssignments)
}

func TestSetAuditLogger_WiresAudit(t *testing.T) {
	t.Parallel()

	h := &FeedbackHandler{}
	require.Nil(t, h.audit)

	a := stubAuditRecorder{}
	h.SetAuditLogger(a)
	require.NotNil(t, h.audit)
}

func TestFeedbackFilterSnapshot_Nil(t *testing.T) {
	t.Parallel()
	require.Nil(t, feedbackFilterSnapshot(nil))
}

func TestFeedbackFilterSnapshot_EmptyFilter(t *testing.T) {
	t.Parallel()
	got := feedbackFilterSnapshot(ptrext.Of(attunev1.FeedbackFilter{}))
	require.Nil(t, got)
}

func TestFeedbackFilterSnapshot_WithContent(t *testing.T) {
	t.Parallel()
	filter := ptrext.Of(attunev1.FeedbackFilter{
		Q: ptrext.Of("bug report"),
	})
	got := feedbackFilterSnapshot(filter)
	require.NotNil(t, got)
	require.Contains(t, got, "q")
}
