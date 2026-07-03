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
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

type fakeDrafter struct {
	draft     string
	genErr    error
	called    bool
	gotID     int64
	gotTenant string

	pcStatus      string
	pcEnabled     bool
	pcFound       bool
	pcGeneratedAt *time.Time
	pcErr         error
}

func (f *fakeDrafter) Precheck(_ context.Context, _ int64, _ string) (string, bool, bool, *time.Time, error) {
	return f.pcStatus, f.pcEnabled, f.pcFound, f.pcGeneratedAt, f.pcErr
}

func (f *fakeDrafter) Generate(_ context.Context, feedbackID int64, tenantID string) (string, time.Time, error) {
	f.called = true
	f.gotID = feedbackID
	f.gotTenant = tenantID
	return f.draft, time.Now(), f.genErr
}

// okDrafter passes all three guards (found / enabled / enriched).
func okDrafter(draft string) *fakeDrafter {
	return &fakeDrafter{draft: draft, pcFound: true, pcEnabled: true, pcStatus: "done"}
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
	drafter := okDrafter("Sorry to hear that — we're on it.")
	h := &FeedbackHandler{drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, drafter.called)
	require.Equal(t, int64(123), drafter.gotID)
	require.Equal(t, dispatchtest.TenantID, drafter.gotTenant) // tenant scope threaded into Generate
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "Sorry to hear that — we're on it.", body["replyDraft"])
}

func TestRegenerate_NotConfigured(t *testing.T) {
	h := &FeedbackHandler{} // no drafter wired

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRegenerate_NotFound(t *testing.T) {
	drafter := &fakeDrafter{pcFound: false}
	h := &FeedbackHandler{drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusNotFound, w.Code)
	require.False(t, drafter.called)
}

func TestRegenerate_Disabled(t *testing.T) {
	drafter := &fakeDrafter{pcFound: true, pcEnabled: false, pcStatus: "done"}
	h := &FeedbackHandler{drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, drafter.called) // opt-in gate enforced before any LLM call
}

func TestRegenerate_NotEnriched(t *testing.T) {
	drafter := &fakeDrafter{pcFound: true, pcEnabled: true, pcStatus: "pending"}
	h := &FeedbackHandler{drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusConflict, w.Code)
	require.False(t, drafter.called) // no draft for an un-enriched row
}

func TestRegenerate_GenerateError(t *testing.T) {
	drafter := &fakeDrafter{pcFound: true, pcEnabled: true, pcStatus: "done", genErr: errors.New("provider down")}
	h := &FeedbackHandler{drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusBadGateway, w.Code)
}

func TestRegenerate_AuditFailureReturnsInternalError(t *testing.T) {
	drafter := okDrafter("fresh draft")
	h := &FeedbackHandler{
		drafter:       drafter,
		replyWorkflow: &fakeReplyWorkflow{snap: testReplySnapshot("suggested")},
		audit:         &fakeAuditRecorder{err: errors.New("audit sink down")},
	}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.True(t, drafter.called)
}

func TestRegenerate_FailsClosedWhenWorkflowSnapshotFails(t *testing.T) {
	drafter := okDrafter("should not be generated")
	h := &FeedbackHandler{
		drafter:       drafter,
		replyWorkflow: &fakeReplyWorkflow{err: errors.New("workflow read failed")},
	}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusBadGateway, w.Code)
	require.False(t, drafter.called)
}

func TestRegenerate_Cooldown(t *testing.T) {
	recent := time.Now().Add(-2 * time.Second) // within the 10s cooldown
	drafter := okDrafter("should not be generated")
	drafter.pcGeneratedAt = &recent
	h := &FeedbackHandler{drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.False(t, drafter.called) // no LLM call spent inside the cooldown window
}

func TestRegenerate_CooldownElapsed(t *testing.T) {
	old := time.Now().Add(-time.Hour) // well past the cooldown
	drafter := okDrafter("fresh draft")
	drafter.pcGeneratedAt = &old
	h := &FeedbackHandler{drafter: drafter}

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, drafter.called)
}

func TestRegenerate_RateLimited(t *testing.T) {
	drafter := okDrafter("should not be generated")
	h := &FeedbackHandler{drafter: drafter}
	h.SetRegenLimiter(ratelimit.New(60, 0, false, nil)) // burst 0 → every request denied

	w := httptest.NewRecorder()
	regenerateHandler(h)(w, regenRequest())

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.False(t, drafter.called) // rejected before any LLM call
}
