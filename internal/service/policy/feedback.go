// SPDX-License-Identifier: Apache-2.0

// Package policy provides resource-level authorization checks.
package policy

import (
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// FeedbackPolicy determines what actions a user can take on feedback items.
type FeedbackPolicy struct {
	role   domain.Role
	userID string
}

// NewFeedbackPolicy creates a policy checker for the given user.
func NewFeedbackPolicy(role domain.Role, userID string) *FeedbackPolicy {
	return ptrext.Of(FeedbackPolicy{role: role, userID: userID})
}

// CanView returns true if the user can view feedback (viewer+).
func (p *FeedbackPolicy) CanView() bool {
	return p.role.AtLeast(domain.RoleViewer)
}

// CanEdit returns true if the user can edit feedback (member+).
// This includes transitioning workflow state and managing tags.
func (p *FeedbackPolicy) CanEdit() bool {
	return p.role.AtLeast(domain.RoleMember)
}

// CanDelete returns true if the user can delete the given feedback.
// Admin can delete any feedback; member and delegated admin can only delete their own.
func (p *FeedbackPolicy) CanDelete(feedbackCreatedByUserID string) bool {
	if p.role == domain.RoleAdmin {
		return true
	}
	if (p.role == domain.RoleMember || p.role == domain.RoleDelegatedAdmin) && feedbackCreatedByUserID == p.userID {
		return true
	}
	return false
}

// CanBatchDelete returns true if the user can batch delete feedback (admin only).
func (p *FeedbackPolicy) CanBatchDelete() bool {
	return p.role == domain.RoleAdmin
}
