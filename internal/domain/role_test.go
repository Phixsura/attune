// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRole_AtLeast(t *testing.T) {
	tests := []struct {
		name     string
		role     Role
		required Role
		expected bool
	}{
		{"admin >= admin", RoleAdmin, RoleAdmin, true},
		{"admin >= delegated admin", RoleAdmin, RoleDelegatedAdmin, true},
		{"admin >= member", RoleAdmin, RoleMember, true},
		{"admin >= viewer", RoleAdmin, RoleViewer, true},
		{"delegated admin >= admin", RoleDelegatedAdmin, RoleAdmin, false},
		{"delegated admin >= delegated admin", RoleDelegatedAdmin, RoleDelegatedAdmin, true},
		{"delegated admin >= member", RoleDelegatedAdmin, RoleMember, true},
		{"delegated admin >= viewer", RoleDelegatedAdmin, RoleViewer, true},
		{"member >= admin", RoleMember, RoleAdmin, false},
		{"member >= delegated admin", RoleMember, RoleDelegatedAdmin, false},
		{"member >= member", RoleMember, RoleMember, true},
		{"member >= viewer", RoleMember, RoleViewer, true},
		{"viewer >= admin", RoleViewer, RoleAdmin, false},
		{"viewer >= delegated admin", RoleViewer, RoleDelegatedAdmin, false},
		{"viewer >= member", RoleViewer, RoleMember, false},
		{"viewer >= viewer", RoleViewer, RoleViewer, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.role.AtLeast(tt.required))
		})
	}
}

func TestRole_CanManage(t *testing.T) {
	tests := []struct {
		name     string
		actor    Role
		target   Role
		expected bool
	}{
		{"admin can manage member", RoleAdmin, RoleMember, true},
		{"admin can manage viewer", RoleAdmin, RoleViewer, true},
		{"admin can manage delegated admin", RoleAdmin, RoleDelegatedAdmin, true},
		{"admin cannot manage admin", RoleAdmin, RoleAdmin, false},
		{"delegated admin can manage member", RoleDelegatedAdmin, RoleMember, true},
		{"delegated admin can manage viewer", RoleDelegatedAdmin, RoleViewer, true},
		{"delegated admin cannot manage admin", RoleDelegatedAdmin, RoleAdmin, false},
		{"delegated admin cannot manage delegated admin", RoleDelegatedAdmin, RoleDelegatedAdmin, false},
		{"member can manage viewer", RoleMember, RoleViewer, true},
		{"member cannot manage member", RoleMember, RoleMember, false},
		{"member cannot manage admin", RoleMember, RoleAdmin, false},
		{"member cannot manage delegated admin", RoleMember, RoleDelegatedAdmin, false},
		{"viewer cannot manage anyone", RoleViewer, RoleViewer, false},
		{"viewer cannot manage member", RoleViewer, RoleMember, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.actor.CanManage(tt.target))
		})
	}
}

func TestRole_String(t *testing.T) {
	assert.Equal(t, "admin", RoleAdmin.String())
	assert.Equal(t, "delegated_admin", RoleDelegatedAdmin.String())
	assert.Equal(t, "member", RoleMember.String())
	assert.Equal(t, "viewer", RoleViewer.String())
}

func TestRole_IsValid(t *testing.T) {
	assert.True(t, RoleAdmin.IsValid())
	assert.True(t, RoleDelegatedAdmin.IsValid())
	assert.True(t, RoleMember.IsValid())
	assert.True(t, RoleViewer.IsValid())
	assert.False(t, Role("invalid").IsValid())
	assert.False(t, Role("").IsValid())
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		input    string
		expected Role
	}{
		{"admin", RoleAdmin},
		{"delegated_admin", RoleDelegatedAdmin},
		{"member", RoleMember},
		{"viewer", RoleViewer},
		{"invalid", RoleViewer}, // defaults to viewer
		{"", RoleViewer},        // defaults to viewer
		{"ADMIN", RoleViewer},   // case-sensitive, defaults to viewer
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseRole(tt.input))
		})
	}
}
