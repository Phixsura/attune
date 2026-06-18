// ptrext:file-allow test fixtures build proto-request pointers

package workflow

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
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	workflowsvc "github.com/Phixsura/attune/internal/service/workflow"
)

// --- fakes ---

type fakeStateStore struct {
	states       []workflowstate.WorkflowState
	created      *workflowstate.WorkflowState
	updated      *workflowstate.WorkflowState
	transitions  []workflowstate.Transition
	createErr    error
	updateErr    error
	getErr       error
	listErr      error
	replaceErr   error
	listTransErr error
}

func (f *fakeStateStore) List(_ context.Context, _ string, _ bool) ([]workflowstate.WorkflowState, error) {
	return f.states, f.listErr
}

func (f *fakeStateStore) Create(_ context.Context, s workflowstate.WorkflowState) (*workflowstate.WorkflowState, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	s.ID = "new-id"
	s.CreatedAt = time.Now()
	s.UpdatedAt = time.Now()
	f.created = &s
	return &s, nil
}

func (f *fakeStateStore) Update(_ context.Context, s workflowstate.WorkflowState) (*workflowstate.WorkflowState, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	s.UpdatedAt = time.Now()
	f.updated = &s
	return &s, nil
}

func (f *fakeStateStore) GetByTenantAndID(_ context.Context, _, _ string) (*workflowstate.WorkflowState, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if len(f.states) > 0 {
		return &f.states[0], nil
	}
	return nil, workflowstate.ErrNotFound
}

func (f *fakeStateStore) ListTransitions(_ context.Context, _ string) ([]workflowstate.Transition, error) {
	return f.transitions, f.listTransErr
}

func (f *fakeStateStore) ReplaceTransitions(_ context.Context, _ string, _ []workflowstate.TransitionEdge) ([]workflowstate.Transition, error) {
	return f.transitions, f.replaceErr
}

type fakeWorkflowService struct {
	archiveErr error
	seedErr    error
}

func (f *fakeWorkflowService) ArchiveState(_ context.Context, _, _ string) error {
	return f.archiveErr
}

func (f *fakeWorkflowService) SeedDefaults(_ context.Context, _ string) error {
	return f.seedErr
}

// --- tests ---

