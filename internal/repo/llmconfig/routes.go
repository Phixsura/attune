// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func (r *Repo) ListRoutes(ctx context.Context) ([]Route, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, purpose, logical_model, enabled, created_at, updated_at
		  FROM llm_routes
		 ORDER BY tenant_id ASC, purpose ASC`)
	if err != nil {
		return nil, fmt.Errorf("list llm routes: %w", err)
	}
	defer rows.Close()
	var out []Route
	for rows.Next() {
		row, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repo) UpsertRoute(ctx context.Context, route Route) (*Route, error) {
	row, err := scanRoute(r.pool.QueryRow(ctx, `
		INSERT INTO llm_routes (tenant_id, purpose, logical_model, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, purpose) DO UPDATE SET
		 logical_model = EXCLUDED.logical_model,
		 enabled = EXCLUDED.enabled,
		 updated_at = NOW()
		RETURNING id, tenant_id, purpose, logical_model, enabled, created_at, updated_at`,
		route.TenantID, route.Purpose, route.LogicalModel, route.Enabled,
	))
	if err != nil {
		return nil, fmt.Errorf("upsert llm route: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) DeleteRoute(ctx context.Context, tenantID, purpose string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM llm_routes WHERE tenant_id = $1 AND purpose = $2`,
		tenantID, purpose,
	)
	if err != nil {
		return fmt.Errorf("delete llm route: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRouteNotFound
	}
	return nil
}

func (r *Repo) TenantExists(ctx context.Context, tenantID string) (bool, error) {
	if tenantID == "" {
		return true, nil
	}
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1 AND is_active = TRUE)`,
		tenantID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check tenant exists: %w", err)
	}
	return exists, nil
}

func (r *Repo) ResolveCandidates(ctx context.Context, tenantID, purpose string) ([]Candidate, error) {
	rows, err := r.pool.Query(ctx, `
		WITH picked_route AS (
			SELECT id, tenant_id, purpose, logical_model, enabled, created_at, updated_at
			  FROM llm_routes
			 WHERE purpose = $2
			   AND (tenant_id = $1 OR tenant_id = '')
			 ORDER BY CASE WHEN tenant_id = $1 THEN 0 ELSE 1 END, updated_at DESC
			 LIMIT 1
		)
		SELECT
		 r.id, r.tenant_id, r.purpose, r.logical_model, r.enabled, r.created_at, r.updated_at,
		 c.id, c.name, c.protocol, c.base_url, c.auth_mode, c.credential_key_id,
		 c.credential_ciphertext, c.status, c.priority, c.weight, c.timeout_seconds,
		 c.created_at, c.updated_at, c.last_tested_at, c.last_test_status, c.last_error,
		 a.id, a.channel_id, a.logical_model, a.provider_model, a.enabled,
		 a.priority, a.weight, a.created_at, a.updated_at
		  FROM picked_route r
		  JOIN llm_channel_abilities a
		    ON a.logical_model = r.logical_model
		   AND r.enabled = TRUE
		   AND a.enabled = TRUE
		  JOIN llm_channels c
		    ON c.id = a.channel_id
		   AND c.status = 'enabled'
		   AND (
		       c.auth_mode = 'none'
		       OR (c.credential_key_id <> '' AND c.credential_ciphertext IS NOT NULL AND octet_length(c.credential_ciphertext) > 0)
		   )
		 ORDER BY a.priority DESC, c.priority DESC, a.weight DESC, c.weight DESC, c.name ASC`,
		tenantID, purpose,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve llm candidates: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		candidate, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNoCandidates
	}
	return out, nil
}

type routeScanner interface {
	Scan(dest ...any) error
}

func scanRoute(row routeScanner) (Route, error) {
	var route Route
	err := row.Scan(
		&route.ID, &route.TenantID, &route.Purpose, &route.LogicalModel,
		&route.Enabled, &route.CreatedAt, &route.UpdatedAt,
	)
	return route, err
}

func scanCandidate(row routeScanner) (Candidate, error) {
	var candidate Candidate
	err := row.Scan(
		&candidate.Route.ID,
		&candidate.Route.TenantID,
		&candidate.Route.Purpose,
		&candidate.Route.LogicalModel,
		&candidate.Route.Enabled,
		&candidate.Route.CreatedAt,
		&candidate.Route.UpdatedAt,
		&candidate.Channel.ID,
		&candidate.Channel.Name,
		&candidate.Channel.Protocol,
		&candidate.Channel.BaseURL,
		&candidate.Channel.AuthMode,
		&candidate.Channel.CredentialKeyID,
		&candidate.Channel.CredentialCiphertext,
		&candidate.Channel.Status,
		&candidate.Channel.Priority,
		&candidate.Channel.Weight,
		&candidate.Channel.TimeoutSeconds,
		&candidate.Channel.CreatedAt,
		&candidate.Channel.UpdatedAt,
		&candidate.Channel.LastTestedAt,
		&candidate.Channel.LastTestStatus,
		&candidate.Channel.LastError,
		&candidate.Ability.ID,
		&candidate.Ability.ChannelID,
		&candidate.Ability.LogicalModel,
		&candidate.Ability.ProviderModel,
		&candidate.Ability.Enabled,
		&candidate.Ability.Priority,
		&candidate.Ability.Weight,
		&candidate.Ability.CreatedAt,
		&candidate.Ability.UpdatedAt,
	)
	return candidate, err
}
