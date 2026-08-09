package webhooksub

import (
	"context"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	repo "github.com/Phixsura/attune/internal/repo/webhooksub"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

// recordAudit is best-effort (same policy as the other console handlers):
// an audit write failure logs a warning but never fails the request.
func (h *Handler) recordAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	action string,
	sub repo.Subscription,
	summary string,
	before, after any,
) {
	if h.audit == nil {
		return
	}
	actorType := ctx.Auth.UserType
	if actorType == "" {
		actorType = "api_key"
	}
	actorID := ctx.Auth.UserID
	if err := h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   sub.TenantID,
		Actor:      auditlogsvc.Actor{Type: actorType, ID: actorID},
		Action:     action,
		TargetType: "webhook_subscription",
		TargetID:   sub.ID.String(),
		Summary:    summary,
		Before:     before,
		After:      after,
	}); err != nil {
		logext.Warnf(ctx, "[console.WebhookSubHandler] audit record failed,action:%s,err:%+v",
			action, err.Error())
	}
}
