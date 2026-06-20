// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrSessionNotFound = errors.New("mcp session not found")

// Session represents an MCP session (Mcp-Session-Id tracking).
type Session struct {
	ID           uuid.UUID
	ClientID     uuid.UUID
	TenantID     string
	Scopes       []string
	LastActiveAt time.Time
	CreatedAt    time.Time
	ClosedAt     *time.Time
}

// CreateSessionParams holds parameters for creating a session.
type CreateSessionParams struct {
	ClientID uuid.UUID
	TenantID string
	Scopes   []string
}

// SessionsRepo handles MCP session persistence.
type SessionsRepo struct {
	pool *pgxpool.Pool
}

// NewSessions creates a new SessionsRepo.
func NewSessions(pool *pgxpool.Pool) *SessionsRepo {
	return ptrext.Of(SessionsRepo{pool: pool})
}

// Create inserts a new MCP session.
func (r *SessionsRepo) Create(ctx context.Context, p CreateSessionParams) (*Session, error) {
	const q = `
		INSERT INTO mcp_sessions (client_id, tenant_id, scopes)
		VALUES ($1, $2, $3)
		RETURNING id, client_id, tenant_id, scopes, last_active_at, created_at, closed_at
	`
	var s Session
	err := r.pool.QueryRow(ctx, q, p.ClientID, p.TenantID, p.Scopes).Scan(
		&s.ID, &s.ClientID, &s.TenantID, &s.Scopes, &s.LastActiveAt, &s.CreatedAt, &s.ClosedAt,
	)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(s), nil
}

// GetByID retrieves an active session by ID.
func (r *SessionsRepo) GetByID(ctx context.Context, id uuid.UUID) (*Session, error) {
	const q = `
		SELECT id, client_id, tenant_id, scopes, last_active_at, created_at, closed_at
		FROM mcp_sessions
		WHERE id = $1 AND closed_at IS NULL
	`
	var s Session
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&s.ID, &s.ClientID, &s.TenantID, &s.Scopes, &s.LastActiveAt, &s.CreatedAt, &s.ClosedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(s), nil
}

// Touch updates the last_active_at timestamp.
func (r *SessionsRepo) Touch(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE mcp_sessions SET last_active_at = NOW() WHERE id = $1 AND closed_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// Close marks a session as closed.
func (r *SessionsRepo) Close(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE mcp_sessions SET closed_at = NOW() WHERE id = $1 AND closed_at IS NULL`
	_, err := r.pool.Exec(ctx, q, id)
	return err
}

// CloseByClient closes all sessions for a client.
func (r *SessionsRepo) CloseByClient(ctx context.Context, clientID uuid.UUID) (int64, error) {
	const q = `UPDATE mcp_sessions SET closed_at = NOW() WHERE client_id = $1 AND closed_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, clientID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CleanupIdle closes sessions idle longer than the threshold.
func (r *SessionsRepo) CleanupIdle(ctx context.Context, idleThreshold time.Duration) (int64, error) {
	const q = `UPDATE mcp_sessions SET closed_at = NOW() WHERE last_active_at < $1 AND closed_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, time.Now().Add(-idleThreshold))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// IsActive checks if a session is active (not closed).
func (r *SessionsRepo) IsActive(ctx context.Context, id uuid.UUID) (bool, error) {
	const q = `SELECT closed_at IS NULL FROM mcp_sessions WHERE id = $1`
	var active bool
	err := r.pool.QueryRow(ctx, q, id).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return active, nil
}
