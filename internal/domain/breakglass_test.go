// SPDX-License-Identifier: Apache-2.0

package domain_test

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestBreakGlassToken_IsValid(t *testing.T) {
	now := time.Now()
	future := now.Add(1 * time.Hour)
	past := now.Add(-1 * time.Hour)

	tests := []struct {
		name  string
		token domain.BreakGlassToken
		valid bool
	}{
		{
			name: "valid token",
			token: domain.BreakGlassToken{
				ExpiresAt: future,
			},
			valid: true,
		},
		{
			name: "expired token",
			token: domain.BreakGlassToken{
				ExpiresAt: past,
			},
			valid: false,
		},
		{
			name: "used token",
			token: domain.BreakGlassToken{
				ExpiresAt: future,
				UsedAt:    ptrext.Of(now),
			},
			valid: false,
		},
		{
			name: "revoked token",
			token: domain.BreakGlassToken{
				ExpiresAt: future,
				RevokedAt: ptrext.Of(now),
			},
			valid: false,
		},
		{
			name: "approval required and missing",
			token: domain.BreakGlassToken{
				ExpiresAt:         future,
				RequiresApproval:  true,
				ApprovalExpiresAt: ptrext.Of(future),
			},
			valid: false,
		},
		{
			name: "approval required and granted",
			token: domain.BreakGlassToken{
				ExpiresAt:         future,
				RequiresApproval:  true,
				ApprovedAt:        ptrext.Of(now),
				ApprovalExpiresAt: ptrext.Of(future),
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsValid(now); got != tt.valid {
				t.Errorf("IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestBreakGlassRecoveryCode_IsValid(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name string
		code domain.BreakGlassRecoveryCode
		want bool
	}{
		{name: "unused and active", want: true},
		{name: "used", code: domain.BreakGlassRecoveryCode{UsedAt: ptrext.Of(now)}, want: false},
		{name: "revoked", code: domain.BreakGlassRecoveryCode{RevokedAt: ptrext.Of(now)}, want: false},
		{
			name: "used and revoked",
			code: domain.BreakGlassRecoveryCode{
				UsedAt:    ptrext.Of(now),
				RevokedAt: ptrext.Of(now),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.code.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBreakGlassToken_ApprovalState(t *testing.T) {
	t.Parallel()

	now := time.Now()
	past := now.Add(-1 * time.Minute)
	future := now.Add(1 * time.Minute)

	tests := []struct {
		name           string
		token          domain.BreakGlassToken
		pending        bool
		approvalExpiry bool
	}{
		{name: "approval not required", token: domain.BreakGlassToken{}, pending: false, approvalExpiry: false},
		{
			name: "approval required and pending",
			token: domain.BreakGlassToken{
				RequiresApproval:  true,
				ApprovalExpiresAt: ptrext.Of(future),
			},
			pending:        true,
			approvalExpiry: false,
		},
		{
			name: "approval required and granted",
			token: domain.BreakGlassToken{
				RequiresApproval:  true,
				ApprovedAt:        ptrext.Of(now),
				ApprovalExpiresAt: ptrext.Of(future),
			},
			pending:        false,
			approvalExpiry: false,
		},
		{
			name: "approval window expired",
			token: domain.BreakGlassToken{
				RequiresApproval:  true,
				ApprovalExpiresAt: ptrext.Of(past),
			},
			pending:        true,
			approvalExpiry: true,
		},
		{
			name: "missing approval expiry is not expired",
			token: domain.BreakGlassToken{
				RequiresApproval: true,
			},
			pending:        true,
			approvalExpiry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.token.IsPendingApproval(); got != tt.pending {
				t.Errorf("IsPendingApproval() = %v, want %v", got, tt.pending)
			}
			if got := tt.token.IsApprovalExpired(now); got != tt.approvalExpiry {
				t.Errorf("IsApprovalExpired() = %v, want %v", got, tt.approvalExpiry)
			}
		})
	}
}

func TestBreakGlassToken_IsExpired(t *testing.T) {
	now := time.Now()
	future := now.Add(1 * time.Hour)
	past := now.Add(-1 * time.Hour)

	futureToken := domain.BreakGlassToken{ExpiresAt: future}
	if futureToken.IsExpired(now) {
		t.Error("future token should not be expired")
	}
	pastToken := domain.BreakGlassToken{ExpiresAt: past}
	if !pastToken.IsExpired(now) {
		t.Error("past token should be expired")
	}
}

func TestBreakGlassToken_IsUsed(t *testing.T) {
	now := time.Now()

	unused := domain.BreakGlassToken{}
	if unused.IsUsed() {
		t.Error("token without UsedAt should not be used")
	}
	used := domain.BreakGlassToken{UsedAt: ptrext.Of(now)}
	if !used.IsUsed() {
		t.Error("token with UsedAt should be used")
	}
}

func TestBreakGlassToken_IsRevoked(t *testing.T) {
	now := time.Now()

	unrevoked := domain.BreakGlassToken{}
	if unrevoked.IsRevoked() {
		t.Error("token without RevokedAt should not be revoked")
	}
	revoked := domain.BreakGlassToken{RevokedAt: ptrext.Of(now)}
	if !revoked.IsRevoked() {
		t.Error("token with RevokedAt should be revoked")
	}
}

func TestBreakGlassToken_IsIPAllowed_IPv4(t *testing.T) {
	tests := []struct {
		name       string
		allowedIPs []string
		clientIP   string
		want       bool
	}{
		{"no restriction allows any IP", nil, "192.168.1.100", true},
		{"empty list allows any IP", []string{}, "10.0.0.1", true},
		{"exact IP match", []string{"192.168.1.100"}, "192.168.1.100", true},
		{"exact IP no match", []string{"192.168.1.100"}, "192.168.1.101", false},
		{"CIDR match /24", []string{"192.168.1.0/24"}, "192.168.1.50", true},
		{"CIDR no match /24", []string{"192.168.1.0/24"}, "192.168.2.50", false},
		{"CIDR match /32", []string{"10.0.0.5/32"}, "10.0.0.5", true},
		{"CIDR no match /32", []string{"10.0.0.5/32"}, "10.0.0.6", false},
		{"multiple CIDRs first match", []string{"10.0.0.0/8", "192.168.0.0/16"}, "10.1.2.3", true},
		{"multiple CIDRs second match", []string{"10.0.0.0/8", "192.168.0.0/16"}, "192.168.100.50", true},
		{"multiple CIDRs no match", []string{"10.0.0.0/8", "192.168.0.0/16"}, "172.16.0.1", false},
		{"invalid client IP", []string{"10.0.0.0/8"}, "not-an-ip", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := domain.BreakGlassToken{AllowedIPs: tt.allowedIPs}
			if got := token.IsIPAllowed(tt.clientIP); got != tt.want {
				t.Errorf("IsIPAllowed(%q) = %v, want %v", tt.clientIP, got, tt.want)
			}
		})
	}
}

func TestBreakGlassToken_IsIPAllowed_IPv6(t *testing.T) {
	tests := []struct {
		name       string
		allowedIPs []string
		clientIP   string
		want       bool
	}{
		{"IPv6 match", []string{"::1/128"}, "::1", true},
		{"IPv6 CIDR match", []string{"2001:db8::/32"}, "2001:db8:1234:5678::1", true},
		{"mixed IPv4 and IPv6", []string{"192.168.1.0/24", "::1/128"}, "::1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := domain.BreakGlassToken{AllowedIPs: tt.allowedIPs}
			if got := token.IsIPAllowed(tt.clientIP); got != tt.want {
				t.Errorf("IsIPAllowed(%q) = %v, want %v", tt.clientIP, got, tt.want)
			}
		})
	}
}
