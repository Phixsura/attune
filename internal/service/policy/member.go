// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// MemberPolicy determines what actions a user can take on tenant members.
type MemberPolicy struct {
	actorRole domain.Role
	actorID   string
}

// NewMemberPolicy creates a policy checker for the given actor.
func NewMemberPolicy(actorRole domain.Role, actorID string) *MemberPolicy {
	return ptrext.Of(MemberPolicy{actorRole: actorRole, actorID: actorID})
}

// CanView returns true if the user can view the member list (viewer+).
func (p *MemberPolicy) CanView() bool {
	return p.actorRole.AtLeast(domain.RoleViewer)
}

// CanInvite returns true if the user can invite new members (admin only).
func (p *MemberPolicy) CanInvite() bool {
	return p.actorRole == domain.RoleAdmin
}

// CanChangeRole returns true if the actor can change a member's role.
// Admin can change roles of members/viewers, but promoting to admin requires CanPromoteToAdmin.
func (p *MemberPolicy) CanChangeRole(currentRole, newRole domain.Role) bool {
	if p.actorRole != domain.RoleAdmin {
		return false
	}
	if !p.actorRole.CanManage(currentRole) {
		return false
	}
	if newRole == domain.RoleAdmin {
		return false
	}
	return true
}

// CanPromoteToAdmin returns true if the actor can promote someone to admin.
// This is a separate action requiring explicit confirmation.
func (p *MemberPolicy) CanPromoteToAdmin() bool {
	return p.actorRole == domain.RoleAdmin
}

// CanRemove returns true if the actor can remove the target member.
// Admin can remove non-admins; cannot remove self.
// Last-admin protection is enforced at repo layer.
func (p *MemberPolicy) CanRemove(targetID string, targetRole domain.Role) bool {
	if p.actorRole != domain.RoleAdmin {
		return false
	}
	if targetID == p.actorID {
		return false
	}
	return p.actorRole.CanManage(targetRole)
}
