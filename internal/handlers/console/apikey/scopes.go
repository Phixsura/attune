package apikey

import (
	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

var scopeDescriptions = map[domain.Scope]string{
	domain.ScopeIngestWrite:   "Submit feedback via API",
	domain.ScopeFeedbackRead:  "View feedback, clusters, and statistics",
	domain.ScopeFeedbackWrite: "Modify feedback state, tags, and batch operations",
	domain.ScopeUsageRead:     "View usage statistics",
	domain.ScopeAuditRead:     "View audit log",
	domain.ScopeLLMRead:       "View LLM channels, abilities, and routes",
	domain.ScopeLLMWrite:      "Configure LLM settings",
	domain.ScopeEnrichRead:    "View enrichment configuration",
	domain.ScopeEnrichWrite:   "Modify enrichment settings",
	domain.ScopeGuardRead:     "View guard policies",
	domain.ScopeGuardWrite:    "Configure guard policies",
	domain.ScopeNotifyRead:    "View notification targets",
	domain.ScopeNotifyWrite:   "Configure notification targets",
	domain.ScopeInboundRead:   "View inbound sources",
	domain.ScopeInboundWrite:  "Configure inbound sources",
	domain.ScopeDigestRead:    "View digest subscription",
	domain.ScopeDigestWrite:   "Configure digest subscription",
	domain.ScopeTagsRead:      "View tags",
	domain.ScopeTagsWrite:     "Configure tags",
	domain.ScopeWorkflowRead:  "View workflow states",
	domain.ScopeWorkflowWrite: "Configure workflow",
	domain.ScopeGDPRAdmin:     "GDPR delete and export operations",
	domain.ScopeMembersAdmin:  "Manage tenant members",
	domain.ScopeAPIKeyAdmin:   "Manage API keys",
}

// ListScopes returns all available scopes with metadata.
func (h *APIKeysHandler) ListScopes(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListScopesRequest) (dispatcher.Result[*attunev1.ListScopesResponse], error) {
	scopes := make([]*attunev1.ScopeInfo, 0, len(domain.AllScopes))
	for _, s := range domain.AllScopes {
		resource, action := parseResourceAction(s)
		scopes = append(scopes, ptrext.Of(attunev1.ScopeInfo{
			Scope:       string(s),
			Resource:    resource,
			Action:      action,
			Description: scopeDescriptions[s],
			Implies:     getImplies(s),
		}))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListScopesResponse{Scopes: scopes}))
}

func parseResourceAction(s domain.Scope) (resource, action string) {
	str := string(s)
	for i := 0; i < len(str); i++ {
		if str[i] == ':' {
			return str[:i], str[i+1:]
		}
	}
	return str, ""
}

func getImplies(s domain.Scope) []string {
	switch s {
	case domain.ScopeFeedbackWrite:
		return []string{string(domain.ScopeFeedbackRead)}
	case domain.ScopeLLMWrite:
		return []string{string(domain.ScopeLLMRead)}
	case domain.ScopeEnrichWrite:
		return []string{string(domain.ScopeEnrichRead)}
	case domain.ScopeGuardWrite:
		return []string{string(domain.ScopeGuardRead)}
	case domain.ScopeNotifyWrite:
		return []string{string(domain.ScopeNotifyRead)}
	case domain.ScopeInboundWrite:
		return []string{string(domain.ScopeInboundRead)}
	case domain.ScopeDigestWrite:
		return []string{string(domain.ScopeDigestRead)}
	case domain.ScopeTagsWrite:
		return []string{string(domain.ScopeTagsRead)}
	case domain.ScopeWorkflowWrite:
		return []string{string(domain.ScopeWorkflowRead)}
	default:
		return nil
	}
}
