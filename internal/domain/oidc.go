// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// OIDCClaims holds extracted user info from IdP.
type OIDCClaims struct {
	Subject string   // sub claim (unique ID from IdP)
	Email   string   // email or configured user_claim
	Name    string   // display name
	Groups  []string // groups claim
}

// OIDCUser represents a user authenticated via OIDC.
type OIDCUser struct {
	ID          string
	Provider    string // "oidc" (for future multi-IdP)
	ExternalID  string // sub claim
	Email       string
	DisplayName string
	Role        string
	Groups      []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastLoginAt time.Time
}
