// SPDX-License-Identifier: Apache-2.0

package auth

import "net/url"

// redirectIsSafe verifies that the user-supplied postLogin path is
// constrained to the same origin as baseURL. Returns true only for a
// non-empty path beginning with a single '/' and parsing back to the
// same scheme + host as baseURL.
//
// Rescued verbatim from the deleted internal/handlers/console/oauth/oauth.go
// and kept in the local-admin auth package (same logic, new home).
//
// Second-character check: both '/' (protocol-relative URL) and '\\'
// (browser-quirk path that some clients interpret as host) must be
// rejected at position 1 — otherwise "/\evil.com" would slip through
// as a same-origin path here and become an open redirect downstream.
func redirectIsSafe(baseURL, postLogin string) bool {
	if len(postLogin) == 0 || postLogin[0] != '/' {
		return false
	}
	if len(postLogin) >= 2 && (postLogin[1] == '/' || postLogin[1] == '\\') {
		return false
	}
	// Reject newlines and other control / non-ASCII characters that
	// could enable header injection or origin confusion.
	for _, r := range postLogin {
		if r < 32 || r > 126 {
			return false
		}
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	combined, err := url.Parse(baseURL + postLogin)
	if err != nil {
		return false
	}
	return combined.Scheme == base.Scheme && combined.Host == base.Host
}
