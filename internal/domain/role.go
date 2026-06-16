// SPDX-License-Identifier: Apache-2.0

package domain

// Role represents a user's permission level within a tenant.
type Role string

const (
	RoleAdmin  Role = "admin"  // Full tenant management
	RoleMember Role = "member" // Standard operations
	RoleViewer Role = "viewer" // Read-only access
)

var roleHierarchy = map[Role]int{
	RoleViewer: 0,
	RoleMember: 1,
	RoleAdmin:  2,
}

// AtLeast checks if this role meets the minimum requirement.
func (r Role) AtLeast(required Role) bool {
	return roleHierarchy[r] >= roleHierarchy[required]
}

// CanManage checks if this role can manage the target role.
// A role can only manage roles strictly below it.
func (r Role) CanManage(target Role) bool {
	return roleHierarchy[r] > roleHierarchy[target]
}

// String returns the string representation of the role.
func (r Role) String() string {
	return string(r)
}

// IsValid returns true if the role is one of the defined roles.
func (r Role) IsValid() bool {
	_, ok := roleHierarchy[r]
	return ok
}

// ParseRole converts a string to a Role, returning RoleViewer if invalid.
func ParseRole(s string) Role {
	r := Role(s)
	if r.IsValid() {
		return r
	}
	return RoleViewer
}
