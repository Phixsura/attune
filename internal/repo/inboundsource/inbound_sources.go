// SPDX-License-Identifier: Apache-2.0

// Package inboundsource is the DB-backed implementation of
// inbound.SourceStore. It belongs to the repo layer and is wired into
// inbound.Deps by cmd/attune; the framework itself imports only the
// SourceStore interface, never this package.
package inboundsource

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrNotFound surfaces from Get / GetBySlugs when no row matches.
var ErrNotFound = errors.New("inbound_source: not found")

// Repo is the inbound_sources-table repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo wires a Repo against the given pool.
func NewRepo(p *pgxpool.Pool) *Repo { return ptrext.Of(Repo{pool: p}) }

// List returns all enabled rows for the given channel.
func (r *Repo) List(ctx context.Context, channel string) ([]inbound.Source, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, channel, name, slug, config, enabled,
		        last_event_at, last_uid, last_error, created_at, updated_at
		   FROM inbound_sources
		  WHERE channel = $1 AND enabled = TRUE`,
		channel,
	)
	if err != nil {
		return nil, fmt.Errorf("inboundsource.List: %w", err)
	}
	defer rows.Close()
	var out []inbound.Source
	for rows.Next() {
		s, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Get fetches a single row by ID.
func (r *Repo) Get(ctx context.Context, id string) (inbound.Source, error) {
	return r.scanOne(ctx,
		`SELECT id, tenant_id, channel, name, slug, config, enabled,
		        last_event_at, last_uid, last_error, created_at, updated_at
		   FROM inbound_sources WHERE id = $1`, id)
}

// GetBySlugs resolves (tenantSlug, channel, sourceSlug) — the URL form
// adapters see in their HTTP routes.
func (r *Repo) GetBySlugs(ctx context.Context, tenantSlug, channel, sourceSlug string) (inbound.Source, error) {
	return r.scanOne(ctx,
		`SELECT s.id, s.tenant_id, s.channel, s.name, s.slug, s.config, s.enabled,
		        s.last_event_at, s.last_uid, s.last_error, s.created_at, s.updated_at
		   FROM inbound_sources s
		   JOIN tenants t ON t.id = s.tenant_id
		  WHERE t.slug = $1 AND s.channel = $2 AND s.slug = $3`,
		tenantSlug, channel, sourceSlug)
}

// UpdateState writes back the per-poll-cycle state columns.
func (r *Repo) UpdateState(ctx context.Context, id string, state inbound.SourceState) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE inbound_sources
		    SET last_event_at = $2,
		        last_uid      = $3,
		        last_error    = $4,
		        updated_at    = now()
		  WHERE id = $1`,
		id, state.LastEventAt, state.LastUID, nilIfEmpty(state.LastError),
	)
	return err
}

// SetEnabled toggles the active flag, optionally recording a reason on
// disable.
func (r *Repo) SetEnabled(ctx context.Context, id string, enabled bool, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE inbound_sources
		    SET enabled = $2,
		        last_error = CASE WHEN $2 THEN NULL ELSE $3 END,
		        updated_at = now()
		  WHERE id = $1`,
		id, enabled, nilIfEmpty(reason),
	)
	return err
}

func (r *Repo) scanOne(ctx context.Context, sql string, args ...any) (inbound.Source, error) {
	row := r.pool.QueryRow(ctx, sql, args...)
	s, err := scanRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return inbound.Source{}, ErrNotFound
	}
	if err != nil {
		return inbound.Source{}, fmt.Errorf("inboundsource.scanOne: %w", err)
	}
	return s, nil
}

// rowScanner is the small read surface both pgx.Row and pgx.Rows expose.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(r rowScanner) (inbound.Source, error) {
	var s inbound.Source
	var lastEventAt *time.Time
	var lastError *string
	if err := r.Scan(&s.ID, &s.TenantID, &s.Channel, &s.Name, &s.Slug,
		&s.Config, &s.Enabled, &lastEventAt, &s.State.LastUID, &lastError,
		&s.CreatedAt, &s.UpdatedAt); err != nil {
		return inbound.Source{}, err
	}
	s.State.LastEventAt = lastEventAt
	s.State.LastError = ptrext.Indirect(lastError)
	return s, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return ptrext.Of(s)
}

// Compile-time assertion that Repo satisfies inbound.SourceStore.
var _ inbound.SourceStore = (*Repo)(nil)
