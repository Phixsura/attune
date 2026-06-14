package workflow

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	workflowsvc "github.com/Phixsura/attune/internal/service/workflow"
)

type stateStore interface {
	List(ctx context.Context, tenantID string, includeArchived bool) ([]workflowstate.WorkflowState, error)
	Create(ctx context.Context, s workflowstate.WorkflowState) (*workflowstate.WorkflowState, error)
	Update(ctx context.Context, s workflowstate.WorkflowState) (*workflowstate.WorkflowState, error)
	GetByTenantAndID(ctx context.Context, tenantID, id string) (*workflowstate.WorkflowState, error)
	ListTransitions(ctx context.Context, tenantID string) ([]workflowstate.Transition, error)
	ReplaceTransitions(ctx context.Context, tenantID string, edges []workflowstate.TransitionEdge) ([]workflowstate.Transition, error)
}

type workflowService interface {
	ArchiveState(ctx context.Context, tenantID, stateID string) error
	SeedDefaults(ctx context.Context, tenantID string) error
}

type Handler struct {
	states  stateStore
	service workflowService
}

func NewHandler(states stateStore, svc workflowService) *Handler {
	return ptrext.Of(Handler{states: states, service: svc})
}

func StateToProto(s workflowstate.WorkflowState) *attunev1.WorkflowState {
	return ptrext.Of(attunev1.WorkflowState{
		Id:        s.ID,
		Name:      s.Name,
		Color:     s.Color,
		Category:  s.Category,
		Position:  int32(s.Position),
		IsDefault: s.IsDefault,
		Archived:  s.ArchivedAt != nil,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) ListStates(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListStatesRequest,
) (dispatcher.Result[*attunev1.ListStatesResponse], error) {
	const where = "console.WorkflowHandler.ListStates"
	auth := ctx.Auth

	states, err := h.states.List(ctx, auth.TenantID, req.GetIncludeArchived())
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListStatesResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list states")
	}

	out := make([]*attunev1.WorkflowState, len(states))
	for i, s := range states {
		out[i] = StateToProto(s)
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListStatesResponse{States: out}))
}

