package apikey

import (
	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// presetData holds non-proto preset information to avoid copying sync.Mutex.
type presetData struct {
	id          string
	name        string
	description string
	scopes      []string
}

var presetsData = []presetData{
	{
		id:          "ingest_only",
		name:        "Ingest Only",
		description: "SDK/webhook submission only",
		scopes:      []string{string(domain.ScopeIngestWrite)},
	},
	{
		id:          "read_only",
		name:        "Read Only",
		description: "View feedback, usage, and audit log",
		scopes:      []string{string(domain.ScopeFeedbackRead), string(domain.ScopeUsageRead), string(domain.ScopeAuditRead)},
	},
	{
		id:          "developer",
		name:        "Developer",
		description: "Typical SDK integration",
		scopes:      []string{string(domain.ScopeIngestWrite), string(domain.ScopeFeedbackRead), string(domain.ScopeFeedbackWrite), string(domain.ScopeUsageRead)},
	},
	{
		id:          "integration",
		name:        "Integration",
		description: "External system sync",
		scopes:      []string{string(domain.ScopeIngestWrite), string(domain.ScopeFeedbackRead), string(domain.ScopeNotifyRead), string(domain.ScopeInboundRead)},
	},
	{
		id:          "full_access",
		name:        "Full Access",
		description: "All permissions except API key management",
		scopes:      fullAccessScopes(),
	},
}

func fullAccessScopes() []string {
	scopes := make([]string, 0, len(domain.AllScopes)-3)
	for _, s := range domain.AllScopes {
		switch s {
		case domain.ScopeAPIKeyAdmin, domain.ScopeMCPClientAdmin, domain.ScopeGDPRAdmin:
			continue
		}
		scopes = append(scopes, string(s))
	}
	return scopes
}

// GetFullAccessScopes returns all scopes except the highest-risk control-plane
// admin scopes.
func GetFullAccessScopes() []domain.Scope {
	scopes := make([]domain.Scope, 0, len(domain.AllScopes)-3)
	for _, s := range domain.AllScopes {
		switch s {
		case domain.ScopeAPIKeyAdmin, domain.ScopeMCPClientAdmin, domain.ScopeGDPRAdmin:
			continue
		}
		scopes = append(scopes, s)
	}
	return scopes
}

// ListScopePresets returns all available scope preset templates.
func (h *APIKeysHandler) ListScopePresets(_ *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListScopePresetsRequest) (dispatcher.Result[*attunev1.ListScopePresetsResponse], error) {
	result := make([]*attunev1.ScopePreset, len(presetsData))
	for i, p := range presetsData {
		result[i] = ptrext.Of(attunev1.ScopePreset{
			Id:          p.id,
			Name:        p.name,
			Description: p.description,
			Scopes:      p.scopes,
		})
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListScopePresetsResponse{Presets: result}))
}
