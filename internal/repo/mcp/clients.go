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
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

var (
	ErrClientNotFound     = errors.New("mcp oauth client not found")
	ErrClientNameConflict = errors.New("mcp oauth client name already exists for tenant")
	ErrInvalidInput       = errors.New("mcp oauth client field violates a constraint")
)

// Client represents an MCP OAuth client (registered agent).
type Client struct {
	ID             uuid.UUID
	TenantID       string
	Name           string
	RedirectURIs   []string
	Scopes         []string
	ToolPolicyMode string
	RateLimitRPM   *int
	RateLimitBurst *int
	CreatedAt      time.Time
	CreatedBy      string
	RevokedAt      *time.Time
}

// CreateClientParams holds parameters for creating a new OAuth client.
type CreateClientParams struct {
	TenantID     string
	Name         string
	RedirectURIs []string
	Scopes       []string
	CreatedBy    string
}

// UpdateClientGovernanceParams holds mutable governance settings for a client.
type UpdateClientGovernanceParams struct {
	ID             uuid.UUID
	ToolPolicyMode string
	RateLimitRPM   *int
	RateLimitBurst *int
}

// ClientsRepo handles MCP OAuth client persistence.
type ClientsRepo struct {
	pool *pgxpool.Pool
}

// NewClients creates a new ClientsRepo.
func NewClients(pool *pgxpool.Pool) *ClientsRepo {
	return ptrext.Of(ClientsRepo{pool: pool})
}

// Create inserts a new OAuth client.
func (r *ClientsRepo) Create(ctx context.Context, p CreateClientParams) (*Client, error) {
	const q = `
		INSERT INTO mcp_oauth_clients (tenant_id, name, redirect_uris, scopes, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, name, redirect_uris, scopes, tool_policy_mode,
		          rate_limit_rpm, rate_limit_burst, created_at, created_by, revoked_at
	`
	var c Client
	err := r.pool.QueryRow(ctx, q, p.TenantID, p.Name, p.RedirectURIs, p.Scopes, p.CreatedBy).Scan(
		&c.ID, &c.TenantID, &c.Name, &c.RedirectURIs, &c.Scopes, &c.ToolPolicyMode,
		&c.RateLimitRPM, &c.RateLimitBurst, &c.CreatedAt, &c.CreatedBy, &c.RevokedAt,
	)
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, ErrClientNameConflict
		}
		if pgxutil.IsCheckViolation(err) {
			return nil, ErrInvalidInput
		}
		return nil, err
	}
	return ptrext.Of(c), nil
}

// GetByID retrieves a client by ID.
func (r *ClientsRepo) GetByID(ctx context.Context, id uuid.UUID) (*Client, error) {
	const q = `
		SELECT id, tenant_id, name, redirect_uris, scopes, tool_policy_mode,
		       rate_limit_rpm, rate_limit_burst, created_at, created_by, revoked_at
		FROM mcp_oauth_clients
		WHERE id = $1
	`
	var c Client
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.TenantID, &c.Name, &c.RedirectURIs, &c.Scopes, &c.ToolPolicyMode,
		&c.RateLimitRPM, &c.RateLimitBurst, &c.CreatedAt, &c.CreatedBy, &c.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClientNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(c), nil
}

// GetActiveByID retrieves an active (non-revoked) client by ID.
func (r *ClientsRepo) GetActiveByID(ctx context.Context, id uuid.UUID) (*Client, error) {
	const q = `
		SELECT id, tenant_id, name, redirect_uris, scopes, tool_policy_mode,
		       rate_limit_rpm, rate_limit_burst, created_at, created_by, revoked_at
		FROM mcp_oauth_clients
		WHERE id = $1 AND revoked_at IS NULL
	`
	var c Client
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.TenantID, &c.Name, &c.RedirectURIs, &c.Scopes, &c.ToolPolicyMode,
		&c.RateLimitRPM, &c.RateLimitBurst, &c.CreatedAt, &c.CreatedBy, &c.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClientNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(c), nil
}

// ListByTenant returns all clients for a tenant.
func (r *ClientsRepo) ListByTenant(ctx context.Context, tenantID string) ([]Client, error) {
	const q = `
		SELECT id, tenant_id, name, redirect_uris, scopes, tool_policy_mode,
		       rate_limit_rpm, rate_limit_burst, created_at, created_by, revoked_at
		FROM mcp_oauth_clients
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []Client
	for rows.Next() {
		var c Client
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.Name, &c.RedirectURIs, &c.Scopes, &c.ToolPolicyMode,
			&c.RateLimitRPM, &c.RateLimitBurst, &c.CreatedAt, &c.CreatedBy, &c.RevokedAt,
		); err != nil {
			return nil, err
		}
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

// Revoke marks a client as revoked.
func (r *ClientsRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE mcp_oauth_clients SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrClientNotFound
	}
	return nil
}

// UpdateGovernance updates mutable governance settings for one client.
func (r *ClientsRepo) UpdateGovernance(ctx context.Context, p UpdateClientGovernanceParams) (*Client, error) {
	const q = `
		UPDATE mcp_oauth_clients
		SET tool_policy_mode = $2,
		    rate_limit_rpm = $3,
		    rate_limit_burst = $4
		WHERE id = $1
		  AND revoked_at IS NULL
		RETURNING id, tenant_id, name, redirect_uris, scopes, tool_policy_mode,
		          rate_limit_rpm, rate_limit_burst, created_at, created_by, revoked_at
	`
	var c Client
	err := r.pool.QueryRow(ctx, q, p.ID, p.ToolPolicyMode, p.RateLimitRPM, p.RateLimitBurst).Scan(
		&c.ID, &c.TenantID, &c.Name, &c.RedirectURIs, &c.Scopes, &c.ToolPolicyMode,
		&c.RateLimitRPM, &c.RateLimitBurst, &c.CreatedAt, &c.CreatedBy, &c.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClientNotFound
	}
	if err != nil {
		if pgxutil.IsCheckViolation(err) {
			return nil, ErrInvalidInput
		}
		return nil, err
	}
	return ptrext.Of(c), nil
}

// IsRevoked checks if a client has been revoked.
func (r *ClientsRepo) IsRevoked(ctx context.Context, id uuid.UUID) (bool, error) {
	const q = `SELECT revoked_at IS NOT NULL FROM mcp_oauth_clients WHERE id = $1`
	var revoked bool
	err := r.pool.QueryRow(ctx, q, id).Scan(&revoked)
	if err != nil {
		return false, err
	}
	return revoked, nil
}
