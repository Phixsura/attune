// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package feedback

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
)

type fakeDrafter struct {
	draft  string
	err    error
	gotID  int64
	called bool
}

func (f *fakeDrafter) Generate(_ context.Context, feedbackID int64) (string, error) {
	f.called = true
	f.gotID = feedbackID
	return f.draft, f.err
}

func regenerateHandler(h *FeedbackHandler) http.HandlerFunc {
	return dispatcher.Bind(
		"console.FeedbackHandler.Regenerate",
		dispatcher.Path(
			func() *attunev1.RegenerateReplyDraftRequest { return &attunev1.RegenerateReplyDraftRequest{} },
			dispatcher.ParamInt64("id", func(req *attunev1.RegenerateReplyDraftRequest, id int64) { req.Id = id }, "id must be an integer"),
		),
		h.Regenerate,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RegenerateReplyDraftRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func regenRequest() *http.Request {
	return dispatchtest.Request(
		http.MethodPost,
		"/fb/v1/console/feedback/123/reply-draft/regenerate",
		"",
		dispatchtest.Param{Name: "id", Value: "123"},
	)
}

func TestRegenerate_Success(t *testing.T) {
	repo := &fakeFeedbackRepo{getRow: &feedbackrepo.ConsoleDetailRow{}}
	drafter := &fakeDrafter{draft: "Sorry to hear that — we're on it."}
	h := &FeedbackHandler{repo: repo, drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, drafter.called)
	require.Equal(t, int64(123), drafter.gotID)
	require.Equal(t, dispatchtest.TenantID, repo.getTenant) // tenant-scoped ownership check ran
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "Sorry to hear that — we're on it.", body["replyDraft"])
}

func TestRegenerate_NotConfigured(t *testing.T) {
	repo := &fakeFeedbackRepo{getRow: &feedbackrepo.ConsoleDetailRow{}}
	h := &FeedbackHandler{repo: repo} // no drafter wired

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRegenerate_NotFound(t *testing.T) {
	repo := &fakeFeedbackRepo{getErr: feedbackrepo.ErrFeedbackNotFound}
	drafter := &fakeDrafter{draft: "x"}
	h := &FeedbackHandler{repo: repo, drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusNotFound, w.Code)
	require.False(t, drafter.called) // never reaches the LLM on a cross-tenant / missing id
}

func TestRegenerate_GenerateError(t *testing.T) {
	repo := &fakeFeedbackRepo{getRow: &feedbackrepo.ConsoleDetailRow{}}
	drafter := &fakeDrafter{err: errors.New("provider down")}
	h := &FeedbackHandler{repo: repo, drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusBadGateway, w.Code)
}
