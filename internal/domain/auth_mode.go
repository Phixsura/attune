// SPDX-License-Identifier: Apache-2.0

package domain

// AuthMode represents the authentication mode for a tenant.
type AuthMode string

const (
	// AuthModeHybrid allows both local admin login and OIDC SSO.
	AuthModeHybrid AuthMode = "hybrid"
	// AuthModeSSOnly enforces SSO-only login, disabling local admin login
	// except via break-glass tokens.
	AuthModeSSOnly AuthMode = "sso_only"
)

// Valid returns true if the mode is a known value.
func (m AuthMode) Valid() bool {
	return m == AuthModeHybrid || m == AuthModeSSOnly
}

// String implements fmt.Stringer.
func (m AuthMode) String() string {
	return string(m)
}
