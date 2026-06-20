// SPDX-License-Identifier: Apache-2.0

package oauth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/mcp/oauth"
)

func TestPKCE_VerifyS256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	assert.True(t, oauth.VerifyCodeChallenge(challenge, verifier, "S256"))
}

func TestPKCE_VerifyS256_Invalid(t *testing.T) {
	verifier := "wrong_verifier"
	challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	assert.False(t, oauth.VerifyCodeChallenge(challenge, verifier, "S256"))
}

func TestPKCE_PlainNotSupported(t *testing.T) {
	verifier := "test"
	challenge := "test"

	assert.False(t, oauth.VerifyCodeChallenge(challenge, verifier, "plain"))
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	assert.Equal(t, expected, oauth.GenerateCodeChallenge(verifier))
}
