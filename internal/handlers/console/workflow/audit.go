package workflow

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

func (h *Handler) recordAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	action, targetType, targetID, summary string,
	before, after any,
) error {
	if h.audit == nil {
		return nil
	}
	actorType := ctx.Auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	return h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   ctx.Auth.TenantID,
		Actor:      auditlogsvc.ActorFromRequest(actorType, ctx.Auth.UserID, ctx.Request()),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}

func stateAuditSnapshot(s workflowstate.WorkflowState) map[string]any {
	return map[string]any{
		"id":         s.ID,
		"name":       s.Name,
		"color":      s.Color,
		"category":   s.Category,
		"position":   s.Position,
		"is_default": s.IsDefault,
		"archived":   s.ArchivedAt != nil,
	}
}

func statesAuditSnapshot(states []workflowstate.WorkflowState) []map[string]any {
	if states == nil {
		return nil
	}
	items := make([]map[string]any, 0, len(states))
	for _, state := range states {
		items = append(items, stateAuditSnapshot(state))
	}
	return items
}

func transitionsAuditSnapshot(transitions []workflowstate.Transition) []map[string]any {
	if transitions == nil {
		return nil
	}
	items := make([]map[string]any, 0, len(transitions))
	for _, transition := range transitions {
		items = append(items, map[string]any{
			"id":            transition.ID,
			"from_state_id": transition.FromStateID,
			"to_state_id":   transition.ToStateID,
		})
	}
	return items
}

func workflowSummary(prefix, suffix string) string {
	if suffix == "" {
		return prefix
	}
	return fmt.Sprintf("%s (%s)", prefix, suffix)
}
