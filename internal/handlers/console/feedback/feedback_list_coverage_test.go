// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package feedback

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
)

type fakeTagAssignmentBatchReader struct {
	tags map[int64][]feedbacktagassignment.TagInfo
	err  error
}

func (f fakeTagAssignmentBatchReader) ListByFeedback(
	context.Context, string, int64,
) ([]feedbacktagassignment.TagInfo, error) {
	return nil, nil
}

func (f fakeTagAssignmentBatchReader) ListByFeedbackBatch(
	_ context.Context, _ string, _ []int64,
) (map[int64][]feedbacktagassignment.TagInfo, error) {
	return f.tags, f.err
}

func TestConsoleListOptsFromRequestMapsAllFields(t *testing.T) {
	t.Parallel()

	req := &attunev1.ListFeedbackRequest{
		Cursor:             ptrext.Of("17"),
		Limit:              ptrext.Of(int32(25)),
		Q:                  ptrext.Of("billing"),
		Source:             ptrext.Of("api"),
		Type:               ptrext.Of("bug"),
		Urgent:             ptrext.Of(true),
		TagId:              ptrext.Of("tag-1"),
		WorkflowStateId:    ptrext.Of("wf-1"),
		WorkflowCategory:   ptrext.Of("open"),
		EnrichmentStatus:   ptrext.Of("failed"),
		TerminalFailedOnly: ptrext.Of(true),
		Ids:                []int64{4, 8},
		ConfidenceLte:      ptrext.Of(0.42),
		CreatedFrom:        ptrext.Of("2026-07-01T00:00:00Z"),
		CreatedTo:          ptrext.Of("2026-07-02T12:00:00Z"),
		EnrichedFrom:       ptrext.Of("2026-07-03T00:00:00Z"),
		EnrichedTo:         ptrext.Of("2026-07-04T12:00:00Z"),
		QualitySignal:      ptrext.Of("low"),
		Attrs: []*attunev1.AttrFilter{
			{Dim: "severity", Value: "critical"},
			{Dim: "labels", Value: "billing"},
			{Dim: "ignored", Value: "nope"},
		},
	}

	opts, err := consoleListOptsFromRequest(req, []domain.Dimension{
		{Name: "severity", Kind: domain.DimSingle},
		{Name: "labels", Kind: domain.DimMulti},
	})
	require.NoError(t, err)

	require.Equal(t, "billing", opts.Q)
	require.Equal(t, int64(17), opts.Cursor)
	require.Equal(t, 25, opts.Limit)
	require.NotNil(t, opts.Source)
	require.Equal(t, "api", *opts.Source)
	require.NotNil(t, opts.Type)
	require.Equal(t, "bug", *opts.Type)
	require.NotNil(t, opts.Urgent)
	require.True(t, *opts.Urgent)
	require.NotNil(t, opts.TagID)
	require.Equal(t, "tag-1", *opts.TagID)
	require.NotNil(t, opts.WorkflowStateID)
	require.Equal(t, "wf-1", *opts.WorkflowStateID)
	require.NotNil(t, opts.WorkflowCategory)
	require.Equal(t, "open", *opts.WorkflowCategory)
	require.NotNil(t, opts.EnrichmentStatus)
	require.Equal(t, "failed", *opts.EnrichmentStatus)
	require.NotNil(t, opts.TerminalFailedOnly)
	require.True(t, *opts.TerminalFailedOnly)
	require.Equal(t, []int64{4, 8}, opts.IDs)
	require.NotNil(t, opts.ConfidenceLTE)
	require.InDelta(t, 0.42, *opts.ConfidenceLTE, 1e-9)
	require.NotNil(t, opts.CreatedFrom)
	require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), *opts.CreatedFrom)
	require.NotNil(t, opts.CreatedTo)
	require.Equal(t, time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC), *opts.CreatedTo)
	require.NotNil(t, opts.EnrichedFrom)
	require.Equal(t, time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), *opts.EnrichedFrom)
	require.NotNil(t, opts.EnrichedTo)
	require.Equal(t, time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC), *opts.EnrichedTo)
	require.NotNil(t, opts.QualitySignal)
	require.Equal(t, "low", *opts.QualitySignal)

	require.Len(t, opts.Attrs, 2)
	gotByDim := map[string]bool{}
	for _, attr := range opts.Attrs {
		gotByDim[attr.Dim+":"+attr.Value] = attr.Multi
	}
	_, ok := gotByDim["severity:critical"]
	require.True(t, ok)
	require.False(t, gotByDim["severity:critical"])
	require.True(t, gotByDim["labels:billing"])
}

