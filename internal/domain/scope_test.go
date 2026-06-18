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

func TestHasScope_EmptyGranted(t *testing.T) {
	assert.False(t, HasScope(nil, ScopeFeedbackRead))
	assert.False(t, HasScope([]Scope{}, ScopeFeedbackRead))
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

func TestAllScopes_Count(t *testing.T) {
	assert.Equal(t, 24, len(AllScopes), "should have 24 scopes")
}
