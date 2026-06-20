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
	verifier := "wrong_verifier_that_is_at_least_43_chars_long_padding"
	challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	assert.False(t, oauth.VerifyCodeChallenge(challenge, verifier, "S256"))
}

func TestPKCE_PlainNotSupported(t *testing.T) {
	verifier := "test_verifier_that_is_at_least_43_characters_long"
	challenge := "test_verifier_that_is_at_least_43_characters_long"

	assert.False(t, oauth.VerifyCodeChallenge(challenge, verifier, "plain"))
}

func TestPKCE_VerifierTooShort(t *testing.T) {
	verifier := "too_short"
	challenge := oauth.GenerateCodeChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")

	assert.False(t, oauth.VerifyCodeChallenge(challenge, verifier, "S256"))
}

func TestPKCE_VerifierTooLong(t *testing.T) {
	verifier := "this_verifier_is_way_too_long_it_exceeds_128_characters_which_is_the_maximum_allowed_by_rfc7636_so_it_should_be_rejected_by_our_validation_logic"
	challenge := oauth.GenerateCodeChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")

	assert.False(t, oauth.VerifyCodeChallenge(challenge, verifier, "S256"))
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	assert.Equal(t, expected, oauth.GenerateCodeChallenge(verifier))
}
