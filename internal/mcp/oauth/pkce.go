// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// VerifyCodeChallenge verifies a PKCE code challenge against a code verifier.
// Only S256 method is supported (required by OAuth 2.1).
// Per RFC 7636, code_verifier must be 43-128 characters.
func VerifyCodeChallenge(challenge, verifier, method string) bool {
	if method != "S256" {
		return false
	}

	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}

	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])

	return subtle.ConstantTimeCompare([]byte(challenge), []byte(expected)) == 1
}

// GenerateCodeChallenge generates a code challenge from a code verifier using S256.
func GenerateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
