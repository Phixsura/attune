package domain

import "errors"

// Scope represents a fine-grained permission on an API key.
// Format: "resource:action" (e.g., "feedback:read").
type Scope string

// Scope constants — 34 total.
const (
	// Core resources
	ScopeIngestWrite   Scope = "ingest:write"
	ScopeFeedbackRead  Scope = "feedback:read"
	ScopeFeedbackWrite Scope = "feedback:write"
	ScopeUsageRead     Scope = "usage:read"
	ScopeAuditRead     Scope = "audit:read"

	// AI configuration
	ScopeLLMRead     Scope = "llm:read"
	ScopeLLMWrite    Scope = "llm:write"
	ScopeEnrichRead  Scope = "enrich:read"
	ScopeEnrichWrite Scope = "enrich:write"
	ScopeGuardRead   Scope = "guard:read"
	ScopeGuardWrite  Scope = "guard:write"

	// Notifications & integrations
	ScopeNotifyRead   Scope = "notify:read"
	ScopeNotifyWrite  Scope = "notify:write"
	ScopeInboundRead  Scope = "inbound:read"
	ScopeInboundWrite Scope = "inbound:write"
	ScopeDigestRead   Scope = "digest:read"
	ScopeDigestWrite  Scope = "digest:write"

	// Workflow & tags
	ScopeTagsRead      Scope = "tags:read"
	ScopeTagsWrite     Scope = "tags:write"
	ScopeWorkflowRead  Scope = "workflow:read"
	ScopeWorkflowWrite Scope = "workflow:write"

	// GDPR
	ScopeGDPRRead   Scope = "gdpr:read"
	ScopeGDPRExport Scope = "gdpr:export"
	ScopeGDPRDelete Scope = "gdpr:delete"

	// Admin
	ScopeGDPRAdmin    Scope = "gdpr:admin"
	ScopeMembersAdmin Scope = "members:admin"
	ScopeAPIKeyAdmin  Scope = "apikey:admin"

	// MCP (Model Context Protocol)
	ScopeMCPRead        Scope = "mcp:read"
	ScopeMCPWrite       Scope = "mcp:write"
	ScopeMCPIngest      Scope = "mcp:ingest"
	ScopeMCPClientAdmin Scope = "mcpclient:admin"

	// Automation surface (#234) — webhook subscriptions + customer requests
	// over API-key auth. All enforced via RequireExplicitScope (no legacy
	// empty-scope grant).
	ScopeHooksManage   Scope = "hooks:manage"
	ScopeRequestsRead  Scope = "requests:read"
	ScopeRequestsWrite Scope = "requests:write"
)

// AllScopes is the complete set of valid scopes.
var AllScopes = []Scope{
	ScopeIngestWrite,
	ScopeFeedbackRead, ScopeFeedbackWrite,
	ScopeUsageRead, ScopeAuditRead,
	ScopeLLMRead, ScopeLLMWrite,
	ScopeEnrichRead, ScopeEnrichWrite,
	ScopeGuardRead, ScopeGuardWrite,
	ScopeNotifyRead, ScopeNotifyWrite,
	ScopeInboundRead, ScopeInboundWrite,
	ScopeDigestRead, ScopeDigestWrite,
	ScopeTagsRead, ScopeTagsWrite,
	ScopeWorkflowRead, ScopeWorkflowWrite,
	ScopeGDPRRead, ScopeGDPRExport, ScopeGDPRDelete, ScopeGDPRAdmin,
	ScopeMembersAdmin, ScopeAPIKeyAdmin,
	ScopeMCPRead, ScopeMCPWrite, ScopeMCPIngest, ScopeMCPClientAdmin,
	ScopeHooksManage, ScopeRequestsRead, ScopeRequestsWrite,
}

// scopeHierarchy defines implicit scope grants: write implies read.
var scopeHierarchy = map[Scope][]Scope{
	ScopeFeedbackWrite: {ScopeFeedbackRead},
	ScopeLLMWrite:      {ScopeLLMRead},
	ScopeEnrichWrite:   {ScopeEnrichRead},
	ScopeGuardWrite:    {ScopeGuardRead},
	ScopeNotifyWrite:   {ScopeNotifyRead},
	ScopeInboundWrite:  {ScopeInboundRead},
	ScopeDigestWrite:   {ScopeDigestRead},
	ScopeTagsWrite:     {ScopeTagsRead},
	ScopeWorkflowWrite: {ScopeWorkflowRead},
	ScopeGDPRExport:    {ScopeGDPRRead},
	ScopeGDPRDelete:    {ScopeGDPRRead},
	ScopeGDPRAdmin:     {ScopeGDPRRead, ScopeGDPRExport, ScopeGDPRDelete},
	ScopeMCPWrite:      {ScopeMCPRead},
	ScopeRequestsWrite: {ScopeRequestsRead},
}

// validScopes is a set for O(1) lookup.
var validScopes = func() map[Scope]struct{} {
	m := make(map[Scope]struct{}, len(AllScopes))
	for _, s := range AllScopes {
		m[s] = struct{}{}
	}
	return m
}()

// ErrInvalidScope signals an unrecognized scope string.
var ErrInvalidScope = errors.New("invalid scope")

// IsValid returns true if the scope is a known valid scope.
func (s Scope) IsValid() bool {
	_, ok := validScopes[s]
	return ok
}

// String returns the scope as a string.
func (s Scope) String() string {
	return string(s)
}

// HasScope checks if the granted scopes satisfy the required scope,
// respecting hierarchy (write implies read).
// Backward compat: empty scopes (legacy keys) grant full access.
func HasScope(granted []Scope, required Scope) bool {
	if len(granted) == 0 {
		return true // legacy key without scopes → full access
	}
	return HasExplicitScope(granted, required)
}

// HasExplicitScope checks if the granted scopes satisfy the required scope,
// respecting hierarchy (write implies read), without the legacy empty-scope
// full-access fallback. Use this for newly exposed APIs so old unscoped keys do
// not silently gain access.
func HasExplicitScope(granted []Scope, required Scope) bool {
	for _, s := range granted {
		if s == required {
			return true
		}
		for _, implied := range scopeHierarchy[s] {
			if implied == required {
				return true
			}
		}
	}
	return false
}

// ParseScope validates and returns a Scope. Returns false if invalid.
func ParseScope(s string) (Scope, bool) {
	scope := Scope(s)
	if scope.IsValid() {
		return scope, true
	}
	return "", false
}

// ParseScopes validates and returns a slice of Scopes.
// Returns an error with the first invalid scope found.
func ParseScopes(strs []string) ([]Scope, error) {
	result := make([]Scope, 0, len(strs))
	for _, s := range strs {
		scope, ok := ParseScope(s)
		if !ok {
			return nil, ErrInvalidScope
		}
		result = append(result, scope)
	}
	return result, nil
}
