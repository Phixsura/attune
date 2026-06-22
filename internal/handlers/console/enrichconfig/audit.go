package enrichconfig

import (
	"context"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	"github.com/Phixsura/attune/internal/service/enrich"
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
		TargetType: "enrich_config",
		TargetID:   ctx.Auth.TenantID,
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}

func enrichConfigSnapshot(v enrich.View) map[string]any {
	return map[string]any{
		"prompt_template": v.PromptTemplate,
		"dimensions":      cloneDimensionSet(v.Dimensions),
		"policy_config":   v.PolicyConfig,
		"prompt_policy": map[string]any{
			"prompt_version": v.PromptPolicy.PromptVersion,
			"policy_id":      v.PromptPolicy.PolicyID,
			"policy_version": v.PromptPolicy.PolicyVersion,
			"mode":           v.PromptPolicy.Mode,
			"prompt_source":  v.PromptPolicy.PromptSource,
		},
	}
}

func cloneDimensionSet(in domain.DimensionSet) domain.DimensionSet {
	if len(in) == 0 {
		return nil
	}
	out := make(domain.DimensionSet, len(in))
	for i, d := range in {
		out[i] = d
		out[i].Taxonomy = append([]domain.Taxonomy(nil), d.Taxonomy...)
		out[i].UrgentSet = append([]string(nil), d.UrgentSet...)
		out[i].Examples = append([]string(nil), d.Examples...)
		if len(d.Renderer.Values) > 0 {
			out[i].Renderer.Values = make(map[string]domain.RendererValue, len(d.Renderer.Values))
			for k, v := range d.Renderer.Values {
				out[i].Renderer.Values[k] = v
			}
		}
	}
	return out
}
