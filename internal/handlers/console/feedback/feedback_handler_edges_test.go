// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	tenantrepo "github.com/Phixsura/attune/internal/repo/tenant"
)

type errorDetailTagReader struct{}

func (errorDetailTagReader) ListByFeedback(
	context.Context, string, int64,
) ([]feedbacktagassignment.TagInfo, error) {
	return nil, errors.New("tag read failed")
}

func (errorDetailTagReader) ListByFeedbackBatch(
	context.Context, string, []int64,
) (map[int64][]feedbacktagassignment.TagInfo, error) {
	return nil, nil
}

func TestListMapsTenantValidationAndRepoErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		h      *FeedbackHandler
		req    *attunev1.ListFeedbackRequest
		status int
		code   attunev1.ErrorCode
	}{
		{
			name: "tenant config",
			h: ptrext.Of(FeedbackHandler{
				repo:    ptrext.Of(fakeFeedbackRepo{}),
				tenants: ptrext.Of(fakeTenantConfigRepo{err: errors.New("config failed")}),
			}),
			req:    ptrext.Of(attunev1.ListFeedbackRequest{}),
			status: http.StatusInternalServerError,
			code:   attunev1.ErrorCode_INTERNAL,
		},
		{
			name: "validation",
			h: ptrext.Of(FeedbackHandler{
				repo:    ptrext.Of(fakeFeedbackRepo{}),
				tenants: ptrext.Of(fakeTenantConfigRepo{}),
			}),
			req: ptrext.Of(attunev1.ListFeedbackRequest{
				CreatedFrom: ptrext.Of("not-a-time"),
			}),
			status: http.StatusBadRequest,
			code:   attunev1.ErrorCode_VALIDATION,
		},
		{
			name: "repo",
			h: ptrext.Of(FeedbackHandler{
				repo:    ptrext.Of(fakeFeedbackRepo{listErr: errors.New("list failed")}),
				tenants: ptrext.Of(fakeTenantConfigRepo{}),
			}),
			req:    ptrext.Of(attunev1.ListFeedbackRequest{}),
			status: http.StatusInternalServerError,
			code:   attunev1.ErrorCode_INTERNAL,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.h.List(replyWorkflowTestCtx(), tt.req)

			requireDispatcherError(t, err, tt.status, tt.code)
		})
	}
}

func TestGetMapsRepoErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		repo   *fakeFeedbackRepo
		status int
		code   attunev1.ErrorCode
	}{
		{
			name:   "not found",
			repo:   ptrext.Of(fakeFeedbackRepo{getErr: feedbackrepo.ErrFeedbackNotFound}),
			status: http.StatusNotFound,
			code:   attunev1.ErrorCode_NOT_FOUND,
		},
		{
			name:   "internal",
			repo:   ptrext.Of(fakeFeedbackRepo{getErr: errors.New("db down")}),
			status: http.StatusInternalServerError,
			code:   attunev1.ErrorCode_INTERNAL,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := ptrext.Of(FeedbackHandler{repo: tt.repo})

			_, err := h.Get(replyWorkflowTestCtx(), ptrext.Of(attunev1.GetFeedbackRequest{Id: 123}))

			requireDispatcherError(t, err, tt.status, tt.code)
		})
	}
}

