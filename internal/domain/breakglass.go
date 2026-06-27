// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"net"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// BreakGlassToken represents a one-time emergency access token for admin
// login when SSO is unavailable or misconfigured.
type BreakGlassToken struct {
	ID         string
	TenantID   string
	AdminEmail string
	TokenHash  string
	ExpiresAt  time.Time
	UsedAt     *time.Time
	IssuedBy   string
	IssuedAt   time.Time
	RevokedAt  *time.Time
	RevokedBy  *string

	// AllowedIPs is a list of CIDR ranges that can use this token.
	// Empty/nil means no IP restriction (any IP allowed).
	AllowedIPs []string

	// UsedFromIP records the client IP that consumed the token.
	UsedFromIP *string

	// Approval workflow fields
	RequiresApproval  bool
	ApprovedBy        *string
	ApprovedAt        *time.Time
	ApprovalExpiresAt *time.Time
	Justification     *string

	// Permission scoping: nil = full admin, otherwise limited to these permissions
	ScopedPermissions []string
}

// BreakGlassRecoveryCode is a one-time recovery code for offline emergency access.
type BreakGlassRecoveryCode struct {
	ID        string
	TenantID  string
	CodeHash  string
	Label     string
	CreatedAt time.Time
	CreatedBy string
	UsedAt    *time.Time
	UsedByIP  *string
	RevokedAt *time.Time
	RevokedBy *string
}

// IsValid returns true if the recovery code has not been used or revoked.
func (c BreakGlassRecoveryCode) IsValid() bool {
	return c.UsedAt == nil && c.RevokedAt == nil
}

// IsValid returns true if the token has not been used, revoked, or expired,
// and if it requires approval, that it has been approved.
func (t BreakGlassToken) IsValid(now time.Time) bool {
	if t.UsedAt != nil || t.RevokedAt != nil || !t.ExpiresAt.After(now) {
		return false
	}
	// If requires approval, must be approved
	if t.RequiresApproval && t.ApprovedAt == nil {
		return false
	}
	return true
}

// IsPendingApproval returns true if the token requires but hasn't received approval.
func (t BreakGlassToken) IsPendingApproval() bool {
	return t.RequiresApproval && t.ApprovedAt == nil
}

// IsApprovalExpired returns true if the approval window has passed.
func (t BreakGlassToken) IsApprovalExpired(now time.Time) bool {
	if !t.RequiresApproval || t.ApprovalExpiresAt == nil {
		return false
	}
	return now.After(ptrext.Indirect(t.ApprovalExpiresAt))
}

// IsExpired returns true if the token's TTL has passed.
func (t BreakGlassToken) IsExpired(now time.Time) bool {
	return now.After(t.ExpiresAt)
}

// IsUsed returns true if the token has been consumed.
func (t BreakGlassToken) IsUsed() bool {
	return t.UsedAt != nil
}

// IsRevoked returns true if the token was manually revoked.
func (t BreakGlassToken) IsRevoked() bool {
	return t.RevokedAt != nil
}

// IsIPAllowed checks if the given IP is permitted to use this token.
// Returns true if AllowedIPs is empty (no restriction) or if the IP
// matches any of the allowed CIDR ranges.
func (t BreakGlassToken) IsIPAllowed(clientIP string) bool {
	if len(t.AllowedIPs) == 0 {
		return true
	}

	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}

	for _, cidr := range t.AllowedIPs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try as single IP (add /32 or /128)
			if singleIP := net.ParseIP(cidr); singleIP != nil {
				if singleIP.Equal(ip) {
					return true
				}
			}
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
