// ptrext:file-allow test fixtures build proto-request pointers

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
	"github.com/Phixsura/attune/internal/repo/feedback"
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

func TestTransitionStateDirectEdges(t *testing.T) {
	t.Parallel()

	ctx := makeRC()
	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		h := &FeedbackHandler{workflow: &fakeWorkflow{err: errors.New("store down")}}

		_, err := h.TransitionState(ctx, ptrext.Of(attunev1.TransitionFeedbackRequest{
			FeedbackId: 1,
			ToStateId:  "s-2",
		}))

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("nil states remain absent", func(t *testing.T) {
		t.Parallel()
		h := &FeedbackHandler{workflow: &fakeWorkflow{result: &workflow.TransitionResult{FeedbackID: 1}}}

		result, err := h.TransitionState(ctx, ptrext.Of(attunev1.TransitionFeedbackRequest{
			FeedbackId: 1,
			ToStateId:  "s-2",
		}))

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.Nil(t, result.Body.GetFromState())
		require.Nil(t, result.Body.GetToState())
	})
}

func TestBatchTransitionStateDirectEdges(t *testing.T) {
	t.Parallel()

	ctx := makeRC()
	t.Run("workflow not configured", func(t *testing.T) {
		t.Parallel()
		_, err := (&FeedbackHandler{}).BatchTransitionState(ctx, ptrext.Of(attunev1.BatchTransitionFeedbackRequest{
			FeedbackIds: []int64{1},
		}))

		requireDispatcherError(t, err, http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("too many ids", func(t *testing.T) {
		t.Parallel()
		ids := make([]int64, 101)
		for i := range ids {
			ids[i] = int64(i + 1)
		}
		h := &FeedbackHandler{workflow: &fakeWorkflow{}}

		_, err := h.BatchTransitionState(ctx, ptrext.Of(attunev1.BatchTransitionFeedbackRequest{
			FeedbackIds: ids,
		}))

		requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST)
	})

	t.Run("service error", func(t *testing.T) {
		t.Parallel()
		h := &FeedbackHandler{workflow: &fakeWorkflow{err: errors.New("batch failed")}}

		_, err := h.BatchTransitionState(ctx, ptrext.Of(attunev1.BatchTransitionFeedbackRequest{
			FeedbackIds: []int64{1},
			ToStateId:   "s-2",
		}))

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("failed items", func(t *testing.T) {
		t.Parallel()
		h := &FeedbackHandler{workflow: &fakeWorkflow{batch: &workflow.BatchResult{
			Succeeded: 1,
			Failed: []workflow.BatchItemFailure{{
				FeedbackID: 2,
				Code:       "INVALID_TRANSITION",
				Message:    "transition not allowed",
			}},
		}}}

		result, err := h.BatchTransitionState(ctx, ptrext.Of(attunev1.BatchTransitionFeedbackRequest{
			FeedbackIds: []int64{1, 2},
			ToStateId:   "s-2",
		}))

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.Equal(t, int32(1), result.Body.GetSucceeded())
		require.Len(t, result.Body.GetFailed(), 1)
		require.Equal(t, int64(2), result.Body.GetFailed()[0].GetFeedbackId())
		require.Equal(t, "INVALID_TRANSITION", result.Body.GetFailed()[0].GetCode())
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

	t.Run("500 audit reader error", func(t *testing.T) {
		h := &FeedbackHandler{}
		h.SetAuditReader(&fakeAuditReader{err: errors.New("audit read failed")})
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

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// ---------------------------------------------------------------------------
// fakeWorkflowStateReader — mock for enrichment tests
// ---------------------------------------------------------------------------

type fakeWorkflowStateReader struct {
	states      []workflowstate.WorkflowState
	transitions []workflowstate.Transition
	allowedNext []workflowstate.WorkflowState
	listErr     error
	transErr    error
	allowedErr  error
}

func (f *fakeWorkflowStateReader) List(_ context.Context, _ string, _ bool) ([]workflowstate.WorkflowState, error) {
	return f.states, f.listErr
}

func (f *fakeWorkflowStateReader) ListTransitions(_ context.Context, _ string) ([]workflowstate.Transition, error) {
	return f.transitions, f.transErr
}

func (f *fakeWorkflowStateReader) AllowedNext(_ context.Context, _, _ string) ([]workflowstate.WorkflowState, error) {
	return f.allowedNext, f.allowedErr
}

func makeRC() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: dispatchtest.TenantID,
			UserID:   dispatchtest.UserID,
		}),
	})
}

// ---------------------------------------------------------------------------
// enrichItemsWithWorkflowState tests
// ---------------------------------------------------------------------------

func TestEnrichItems_HappyPath(t *testing.T) {
	t.Parallel()
	now := time.Now()
	reader := &fakeWorkflowStateReader{
		states: []workflowstate.WorkflowState{
			{ID: "s-1", Name: "Open", Color: "#3b82f6", Category: "open", CreatedAt: now, UpdatedAt: now},
			{ID: "s-2", Name: "Done", Color: "#10b981", Category: "closed", CreatedAt: now, UpdatedAt: now},
		},
		transitions: []workflowstate.Transition{
			{FromStateID: "s-1", ToStateID: "s-2"},
		},
	}
	h := &FeedbackHandler{}
	h.SetWorkflowStates(reader)

	rows := []feedback.ConsoleListRow{
		{ID: 1, WorkflowStateID: ptrext.Of("s-1")},
		{ID: 2, WorkflowStateID: nil},
	}
	items := []*attunev1.Feedback{{}, {}}

	h.enrichItemsWithWorkflowState(makeRC(), "test", "t-1", rows, items)

	require.NotNil(t, items[0].WorkflowState)
	require.Equal(t, "Open", items[0].WorkflowState.GetName())
	require.Len(t, items[0].AllowedNextStates, 1)
	require.Equal(t, "Done", items[0].AllowedNextStates[0].GetName())
	require.Nil(t, items[1].WorkflowState)
}

func TestEnrichItems_NilReader(t *testing.T) {
	t.Parallel()
	h := &FeedbackHandler{}
	items := []*attunev1.Feedback{{}}
	h.enrichItemsWithWorkflowState(makeRC(), "test", "t-1",
		[]feedback.ConsoleListRow{{ID: 1, WorkflowStateID: ptrext.Of("s-1")}}, items)
	require.Nil(t, items[0].WorkflowState)
}

func TestEnrichItems_EmptyRows(t *testing.T) {
	t.Parallel()
	h := &FeedbackHandler{}
	h.SetWorkflowStates(&fakeWorkflowStateReader{})
	h.enrichItemsWithWorkflowState(makeRC(), "test", "t-1", nil, nil)
}

func TestEnrichItems_NoWorkflowStateIDs(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkflowStateReader{}
	h := &FeedbackHandler{}
	h.SetWorkflowStates(reader)

	rows := []feedback.ConsoleListRow{{ID: 1, WorkflowStateID: nil}}
	items := []*attunev1.Feedback{{}}
	h.enrichItemsWithWorkflowState(makeRC(), "test", "t-1", rows, items)
	require.Nil(t, items[0].WorkflowState)
}

func TestEnrichItems_ListError(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkflowStateReader{listErr: errors.New("db error")}
	h := &FeedbackHandler{}
	h.SetWorkflowStates(reader)

	rows := []feedback.ConsoleListRow{{ID: 1, WorkflowStateID: ptrext.Of("s-1")}}
	items := []*attunev1.Feedback{{}}
	h.enrichItemsWithWorkflowState(makeRC(), "test", "t-1", rows, items)
	require.Nil(t, items[0].WorkflowState)
}

func TestEnrichItems_TransitionsError(t *testing.T) {
	t.Parallel()
	now := time.Now()
	reader := &fakeWorkflowStateReader{
		states: []workflowstate.WorkflowState{
			{ID: "s-1", Name: "Open", Color: "#3b82f6", Category: "open", CreatedAt: now, UpdatedAt: now},
		},
		transErr: errors.New("transitions fail"),
	}
	h := &FeedbackHandler{}
	h.SetWorkflowStates(reader)

	rows := []feedback.ConsoleListRow{{ID: 1, WorkflowStateID: ptrext.Of("s-1")}}
	items := []*attunev1.Feedback{{}}
	h.enrichItemsWithWorkflowState(makeRC(), "test", "t-1", rows, items)
	require.Nil(t, items[0].WorkflowState)
}

// ---------------------------------------------------------------------------
// enrichDetailWithWorkflowState tests
// ---------------------------------------------------------------------------

func TestEnrichDetail_HappyPath(t *testing.T) {
	t.Parallel()
	now := time.Now()
	reader := &fakeWorkflowStateReader{
		states: []workflowstate.WorkflowState{
			{ID: "s-1", Name: "Open", Color: "#3b82f6", Category: "open", CreatedAt: now, UpdatedAt: now},
		},
		allowedNext: []workflowstate.WorkflowState{
			{ID: "s-2", Name: "Done", Color: "#10b981", Category: "closed", CreatedAt: now, UpdatedAt: now},
		},
	}
	h := &FeedbackHandler{}
	h.SetWorkflowStates(reader)

	detail := &attunev1.FeedbackDetail{}
	h.enrichDetailWithWorkflowState(makeRC(), "test", "t-1", ptrext.Of("s-1"), detail)

	require.NotNil(t, detail.WorkflowState)
	require.Equal(t, "Open", detail.WorkflowState.GetName())
	require.Len(t, detail.AllowedNextStates, 1)
	require.Equal(t, "Done", detail.AllowedNextStates[0].GetName())
}

func TestEnrichDetail_NilStateID(t *testing.T) {
	t.Parallel()
	h := &FeedbackHandler{}
	h.SetWorkflowStates(&fakeWorkflowStateReader{})
	detail := &attunev1.FeedbackDetail{}
	h.enrichDetailWithWorkflowState(makeRC(), "test", "t-1", nil, detail)
	require.Nil(t, detail.WorkflowState)
}

func TestEnrichDetail_NilReader(t *testing.T) {
	t.Parallel()
	h := &FeedbackHandler{}
	detail := &attunev1.FeedbackDetail{}
	h.enrichDetailWithWorkflowState(makeRC(), "test", "t-1", ptrext.Of("s-1"), detail)
	require.Nil(t, detail.WorkflowState)
}

func TestEnrichDetail_AllowedNextError(t *testing.T) {
	t.Parallel()
	now := time.Now()
	reader := &fakeWorkflowStateReader{
		states: []workflowstate.WorkflowState{
			{ID: "s-1", Name: "Open", Color: "#3b82f6", Category: "open", CreatedAt: now, UpdatedAt: now},
		},
		allowedErr: errors.New("allowed fail"),
	}
	h := &FeedbackHandler{}
	h.SetWorkflowStates(reader)

	detail := &attunev1.FeedbackDetail{}
	h.enrichDetailWithWorkflowState(makeRC(), "test", "t-1", ptrext.Of("s-1"), detail)
	require.NotNil(t, detail.WorkflowState)
	require.Empty(t, detail.AllowedNextStates)
}

func TestEnrichDetail_ListError(t *testing.T) {
	t.Parallel()
	reader := &fakeWorkflowStateReader{
		allowedNext: []workflowstate.WorkflowState{},
		listErr:     errors.New("list fail"),
	}
	h := &FeedbackHandler{}
	h.SetWorkflowStates(reader)

	detail := &attunev1.FeedbackDetail{}
	h.enrichDetailWithWorkflowState(makeRC(), "test", "t-1", ptrext.Of("s-1"), detail)
	require.Nil(t, detail.WorkflowState)
}