func TestConsoleListOptsFromRequestRejectsInvalidTime(t *testing.T) {
	t.Parallel()

	req := &attunev1.ListFeedbackRequest{
		CreatedFrom: ptrext.Of("not-a-time"),
	}

	_, err := consoleListOptsFromRequest(req, nil)
	require.Error(t, err)
}

func TestOptionalListTimeAndParseListTime(t *testing.T) {
	t.Parallel()

	got, err := optionalListTime(nil)
	require.NoError(t, err)
	require.Nil(t, got)

	want := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	gotTime, err := optionalListTime(ptrext.Of("2026-07-05T12:00:00Z"))
	require.NoError(t, err)
	require.NotNil(t, gotTime)
	require.Equal(t, want, *gotTime)

	parsed, err := parseListTime("2026-07-05T12:00:00-07:00")
	require.NoError(t, err)
	require.Equal(t, want.Add(7*time.Hour), parsed)

	_, err = parseListTime("bad-time")
	require.Error(t, err)
}

func TestEnrichFeedbackItemsWithTags(t *testing.T) {
	t.Parallel()

	ctx := &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "user-1",
		}),
	}
	rows := []feedbackrepo.ConsoleListRow{
		{ID: 1, Content: "one", Source: "api", EnrichmentStatus: "done", CreatedAt: time.Now().UTC()},
		{ID: 2, Content: "two", Source: "api", EnrichmentStatus: "done", CreatedAt: time.Now().UTC()},
	}
	items := []*attunev1.Feedback{toProtoFeedback(rows[0]), toProtoFeedback(rows[1])}

	tags := map[int64][]feedbacktagassignment.TagInfo{
		1: {
			{
				TagID:        uuidFromString(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
				Name:         "priority",
				Color:        "#ff0000",
				Description:  "High priority",
				UsageCount:   7,
				Archived:     false,
				CreatedBy:    "admin@example.com",
				TagCreatedAt: time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC),
				TagUpdatedAt: time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
			},
		},
		2: {
			{
				TagID:        uuidFromString(t, "ffffffff-eeee-dddd-cccc-bbbbbbbbbbbb"),
				Name:         "customer-facing",
				Color:        "#00ff00",
				Description:  "Customer visible",
				UsageCount:   3,
				Archived:     true,
				CreatedBy:    "ops@example.com",
				TagCreatedAt: time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC),
				TagUpdatedAt: time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC),
			},
		},
	}

	enrichFeedbackItemsWithTags(ctx, "console.FeedbackHandler.List", "tenant-1", rows, items, fakeTagAssignmentBatchReader{tags: tags})

	require.Len(t, items[0].GetTags(), 1)
	require.Equal(t, "priority", items[0].GetTags()[0].GetName())
	require.Equal(t, "#ff0000", items[0].GetTags()[0].GetColor())
	require.Len(t, items[1].GetTags(), 1)
	require.Equal(t, "customer-facing", items[1].GetTags()[0].GetName())
	require.Equal(t, "ops@example.com", items[1].GetTags()[0].GetCreatedBy())
}

func TestEnrichFeedbackItemsWithTags_SkipsWhenReaderNilOrRowsEmpty(t *testing.T) {
	t.Parallel()

	ctx := &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "user-1",
		}),
	}
	items := []*attunev1.Feedback{ptrext.Of(attunev1.Feedback{})}

	enrichFeedbackItemsWithTags(ctx, "console.FeedbackHandler.List", "tenant-1", nil, items, nil)
	require.Empty(t, items[0].GetTags())

	enrichFeedbackItemsWithTags(ctx, "console.FeedbackHandler.List", "tenant-1", []feedbackrepo.ConsoleListRow{}, items, fakeTagAssignmentBatchReader{})
	require.Empty(t, items[0].GetTags())
}

func TestEnrichFeedbackItemsWithTags_IgnoresReaderError(t *testing.T) {
	t.Parallel()

	ctx := &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "user-1",
		}),
	}
	rows := []feedbackrepo.ConsoleListRow{{ID: 1, Content: "one", Source: "api", EnrichmentStatus: "done", CreatedAt: time.Now().UTC()}}
	items := []*attunev1.Feedback{toProtoFeedback(rows[0])}

	enrichFeedbackItemsWithTags(
		ctx,
		"console.FeedbackHandler.List",
		"tenant-1",
		rows,
		items,
		fakeTagAssignmentBatchReader{err: errors.New("boom")},
	)

	require.Empty(t, items[0].GetTags())
}

func uuidFromString(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	u, err := uuid.Parse(raw)
	require.NoError(t, err)
	return u
}
