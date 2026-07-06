// SPDX-License-Identifier: Apache-2.0

package policy_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/policy"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type stubClientReader struct {
	client *oauth.Client
	err    error
}

func (s stubClientReader) GetByID(context.Context, uuid.UUID) (*oauth.Client, error) {
	return s.client, s.err
}

type stubToolPolicyReader struct {
	policy *policy.ToolPolicy
	err    error
}

func (s stubToolPolicyReader) GetByClientAndTool(context.Context, uuid.UUID, string) (*policy.ToolPolicy, error) {
	return s.policy, s.err
}

func TestEvaluatorEvaluate(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	sessionID := uuid.New()
	readTool := mustTool(t, "list_feedback")

	tests := []struct {
		name       string
		client     *oauth.Client
		scopes     []string
		override   *policy.ToolPolicy
		wantAllow  bool
		wantReason string
	}{
		{
			name: "legacy mode allows known tool without override",
			client: ptrext.Of(oauth.Client{
				ID:             clientID,
				ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
			}),
			scopes:     []string{domain.MCPScopeRead},
			wantAllow:  true,
			wantReason: policy.DecisionAllowed,
		},
		{
			name: "allow list denies tool without explicit allow",
			client: ptrext.Of(oauth.Client{
				ID:             clientID,
				ToolPolicyMode: domain.MCPToolPolicyModeAllowList,
			}),
			scopes:     []string{domain.MCPScopeRead},
			wantAllow:  false,
			wantReason: policy.DecisionDeniedPolicy,
		},
		{
			name: "allow list allows explicit allow override",
			client: ptrext.Of(oauth.Client{
				ID:             clientID,
				ToolPolicyMode: domain.MCPToolPolicyModeAllowList,
			}),
			scopes: []string{domain.MCPScopeRead},
			override: ptrext.Of(policy.ToolPolicy{
				Effect: domain.MCPToolPolicyEffectAllow,
			}),
			wantAllow:  true,
			wantReason: policy.DecisionAllowed,
		},
		{
			name: "explicit deny wins in legacy mode",
			client: ptrext.Of(oauth.Client{
				ID:             clientID,
				ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
			}),
			scopes: []string{domain.MCPScopeRead},
			override: ptrext.Of(policy.ToolPolicy{
				Effect: domain.MCPToolPolicyEffectDeny,
			}),
			wantAllow:  false,
			wantReason: policy.DecisionDeniedPolicy,
		},
		{
			name: "missing required scope denies request",
			client: ptrext.Of(oauth.Client{
				ID:             clientID,
				ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
			}),
			scopes:     []string{domain.MCPScopeIngest},
			wantAllow:  false,
			wantReason: policy.DecisionDeniedScope,
		},
		{
			name: "write scope implies read scope",
			client: ptrext.Of(oauth.Client{
				ID:             clientID,
				ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
			}),
			scopes:     []string{domain.MCPScopeWrite},
			wantAllow:  true,
			wantReason: policy.DecisionAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			evaluator := policy.NewEvaluator(
				stubClientReader{client: tt.client},
				stubToolPolicyReader{policy: tt.override},
			)

			decision, err := evaluator.Evaluate(context.Background(), ptrext.Of(oauth.AccessTokenClaims{
				TenantID:  "tenant-1",
				ClientID:  clientID,
				SessionID: sessionID,
				Scopes:    tt.scopes,
			}), readTool)
			require.NoError(t, err)
			require.Equal(t, tt.wantAllow, decision.Allowed)
			require.Equal(t, tt.wantReason, decision.Reason)
		})
	}
}

func TestEvaluatorEvaluateInvalidClient(t *testing.T) {
	t.Parallel()

	evaluator := policy.NewEvaluator(stubClientReader{err: oauth.ErrInvalidClient}, nil)
	tool := mustTool(t, "list_feedback")

	decision, err := evaluator.Evaluate(context.Background(), ptrext.Of(oauth.AccessTokenClaims{
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{domain.MCPScopeRead},
	}), tool)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, policy.DecisionDeniedClient, decision.Reason)
}

func TestEvaluatorRejectsDisabledExtensions(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	sessionID := uuid.New()
	evaluator := policy.NewEvaluator(
		stubClientReader{client: ptrext.Of(oauth.Client{
			ID:             clientID,
			ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		})},
		stubToolPolicyReader{policy: ptrext.Of(policy.ToolPolicy{
			Effect: domain.MCPToolPolicyEffectAllow,
		})},
	)

	decision, err := evaluator.Evaluate(context.Background(), ptrext.Of(oauth.AccessTokenClaims{
		ClientID:  clientID,
		SessionID: sessionID,
		Scopes:    []string{domain.MCPScopeRead},
	}), tools.ToolMeta{
		Name:             "external_example",
		Kind:             tools.ExtensionExternal,
		EnabledByDefault: false,
		RequiredScope:    domain.MCPScopeRead,
	})
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, policy.DecisionDeniedExtensionDisabled, decision.Reason)
}

func mustTool(t *testing.T, name string) tools.ToolMeta {
	t.Helper()

	tool, ok := tools.LookupTool(name)
	require.True(t, ok, "tool %s should exist in catalog", name)
	return tool
}
