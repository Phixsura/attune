// SPDX-License-Identifier: Apache-2.0

package tools_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/tools"
)

func TestLookupToolAndListTools(t *testing.T) {
	t.Parallel()

	meta, ok := tools.LookupTool("list_feedback")
	require.True(t, ok)
	require.Equal(t, "list_feedback", meta.Name)
	require.Equal(t, domain.MCPScopeRead, meta.RequiredScope)
	require.True(t, meta.ReadOnlyHint)

	_, ok = tools.LookupTool("does_not_exist")
	require.False(t, ok)

	list := tools.ListTools()
	require.NotEmpty(t, list)
	require.Equal(t, "add_tag", list[0].Name)
	require.Equal(t, "update_workflow_state", list[len(list)-1].Name)

	for i := 1; i < len(list); i++ {
		require.LessOrEqual(t, list[i-1].Name, list[i].Name)
	}
}
