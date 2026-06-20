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

func TestJWT_WrongSecret(t *testing.T) {
	secret1 := []byte("test-secret-key-for-jwt-signing-32b")
	secret2 := []byte("different-secret-key-for-testing32")
	issuer := "https://attune.example.com/mcp/oauth"

	signer1 := oauth.NewJWTSigner(secret1, issuer)
	signer2 := oauth.NewJWTSigner(secret2, issuer)

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}

	token, err := signer1.Sign(claims, 1*time.Hour)
	require.NoError(t, err)

	_, err = signer2.Verify(token)
	assert.ErrorIs(t, err, oauth.ErrInvalidToken)
}

func TestJWT_WrongIssuer(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b")
	issuer1 := "https://attune.example.com/mcp/oauth"
	issuer2 := "https://other.example.com/mcp/oauth"

	signer1 := oauth.NewJWTSigner(secret, issuer1)
	signer2 := oauth.NewJWTSigner(secret, issuer2)

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}

	token, err := signer1.Sign(claims, 1*time.Hour)
	require.NoError(t, err)

	_, err = signer2.Verify(token)
	assert.ErrorIs(t, err, oauth.ErrInvalidToken)
}
