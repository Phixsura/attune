// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	DecisionAllowed                 = "allowed"
	DecisionDeniedScope             = "denied_scope"
	DecisionDeniedExtensionDisabled = "denied_extension_disabled"
	DecisionDeniedPolicy            = "denied_policy"
	DecisionDeniedClient            = "denied_client"
	DecisionDeniedRateLimited       = "rate_limited"
)

// ToolPolicy is the per-client override relevant to runtime authorization.
type ToolPolicy struct {
	Effect         string
	RateLimitRPM   *int
	RateLimitBurst *int
}

// Decision is the normalized result of evaluating one client/tool request.
type Decision struct {
	Allowed          bool
	Reason           string
	ClientID         uuid.UUID
	SessionID        uuid.UUID
	ToolName         string
	Tool             tools.ToolMeta
	ClientPolicyMode string
	PolicyEffect     string
	ClientRateRPM    *int
	ClientRateBurst  *int
	ToolRateRPM      *int
	ToolRateBurst    *int
}

// ClientReader loads the effective runtime client configuration.
type ClientReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*oauth.Client, error)
}

// ToolPolicyReader loads a per-client per-tool override.
type ToolPolicyReader interface {
	GetByClientAndTool(ctx context.Context, clientID uuid.UUID, toolName string) (*ToolPolicy, error)
}

// Evaluator decides whether one client/session may invoke one tool.
type Evaluator struct {
	clients  ClientReader
	policies ToolPolicyReader
}

// NewEvaluator creates a new runtime policy evaluator.
func NewEvaluator(clients ClientReader, policies ToolPolicyReader) *Evaluator {
	return ptrext.Of(Evaluator{clients: clients, policies: policies})
}

// Evaluate returns the normalized authorization decision for one tool call.
func (e *Evaluator) Evaluate(ctx context.Context, claims *oauth.AccessTokenClaims, tool tools.ToolMeta) (*Decision, error) {
	if e == nil || e.clients == nil {
		return ptrext.Of(Decision{
			Allowed:   false,
			Reason:    DecisionDeniedClient,
			ClientID:  claims.ClientID,
			SessionID: claims.SessionID,
			ToolName:  tool.Name,
			Tool:      tool,
		}), nil
	}

	client, err := e.clients.GetByID(ctx, claims.ClientID)
	if err != nil || client == nil {
		return ptrext.Of(Decision{
			Allowed:   false,
			Reason:    DecisionDeniedClient,
			ClientID:  claims.ClientID,
			SessionID: claims.SessionID,
			ToolName:  tool.Name,
			Tool:      tool,
		}), nil
	}

	decision := ptrext.Of(Decision{
		Allowed:          true,
		Reason:           DecisionAllowed,
		ClientID:         claims.ClientID,
		SessionID:        claims.SessionID,
		ToolName:         tool.Name,
		Tool:             tool,
		ClientPolicyMode: normalizePolicyMode(client.ToolPolicyMode),
		ClientRateRPM:    client.RateLimitRPM,
		ClientRateBurst:  client.RateLimitBurst,
	})

	if !hasScope(claims.Scopes, tool.RequiredScope) {
		decision.Allowed = false
		decision.Reason = DecisionDeniedScope
		return decision, nil
	}

	if !tool.EnabledByDefault {
		decision.Allowed = false
		decision.Reason = DecisionDeniedExtensionDisabled
		return decision, nil
	}

	override, err := e.lookupPolicy(ctx, claims.ClientID, tool.Name)
	if err != nil {
		return nil, err
	}
	if override != nil {
		decision.PolicyEffect = override.Effect
		decision.ToolRateRPM = override.RateLimitRPM
		decision.ToolRateBurst = override.RateLimitBurst
		if override.Effect == domain.MCPToolPolicyEffectDeny {
			decision.Allowed = false
			decision.Reason = DecisionDeniedPolicy
			return decision, nil
		}
	}

	if decision.ClientPolicyMode == domain.MCPToolPolicyModeAllowList {
		if override == nil || override.Effect != domain.MCPToolPolicyEffectAllow {
			decision.Allowed = false
			decision.Reason = DecisionDeniedPolicy
			return decision, nil
		}
	}

	return decision, nil
}

func (e *Evaluator) lookupPolicy(ctx context.Context, clientID uuid.UUID, toolName string) (*ToolPolicy, error) {
	if e.policies == nil {
		return nil, nil
	}
	return e.policies.GetByClientAndTool(ctx, clientID, toolName)
}

func normalizePolicyMode(mode string) string {
	switch mode {
	case domain.MCPToolPolicyModeAllowList:
		return domain.MCPToolPolicyModeAllowList
	default:
		return domain.MCPToolPolicyModeLegacyAllowAll
	}
}

func hasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
		if scope == domain.MCPScopeWrite && required == domain.MCPScopeRead {
			return true
		}
	}
	return false
}
