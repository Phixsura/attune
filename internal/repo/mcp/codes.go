// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrCodeNotFound = errors.New("authorization code not found or expired")

// AuthCode represents an OAuth authorization code.
type AuthCode struct {
	Code          string
	ClientID      uuid.UUID
	RedirectURI   string
	Scopes        []string
	CodeChallenge string
	UserID        string
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

// CreateCodeParams holds parameters for creating an authorization code.
type CreateCodeParams struct {
	ClientID      uuid.UUID
	RedirectURI   string
	Scopes        []string
	CodeChallenge string
	UserID        string
	ExpiresAt     time.Time
}

// CodesRepo handles OAuth authorization code persistence.
type CodesRepo struct {
	pool *pgxpool.Pool
}

// NewCodes creates a new CodesRepo.
func NewCodes(pool *pgxpool.Pool) *CodesRepo {
	return ptrext.Of(CodesRepo{pool: pool})
}

// Create generates and stores a new authorization code.
func (r *CodesRepo) Create(ctx context.Context, p CreateCodeParams) (*AuthCode, error) {
	code, err := generateCode()
	if err != nil {
		return nil, err
	}

	const q = `
		INSERT INTO mcp_oauth_codes (code, client_id, redirect_uri, scopes, code_challenge, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING code, client_id, redirect_uri, scopes, code_challenge, user_id, expires_at, created_at
	`
	var c AuthCode
	err = r.pool.QueryRow(ctx, q, code, p.ClientID, p.RedirectURI, p.Scopes, p.CodeChallenge, p.UserID, p.ExpiresAt).Scan(
		&c.Code, &c.ClientID, &c.RedirectURI, &c.Scopes, &c.CodeChallenge, &c.UserID, &c.ExpiresAt, &c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(c), nil
}

// Consume retrieves and deletes an authorization code (single use).
func (r *CodesRepo) Consume(ctx context.Context, code string) (*AuthCode, error) {
	const q = `
		DELETE FROM mcp_oauth_codes
		WHERE code = $1 AND expires_at > NOW()
		RETURNING code, client_id, redirect_uri, scopes, code_challenge, user_id, expires_at, created_at
	`
	var c AuthCode
	err := r.pool.QueryRow(ctx, q, code).Scan(
		&c.Code, &c.ClientID, &c.RedirectURI, &c.Scopes, &c.CodeChallenge, &c.UserID, &c.ExpiresAt, &c.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(c), nil
}

// Cleanup deletes expired authorization codes.
func (r *CodesRepo) Cleanup(ctx context.Context) (int64, error) {
	const q = `DELETE FROM mcp_oauth_codes WHERE expires_at < NOW()`
	tag, err := r.pool.Exec(ctx, q)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func generateCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