func TestCreateState_HTTP(t *testing.T) {
	t.Parallel()

	t.Run("200 OK", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.CreateState",
			dispatcher.JSON(func() *attunev1.CreateStateRequest { return ptrext.Of(attunev1.CreateStateRequest{}) }),
			h.CreateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/workflow/states",
			`{"name":"open","displayName":{"entries":{"default":"Open"}},"category":"open","color":"#3b82f6"}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		state := body["state"].(map[string]any)
		require.Equal(t, "open", state["name"])
		dn := state["displayName"].(map[string]any)["entries"].(map[string]any)
		require.Equal(t, "Open", dn["default"])
	})

	t.Run("400 empty name", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.CreateState",
			dispatcher.JSON(func() *attunev1.CreateStateRequest { return ptrext.Of(attunev1.CreateStateRequest{}) }),
			h.CreateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/workflow/states",
			`{"name":"","category":"open"}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 bad category", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.CreateState",
			dispatcher.JSON(func() *attunev1.CreateStateRequest { return ptrext.Of(attunev1.CreateStateRequest{}) }),
			h.CreateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/workflow/states",
			`{"name":"x","displayName":{"entries":{"default":"X"}},"category":"invalid"}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("409 name conflict", func(t *testing.T) {
		h := NewHandler(
			&fakeStateStore{createErr: workflowstate.ErrNameConflict},
			&fakeWorkflowService{},
		)
		handler := dispatcher.Bind(
			"console.WorkflowHandler.CreateState",
			dispatcher.JSON(func() *attunev1.CreateStateRequest { return ptrext.Of(attunev1.CreateStateRequest{}) }),
			h.CreateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/workflow/states",
			`{"name":"dup","displayName":{"entries":{"default":"Dup"}},"category":"open"}`))

		require.Equal(t, http.StatusConflict, w.Code)
	})
}

func TestArchiveState_HTTP(t *testing.T) {
	t.Parallel()

	t.Run("200 OK", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.ArchiveState",
			dispatcher.Path(
				func() *attunev1.ArchiveStateRequest { return ptrext.Of(attunev1.ArchiveStateRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ArchiveStateRequest, id string) { req.Id = id }),
			),
			h.ArchiveState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ArchiveStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodDelete, "/workflow/states/s-1", "",
			dispatchtest.Param{Name: "id", Value: "s-1"}))

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("404 not found", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{archiveErr: workflowsvc.ErrStateNotFound})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.ArchiveState",
			dispatcher.Path(
				func() *attunev1.ArchiveStateRequest { return ptrext.Of(attunev1.ArchiveStateRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ArchiveStateRequest, id string) { req.Id = id }),
			),
			h.ArchiveState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ArchiveStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodDelete, "/workflow/states/missing", "",
			dispatchtest.Param{Name: "id", Value: "missing"}))

		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("409 state in use", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{archiveErr: workflowsvc.ErrStateInUse})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.ArchiveState",
			dispatcher.Path(
				func() *attunev1.ArchiveStateRequest { return ptrext.Of(attunev1.ArchiveStateRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ArchiveStateRequest, id string) { req.Id = id }),
			),
			h.ArchiveState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ArchiveStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodDelete, "/workflow/states/busy", "",
			dispatchtest.Param{Name: "id", Value: "busy"}))

		require.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("409 last default", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{archiveErr: workflowsvc.ErrLastDefault})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.ArchiveState",
			dispatcher.Path(
				func() *attunev1.ArchiveStateRequest { return ptrext.Of(attunev1.ArchiveStateRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ArchiveStateRequest, id string) { req.Id = id }),
			),
			h.ArchiveState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ArchiveStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodDelete, "/workflow/states/last", "",
			dispatchtest.Param{Name: "id", Value: "last"}))

		require.Equal(t, http.StatusConflict, w.Code)
	})
}

func TestListStates_HTTP(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	h := NewHandler(&fakeStateStore{
		states: []workflowstate.WorkflowState{
			{ID: "s-1", Name: "Open", Color: "#3b82f6", Category: "open", CreatedAt: now, UpdatedAt: now},
			{ID: "s-2", Name: "Done", Color: "#10b981", Category: "closed", CreatedAt: now, UpdatedAt: now},
		},
	}, &fakeWorkflowService{})

	handler := dispatcher.Bind(
		"console.WorkflowHandler.ListStates",
		dispatcher.Empty(func() *attunev1.ListStatesRequest { return ptrext.Of(attunev1.ListStatesRequest{}) }),
		h.ListStates,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListStatesRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/workflow/states", ""))

	require.Equal(t, http.StatusOK, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	states := body["states"].([]any)
	require.Len(t, states, 2)
}

func TestUpdateState_HTTP(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)

	t.Run("200 OK", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{
			states: []workflowstate.WorkflowState{
				{ID: "s-1", TenantID: "tenant-1", Name: "Open", Color: "#3b82f6", Category: "open", CreatedAt: now, UpdatedAt: now},
			},
		}, &fakeWorkflowService{})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.UpdateState",
			dispatcher.JSON(func() *attunev1.UpdateStateRequest { return ptrext.Of(attunev1.UpdateStateRequest{}) }),
			h.UpdateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPut, "/workflow/states/s-1",
			`{"id":"s-1","displayName":{"entries":{"default":"Renamed"}},"color":"#ef4444","position":2,"isDefault":true}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		state := body["state"].(map[string]any)
		require.Equal(t, "Open", state["name"]) // key immutable on update
		dn := state["displayName"].(map[string]any)["entries"].(map[string]any)
		require.Equal(t, "Renamed", dn["default"])
	})

	t.Run("404 not found", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.UpdateState",
			dispatcher.JSON(func() *attunev1.UpdateStateRequest { return ptrext.Of(attunev1.UpdateStateRequest{}) }),
			h.UpdateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPut, "/workflow/states/missing",
			`{"id":"missing","name":"X"}`))

		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("409 name conflict on update", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{
			states:    []workflowstate.WorkflowState{{ID: "s-1", TenantID: "tenant-1", Name: "Open", Category: "open", CreatedAt: now, UpdatedAt: now}},
			updateErr: workflowstate.ErrNameConflict,
		}, &fakeWorkflowService{})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.UpdateState",
			dispatcher.JSON(func() *attunev1.UpdateStateRequest { return ptrext.Of(attunev1.UpdateStateRequest{}) }),
			h.UpdateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateStateRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPut, "/workflow/states/s-1",
			`{"id":"s-1","name":"Dup"}`))

		require.Equal(t, http.StatusConflict, w.Code)
	})
}

