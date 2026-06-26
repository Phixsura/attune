// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package guardpolicy

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	serviceguard "github.com/Phixsura/attune/internal/service/guardpolicy"
)

func TestGuardPolicySummary_EmptyName(t *testing.T) {
	t.Parallel()
	got := guardPolicySummary("created", llmguard.Policy{})
	assert.Equal(t, "created", got)
}

func TestGuardPolicySummary_WithName(t *testing.T) {
	t.Parallel()
	got := guardPolicySummary("updated", llmguard.Policy{Name: "pii-filter"})
	assert.Equal(t, "updated (pii-filter)", got)
}

func TestParsePolicyID_Valid(t *testing.T) {
	t.Parallel()
	id, err := parsePolicyID("550e8400-e29b-41d4-a716-446655440000")
	require.NoError(t, err)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", id)
}

func TestParsePolicyID_Invalid(t *testing.T) {
	t.Parallel()
	_, err := parsePolicyID("not-a-uuid")
	require.Error(t, err)
}

func TestStagesToStrings_Empty(t *testing.T) {
	t.Parallel()
	got := stagesToStrings(nil)
	assert.Empty(t, got)
}

func TestStagesToStrings_Multiple(t *testing.T) {
	t.Parallel()
	got := stagesToStrings([]llmguard.Stage{"pre", "post"})
	assert.Equal(t, []string{"pre", "post"}, got)
}

func TestStringsToStages_Empty(t *testing.T) {
	t.Parallel()
	got := stringsToStages(nil)
	assert.Empty(t, got)
}

func TestStringsToStages_Multiple(t *testing.T) {
	t.Parallel()
	got := stringsToStages([]string{"pre", "post"})
	assert.Equal(t, []llmguard.Stage{"pre", "post"}, got)
}

func TestGuardPolicyWriteError_ValidationCode(t *testing.T) {
	t.Parallel()

	ctx := &dispatcher.RequestContext[*session.AuthCtx]{
		Context: t.Context(),
	}

	_, err := guardPolicyWriteError(ctx, "test", "t1", serviceguard.ErrPolicyNameRequired)
	require.Error(t, err)
}

func TestGuardPolicyWriteError_NotFound(t *testing.T) {
	t.Parallel()

	ctx := &dispatcher.RequestContext[*session.AuthCtx]{
		Context: t.Context(),
	}

	_, err := guardPolicyWriteError(ctx, "test", "t1", llmguard.ErrPolicyNotFound)
	require.Error(t, err)
}

func TestGuardPolicyWriteError_Internal(t *testing.T) {
	t.Parallel()

	ctx := &dispatcher.RequestContext[*session.AuthCtx]{
		Context: t.Context(),
	}

	_, err := guardPolicyWriteError(ctx, "test", "t1", errors.New("unexpected"))
	require.Error(t, err)
}
