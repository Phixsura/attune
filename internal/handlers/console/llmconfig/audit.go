package llmconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
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

func (h *Handler) findAbility(ctx context.Context, channelID uuid.UUID, logicalModel string) *llmrepo.Ability {
	items, err := h.svc.ListAbilities(ctx, channelID)
	if err != nil {
		return nil
	}
	for _, item := range items {
		if item.LogicalModel == logicalModel {
			found := item
			return ptrext.Of(found)
		}
	}
	return nil
}

func (h *Handler) findRoute(ctx context.Context, tenantID, purpose string) *llmrepo.Route {
	items, err := h.svc.ListRoutes(ctx)
	if err != nil {
		return nil
	}
	for _, item := range items {
		if item.TenantID == tenantID && item.Purpose == purpose {
			found := item
			return ptrext.Of(found)
		}
	}
	return nil
}

func channelAuditSnapshot(row llmrepo.Channel) map[string]any {
	return map[string]any{
		"id":                  row.ID.String(),
		"name":                row.Name,
		"protocol":            row.Protocol,
		"base_url":            row.BaseURL,
		"auth_mode":           row.AuthMode,
		"has_api_key":         len(row.CredentialCiphertext) > 0,
		"credential_key_id":   row.CredentialKeyID,
		"status":              row.Status,
		"priority":            row.Priority,
		"weight":              row.Weight,
		"timeout_seconds":     row.TimeoutSeconds,
		"last_test_status":    row.LastTestStatus,
		"last_tested_present": row.LastTestedAt != nil,
	}
}

func abilityAuditSnapshot(row llmrepo.Ability) map[string]any {
	return map[string]any{
		"id":             row.ID.String(),
		"channel_id":     row.ChannelID.String(),
		"logical_model":  row.LogicalModel,
		"provider_model": row.ProviderModel,
		"enabled":        row.Enabled,
		"priority":       row.Priority,
		"weight":         row.Weight,
	}
}

func routeAuditSnapshot(row llmrepo.Route) map[string]any {
	return map[string]any{
		"id":            row.ID.String(),
		"tenant_id":     row.TenantID,
		"purpose":       row.Purpose,
		"logical_model": row.LogicalModel,
		"enabled":       row.Enabled,
	}
}

func llmTargetID(id string, fallback ...string) string {
	id = strings.TrimSpace(id)
	if id != "" {
		return id
	}
	return strings.Join(fallback, ":")
}

func llmChannelSummary(prefix string, row llmrepo.Channel) string {
	if row.Name == "" {
		return prefix
	}
	return fmt.Sprintf("%s (%s)", prefix, row.Name)
}

func llmAbilitySummary(prefix string, row llmrepo.Ability) string {
	if row.LogicalModel == "" {
		return prefix
	}
	return fmt.Sprintf("%s (%s)", prefix, row.LogicalModel)
}

func llmRouteSummary(prefix string, row llmrepo.Route) string {
	if row.Purpose == "" {
		return prefix
	}
	return fmt.Sprintf("%s (%s)", prefix, row.Purpose)
}

func channelTestAuditSnapshot(id, providerModel string, ok bool, err error) map[string]any {
	out := map[string]any{
		"channel_id":     id,
		"provider_model": providerModel,
		"ok":             ok,
		"failure_reason": "",
	}
	if ok || err == nil {
		delete(out, "failure_reason")
		return out
	}
	switch {
	case errors.Is(err, llmrepo.ErrChannelNotFound):
		out["failure_reason"] = "not_found"
	case isValidation(err):
		out["failure_reason"] = "validation_failed"
	default:
		out["failure_reason"] = "provider_failed"
	}
	return out
}
