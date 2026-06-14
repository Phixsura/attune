// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test fixtures build proto-request pointers

package feedback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/feedbackbatch"
)

// fakeBatchService implements feedbackbatch.Service for testing.
type fakeBatchService struct {
	resp *feedbackbatch.BatchResponse
	err  error
}

func (f *fakeBatchService) Execute(_ context.Context, _ *feedbackbatch.BatchRequest) (*feedbackbatch.BatchResponse, error) {
	return f.resp, f.err
}

func (f *fakeBatchService) GetJobStatus(_ context.Context, _, _ string) (*attunev1.JobStatusResponse, error) {
	return nil, nil
}

func (f *fakeBatchService) ListJobs(_ context.Context, _ string, _ *string, _ int) ([]*attunev1.JobStatusResponse, error) {
	return nil, nil
}

func (f *fakeBatchService) CancelJob(_ context.Context, _, _ string) (*attunev1.CancelJobResponse, error) {
	return nil, nil
}

func newBatchHandler(svc feedbackbatch.Service) http.HandlerFunc {
	h := NewBatchHandler(svc)
	return dispatcher.Bind(
		"console.BatchHandler.Execute",
		dispatcher.JSON(func() *attunev1.BatchFeedbackRequest {
			return ptrext.Of(attunev1.BatchFeedbackRequest{})
		}),
		h.Execute,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BatchFeedbackRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func TestBatchHandler_Execute(t *testing.T) {
	t.Parallel()

	t.Run("200 OK tag operation", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			resp: &feedbackbatch.BatchResponse{
				TotalMatched: 3,
				Succeeded:    3,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1,2,3],"operation":{"tag":{"add_tag_ids":["tag-1"]}}}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, float64(3), body["totalMatched"])
		require.Equal(t, float64(3), body["succeeded"])
	})

	t.Run("200 OK workflow operation", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			resp: &feedbackbatch.BatchResponse{
				TotalMatched: 2,
				Succeeded:    2,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1,2],"operation":{"workflow":{"to_state_id":"s-1"}}}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, float64(2), body["totalMatched"])
	})

	t.Run("200 OK delete operation", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			resp: &feedbackbatch.BatchResponse{
				TotalMatched: 5,
				Succeeded:    5,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1,2,3,4,5],"operation":{"delete":{}}}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, float64(5), body["succeeded"])
	})

	t.Run("200 OK query-based with filter", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			resp: &feedbackbatch.BatchResponse{
				TotalMatched: 10,
				Succeeded:    10,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"query":{"urgent":true},"max_affected":100,"operation":{"tag":{"add_tag_ids":["tag-2"]}}}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, float64(10), body["totalMatched"])
	})

	t.Run("200 OK dry run returns count", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			resp: &feedbackbatch.BatchResponse{
				TotalMatched: 50,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1,2,3],"dry_run":true,"operation":{"delete":{}}}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, float64(50), body["totalMatched"])
		require.Equal(t, float64(0), body["succeeded"])
	})

	t.Run("202 Accepted async job creation", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			resp: &feedbackbatch.BatchResponse{
				TotalMatched: 150,
				JobID:        "job-123",
				JobStatusURL: "/fb/v1/console/jobs/job-123",
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1,2,3],"operation":{"tag":{"add_tag_ids":["tag-1"]}}}`))

		require.Equal(t, http.StatusAccepted, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "job-123", body["jobId"])
		require.Equal(t, "/fb/v1/console/jobs/job-123", body["jobStatusUrl"])
	})

	t.Run("429 rate limit", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			err: feedbackbatch.ErrRateLimited,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1],"operation":{"delete":{}}}`))

		require.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("409 idempotency conflict", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			err: feedbackbatch.ErrIdempotencyConflict,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1],"idempotency_key":"key-1","operation":{"delete":{}}}`))

		require.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("409 request in progress", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			err: feedbackbatch.ErrRequestInProgress,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1],"idempotency_key":"key-2","operation":{"delete":{}}}`))

		require.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("400 count mismatch", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			err: feedbackbatch.ErrCountMismatch,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"query":{},"confirm_count":5,"operation":{"delete":{}}}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 exceeds max affected", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			err: feedbackbatch.ErrExceedsMaxAffected,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"query":{},"max_affected":10,"operation":{"delete":{}}}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 no operation", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			err: feedbackbatch.ErrNoOperation,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1]}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 no selection", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			err: feedbackbatch.ErrNoSelection,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"operation":{"delete":{}}}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 mixed selection", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			err: feedbackbatch.ErrMixedSelection,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1],"query":{},"operation":{"delete":{}}}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 invalid if_unmodified_since format", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			resp: &feedbackbatch.BatchResponse{},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1],"if_unmodified_since":"not-a-date","operation":{"delete":{}}}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("with valid if_unmodified_since", func(t *testing.T) {
		handler := newBatchHandler(&fakeBatchService{
			resp: &feedbackbatch.BatchResponse{
				TotalMatched: 1,
				Succeeded:    1,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/batch",
			`{"feedback_ids":[1],"if_unmodified_since":"2025-01-15T10:00:00Z","operation":{"delete":{}}}`))

		require.Equal(t, http.StatusOK, w.Code)
	})
}
