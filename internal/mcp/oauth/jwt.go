// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrInvalidToken = errors.New("invalid or expired token")

// AccessTokenClaims holds the custom claims for an MCP access token.
type AccessTokenClaims struct {
	TenantID  string    `json:"tenant_id"`
	ClientID  uuid.UUID `json:"client_id"`
	SessionID uuid.UUID `json:"session_id"`
	Scopes    []string  `json:"scopes"`
}

type jwtClaims struct {
	jwt.RegisteredClaims
	AccessTokenClaims
}

// JWTSigner handles signing and verifying MCP access tokens.
type JWTSigner struct {
	secret []byte
	issuer string
}

// NewJWTSigner creates a new JWTSigner.
func NewJWTSigner(secret []byte, issuer string) *JWTSigner {
	return ptrext.Of(JWTSigner{secret: secret, issuer: issuer})
}

// Sign creates a signed JWT access token.
func (s *JWTSigner) Sign(claims AccessTokenClaims, ttl time.Duration) (string, error) {
	now := time.Now()
	c := jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        uuid.New().String(),
		},
		AccessTokenClaims: claims,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString(s.secret)
}

// Verify validates a JWT and returns the claims.
func (s *JWTSigner) Verify(tokenString string) (*AccessTokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, ptrext.Of(jwtClaims{}), func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*jwtClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	if claims.Issuer != s.issuer {
		return nil, ErrInvalidToken
	}

	return ptrext.Of(claims.AccessTokenClaims), nil
}
