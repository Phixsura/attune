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

var ErrToolPolicyNotFound = errors.New("mcp tool policy not found")

// ToolPolicy is one per-client per-tool governance override.
type ToolPolicy struct {
	ClientID       uuid.UUID
	ToolName       string
	Effect         string
	RateLimitRPM   *int
	RateLimitBurst *int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// UpsertToolPolicyParams holds parameters for creating or replacing a tool policy.
type UpsertToolPolicyParams struct {
	ClientID       uuid.UUID
	ToolName       string
	Effect         string
	RateLimitRPM   *int
	RateLimitBurst *int
}

// ToolPoliciesRepo handles MCP per-tool governance overrides.
type ToolPoliciesRepo struct {
	pool *pgxpool.Pool
}

// NewToolPolicies creates a new ToolPoliciesRepo.
func NewToolPolicies(pool *pgxpool.Pool) *ToolPoliciesRepo {
	return ptrext.Of(ToolPoliciesRepo{pool: pool})
}

// GetByClientAndTool returns the override for a specific client/tool pair.
func (r *ToolPoliciesRepo) GetByClientAndTool(ctx context.Context, clientID uuid.UUID, toolName string) (*ToolPolicy, error) {
	const q = `
		SELECT client_id, tool_name, effect, rate_limit_rpm, rate_limit_burst, created_at, updated_at
		FROM mcp_client_tool_policies
		WHERE client_id = $1 AND tool_name = $2
	`
	var p ToolPolicy
	err := r.pool.QueryRow(ctx, q, clientID, toolName).Scan(
		&p.ClientID, &p.ToolName, &p.Effect, &p.RateLimitRPM, &p.RateLimitBurst, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrToolPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(p), nil
}

// ListByClient returns all explicit tool policies for one client.
func (r *ToolPoliciesRepo) ListByClient(ctx context.Context, clientID uuid.UUID) ([]ToolPolicy, error) {
	const q = `
		SELECT client_id, tool_name, effect, rate_limit_rpm, rate_limit_burst, created_at, updated_at
		FROM mcp_client_tool_policies
		WHERE client_id = $1
		ORDER BY tool_name ASC
	`
	rows, err := r.pool.Query(ctx, q, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ToolPolicy
	for rows.Next() {
		var p ToolPolicy
		if err := rows.Scan(
			&p.ClientID, &p.ToolName, &p.Effect, &p.RateLimitRPM, &p.RateLimitBurst, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Upsert creates or updates one client/tool policy row.
func (r *ToolPoliciesRepo) Upsert(ctx context.Context, p UpsertToolPolicyParams) (*ToolPolicy, error) {
	const q = `
		INSERT INTO mcp_client_tool_policies
		    (client_id, tool_name, effect, rate_limit_rpm, rate_limit_burst)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (client_id, tool_name)
		DO UPDATE
		SET effect = EXCLUDED.effect,
		    rate_limit_rpm = EXCLUDED.rate_limit_rpm,
		    rate_limit_burst = EXCLUDED.rate_limit_burst,
		    updated_at = NOW()
		RETURNING client_id, tool_name, effect, rate_limit_rpm, rate_limit_burst, created_at, updated_at
	`
	var out ToolPolicy
	err := r.pool.QueryRow(ctx, q, p.ClientID, p.ToolName, p.Effect, p.RateLimitRPM, p.RateLimitBurst).Scan(
		&out.ClientID, &out.ToolName, &out.Effect, &out.RateLimitRPM, &out.RateLimitBurst, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(out), nil
}

// ReplaceByClient atomically replaces all explicit policies for one client.
func (r *ToolPoliciesRepo) ReplaceByClient(ctx context.Context, clientID uuid.UUID, policies []UpsertToolPolicyParams) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `DELETE FROM mcp_client_tool_policies WHERE client_id = $1`, clientID); err != nil {
		return err
	}

	if len(policies) > 0 {
		const q = `
			INSERT INTO mcp_client_tool_policies
			    (client_id, tool_name, effect, rate_limit_rpm, rate_limit_burst)
			VALUES ($1, $2, $3, $4, $5)
		`
		for _, p := range policies {
			if _, err := tx.Exec(ctx, q, p.ClientID, p.ToolName, p.Effect, p.RateLimitRPM, p.RateLimitBurst); err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}