func (h *Handler) CreateState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateStateRequest,
) (dispatcher.Result[*attunev1.CreateStateResponse], error) {
	const where = "console.WorkflowHandler.CreateState"
	auth := ctx.Auth

	name := req.GetName()
	if name == "" || len(name) > 48 {
		return dispatcher.Fail[*attunev1.CreateStateResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "name must be 1-48 chars")
	}
	cat := req.GetCategory()
	if cat != "open" && cat != "active" && cat != "closed" {
		return dispatcher.Fail[*attunev1.CreateStateResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "category must be open, active, or closed")
	}
	color := req.GetColor()
	if color == "" {
		color = "#6b7280"
	}

	created, err := h.states.Create(ctx, workflowstate.WorkflowState{
		TenantID: auth.TenantID,
		Name:     name,
		Color:    color,
		Category: cat,
		Position: int(req.GetPosition()),
	})
	if err != nil {
		if errors.Is(err, workflowstate.ErrNameConflict) {
			return dispatcher.Fail[*attunev1.CreateStateResponse](
				http.StatusConflict, attunev1.ErrorCode_CONFLICT, "state name already exists")
		}
		logext.Errorf(ctx, "[%s] create failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateStateResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create state")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,state_id:%s,name:%s", where, auth.TenantID, created.ID, created.Name)
	return dispatcher.OK(ptrext.Of(attunev1.CreateStateResponse{State: StateToProto(ptrext.Indirect(created))}))
}

func (h *Handler) UpdateState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateStateRequest,
) (dispatcher.Result[*attunev1.UpdateStateResponse], error) {
	const where = "console.WorkflowHandler.UpdateState"
	auth := ctx.Auth

	existing, err := h.states.GetByTenantAndID(ctx, auth.TenantID, req.GetId())
	if err != nil {
		if errors.Is(err, workflowstate.ErrNotFound) {
			return dispatcher.Fail[*attunev1.UpdateStateResponse](
				http.StatusNotFound, attunev1.ErrorCode_WORKFLOW_STATE_NOT_FOUND, "state not found")
		}
		logext.Errorf(ctx, "[%s] lookup failed,id:%s,err:%+v", where, req.GetId(), err.Error())
		return dispatcher.Fail[*attunev1.UpdateStateResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "lookup failed")
	}

	if req.Name != nil {
		existing.Name = req.GetName()
	}
	if req.Color != nil {
		existing.Color = req.GetColor()
	}
	if req.Position != nil {
		existing.Position = int(req.GetPosition())
	}
	if req.IsDefault != nil {
		existing.IsDefault = req.GetIsDefault()
	}

	updated, err := h.states.Update(ctx, ptrext.Indirect(existing))
	if err != nil {
		if errors.Is(err, workflowstate.ErrNameConflict) {
			return dispatcher.Fail[*attunev1.UpdateStateResponse](
				http.StatusConflict, attunev1.ErrorCode_CONFLICT, "state name already exists")
		}
		if errors.Is(err, workflowstate.ErrNotFound) {
			return dispatcher.Fail[*attunev1.UpdateStateResponse](
				http.StatusNotFound, attunev1.ErrorCode_WORKFLOW_STATE_NOT_FOUND, "state not found")
		}
		logext.Errorf(ctx, "[%s] update failed,id:%s,err:%+v", where, req.GetId(), err.Error())
		return dispatcher.Fail[*attunev1.UpdateStateResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update state")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,state_id:%s", where, auth.TenantID, updated.ID)
	return dispatcher.OK(ptrext.Of(attunev1.UpdateStateResponse{State: StateToProto(ptrext.Indirect(updated))}))
}

func (h *Handler) ArchiveState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ArchiveStateRequest,
) (dispatcher.Result[*attunev1.ArchiveStateResponse], error) {
	const where = "console.WorkflowHandler.ArchiveState"
	auth := ctx.Auth

	err := h.service.ArchiveState(ctx, auth.TenantID, req.GetId())
	if err != nil {
		switch {
		case errors.Is(err, workflowsvc.ErrStateNotFound):
			return dispatcher.Fail[*attunev1.ArchiveStateResponse](
				http.StatusNotFound, attunev1.ErrorCode_WORKFLOW_STATE_NOT_FOUND, "state not found")
		case errors.Is(err, workflowsvc.ErrStateInUse):
			return dispatcher.Fail[*attunev1.ArchiveStateResponse](
				http.StatusConflict, attunev1.ErrorCode_CONFLICT, "state has active feedback")
		case errors.Is(err, workflowsvc.ErrLastDefault):
			return dispatcher.Fail[*attunev1.ArchiveStateResponse](
				http.StatusConflict, attunev1.ErrorCode_CONFLICT, "cannot archive last default state")
		default:
			logext.Errorf(ctx, "[%s] archive failed,id:%s,err:%+v", where, req.GetId(), err.Error())
			return dispatcher.Fail[*attunev1.ArchiveStateResponse](
				http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to archive state")
		}
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,state_id:%s", where, auth.TenantID, req.GetId())
	return dispatcher.OK(ptrext.Of(attunev1.ArchiveStateResponse{}))
}

func (h *Handler) ListTransitions(
	ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListTransitionsRequest,
) (dispatcher.Result[*attunev1.ListTransitionsResponse], error) {
	const where = "console.WorkflowHandler.ListTransitions"
	auth := ctx.Auth

	transitions, err := h.states.ListTransitions(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListTransitionsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list transitions")
	}

	out := make([]*attunev1.WorkflowTransition, len(transitions))
	for i, t := range transitions {
		out[i] = ptrext.Of(attunev1.WorkflowTransition{
			Id:          t.ID,
			FromStateId: t.FromStateID,
			ToStateId:   t.ToStateID,
		})
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListTransitionsResponse{Transitions: out}))
}

func (h *Handler) ReplaceTransitions(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ReplaceTransitionsRequest,
) (dispatcher.Result[*attunev1.ReplaceTransitionsResponse], error) {
	const where = "console.WorkflowHandler.ReplaceTransitions"
	auth := ctx.Auth

	edges := make([]workflowstate.TransitionEdge, len(req.GetTransitions()))
	for i, e := range req.GetTransitions() {
		edges[i] = workflowstate.TransitionEdge{
			FromStateID: e.GetFromStateId(),
			ToStateID:   e.GetToStateId(),
		}
	}

	transitions, err := h.states.ReplaceTransitions(ctx, auth.TenantID, edges)
	if err != nil {
		logext.Errorf(ctx, "[%s] replace failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ReplaceTransitionsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to replace transitions")
	}

	out := make([]*attunev1.WorkflowTransition, len(transitions))
	for i, t := range transitions {
		out[i] = ptrext.Of(attunev1.WorkflowTransition{
			Id:          t.ID,
			FromStateId: t.FromStateID,
			ToStateId:   t.ToStateID,
		})
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,edges:%d", where, auth.TenantID, len(transitions))
	return dispatcher.OK(ptrext.Of(attunev1.ReplaceTransitionsResponse{Transitions: out}))
}

func (h *Handler) SeedDefaults(
	ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.SeedDefaultsRequest,
) (dispatcher.Result[*attunev1.SeedDefaultsResponse], error) {
	const where = "console.WorkflowHandler.SeedDefaults"
	auth := ctx.Auth

	if err := h.service.SeedDefaults(ctx, auth.TenantID); err != nil {
		logext.Errorf(ctx, "[%s] seed failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.SeedDefaultsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to seed defaults")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s", where, auth.TenantID)
	return dispatcher.OK(ptrext.Of(attunev1.SeedDefaultsResponse{}))
}
