package feedback

import (
	"errors"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/service/workflow"

	workflowconsole "github.com/Phixsura/attune/internal/handlers/console/workflow"
)

// workflowStateToProto delegates to the workflow handler's single source of
// the state→proto mapping (CLAUDE.md §6: don't keep a second copy).
func workflowStateToProto(s workflowstate.WorkflowState) *attunev1.WorkflowState {
	return workflowconsole.StateToProto(s)
}

func (h *FeedbackHandler) TransitionState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.TransitionFeedbackRequest,
) (dispatcher.Result[*attunev1.TransitionFeedbackResponse], error) {
	const where = "console.FeedbackHandler.TransitionState"
	auth := ctx.Auth

	if h.workflow == nil {
		return dispatcher.Fail[*attunev1.TransitionFeedbackResponse](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "workflow not configured",
		)
	}

	result, err := h.workflow.Transition(ctx, auth.TenantID, req.GetFeedbackId(),
		req.GetToStateId(), auth.UserID, req.GetComment())
	if err != nil {
		switch {
		case errors.Is(err, workflow.ErrInvalidTransition):
			return dispatcher.Fail[*attunev1.TransitionFeedbackResponse](
				http.StatusConflict, attunev1.ErrorCode_INVALID_TRANSITION, "transition not allowed",
			)
		case errors.Is(err, workflow.ErrStateNotFound):
			return dispatcher.Fail[*attunev1.TransitionFeedbackResponse](
				http.StatusNotFound, attunev1.ErrorCode_WORKFLOW_STATE_NOT_FOUND, "state not found",
			)
		case errors.Is(err, workflow.ErrNoWorkflowState):
			return dispatcher.Fail[*attunev1.TransitionFeedbackResponse](
				http.StatusConflict, attunev1.ErrorCode_NO_WORKFLOW_STATE, "feedback has no workflow state",
			)
		default:
			logext.Errorf(ctx, "[%s] transition failed,feedback_id:%d,err:%+v",
				where, req.GetFeedbackId(), err.Error())
			return dispatcher.Fail[*attunev1.TransitionFeedbackResponse](
				http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "transition failed",
			)
		}
	}

	var fromState, toState *attunev1.WorkflowState
	if result.FromState != nil {
		fromState = workflowStateToProto(ptrext.Indirect(result.FromState))
	}
	if result.ToState != nil {
		toState = workflowStateToProto(ptrext.Indirect(result.ToState))
	}
	return dispatcher.OK(ptrext.Of(attunev1.TransitionFeedbackResponse{
		FeedbackId: result.FeedbackID,
		FromState:  fromState,
		ToState:    toState,
	}))
}

func (h *FeedbackHandler) BatchTransitionState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.BatchTransitionFeedbackRequest,
) (dispatcher.Result[*attunev1.BatchTransitionFeedbackResponse], error) {
	const where = "console.FeedbackHandler.BatchTransitionState"
	auth := ctx.Auth

	if h.workflow == nil {
		return dispatcher.Fail[*attunev1.BatchTransitionFeedbackResponse](
			http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "workflow not configured",
		)
	}

	if len(req.GetFeedbackIds()) == 0 {
		return dispatcher.Fail[*attunev1.BatchTransitionFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "feedback_ids required",
		)
	}
	if len(req.GetFeedbackIds()) > 100 {
		return dispatcher.Fail[*attunev1.BatchTransitionFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "max 100 feedback_ids per batch",
		)
	}

	result, err := h.workflow.BatchTransition(ctx, auth.TenantID, req.GetFeedbackIds(),
		req.GetToStateId(), auth.UserID, req.GetComment())
	if err != nil {
		logext.Errorf(ctx, "[%s] batch failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.BatchTransitionFeedbackResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "batch transition failed",
		)
	}

	failures := make([]*attunev1.BatchTransitionFailure, len(result.Failed))
	for i, f := range result.Failed {
		failures[i] = ptrext.Of(attunev1.BatchTransitionFailure{
			FeedbackId: f.FeedbackID,
			Code:       f.Code,
			Message:    f.Message,
		})
	}

	return dispatcher.OK(ptrext.Of(attunev1.BatchTransitionFeedbackResponse{
		Succeeded: int32(result.Succeeded),
		Failed:    failures,
	}))
}

