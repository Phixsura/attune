package tag

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
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
	action string,
	tag feedbacktag.Tag,
	summary string,
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
		TargetType: "tag",
		TargetID:   tag.ID.String(),
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}

func tagAuditSnapshot(tag feedbacktag.Tag) map[string]any {
	return map[string]any{
		"id":              tag.ID.String(),
		"name":            tag.Name,
		"color":           tag.Color,
		"description":     tag.Description,
		"exclusive_scope": tag.ExclusiveScope,
		"usage_count":     tag.UsageCount,
		"archived":        tag.ArchivedAt != nil,
		"created_by":      tag.CreatedBy,
	}
}

func tagAuditSummary(action string, tag feedbacktag.Tag) string {
	if tag.Name == "" {
		return action
	}
	return fmt.Sprintf("%s (%s)", action, tag.Name)
}
