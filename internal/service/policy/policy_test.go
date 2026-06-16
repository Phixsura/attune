// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/domain"
)

func TestFeedbackPolicy_CanView(t *testing.T) {
	tests := []struct {
		role     domain.Role
		expected bool
	}{
		{domain.RoleAdmin, true},
		{domain.RoleMember, true},
		{domain.RoleViewer, true},
	}
	for _, tt := range tests {
		t.Run(tt.role.String(), func(t *testing.T) {
			p := NewFeedbackPolicy(tt.role, "user-1")
			assert.Equal(t, tt.expected, p.CanView())
		})
	}
}

func TestFeedbackPolicy_CanEdit(t *testing.T) {
	tests := []struct {
		role     domain.Role
		expected bool
	}{
		{domain.RoleAdmin, true},
		{domain.RoleMember, true},
		{domain.RoleViewer, false},
	}
	for _, tt := range tests {
		t.Run(tt.role.String(), func(t *testing.T) {
			p := NewFeedbackPolicy(tt.role, "user-1")
			assert.Equal(t, tt.expected, p.CanEdit())
		})
	}
}

func TestFeedbackPolicy_CanDelete(t *testing.T) {
	tests := []struct {
		name      string
		role      domain.Role
		userID    string
		createdBy string
		expected  bool
	}{
		{"admin can delete any", domain.RoleAdmin, "admin-1", "other-user", true},
		{"member can delete own", domain.RoleMember, "user-1", "user-1", true},
		{"member cannot delete others", domain.RoleMember, "user-1", "user-2", false},
		{"viewer cannot delete any", domain.RoleViewer, "user-1", "user-1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewFeedbackPolicy(tt.role, tt.userID)
			assert.Equal(t, tt.expected, p.CanDelete(tt.createdBy))
		})
	}
}

func TestFeedbackPolicy_CanBatchDelete(t *testing.T) {
	assert.True(t, NewFeedbackPolicy(domain.RoleAdmin, "a").CanBatchDelete())
	assert.False(t, NewFeedbackPolicy(domain.RoleMember, "a").CanBatchDelete())
	assert.False(t, NewFeedbackPolicy(domain.RoleViewer, "a").CanBatchDelete())
}

func TestMemberPolicy_CanView(t *testing.T) {
	assert.True(t, NewMemberPolicy(domain.RoleAdmin, "a").CanView())
	assert.True(t, NewMemberPolicy(domain.RoleMember, "a").CanView())
	assert.True(t, NewMemberPolicy(domain.RoleViewer, "a").CanView())
}

func TestMemberPolicy_CanInvite(t *testing.T) {
	assert.True(t, NewMemberPolicy(domain.RoleAdmin, "a").CanInvite())
	assert.False(t, NewMemberPolicy(domain.RoleMember, "a").CanInvite())
	assert.False(t, NewMemberPolicy(domain.RoleViewer, "a").CanInvite())
}

func TestMemberPolicy_CanChangeRole(t *testing.T) {
	tests := []struct {
		name        string
		actorRole   domain.Role
		currentRole domain.Role
		newRole     domain.Role
		expected    bool
	}{
		{"admin can demote member to viewer", domain.RoleAdmin, domain.RoleMember, domain.RoleViewer, true},
		{"admin can promote viewer to member", domain.RoleAdmin, domain.RoleViewer, domain.RoleMember, true},
		{"admin cannot promote to admin via CanChangeRole", domain.RoleAdmin, domain.RoleMember, domain.RoleAdmin, false},
		{"admin cannot change another admin", domain.RoleAdmin, domain.RoleAdmin, domain.RoleMember, false},
		{"member cannot change anyone", domain.RoleMember, domain.RoleViewer, domain.RoleMember, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewMemberPolicy(tt.actorRole, "actor-1")
			assert.Equal(t, tt.expected, p.CanChangeRole(tt.currentRole, tt.newRole))
		})
	}
}

func TestMemberPolicy_CanPromoteToAdmin(t *testing.T) {
	assert.True(t, NewMemberPolicy(domain.RoleAdmin, "a").CanPromoteToAdmin())
	assert.False(t, NewMemberPolicy(domain.RoleMember, "a").CanPromoteToAdmin())
}

func TestMemberPolicy_CanRemove(t *testing.T) {
	tests := []struct {
		name       string
		actorRole  domain.Role
		actorID    string
		targetID   string
		targetRole domain.Role
		expected   bool
	}{
		{"admin can remove member", domain.RoleAdmin, "admin-1", "member-1", domain.RoleMember, true},
		{"admin can remove viewer", domain.RoleAdmin, "admin-1", "viewer-1", domain.RoleViewer, true},
		{"admin cannot remove another admin", domain.RoleAdmin, "admin-1", "admin-2", domain.RoleAdmin, false},
		{"admin cannot remove self", domain.RoleAdmin, "admin-1", "admin-1", domain.RoleAdmin, false},
		{"member cannot remove anyone", domain.RoleMember, "member-1", "viewer-1", domain.RoleViewer, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewMemberPolicy(tt.actorRole, tt.actorID)
			assert.Equal(t, tt.expected, p.CanRemove(tt.targetID, tt.targetRole))
		})
	}
}
