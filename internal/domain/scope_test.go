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

func TestHasExplicitScope_Hierarchy(t *testing.T) {
	granted := []Scope{ScopeFeedbackWrite}

	assert.True(t, HasExplicitScope(granted, ScopeFeedbackWrite))
	assert.True(t, HasExplicitScope(granted, ScopeFeedbackRead), "write should imply read")
	assert.False(t, HasExplicitScope(granted, ScopeLLMRead), "should not imply unrelated scope")
}

func TestHasScope_EmptyGranted_LegacyFullAccess(t *testing.T) {
	// Backward compat: legacy keys without scopes have full access
	assert.True(t, HasScope(nil, ScopeFeedbackRead), "nil scopes = legacy full access")
	assert.True(t, HasScope([]Scope{}, ScopeFeedbackRead), "empty scopes = legacy full access")
	assert.True(t, HasScope(nil, ScopeAPIKeyAdmin), "legacy keys can even access admin scopes")
}

func TestHasExplicitScope_EmptyGrantedDenied(t *testing.T) {
	assert.False(t, HasExplicitScope(nil, ScopeFeedbackRead), "nil scopes must not satisfy explicit scope checks")
	assert.False(t, HasExplicitScope([]Scope{}, ScopeFeedbackRead), "empty scopes must not satisfy explicit scope checks")
	assert.False(t, HasExplicitScope(nil, ScopeAPIKeyAdmin), "legacy unscoped keys must not satisfy admin explicit scopes")
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
	assert.Equal(t, 34, len(AllScopes), "should have 34 scopes")
}

func TestMCPScopes(t *testing.T) {
	t.Run("MCP scopes are valid", func(t *testing.T) {
		assert.True(t, ScopeMCPRead.IsValid())
		assert.True(t, ScopeMCPWrite.IsValid())
		assert.True(t, ScopeMCPIngest.IsValid())
		assert.True(t, ScopeMCPClientAdmin.IsValid())
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

	t.Run("MCP runtime scopes do not imply governance", func(t *testing.T) {
		granted := []Scope{ScopeMCPWrite}
		assert.False(t, HasScope(granted, ScopeMCPClientAdmin))
		assert.False(t, HasExplicitScope(granted, ScopeMCPClientAdmin))
	})
}

func TestGDPRScopes(t *testing.T) {
	t.Run("granular GDPR scopes are valid", func(t *testing.T) {
		assert.True(t, ScopeGDPRRead.IsValid())
		assert.True(t, ScopeGDPRExport.IsValid())
		assert.True(t, ScopeGDPRDelete.IsValid())
		assert.True(t, ScopeGDPRAdmin.IsValid())
	})

	t.Run("gdpr export implies read", func(t *testing.T) {
		granted := []Scope{ScopeGDPRExport}
		assert.True(t, HasScope(granted, ScopeGDPRExport))
		assert.True(t, HasScope(granted, ScopeGDPRRead))
		assert.False(t, HasScope(granted, ScopeGDPRDelete))
	})

	t.Run("gdpr delete implies read", func(t *testing.T) {
		granted := []Scope{ScopeGDPRDelete}
		assert.True(t, HasScope(granted, ScopeGDPRDelete))
		assert.True(t, HasScope(granted, ScopeGDPRRead))
		assert.False(t, HasScope(granted, ScopeGDPRExport))
	})

	t.Run("gdpr admin implies granular scopes", func(t *testing.T) {
		granted := []Scope{ScopeGDPRAdmin}
		assert.True(t, HasScope(granted, ScopeGDPRRead))
		assert.True(t, HasScope(granted, ScopeGDPRExport))
		assert.True(t, HasScope(granted, ScopeGDPRDelete))
	})
}

func TestAutomationScopes(t *testing.T) {
	for _, s := range []Scope{ScopeHooksManage, ScopeRequestsRead, ScopeRequestsWrite} {
		assert.True(t, s.IsValid(), "%s should be valid", s)
	}
	assert.True(t, HasExplicitScope([]Scope{ScopeRequestsWrite}, ScopeRequestsRead),
		"requests:write must imply requests:read")
	assert.False(t, HasExplicitScope([]Scope{}, ScopeHooksManage),
		"legacy empty-scope keys must NOT get hooks:manage")
	assert.False(t, HasExplicitScope([]Scope{ScopeNotifyWrite}, ScopeHooksManage),
		"notify:write must not imply hooks:manage")
}

func TestAutomationEventVocabulary(t *testing.T) {
	for _, e := range AutomationEvents {
		assert.True(t, IsAutomationEvent(e), "%s must be subscribable", e)
	}
	assert.False(t, IsAutomationEvent(EventFeedbackEnriched), "legacy event is not subscribable")
	assert.False(t, IsAutomationEvent("feedback.deleted"))
	assert.False(t, IsAutomationEvent(""))
	assert.Len(t, AutomationEvents, 4, "append-only vocabulary — additions must update tests deliberately")
}
