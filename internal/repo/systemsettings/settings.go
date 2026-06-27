// SPDX-License-Identifier: Apache-2.0

// Package systemsettings manages runtime system configuration stored in
// the database (#158).
package systemsettings

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Well-known setting keys.
const (
	KeyAuthMode = "auth.mode"
)

// ErrNotFound — setting not found.
var ErrNotFound = errors.New("systemsettings: not found")

// Repo is the system_settings table repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo wires a Repo against the given pool.
func NewRepo(p *pgxpool.Pool) *Repo { return ptrext.Of(Repo{pool: p}) }

// Get retrieves a setting value by key.
func (r *Repo) Get(ctx context.Context, tenantID, key string) (string, error) {
	var value string
	err := r.pool.QueryRow(ctx,
		`SELECT value FROM system_settings WHERE tenant_id = $1 AND key = $2`,
		tenantID, key,
	).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("systemsettings.Get: %w", err)
	}
	return value, nil
}

// Set upserts a setting value.
func (r *Repo) Set(ctx context.Context, tenantID, key, value, updatedBy string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO system_settings (tenant_id, key, value, updated_at, updated_by)
		 VALUES ($1, $2, $3, NOW(), $4)
		 ON CONFLICT (tenant_id, key) DO UPDATE
		 SET value = EXCLUDED.value, updated_at = NOW(), updated_by = EXCLUDED.updated_by`,
		tenantID, key, value, updatedBy,
	)
	if err != nil {
		return fmt.Errorf("systemsettings.Set: %w", err)
	}
	return nil
}

// GetAuthMode retrieves the auth mode for a tenant, defaulting to hybrid.
func (r *Repo) GetAuthMode(ctx context.Context, tenantID string) (domain.AuthMode, error) {
	value, err := r.Get(ctx, tenantID, KeyAuthMode)
	if errors.Is(err, ErrNotFound) {
		return domain.AuthModeHybrid, nil
	}
	if err != nil {
		return "", err
	}
	mode := domain.AuthMode(value)
	if !mode.Valid() {
		return domain.AuthModeHybrid, nil
	}
	return mode, nil
}

// SetAuthMode sets the auth mode for a tenant.
func (r *Repo) SetAuthMode(ctx context.Context, tenantID string, mode domain.AuthMode, updatedBy string) error {
	if !mode.Valid() {
		return fmt.Errorf("systemsettings.SetAuthMode: invalid mode %q", mode)
	}
	return r.Set(ctx, tenantID, KeyAuthMode, string(mode), updatedBy)
}