func TestListTransitions_HTTP(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	h := NewHandler(&fakeStateStore{
		transitions: []workflowstate.Transition{
			{ID: "t-1", TenantID: "tenant-1", FromStateID: "s-1", ToStateID: "s-2", CreatedAt: now},
		},
	}, &fakeWorkflowService{})

	handler := dispatcher.Bind(
		"console.WorkflowHandler.ListTransitions",
		dispatcher.Empty(func() *attunev1.ListTransitionsRequest { return ptrext.Of(attunev1.ListTransitionsRequest{}) }),
		h.ListTransitions,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListTransitionsRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/workflow/transitions", ""))

	require.Equal(t, http.StatusOK, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	transitions := body["transitions"].([]any)
	require.Len(t, transitions, 1)
}

func TestReplaceTransitions_HTTP(t *testing.T) {
	t.Parallel()

	t.Run("200 OK", func(t *testing.T) {
		now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
		h := NewHandler(&fakeStateStore{
			transitions: []workflowstate.Transition{
				{ID: "t-new", TenantID: "tenant-1", FromStateID: "s-1", ToStateID: "s-2", CreatedAt: now},
			},
		}, &fakeWorkflowService{})

		handler := dispatcher.Bind(
			"console.WorkflowHandler.ReplaceTransitions",
			dispatcher.JSON(func() *attunev1.ReplaceTransitionsRequest {
				return ptrext.Of(attunev1.ReplaceTransitionsRequest{})
			}),
			h.ReplaceTransitions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ReplaceTransitionsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPut, "/workflow/transitions",
			`{"transitions":[{"fromStateId":"s-1","toStateId":"s-2"}]}`))

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("500 replace error", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{replaceErr: errors.New("replace fail")}, &fakeWorkflowService{})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.ReplaceTransitions",
			dispatcher.JSON(func() *attunev1.ReplaceTransitionsRequest {
				return ptrext.Of(attunev1.ReplaceTransitionsRequest{})
			}),
			h.ReplaceTransitions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ReplaceTransitionsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPut, "/workflow/transitions",
			`{"transitions":[{"fromStateId":"s-1","toStateId":"s-2"}]}`))

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestSeedDefaults_HTTP(t *testing.T) {
	t.Parallel()

	t.Run("200 OK", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.SeedDefaults",
			dispatcher.Empty(func() *attunev1.SeedDefaultsRequest { return ptrext.Of(attunev1.SeedDefaultsRequest{}) }),
			h.SeedDefaults,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SeedDefaultsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/workflow/seed", ""))

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("500 seed error", func(t *testing.T) {
		h := NewHandler(&fakeStateStore{}, &fakeWorkflowService{seedErr: errors.New("db down")})
		handler := dispatcher.Bind(
			"console.WorkflowHandler.SeedDefaults",
			dispatcher.Empty(func() *attunev1.SeedDefaultsRequest { return ptrext.Of(attunev1.SeedDefaultsRequest{}) }),
			h.SeedDefaults,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SeedDefaultsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/workflow/seed", ""))

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
