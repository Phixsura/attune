// SPDX-License-Identifier: Apache-2.0

package feedbackjob

import (
	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func (h *Handler) recordAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	action, targetID, summary string,
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
		TargetType: "feedback_job",
		TargetID:   targetID,
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}