func TestGetToleratesInvalidEmbeddedJSONAndTagErrors(t *testing.T) {
	t.Parallel()

	row := ptrext.Of(feedbackrepo.ConsoleDetailRow{
		ConsoleListRow: feedbackrepo.ConsoleListRow{
			ID:               123,
			Content:          "billing issue",
			Source:           "api",
			EnrichmentStatus: "done",
			CreatedAt:        time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC),
		},
		SourceMeta:  []byte(`{bad-json`),
		Attachments: []byte(`{bad-json`),
	})
	h := ptrext.Of(FeedbackHandler{
		repo:           ptrext.Of(fakeFeedbackRepo{getRow: row}),
		tagAssignments: errorDetailTagReader{},
	})

	result, err := h.Get(replyWorkflowTestCtx(), ptrext.Of(attunev1.GetFeedbackRequest{Id: 123}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Nil(t, result.Body.GetSourceMeta())
	require.Empty(t, result.Body.GetAttachments())
	require.Empty(t, result.Body.GetTags())
}

func TestGetHydratesOptionalDetailFields(t *testing.T) {
	t.Parallel()

	enrichedAt := time.Date(2026, 7, 3, 10, 5, 0, 0, time.UTC)
	draftedAt := time.Date(2026, 7, 3, 10, 6, 0, 0, time.UTC)
	row := ptrext.Of(feedbackrepo.ConsoleDetailRow{
		ConsoleListRow: feedbackrepo.ConsoleListRow{
			ID:               123,
			Content:          "billing issue",
			Source:           "api",
			EnrichmentStatus: "done",
			CreatedAt:        time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC),
		},
		SourceMeta:               []byte(`{"channel":"web"}`),
		Attachments:              []byte(`[{"url":"https://example.com/a.png","mime":"image/png","size":42}]`),
		EnrichedAt:               ptrext.Of(enrichedAt),
		ReplyDraftGeneratedAt:    ptrext.Of(draftedAt),
		EnrichedRationale:        "rationale",
		EnrichedDisplayRationale: "display rationale",
		ReplyDraft:               "reply",
		ReplyDraftEnabled:        true,
	})
	h := ptrext.Of(FeedbackHandler{repo: ptrext.Of(fakeFeedbackRepo{getRow: row})})
	h.SetTagAssignments(ptrext.Of(testSearchTagReader{
		byFeedback: map[int64][]feedbacktagassignment.TagInfo{
			123: {{Name: "billing", Color: "#38bdf8"}},
		},
	}))

	result, err := h.Get(replyWorkflowTestCtx(), ptrext.Of(attunev1.GetFeedbackRequest{Id: 123}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, "web", result.Body.GetSourceMeta().GetFields()["channel"].GetStringValue())
	require.Len(t, result.Body.GetAttachments(), 1)
	require.Equal(t, "https://example.com/a.png", result.Body.GetAttachments()[0].GetUrl())
	require.Equal(t, "image/png", result.Body.GetAttachments()[0].GetMime())
	require.Equal(t, int64(42), result.Body.GetAttachments()[0].GetSize())
	require.Equal(t, enrichedAt.Format(time.RFC3339), result.Body.GetEnrichedAt())
	require.Equal(t, draftedAt.Format(time.RFC3339), result.Body.GetReplyDraftGeneratedAt())
	require.Equal(t, "rationale", result.Body.GetEnrichedRationale())
	require.Equal(t, "display rationale", result.Body.GetEnrichedDisplayRationale())
	require.Equal(t, "reply", result.Body.GetReplyDraft())
	require.True(t, result.Body.GetReplyDraftEnabled())
	require.Len(t, result.Body.GetTags(), 1)
	require.Equal(t, "billing", result.Body.GetTags()[0].GetName())
}

func TestStatsMapsErrorAndPartialFailureBranches(t *testing.T) {
	t.Parallel()

	ctx := replyWorkflowTestCtx()
	t.Run("tenant config error", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(FeedbackHandler{
			repo:    ptrext.Of(fakeFeedbackRepo{}),
			tenants: ptrext.Of(fakeTenantConfigRepo{err: errors.New("config failed")}),
		})

		_, err := h.Stats(ctx, ptrext.Of(attunev1.GetFeedbackStatsRequest{}))

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("urgent error", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(FeedbackHandler{
			repo:    ptrext.Of(fakeFeedbackRepo{urgentErr: errors.New("urgent failed")}),
			tenants: ptrext.Of(fakeTenantConfigRepo{}),
		})

		_, err := h.Stats(ctx, ptrext.Of(attunev1.GetFeedbackStatsRequest{}))

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("usage error ignored and top errors skipped", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(FeedbackHandler{
			repo: ptrext.Of(fakeFeedbackRepo{
				usageErr: errors.New("usage failed"),
				urgent:   2,
				topErr:   errors.New("top failed"),
			}),
			tenants: ptrext.Of(fakeTenantConfigRepo{cfg: tenantrepo.EnrichConfig{
				Dimensions: domain.DimensionSet{{Name: "severity", Kind: domain.DimSingle}},
			}}),
		})

		result, err := h.Stats(ctx, ptrext.Of(attunev1.GetFeedbackStatsRequest{}))

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.Equal(t, int64(0), result.Body.GetTotal())
		require.Equal(t, int64(2), result.Body.GetUrgentCount())
		require.Empty(t, result.Body.GetDims())
	})
}

func TestTerminalFailureWorkbenchMapsErrorBranches(t *testing.T) {
	t.Parallel()

	ctx := replyWorkflowTestCtx()
	t.Run("repo error", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(FeedbackHandler{repo: ptrext.Of(fakeFeedbackRepo{workbenchErr: errors.New("workbench failed")})})

		_, err := h.GetTerminalFailureWorkbench(ctx, ptrext.Of(attunev1.GetTerminalFailureWorkbenchRequest{}))

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("nil workbench", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(FeedbackHandler{repo: ptrext.Of(fakeFeedbackRepo{})})

		_, err := h.GetTerminalFailureWorkbench(ctx, ptrext.Of(attunev1.GetTerminalFailureWorkbenchRequest{}))

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})
}
