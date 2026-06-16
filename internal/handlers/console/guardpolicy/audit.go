package guardpolicy

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
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

func (h *Handler) policyByID(ctx *dispatcher.RequestContext[*session.AuthCtx], tenantID, policyID string) *llmguard.Policy {
	items, err := h.svc.List(ctx, tenantID)
	if err != nil {
		logext.Warnf(ctx, "[guardpolicy.policyByID] list failed,tenant_id:%s,policy_id:%s,err:%+v",
			tenantID, policyID, err.Error())
		return nil
	}
	for _, item := range items {
		if item.ID == policyID {
			found := item
			return ptrext.Of(found)
		}
	}
	return nil
}

func auditPolicySnapshot(policy llmguard.Policy) map[string]any {
	return map[string]any{
		"id":        policy.ID,
		"tenant_id": policy.TenantID,
		"name":      policy.Name,
		"kind":      string(policy.Kind),
		"enabled":   policy.Enabled,
		"priority":  policy.Priority,
		"target": map[string]any{
			"channels":     policy.Target.Channels,
			"source_ids":   policy.Target.SourceIDs,
			"source_tags":  policy.Target.SourceTags,
			"purposes":     policy.Target.Purposes,
			"stages":       stagesToStrings(policy.Target.Stages),
			"environments": policy.Target.Environments,
		},
		"rules": rulesToAudit(policy.Rules),
	}
}

func auditPoliciesSnapshot(policies []llmguard.Policy) []map[string]any {
	if policies == nil {
		return nil
	}
	items := make([]map[string]any, 0, len(policies))
	for _, policy := range policies {
		items = append(items, auditPolicySnapshot(policy))
	}
	return items
}

func rulesToAudit(rules []llmguard.Rule) []map[string]any {
	items := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		items = append(items, map[string]any{
			"guard":       rule.Guard,
			"stage":       string(rule.Stage),
			"entities":    rule.Entities,
			"action":      string(rule.Action),
			"replacement": rule.Replacement,
		})
	}
	return items
}

func guardPolicySummary(action string, policy llmguard.Policy) string {
	if policy.Name == "" {
		return action
	}
	return fmt.Sprintf("%s (%s)", action, policy.Name)
}
