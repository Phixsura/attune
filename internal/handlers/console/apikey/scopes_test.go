// SPDX-License-Identifier: Apache-2.0

package apikey

import (
	"testing"

	"github.com/Phixsura/attune/internal/domain"
)

func TestParseResourceAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scope        domain.Scope
		wantResource string
		wantAction   string
	}{
		{domain.ScopeIngestWrite, "ingest", "write"},
		{domain.ScopeFeedbackRead, "feedback", "read"},
		{domain.ScopeFeedbackWrite, "feedback", "write"},
		{domain.ScopeUsageRead, "usage", "read"},
		{domain.ScopeAuditRead, "audit", "read"},
		{domain.ScopeLLMRead, "llm", "read"},
		{domain.ScopeLLMWrite, "llm", "write"},
		{domain.ScopeAPIKeyAdmin, "apikey", "admin"},
		{domain.ScopeGDPRRead, "gdpr", "read"},
		{domain.ScopeGDPRExport, "gdpr", "export"},
		{domain.ScopeGDPRDelete, "gdpr", "delete"},
		{domain.ScopeGDPRAdmin, "gdpr", "admin"},
	}

	for _, tt := range tests {
		t.Run(string(tt.scope), func(t *testing.T) {
			resource, action := parseResourceAction(tt.scope)
			if resource != tt.wantResource {
				t.Errorf("parseResourceAction(%q) resource = %q, want %q", tt.scope, resource, tt.wantResource)
			}
			if action != tt.wantAction {
				t.Errorf("parseResourceAction(%q) action = %q, want %q", tt.scope, action, tt.wantAction)
			}
		})
	}
}

func TestParseResourceAction_NoColon(t *testing.T) {
	t.Parallel()

	resource, action := parseResourceAction(domain.Scope("nocolon"))
	if resource != "nocolon" {
		t.Errorf("resource = %q, want %q", resource, "nocolon")
	}
	if action != "" {
		t.Errorf("action = %q, want empty", action)
	}
}

func TestGetImplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scope   domain.Scope
		want    []string
		wantNil bool
	}{
		{domain.ScopeFeedbackWrite, []string{string(domain.ScopeFeedbackRead)}, false},
		{domain.ScopeLLMWrite, []string{string(domain.ScopeLLMRead)}, false},
		{domain.ScopeEnrichWrite, []string{string(domain.ScopeEnrichRead)}, false},
		{domain.ScopeGuardWrite, []string{string(domain.ScopeGuardRead)}, false},
		{domain.ScopeNotifyWrite, []string{string(domain.ScopeNotifyRead)}, false},
		{domain.ScopeInboundWrite, []string{string(domain.ScopeInboundRead)}, false},
		{domain.ScopeDigestWrite, []string{string(domain.ScopeDigestRead)}, false},
		{domain.ScopeTagsWrite, []string{string(domain.ScopeTagsRead)}, false},
		{domain.ScopeWorkflowWrite, []string{string(domain.ScopeWorkflowRead)}, false},
		{domain.ScopeGDPRExport, []string{string(domain.ScopeGDPRRead)}, false},
		{domain.ScopeGDPRDelete, []string{string(domain.ScopeGDPRRead)}, false},
		{domain.ScopeGDPRAdmin, []string{string(domain.ScopeGDPRRead), string(domain.ScopeGDPRExport), string(domain.ScopeGDPRDelete)}, false},
		{domain.ScopeMCPWrite, []string{string(domain.ScopeMCPRead)}, false},
		{domain.ScopeFeedbackRead, nil, true},
		{domain.ScopeIngestWrite, nil, true},
		{domain.ScopeAPIKeyAdmin, nil, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.scope), func(t *testing.T) {
			got := getImplies(tt.scope)
			if tt.wantNil {
				if got != nil {
					t.Errorf("getImplies(%q) = %v, want nil", tt.scope, got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("getImplies(%q) = %v, want %v", tt.scope, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("getImplies(%q)[%d] = %q, want %q", tt.scope, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestScopeDescriptions_NonEmpty(t *testing.T) {
	t.Parallel()

	if len(scopeDescriptions) == 0 {
		t.Fatal("scopeDescriptions is empty")
	}

	for scope, desc := range scopeDescriptions {
		if desc == "" {
			t.Errorf("scope %q has empty description", scope)
		}
		found := false
		for _, s := range domain.AllScopes {
			if s == scope {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("scopeDescriptions contains unknown scope %q", scope)
		}
	}
}
