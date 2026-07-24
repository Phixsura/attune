// SPDX-License-Identifier: Apache-2.0

package tenantmember

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/domain"
)

func TestRoleHierarchy(t *testing.T) {
	tests := []struct {
		name     string
		role     domain.Role
		min      domain.Role
		expected bool
	}{
		{"admin >= admin", domain.RoleAdmin, domain.RoleAdmin, true},
		{"admin >= delegated admin", domain.RoleAdmin, domain.RoleDelegatedAdmin, true},
		{"admin >= member", domain.RoleAdmin, domain.RoleMember, true},
		{"admin >= viewer", domain.RoleAdmin, domain.RoleViewer, true},
		{"delegated admin >= delegated admin", domain.RoleDelegatedAdmin, domain.RoleDelegatedAdmin, true},
		{"delegated admin >= member", domain.RoleDelegatedAdmin, domain.RoleMember, true},
		{"delegated admin >= viewer", domain.RoleDelegatedAdmin, domain.RoleViewer, true},
		{"delegated admin < admin", domain.RoleDelegatedAdmin, domain.RoleAdmin, false},
		{"member >= member", domain.RoleMember, domain.RoleMember, true},
		{"member >= viewer", domain.RoleMember, domain.RoleViewer, true},
		{"member < delegated admin", domain.RoleMember, domain.RoleDelegatedAdmin, false},
		{"member < admin", domain.RoleMember, domain.RoleAdmin, false},
		{"viewer >= viewer", domain.RoleViewer, domain.RoleViewer, true},
		{"viewer < member", domain.RoleViewer, domain.RoleMember, false},
		{"viewer < delegated admin", domain.RoleViewer, domain.RoleDelegatedAdmin, false},
		{"viewer < admin", domain.RoleViewer, domain.RoleAdmin, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.role.AtLeast(tt.min))
		})
	}
}

func TestRoleCanManage(t *testing.T) {
	tests := []struct {
		name     string
		role     domain.Role
		target   domain.Role
		expected bool
	}{
		{"admin can manage member", domain.RoleAdmin, domain.RoleMember, true},
		{"admin can manage viewer", domain.RoleAdmin, domain.RoleViewer, true},
		{"admin can manage delegated admin", domain.RoleAdmin, domain.RoleDelegatedAdmin, true},
		{"admin cannot manage admin", domain.RoleAdmin, domain.RoleAdmin, false},
		{"delegated admin can manage member", domain.RoleDelegatedAdmin, domain.RoleMember, true},
		{"delegated admin can manage viewer", domain.RoleDelegatedAdmin, domain.RoleViewer, true},
		{"delegated admin cannot manage delegated admin", domain.RoleDelegatedAdmin, domain.RoleDelegatedAdmin, false},
		{"delegated admin cannot manage admin", domain.RoleDelegatedAdmin, domain.RoleAdmin, false},
		{"member can manage viewer", domain.RoleMember, domain.RoleViewer, true},
		{"member cannot manage member", domain.RoleMember, domain.RoleMember, false},
		{"member cannot manage admin", domain.RoleMember, domain.RoleAdmin, false},
		{"member cannot manage delegated admin", domain.RoleMember, domain.RoleDelegatedAdmin, false},
		{"viewer cannot manage anyone", domain.RoleViewer, domain.RoleViewer, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.role.CanManage(tt.target))
		})
	}
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		input    string
		expected domain.Role
	}{
		{"admin", domain.RoleAdmin},
		{"delegated_admin", domain.RoleDelegatedAdmin},
		{"member", domain.RoleMember},
		{"viewer", domain.RoleViewer},
		{"invalid", domain.RoleViewer},
		{"", domain.RoleViewer},
		{"ADMIN", domain.RoleViewer}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, domain.ParseRole(tt.input))
		})
	}
}

func TestRoleIsValid(t *testing.T) {
	assert.True(t, domain.RoleAdmin.IsValid())
	assert.True(t, domain.RoleDelegatedAdmin.IsValid())
	assert.True(t, domain.RoleMember.IsValid())
	assert.True(t, domain.RoleViewer.IsValid())
	assert.False(t, domain.Role("invalid").IsValid())
}

func TestNormalizeMemberType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"legacy admin empty", "", "admin"},
		{"explicit admin", "admin", "admin"},
		{"oidc session alias", "oidc", "oidc_user"},
		{"canonical oidc user", "oidc_user", "oidc_user"},
		{"api key unchanged", "api_key", "api_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeMemberType(tt.in))
		})
	}
}
