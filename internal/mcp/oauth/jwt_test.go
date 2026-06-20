// SPDX-License-Identifier: Apache-2.0

package oauth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/mcp/oauth"
)

func TestJWT_SignAndVerify(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b")
	issuer := "https://attune.example.com/mcp/oauth"

	signer := oauth.NewJWTSigner(secret, issuer)

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read", "mcp:write"},
	}

	token, err := signer.Sign(claims, 1*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	verified, err := signer.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, claims.TenantID, verified.TenantID)
	assert.Equal(t, claims.ClientID, verified.ClientID)
	assert.Equal(t, claims.Scopes, verified.Scopes)
}

func TestJWT_ExpiredToken(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b")
	issuer := "https://attune.example.com/mcp/oauth"

	signer := oauth.NewJWTSigner(secret, issuer)

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}

	token, err := signer.Sign(claims, -1*time.Hour)
	require.NoError(t, err)

	_, err = signer.Verify(token)
	assert.ErrorIs(t, err, oauth.ErrInvalidToken)
}

func TestJWT_VerifyMismatch(t *testing.T) {
	claims := oauth.AccessTokenClaims{TenantID: "tenant-123", ClientID: uuid.New(), SessionID: uuid.New(), Scopes: []string{"mcp:read"}}
	tests := []struct {
		name                     string
		signSecret, verifySecret string
		signIssuer, verifyIssuer string
	}{
		{"wrong_secret", "test-secret-key-for-jwt-signing-32b", "different-secret-key-for-testing32", "https://attune.example.com/mcp/oauth", "https://attune.example.com/mcp/oauth"},
		{"wrong_issuer", "test-secret-key-for-jwt-signing-32b", "test-secret-key-for-jwt-signing-32b", "https://attune.example.com/mcp/oauth", "https://other.example.com/mcp/oauth"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signer := oauth.NewJWTSigner([]byte(tt.signSecret), tt.signIssuer)
			verifier := oauth.NewJWTSigner([]byte(tt.verifySecret), tt.verifyIssuer)
			token, err := signer.Sign(claims, time.Hour)
			require.NoError(t, err)
			_, err = verifier.Verify(token)
			assert.ErrorIs(t, err, oauth.ErrInvalidToken)
		})
	}
}
