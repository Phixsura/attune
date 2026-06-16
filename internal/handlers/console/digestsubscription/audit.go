package digestsubscription

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
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
	action, summary string,
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
		TargetType: "digest_subscription",
		TargetID:   ctx.Auth.TenantID,
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}

func digestSubscriptionSummary(action string, sub digestSubscriptionSnapshot) string {
	if sub.Frequency == "" {
		return action
	}
	return fmt.Sprintf("%s (%s @ %02d:00)", action, sub.Frequency, sub.SendHour)
}

type digestSubscriptionSnapshot struct {
	Enabled           bool    `json:"enabled"`
	Frequency         string  `json:"frequency"`
	SendHour          int     `json:"send_hour"`
	Byweekday         *int    `json:"byweekday,omitempty"`
	Timezone          *string `json:"timezone,omitempty"`
	LLMMinFeedback    int     `json:"llm_min_feedback"`
	SendOnEmpty       bool    `json:"send_on_empty"`
	ThemePrompt       *string `json:"theme_prompt,omitempty"`
	ClusteringEnabled bool    `json:"clustering_enabled"`
}

func toDigestSubscriptionSnapshot(sub digestsubrepo.Subscription, clusteringEnabled bool) digestSubscriptionSnapshot {
	return digestSubscriptionSnapshot{
		Enabled:           sub.Enabled,
		Frequency:         sub.Frequency,
		SendHour:          sub.SendHour,
		Byweekday:         sub.Byweekday,
		Timezone:          sub.Timezone,
		LLMMinFeedback:    sub.LLMMinFeedback,
		SendOnEmpty:       sub.SendOnEmpty,
		ThemePrompt:       sub.ThemePrompt,
		ClusteringEnabled: clusteringEnabled,
	}
}
