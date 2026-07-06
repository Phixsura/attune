// SPDX-License-Identifier: Apache-2.0

package console

import (
	"testing"

	"github.com/stretchr/testify/require"

	consolemcpclient "github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestMCPToolPolicyListProtoCarriesKind(t *testing.T) {
	t.Parallel()

	resp := mcpToolPolicyListProto([]consolemcpclient.ClientToolPolicy{
		{
			Name:             "list_feedback",
			Kind:             "core",
			Owner:            "feedback",
			EnabledByDefault: true,
			Deprecated:       true,
			Replacement:      "get_feedback",
			Aliases: []consolemcpclient.ClientToolAlias{{
				Name:        "feedback.list",
				Deprecated:  true,
				Replacement: "list_feedback",
			}},
			RequiredScope: "mcp:read",
			Risk:          "read",
			DataClass:     "user_content",
		},
	})

	require.Len(t, resp, 1)
	require.Equal(t, ptrext.Of(attunev1.MCPClientToolPolicy{
		Name:             "list_feedback",
		Kind:             "core",
		Owner:            "feedback",
		EnabledByDefault: true,
		Deprecated:       true,
		Replacement:      "get_feedback",
		Aliases: []*attunev1.MCPClientToolAlias{{
			Name:        "feedback.list",
			Deprecated:  true,
			Replacement: "list_feedback",
		}},
		RequiredScope: "mcp:read",
		Risk:          "read",
		DataClass:     "user_content",
	}), resp[0])
}
