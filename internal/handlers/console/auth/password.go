// SPDX-License-Identifier: Apache-2.0

// Package auth implements the local admin password login path for the
// console (#66 Plan T11). It owns: bcrypt hashing with a constant
// equalising-dummy verify, the login + logout HTTP endpoints, and the
// startup BootstrapAdmin helper.
//
// This package replaces internal/handlers/console/oauth — the Lark OAuth
// flow is removed in Plan T17, the dev_login backdoor with it. The
// post-login redirect helper redirectIsSafe is rescued verbatim from
// oauth.go into redirect.go (same logic, new home).
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// stubHash — pre-computed bcrypt(cost=12) of a fixed string. Used to
// equalise wall-clock time between "unknown email" and "wrong password"
// login responses so a timing side-channel cannot enumerate valid admin
// addresses. Result is always discarded; the function ONLY exists for
// its CPU cost.
const stubHash = "$2a$12$fWWdBD5VvBT.GzI5enkEAeQbQKWOmrEUfmTKn23hx1lAohLc.f36O"

// VerifyOrDummy returns true iff bcrypt.CompareHashAndPassword succeeds
// against the supplied real hash. When realHash is empty (i.e., the
// email was not found in admins), the function runs bcrypt against the
// stubHash and discards the result so the response time matches the
// real-hash path.
func VerifyOrDummy(realHash, password string) bool {
	if realHash == "" {
		_ = bcrypt.CompareHashAndPassword([]byte(stubHash), []byte(password))
		return false
	}
	return errors.Is(bcrypt.CompareHashAndPassword([]byte(realHash), []byte(password)), nil)
}

// HashPassword wraps bcrypt.GenerateFromPassword at cost 12 (the 2024
// OWASP baseline). Returns the encoded hash ready for storage.
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
