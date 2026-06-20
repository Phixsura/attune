// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrRefreshTokenNotFound = errors.New("refresh token not found or expired")

// RefreshToken represents an OAuth refresh token.
type RefreshToken struct {
	ID        uuid.UUID
	ClientID  uuid.UUID
	TokenHash string
	Scopes    []string
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
	RevokedAt *time.Time
}

// CreateRefreshTokenParams holds parameters for creating a refresh token.
type CreateRefreshTokenParams struct {
	ClientID  uuid.UUID
	Scopes    []string
	UserID    string
	ExpiresAt time.Time
}

// CreateRefreshTokenResult contains the token and its raw value.
type CreateRefreshTokenResult struct {
	Token    *RefreshToken
	RawToken string
}

// TokensRepo handles OAuth refresh token persistence.
type TokensRepo struct {
	pool *pgxpool.Pool
}

// NewTokens creates a new TokensRepo.
func NewTokens(pool *pgxpool.Pool) *TokensRepo {
	return ptrext.Of(TokensRepo{pool: pool})
}

// Create generates and stores a new refresh token.
func (r *TokensRepo) Create(ctx context.Context, p CreateRefreshTokenParams) (*CreateRefreshTokenResult, error) {
	rawToken, err := generateRefreshToken()
	if err != nil {
		return nil, err
	}
	tokenHash := hashToken(rawToken)

	const q = `
		INSERT INTO mcp_oauth_refresh_tokens (client_id, token_hash, scopes, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, client_id, token_hash, scopes, user_id, expires_at, created_at, revoked_at
	`
	var t RefreshToken
	err = r.pool.QueryRow(ctx, q, p.ClientID, tokenHash, p.Scopes, p.UserID, p.ExpiresAt).Scan(
		&t.ID, &t.ClientID, &t.TokenHash, &t.Scopes, &t.UserID, &t.ExpiresAt, &t.CreatedAt, &t.RevokedAt,
	)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(CreateRefreshTokenResult{Token: ptrext.Of(t), RawToken: rawToken}), nil
}

// GetByToken retrieves a refresh token by its raw value.
func (r *TokensRepo) GetByToken(ctx context.Context, rawToken string) (*RefreshToken, error) {
	tokenHash := hashToken(rawToken)
	const q = `
		SELECT id, client_id, token_hash, scopes, user_id, expires_at, created_at, revoked_at
		FROM mcp_oauth_refresh_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW()
	`
	var t RefreshToken
	err := r.pool.QueryRow(ctx, q, tokenHash).Scan(
		&t.ID, &t.ClientID, &t.TokenHash, &t.Scopes, &t.UserID, &t.ExpiresAt, &t.CreatedAt, &t.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRefreshTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(t), nil
}

// Revoke marks a refresh token as revoked.
func (r *TokensRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE mcp_oauth_refresh_tokens SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`
	_, err := r.pool.Exec(ctx, q, id)
	return err
}

// RevokeByClient revokes all refresh tokens for a client.
func (r *TokensRepo) RevokeByClient(ctx context.Context, clientID uuid.UUID) (int64, error) {
	const q = `UPDATE mcp_oauth_refresh_tokens SET revoked_at = NOW() WHERE client_id = $1 AND revoked_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, clientID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Cleanup deletes expired refresh tokens.
func (r *TokensRepo) Cleanup(ctx context.Context) (int64, error) {
	const q = `DELETE FROM mcp_oauth_refresh_tokens WHERE expires_at < NOW() OR revoked_at IS NOT NULL`
	tag, err := r.pool.Exec(ctx, q)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
