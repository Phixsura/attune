// SPDX-License-Identifier: Apache-2.0

package mcpclient

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/repo/mcp"
)

func TestBuildClientToolPolicyCarriesCatalogDeprecation(t *testing.T) {
	t.Parallel()

	dto := buildClientToolPolicy(
		mcp.Client{
			Scopes:         []string{domain.MCPScopeRead},
			ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		},
		tools.ToolMeta{
			Name:             "list_feedback",
			Kind:             tools.ExtensionCore,
			Owner:            "feedback",
			EnabledByDefault: true,
			Deprecated:       true,
			Replacement:      "get_feedback",
			RequiredScope:    domain.MCPScopeRead,
			Risk:             tools.RiskRead,
			DataClass:        tools.DataUserContent,
			ReadOnlyHint:     true,
		},
		nil,
		false,
	)

	require.True(t, dto.Deprecated)
	require.Equal(t, "get_feedback", dto.Replacement)
	require.True(t, dto.ScopeGranted)
	require.True(t, dto.EffectiveAllowed)
}
