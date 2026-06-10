// Package guardpolicy serves the Console guard policy management surface.
package guardpolicy

import (
	"context"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	serviceguard "github.com/Phixsura/attune/internal/service/guardpolicy"
)

type Handler struct {
	svc service
}

type service interface {
	List(ctx context.Context, tenantID string) ([]llmguard.Policy, error)
	ReplaceTenantPolicies(ctx context.Context, tenantID, actor string, policies []llmguard.Policy) ([]llmguard.Policy, error)
	CreatePolicy(ctx context.Context, tenantID, actor string, policy llmguard.Policy) (llmguard.Policy, error)
	UpdatePolicy(ctx context.Context, tenantID, actor, id string, policy llmguard.Policy) (llmguard.Policy, error)
	DeletePolicy(ctx context.Context, tenantID, id string) error
	Resolve(ctx context.Context, meta llmclient.GuardMetadata) (llmguard.Plan, error)
}

func NewHandler(svc *serviceguard.Service) *Handler {
	return ptrext.Of(Handler{svc: svc})
}

func policyToProto(p llmguard.Policy) *attunev1.GuardPolicy {
	scope := "tenant"
	var tenantID *string
	if p.TenantID == "" {
		scope = "system"
	} else {
		tenantID = ptrext.Of(p.TenantID)
	}
	return ptrext.Of(attunev1.GuardPolicy{
		Id:       p.ID,
		TenantId: tenantID,
		Name:     p.Name,
		Kind:     string(p.Kind),
		Enabled:  p.Enabled,
		Priority: int32(p.Priority),
		Target:   targetToProto(p.Target),
		Rules:    rulesToProto(p.Rules),
		Scope:    scope,
	})
}

func policiesToProto(policies []llmguard.Policy) []*attunev1.GuardPolicy {
	out := make([]*attunev1.GuardPolicy, 0, len(policies))
	for _, p := range policies {
		out = append(out, policyToProto(p))
	}
	return out
}

func policyFromProto(p *attunev1.GuardPolicy) llmguard.Policy {
	if p == nil {
		return llmguard.Policy{}
	}
	return llmguard.Policy{
		ID:       p.GetId(),
		Name:     p.GetName(),
		Kind:     llmguard.PolicyKind(p.GetKind()),
		Enabled:  p.GetEnabled(),
		Priority: int(p.GetPriority()),
		Target:   targetFromProto(p.GetTarget()),
		Rules:    rulesFromProto(p.GetRules()),
	}
}

func policiesFromProto(in []*attunev1.GuardPolicy) []llmguard.Policy {
	out := make([]llmguard.Policy, 0, len(in))
	for _, p := range in {
		if p.GetScope() == "system" {
			continue
		}
		out = append(out, policyFromProto(p))
	}
	return out
}

func targetToProto(t llmguard.Target) *attunev1.GuardTarget {
	return ptrext.Of(attunev1.GuardTarget{
		Channels:     t.Channels,
		SourceIds:    t.SourceIDs,
		SourceTags:   t.SourceTags,
		Purposes:     t.Purposes,
		Stages:       stagesToStrings(t.Stages),
		Environments: t.Environments,
	})
}

func targetFromProto(t *attunev1.GuardTarget) llmguard.Target {
	if t == nil {
		return llmguard.Target{}
	}
	return llmguard.Target{
		Channels:     t.GetChannels(),
		SourceIDs:    t.GetSourceIds(),
		SourceTags:   t.GetSourceTags(),
		Purposes:     t.GetPurposes(),
		Stages:       stringsToStages(t.GetStages()),
		Environments: t.GetEnvironments(),
	}
}

func rulesToProto(rules []llmguard.Rule) []*attunev1.GuardRule {
	out := make([]*attunev1.GuardRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, ptrext.Of(attunev1.GuardRule{
			Guard:       r.Guard,
			Stage:       string(r.Stage),
			Entities:    r.Entities,
			Action:      string(r.Action),
			Replacement: r.Replacement,
		}))
	}
	return out
}

func rulesFromProto(rules []*attunev1.GuardRule) []llmguard.Rule {
	out := make([]llmguard.Rule, 0, len(rules))
	for _, r := range rules {
		out = append(out, llmguard.Rule{
			Guard:       r.GetGuard(),
			Stage:       llmguard.Stage(r.GetStage()),
			Entities:    r.GetEntities(),
			Action:      llmguard.Action(r.GetAction()),
			Replacement: r.GetReplacement(),
		})
	}
	return out
}

func stagesToStrings(in []llmguard.Stage) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, string(s))
	}
	return out
}

func stringsToStages(in []string) []llmguard.Stage {
	out := make([]llmguard.Stage, 0, len(in))
	for _, s := range in {
		out = append(out, llmguard.Stage(s))
	}
	return out
}
