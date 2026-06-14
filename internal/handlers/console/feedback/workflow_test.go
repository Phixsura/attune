// ptrext:file-allow test fixtures build proto-request pointers

package feedback

import (
	"context"
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
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/service/workflow"
)

type fakeWorkflow struct {
	result *workflow.TransitionResult
	batch  *workflow.BatchResult
	err    error
}

func (f *fakeWorkflow) Transition(_ context.Context, _ string, _ int64, _, _, _ string) (*workflow.TransitionResult, error) {
	return f.result, f.err
}

func (f *fakeWorkflow) BatchTransition(_ context.Context, _ string, _ []int64, _, _, _ string) (*workflow.BatchResult, error) {
	return f.batch, f.err
}

type fakeAuditReader struct {
	entries []feedbackaudit.Entry
	cursor  string
	err     error
}

func (f *fakeAuditReader) List(_ context.Context, _ string, _ int64, _ string, _ int) ([]feedbackaudit.Entry, string, error) {
	return f.entries, f.cursor, f.err
}

func newTransitionHandler(wf workflowTransitioner) http.HandlerFunc {
	h := &FeedbackHandler{}
	h.SetWorkflow(wf)
	return dispatcher.Bind(
		"console.FeedbackHandler.TransitionState",
		dispatcher.JSON(func() *attunev1.TransitionFeedbackRequest {
			return ptrext.Of(attunev1.TransitionFeedbackRequest{})
		}),
		h.TransitionState,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TransitionFeedbackRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func TestTransitionState_HTTP(t *testing.T) {
	t.Parallel()

	now := time.Now()

	t.Run("200 OK", func(t *testing.T) {
		handler := newTransitionHandler(&fakeWorkflow{
			result: &workflow.TransitionResult{
				FeedbackID: 1,
				FromState:  &workflowstate.WorkflowState{ID: "s-1", Name: "Open", Color: "#3b82f6", Category: "open", CreatedAt: now, UpdatedAt: now},
				ToState:    &workflowstate.WorkflowState{ID: "s-2", Name: "In Progress", Color: "#f59e0b", Category: "active", CreatedAt: now, UpdatedAt: now},
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/1/transition",
			`{"feedbackId":"1","toStateId":"s-2"}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		to := body["toState"].(map[string]any)
		require.Equal(t, "In Progress", to["name"])
	})

	t.Run("409 invalid transition", func(t *testing.T) {
		handler := newTransitionHandler(&fakeWorkflow{err: workflow.ErrInvalidTransition})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/1/transition",
			`{"feedbackId":"1","toStateId":"s-bad"}`))

		require.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("404 state not found", func(t *testing.T) {
		handler := newTransitionHandler(&fakeWorkflow{err: workflow.ErrStateNotFound})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/1/transition",
			`{"feedbackId":"1","toStateId":"s-gone"}`))

		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("409 no workflow state", func(t *testing.T) {
		handler := newTransitionHandler(&fakeWorkflow{err: workflow.ErrNoWorkflowState})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/1/transition",
			`{"feedbackId":"1","toStateId":"s-2"}`))

		require.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("501 workflow not configured", func(t *testing.T) {
		h := &FeedbackHandler{}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.TransitionState",
			dispatcher.JSON(func() *attunev1.TransitionFeedbackRequest {
				return ptrext.Of(attunev1.TransitionFeedbackRequest{})
			}),
			h.TransitionState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TransitionFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/1/transition",
			`{"feedbackId":"1","toStateId":"s-2"}`))

		require.Equal(t, http.StatusNotImplemented, w.Code)
	})
}

func TestBatchTransitionState_HTTP(t *testing.T) {
	t.Parallel()

	t.Run("200 OK", func(t *testing.T) {
		h := &FeedbackHandler{}
		h.SetWorkflow(&fakeWorkflow{batch: &workflow.BatchResult{Succeeded: 2}})
		handler := dispatcher.Bind(
			"console.FeedbackHandler.BatchTransitionState",
			dispatcher.JSON(func() *attunev1.BatchTransitionFeedbackRequest {
				return ptrext.Of(attunev1.BatchTransitionFeedbackRequest{})
			}),
			h.BatchTransitionState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BatchTransitionFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/transition/batch",
			`{"feedbackIds":["1","2"],"toStateId":"s-2"}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, float64(2), body["succeeded"])
	})

	t.Run("400 empty feedback_ids", func(t *testing.T) {
		h := &FeedbackHandler{}
		h.SetWorkflow(&fakeWorkflow{})
		handler := dispatcher.Bind(
			"console.FeedbackHandler.BatchTransitionState",
			dispatcher.JSON(func() *attunev1.BatchTransitionFeedbackRequest {
				return ptrext.Of(attunev1.BatchTransitionFeedbackRequest{})
			}),
			h.BatchTransitionState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BatchTransitionFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/transition/batch",
			`{"feedbackIds":[],"toStateId":"s-2"}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestListAudit_HTTP(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)

	t.Run("200 with entries", func(t *testing.T) {
		h := &FeedbackHandler{}
		h.SetAuditReader(&fakeAuditReader{
			entries: []feedbackaudit.Entry{
				{
					ID: 1, FeedbackID: 1, EntityType: "workflow_state",
					FieldName: "state", OldValue: ptrext.Of("Open"), NewValue: ptrext.Of("Done"),
					ChangedBy: "user-1", CreatedAt: now,
				},
			},
			cursor: "next-page",
		})
		handler := dispatcher.Bind(
			"console.FeedbackHandler.ListAudit",
			dispatcher.JSON(func() *attunev1.ListAuditRequest {
				return ptrext.Of(attunev1.ListAuditRequest{})
			}),
			h.ListAudit,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListAuditRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/feedback/1/audit",
			`{"feedbackId":"1"}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		entries := body["entries"].([]any)
		require.Len(t, entries, 1)
		require.Equal(t, "next-page", body["nextCursor"])
	})

	t.Run("200 nil audit reader returns empty", func(t *testing.T) {
		h := &FeedbackHandler{}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.ListAudit",
			dispatcher.JSON(func() *attunev1.ListAuditRequest {
				return ptrext.Of(attunev1.ListAuditRequest{})
			}),
			h.ListAudit,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListAuditRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/feedback/1/audit",
			`{"feedbackId":"1"}`))

		require.Equal(t, http.StatusOK, w.Code)
	})
}
