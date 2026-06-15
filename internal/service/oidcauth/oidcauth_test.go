// SPDX-License-Identifier: Apache-2.0

package oidcauth

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestValidateState(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{})})

	tests := []struct {
		name string
		got  string
		want string
		ok   bool
	}{
		{"match", "abc123", "abc123", true},
		{"mismatch", "abc123", "xyz789", false},
		{"empty got", "", "abc123", false},
		{"empty want", "abc123", "", false},
		{"both empty", "", "", true},
		{"timing safe", "aaaa", "aaab", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.ok, svc.ValidateState(tt.got, tt.want))
		})
	}
}

func TestCheckAllowedGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		allowedGroups []string
		userGroups    []string
		ok            bool
	}{
		{"no restriction", nil, []string{"any"}, true},
		{"empty restriction", []string{}, []string{"any"}, true},
		{"user in allowed", []string{"admins", "users"}, []string{"admins"}, true},
		{"user not in allowed", []string{"admins"}, []string{"users"}, false},
		{"user has one match", []string{"admins", "devs"}, []string{"guests", "devs"}, true},
		{"empty user groups", []string{"admins"}, []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{AllowedGroups: tt.allowedGroups})})
			assert.Equal(t, tt.ok, svc.CheckAllowedGroups(tt.userGroups))
		})
	}
}

func TestMapRole(t *testing.T) {
	t.Parallel()

	cfg := ptrext.Of(config.OIDCConfig{
		RoleMapping: []config.RoleMappingEntry{
			{Role: "admin", Groups: []string{"platform-admins", "superusers"}},
			{Role: "editor", Groups: []string{"editors", "writers"}},
			{Role: "viewer", Groups: []string{"viewers"}},
		},
	})
	svc := ptrext.Of(Service{cfg: cfg})

	tests := []struct {
		name       string
		userGroups []string
		wantRole   string
	}{
		{"admin match first group", []string{"platform-admins"}, "admin"},
		{"admin match second group", []string{"superusers"}, "admin"},
		{"editor match", []string{"editors"}, "editor"},
		{"viewer match", []string{"viewers"}, "viewer"},
		{"first match wins", []string{"viewers", "platform-admins"}, "admin"},
		{"no match defaults to member", []string{"unknown"}, "member"},
		{"empty groups defaults to member", []string{}, "member"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantRole, svc.MapRole(tt.userGroups))
		})
	}
}

func TestSanitizeGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  []string
		expect int
	}{
		{"normal groups", []string{"a", "b", "c"}, 3},
		{"exceeds max count", make([]string, 150), maxGroupsCount},
		{"long group name filtered", []string{"short", string(make([]byte, 300))}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeGroups(tt.input)
			assert.Len(t, result, tt.expect)
		})
	}
}

func TestExtractStringSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		claims map[string]any
		key    string
		expect []string
	}{
		{
			"string slice",
			map[string]any{"groups": []string{"a", "b"}},
			"groups",
			[]string{"a", "b"},
		},
		{
			"any slice",
			map[string]any{"groups": []any{"a", "b", 123}},
			"groups",
			[]string{"a", "b"},
		},
		{
			"space-separated string",
			map[string]any{"groups": "admin user guest"},
			"groups",
			[]string{"admin", "user", "guest"},
		},
		{
			"missing key",
			map[string]any{},
			"groups",
			nil,
		},
		{
			"wrong type",
			map[string]any{"groups": 123},
			"groups",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractStringSlice(tt.claims, tt.key)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestProviderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		providerName string
		expect       string
	}{
		{"custom name", "Okta", "Okta"},
		{"default after ApplyDefaults", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{ProviderName: tt.providerName})})
			assert.Equal(t, tt.expect, svc.ProviderName())
		})
	}
}

func TestOIDCOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		oidcOnly bool
		expect   bool
	}{
		{"oidc only enabled", true, true},
		{"oidc only disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{OIDCOnly: tt.oidcOnly})})
			assert.Equal(t, tt.expect, svc.OIDCOnly())
		})
	}
}

func TestResolveDefaultTenant_NilResolver(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{}), tenants: nil})
	result := svc.ResolveDefaultTenant(t.Context())
	assert.Equal(t, "", result)
}
