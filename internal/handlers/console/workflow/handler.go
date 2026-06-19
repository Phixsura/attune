package workflow

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/google/uuid"

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
	audit   auditRecorder
}

func NewHandler(states stateStore, svc workflowService) *Handler {
	return ptrext.Of(Handler{states: states, service: svc})
}

// stateKeyPattern validates the stable machine key (mirrors Dimension.name).
var stateKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)

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
	if !stateKeyPattern.MatchString(name) {
		return dispatcher.Fail[*attunev1.CreateStateResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "name (machine key) must match ^[a-z][a-z0-9_]{0,30}$")
	}
	display := i18nFromProto(req.GetDisplayName())
	if !display.NonEmpty() {
		return dispatcher.Fail[*attunev1.CreateStateResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "display_name must have at least one non-empty entry")
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
		TenantID:    auth.TenantID,
		Name:        name,
		DisplayName: display,
		Color:       color,
		Category:    cat,
		Position:    int(req.GetPosition()),
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
	if err := h.recordAudit(ctx, "workflow_state.create", "workflow_state", created.ID,
		workflowSummary("Created workflow state", created.DisplayName.Resolve(nil)), nil, stateAuditSnapshot(ptrext.Indirect(created))); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,state_id:%s,err:%+v", where, auth.TenantID, created.ID, err.Error())
		return dispatcher.Fail[*attunev1.CreateStateResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(ptrext.Of(attunev1.CreateStateResponse{State: StateToProto(ptrext.Indirect(created))}))
}

func (h *Handler) UpdateState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateStateRequest,
) (dispatcher.Result[*attunev1.UpdateStateResponse], error) {
	const where = "console.WorkflowHandler.UpdateState"
	auth := ctx.Auth

	// Guard the id before it reaches the UUID-typed column: a non-UUID would
	// otherwise surface as a Postgres 22P02 → opaque 500 (and a retried request),
	// instead of a clean 400. Forward the CANONICAL form (not the raw input) so an
	// id uuid.Parse accepts but Postgres rejects (e.g. "urn:uuid:…") can't slip
	// through. Mirrors the tag handler. Reachable by SDK callers.
	stateID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.UpdateStateResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid workflow state id")
	}

	existing, err := h.states.GetByTenantAndID(ctx, auth.TenantID, stateID.String())
	if err != nil {
		if errors.Is(err, workflowstate.ErrNotFound) {
			return dispatcher.Fail[*attunev1.UpdateStateResponse](
				http.StatusNotFound, attunev1.ErrorCode_WORKFLOW_STATE_NOT_FOUND, "state not found")
		}
		logext.Errorf(ctx, "[%s] lookup failed,id:%s,err:%+v", where, req.GetId(), err.Error())
		return dispatcher.Fail[*attunev1.UpdateStateResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "lookup failed")
	}
	beforeSnapshot := stateAuditSnapshot(ptrext.Indirect(existing))

	if req.DisplayName != nil {
		display := i18nFromProto(req.GetDisplayName())
		if !display.NonEmpty() {
			return dispatcher.Fail[*attunev1.UpdateStateResponse](
				http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "display_name must have at least one non-empty entry")
		}
		existing.DisplayName = display
	}
	// req.Name (the machine key) is immutable; ignore it on update.
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
	if err := h.recordAudit(ctx, "workflow_state.update", "workflow_state", updated.ID,
		workflowSummary("Updated workflow state", updated.DisplayName.Resolve(nil)), beforeSnapshot, stateAuditSnapshot(ptrext.Indirect(updated))); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,state_id:%s,err:%+v", where, auth.TenantID, updated.ID, err.Error())
		return dispatcher.Fail[*attunev1.UpdateStateResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(ptrext.Of(attunev1.UpdateStateResponse{State: StateToProto(ptrext.Indirect(updated))}))
}