func (h *FeedbackHandler) ListAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListAuditRequest,
) (dispatcher.Result[*attunev1.ListAuditResponse], error) {
	const where = "console.FeedbackHandler.ListAudit"

	if h.auditReader == nil {
		return dispatcher.OK(ptrext.Of(attunev1.ListAuditResponse{}))
	}

	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}

	auth := ctx.Auth
	entries, nextCursor, err := h.auditReader.List(ctx, auth.TenantID, req.GetFeedbackId(), req.GetCursor(), limit)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,feedback_id:%d,err:%+v",
			where, req.GetFeedbackId(), err.Error())
		return dispatcher.Fail[*attunev1.ListAuditResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list audit",
		)
	}

	out := make([]*attunev1.AuditEntry, len(entries))
	for i, e := range entries {
		out[i] = ptrext.Of(attunev1.AuditEntry{
			Id:         e.ID,
			FeedbackId: e.FeedbackID,
			EntityType: e.EntityType,
			FieldName:  e.FieldName,
			OldValue:   ptrext.IndirectOr(e.OldValue, ""),
			NewValue:   ptrext.IndirectOr(e.NewValue, ""),
			Comment:    e.Comment,
			ChangedBy:  e.ChangedBy,
			CreatedAt:  e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	var cursor *string
	if nextCursor != "" {
		cursor = ptrext.Of(nextCursor)
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListAuditResponse{
		Entries:    out,
		NextCursor: cursor,
	}))
}

func (h *FeedbackHandler) enrichItemsWithWorkflowState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], where, tenantID string,
	rows []feedback.ConsoleListRow, items []*attunev1.Feedback,
) {
	enrichFeedbackItemsWithWorkflowState(ctx, where, tenantID, rows, items, h.workflowStates)
}

func enrichFeedbackItemsWithWorkflowState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], where, tenantID string,
	rows []feedback.ConsoleListRow, items []*attunev1.Feedback, reader workflowStateReader,
) {
	if reader == nil || len(rows) == 0 {
		return
	}
	hasAny := false
	for _, row := range rows {
		if row.WorkflowStateID != nil {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return
	}

	states, err := reader.List(ctx, tenantID, false)
	if err != nil {
		logext.Warnf(ctx, "[%s] workflow state load failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return
	}
	stateMap := make(map[string]workflowstate.WorkflowState, len(states))
	for _, s := range states {
		stateMap[s.ID] = s
	}

	transitions, err := reader.ListTransitions(ctx, tenantID)
	if err != nil {
		logext.Warnf(ctx, "[%s] workflow transitions load failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return
	}
	adjacency := make(map[string][]string)
	for _, t := range transitions {
		adjacency[t.FromStateID] = append(adjacency[t.FromStateID], t.ToStateID)
	}

	for i, row := range rows {
		if row.WorkflowStateID == nil {
			continue
		}
		stateID := ptrext.Indirect(row.WorkflowStateID)
		if s, ok := stateMap[stateID]; ok {
			items[i].WorkflowState = workflowStateToProto(s)
		}
		for _, nid := range adjacency[stateID] {
			if ns, ok := stateMap[nid]; ok {
				items[i].AllowedNextStates = append(items[i].AllowedNextStates, workflowStateToProto(ns))
			}
		}
	}
}

func (h *FeedbackHandler) enrichDetailWithWorkflowState(
	ctx *dispatcher.RequestContext[*session.AuthCtx], where, tenantID string,
	stateID *string, detail *attunev1.FeedbackDetail,
) {
	if h.workflowStates == nil || stateID == nil {
		return
	}
	sid := ptrext.Indirect(stateID)
	allowed, err := h.workflowStates.AllowedNext(ctx, tenantID, sid)
	if err != nil {
		logext.Warnf(ctx, "[%s] allowed-next load failed,tenant_id:%s,state_id:%s,err:%+v",
			where, tenantID, sid, err.Error())
	}

	states, err := h.workflowStates.List(ctx, tenantID, false)
	if err != nil {
		logext.Warnf(ctx, "[%s] workflow state load failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return
	}
	for _, s := range states {
		if s.ID == sid {
			detail.WorkflowState = workflowStateToProto(s)
			break
		}
	}
	for _, ns := range allowed {
		detail.AllowedNextStates = append(detail.AllowedNextStates, workflowStateToProto(ns))
	}
}
