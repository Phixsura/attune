// SPDX-License-Identifier: Apache-2.0

package apikey

import (
	"testing"

	"github.com/Phixsura/attune/internal/domain"
)

func TestFullAccessScopes(t *testing.T) {
	t.Parallel()

	scopes := fullAccessScopes()

	if len(scopes) == 0 {
		t.Fatal("fullAccessScopes returned empty slice")
	}

	for _, s := range scopes {
		if s == string(domain.ScopeAPIKeyAdmin) {
			t.Error("fullAccessScopes should not include apikey:admin")
		}
		if s == string(domain.ScopeMCPClientAdmin) {
			t.Error("fullAccessScopes should not include mcpclient:admin")
		}
	}

	if len(scopes) != len(domain.AllScopes)-2 {
		t.Errorf("fullAccessScopes length = %d, want %d (all scopes minus privileged admin scopes)",
			len(scopes), len(domain.AllScopes)-2)
	}
}

func TestGetFullAccessScopes(t *testing.T) {
	t.Parallel()

	scopes := GetFullAccessScopes()

	if len(scopes) == 0 {
		t.Fatal("GetFullAccessScopes returned empty slice")
	}

	for _, s := range scopes {
		if s == domain.ScopeAPIKeyAdmin {
			t.Error("GetFullAccessScopes should not include apikey:admin")
		}
		if s == domain.ScopeMCPClientAdmin {
			t.Error("GetFullAccessScopes should not include mcpclient:admin")
		}
	}

	if len(scopes) != len(domain.AllScopes)-2 {
		t.Errorf("GetFullAccessScopes length = %d, want %d (all scopes minus privileged admin scopes)",
			len(scopes), len(domain.AllScopes)-2)
	}
}

func TestPresetsData(t *testing.T) {
	t.Parallel()

	if len(presetsData) == 0 {
		t.Fatal("presetsData is empty")
	}

	ids := make(map[string]bool)
	for _, p := range presetsData {
		if p.id == "" {
			t.Error("preset has empty id")
		}
		if p.name == "" {
			t.Error("preset has empty name")
		}
		if len(p.scopes) == 0 {
			t.Errorf("preset %q has no scopes", p.id)
		}
		if ids[p.id] {
			t.Errorf("duplicate preset id: %q", p.id)
		}
		ids[p.id] = true
	}
}
