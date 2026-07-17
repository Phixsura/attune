// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package feedback

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
)

type fakeRetryRepo struct {
	*fakeFeedbackRepo
	retryResult *feedbackrepo.RetryResult
	retryErr    error
	calledID    int64
}

func (f *fakeRetryRepo) RetryEnrichment(_ context.Context, _ string, id int64) (*feedbackrepo.RetryResult, error) {
	f.calledID = id
	return f.retryResult, f.retryErr
}

func retryHandler(h *FeedbackHandler) http.HandlerFunc {
	return dispatcher.Bind(
		"console.FeedbackHandler.RetryEnrichment",
		dispatcher.Path(
			func() *attunev1.RetryEnrichmentRequest { return &attunev1.RetryEnrichmentRequest{} },
			dispatcher.ParamInt64("id", func(req *attunev1.RetryEnrichmentRequest, id int64) { req.Id = id }, "id must be an integer"),
		),
		h.RetryEnrichment,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RetryEnrichmentRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func retryRequest() *http.Request {
	return dispatchtest.Request(
		http.MethodPost,
		"/fb/v1/console/feedback/123/retry-enrichment",
		"",
		dispatchtest.Param{Name: "id", Value: "123"},
	)
}

func TestRetryEnrichment_Success(t *testing.T) {
	now := time.Now()
	repo := &fakeRetryRepo{
		fakeFeedbackRepo: &fakeFeedbackRepo{},
		retryResult: &feedbackrepo.RetryResult{
			ID:          123,
			Status:      "pending",
			Attempts:    0,
			NextRetryAt: ptrext.Of(now),
		},
	}
	h := &FeedbackHandler{repo: repo}

	w := httptest.NewRecorder()
	retryHandler(h)(w, retryRequest())

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(123), repo.calledID)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "123", body["id"])
	require.Equal(t, "pending", body["enrichmentStatus"])
	require.Equal(t, float64(0), body["enrichmentAttempts"])
}

func TestRetryEnrichment_AuditsSuccess(t *testing.T) {
	now := time.Now()
	repo := &fakeRetryRepo{
		fakeFeedbackRepo: &fakeFeedbackRepo{},
		retryResult: &feedbackrepo.RetryResult{
			ID:          123,
			Status:      "pending",
			Attempts:    0,
			NextRetryAt: ptrext.Of(now),
		},
	}
	audit := &fakeAuditRecorder{}
	h := &FeedbackHandler{repo: repo, audit: audit}

	w := httptest.NewRecorder()
	retryHandler(h)(w, retryRequest())

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, audit.events, 1)
	require.Equal(t, "retry_enrichment", audit.events[0].Action)
	require.Equal(t, "feedback", audit.events[0].TargetType)
	require.Equal(t, "123", audit.events[0].TargetID)
}

func TestRetryEnrichment_NotFound(t *testing.T) {
	repo := &fakeRetryRepo{
		fakeFeedbackRepo: &fakeFeedbackRepo{},
		retryErr:         feedbackrepo.ErrFeedbackNotFound,
	}
	h := &FeedbackHandler{repo: repo}

	w := httptest.NewRecorder()
	retryHandler(h)(w, retryRequest())

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRetryEnrichment_InvalidState(t *testing.T) {
	repo := &fakeRetryRepo{
		fakeFeedbackRepo: &fakeFeedbackRepo{},
		retryErr:         feedbackrepo.ErrInvalidStateForRetry,
	}
	h := &FeedbackHandler{repo: repo}

	w := httptest.NewRecorder()
	retryHandler(h)(w, retryRequest())

	require.Equal(t, http.StatusConflict, w.Code)
}

func TestRetryEnrichment_InternalError(t *testing.T) {
	repo := &fakeRetryRepo{
		fakeFeedbackRepo: &fakeFeedbackRepo{},
		retryErr:         errors.New("database error"),
	}
	h := &FeedbackHandler{repo: repo}

	w := httptest.NewRecorder()
	retryHandler(h)(w, retryRequest())

	require.Equal(t, http.StatusInternalServerError, w.Code)
}
