package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScope_IsValid(t *testing.T) {
	assert.True(t, ScopeIngestWrite.IsValid())
	assert.True(t, ScopeFeedbackRead.IsValid())
	assert.False(t, Scope("invalid:scope").IsValid())
	assert.False(t, Scope("").IsValid())
}

func TestHasScope_Direct(t *testing.T) {
	granted := []Scope{ScopeFeedbackRead, ScopeUsageRead}

	assert.True(t, HasScope(granted, ScopeFeedbackRead))
	assert.True(t, HasScope(granted, ScopeUsageRead))
	assert.False(t, HasScope(granted, ScopeFeedbackWrite))
	assert.False(t, HasScope(granted, ScopeIngestWrite))
}

func TestHasScope_Hierarchy(t *testing.T) {
	granted := []Scope{ScopeFeedbackWrite}

	assert.True(t, HasScope(granted, ScopeFeedbackWrite))
	assert.True(t, HasScope(granted, ScopeFeedbackRead), "write should imply read")
	assert.False(t, HasScope(granted, ScopeLLMRead), "should not imply unrelated scope")
}

func TestHasScope_EmptyGranted_LegacyFullAccess(t *testing.T) {
	// Backward compat: legacy keys without scopes have full access
	assert.True(t, HasScope(nil, ScopeFeedbackRead), "nil scopes = legacy full access")
	assert.True(t, HasScope([]Scope{}, ScopeFeedbackRead), "empty scopes = legacy full access")
	assert.True(t, HasScope(nil, ScopeAPIKeyAdmin), "legacy keys can even access admin scopes")
}

func TestParseScope(t *testing.T) {
	scope, ok := ParseScope("feedback:read")
	assert.True(t, ok)
	assert.Equal(t, ScopeFeedbackRead, scope)

	_, ok = ParseScope("invalid")
	assert.False(t, ok)
}

func TestParseScopes(t *testing.T) {
	scopes, err := ParseScopes([]string{"feedback:read", "ingest:write"})
	assert.NoError(t, err)
	assert.Equal(t, []Scope{ScopeFeedbackRead, ScopeIngestWrite}, scopes)

	_, err = ParseScopes([]string{"feedback:read", "invalid"})
	assert.ErrorIs(t, err, ErrInvalidScope)
}

func TestScope_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "feedback:read", ScopeFeedbackRead.String())
	assert.Equal(t, "ingest:write", ScopeIngestWrite.String())
}

func TestAllScopes_Count(t *testing.T) {
	assert.Equal(t, 27, len(AllScopes), "should have 27 scopes")
}

func TestMCPScopes(t *testing.T) {
	t.Run("MCP scopes are valid", func(t *testing.T) {
		assert.True(t, ScopeMCPRead.IsValid())
		assert.True(t, ScopeMCPWrite.IsValid())
		assert.True(t, ScopeMCPIngest.IsValid())
	})

	t.Run("MCP write implies read", func(t *testing.T) {
		granted := []Scope{ScopeMCPWrite}
		assert.True(t, HasScope(granted, ScopeMCPRead))
		assert.True(t, HasScope(granted, ScopeMCPWrite))
	})

	t.Run("MCP ingest does not imply read", func(t *testing.T) {
		granted := []Scope{ScopeMCPIngest}
		assert.False(t, HasScope(granted, ScopeMCPRead))
		assert.True(t, HasScope(granted, ScopeMCPIngest))
	})

	t.Run("MCP read does not imply write or ingest", func(t *testing.T) {
		granted := []Scope{ScopeMCPRead}
		assert.True(t, HasScope(granted, ScopeMCPRead))
		assert.False(t, HasScope(granted, ScopeMCPWrite))
		assert.False(t, HasScope(granted, ScopeMCPIngest))
	})
}