func (h *Handler) ArchiveState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ArchiveStateRequest,
) (dispatcher.Result[*attunev1.ArchiveStateResponse], error) {
	const where = "console.WorkflowHandler.ArchiveState"
	auth := ctx.Auth
	stateID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ArchiveStateResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid workflow state id")
	}
	canonicalID := stateID.String()
	before, beforeErr := h.states.GetByTenantAndID(ctx, auth.TenantID, canonicalID)
	if beforeErr != nil && !errors.Is(beforeErr, workflowstate.ErrNotFound) {
		logext.Warnf(ctx, "[%s] prefetch failed,id:%s,err:%+v", where, req.GetId(), beforeErr.Error())
	}

	err = h.service.ArchiveState(ctx, auth.TenantID, canonicalID)
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
	var beforeSnapshot any
	summary := "Archived workflow state"
	if before != nil {
		beforeSnapshot = stateAuditSnapshot(ptrext.Indirect(before))
		summary = workflowSummary(summary, before.DisplayName.Resolve(nil))
	}
	if err := h.recordAudit(ctx, "workflow_state.archive", "workflow_state", req.GetId(), summary, beforeSnapshot, map[string]any{
		"id":       req.GetId(),
		"archived": true,
	}); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,state_id:%s,err:%+v", where, auth.TenantID, req.GetId(), err.Error())
		return dispatcher.Fail[*attunev1.ArchiveStateResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
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
	before, beforeErr := h.states.ListTransitions(ctx, auth.TenantID)
	if beforeErr != nil {
		logext.Warnf(ctx, "[%s] prefetch transitions failed,tenant_id:%s,err:%+v", where, auth.TenantID, beforeErr.Error())
	}

	// Validate every referenced state belongs to this tenant before inserting.
	// The transitions FK only checks global state-id existence, so without this a
	// caller could reference another tenant's state ids (cross-tenant). Also turns
	// an unknown-state reference into a clear 400 instead of an opaque 500.
	// includeArchived=true on purpose: the guard is "is this state owned by this
	// tenant", not "is it active". Owned-but-archived states must validate so a
	// config round-trip after archiving a referenced state doesn't 400; the
	// runtime Transition() still refuses to move a feedback item into an archived
	// state (service/workflow.go), so a dangling edge to one is inert.
	owned, ownErr := h.states.List(ctx, auth.TenantID, true)
	if ownErr != nil {
		logext.Errorf(ctx, "[%s] state prefetch failed,tenant_id:%s,err:%+v", where, auth.TenantID, ownErr.Error())
		return dispatcher.Fail[*attunev1.ReplaceTransitionsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to validate states")
	}
	valid := make(map[string]bool, len(owned))
	for _, s := range owned {
		valid[s.ID] = true
	}
	edges := make([]workflowstate.TransitionEdge, len(req.GetTransitions()))
	seenEdge := make(map[string]bool, len(req.GetTransitions()))
	for i, e := range req.GetTransitions() {
		from, to := e.GetFromStateId(), e.GetToStateId()
		if !valid[from] || !valid[to] {
			return dispatcher.Fail[*attunev1.ReplaceTransitionsResponse](
				http.StatusBadRequest, attunev1.ErrorCode_VALIDATION,
				"transition references a workflow state that does not exist in this tenant")
		}
		// The DB enforces these too (chk_wt_no_self_loop + a unique edge index);
		// catch them here so a bad payload is a clean 400, not an opaque 500.
		if from == to {
			return dispatcher.Fail[*attunev1.ReplaceTransitionsResponse](
				http.StatusBadRequest, attunev1.ErrorCode_VALIDATION,
				"a transition cannot loop a state to itself")
		}
		if seenEdge[from+"->"+to] {
			return dispatcher.Fail[*attunev1.ReplaceTransitionsResponse](
				http.StatusBadRequest, attunev1.ErrorCode_VALIDATION,
				"duplicate transition")
		}
		seenEdge[from+"->"+to] = true
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
	var beforeSnapshot any
	if beforeErr == nil {
		beforeSnapshot = transitionsAuditSnapshot(before)
	}
	if err := h.recordAudit(ctx, "workflow_transition.replace", "workflow_transition_set", auth.TenantID,
		"Replaced workflow transitions", beforeSnapshot, transitionsAuditSnapshot(transitions)); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ReplaceTransitionsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(ptrext.Of(attunev1.ReplaceTransitionsResponse{Transitions: out}))
}

func (h *Handler) SeedDefaults(
	ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.SeedDefaultsRequest,
) (dispatcher.Result[*attunev1.SeedDefaultsResponse], error) {
	const where = "console.WorkflowHandler.SeedDefaults"
	auth := ctx.Auth
	beforeStates, beforeStatesErr := h.states.List(ctx, auth.TenantID, true)
	if beforeStatesErr != nil {
		logext.Warnf(ctx, "[%s] prefetch states failed,tenant_id:%s,err:%+v", where, auth.TenantID, beforeStatesErr.Error())
	}
	beforeTransitions, beforeTransitionsErr := h.states.ListTransitions(ctx, auth.TenantID)
	if beforeTransitionsErr != nil {
		logext.Warnf(ctx, "[%s] prefetch transitions failed,tenant_id:%s,err:%+v", where, auth.TenantID, beforeTransitionsErr.Error())
	}

	if err := h.service.SeedDefaults(ctx, auth.TenantID); err != nil {
		logext.Errorf(ctx, "[%s] seed failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.SeedDefaultsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to seed defaults")
	}
	afterStates, afterStatesErr := h.states.List(ctx, auth.TenantID, true)
	if afterStatesErr != nil {
		logext.Warnf(ctx, "[%s] post-seed states failed,tenant_id:%s,err:%+v", where, auth.TenantID, afterStatesErr.Error())
	}
	afterTransitions, afterTransitionsErr := h.states.ListTransitions(ctx, auth.TenantID)
	if afterTransitionsErr != nil {
		logext.Warnf(ctx, "[%s] post-seed transitions failed,tenant_id:%s,err:%+v", where, auth.TenantID, afterTransitionsErr.Error())
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s", where, auth.TenantID)
	var beforeSnapshot any
	if beforeStatesErr == nil || beforeTransitionsErr == nil {
		snapshot := map[string]any{}
		if beforeStatesErr == nil {
			snapshot["states"] = statesAuditSnapshot(beforeStates)
		}
		if beforeTransitionsErr == nil {
			snapshot["transitions"] = transitionsAuditSnapshot(beforeTransitions)
		}
		beforeSnapshot = snapshot
	}
	var afterSnapshot any
	if afterStatesErr == nil || afterTransitionsErr == nil {
		snapshot := map[string]any{}
		if afterStatesErr == nil {
			snapshot["states"] = statesAuditSnapshot(afterStates)
		}
		if afterTransitionsErr == nil {
			snapshot["transitions"] = transitionsAuditSnapshot(afterTransitions)
		}
		afterSnapshot = snapshot
	}
	if err := h.recordAudit(ctx, "workflow_seed_defaults.run", "workflow_state_set", auth.TenantID,
		"Seeded default workflow states", beforeSnapshot, afterSnapshot); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.SeedDefaultsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(ptrext.Of(attunev1.SeedDefaultsResponse{}))
}
