// SPDX-License-Identifier: Apache-2.0

// Package digestsubscription owns the digest_subscriptions table — the
// per-tenant scheduled roll-up config (#27). v1 allows one subscription per
// tenant (UNIQUE tenant_id); the entity generalizes to many later. The digest
// scheduler reads FindDue + AdvanceCursorTx; Console drives Upsert / GetByTenant
// / DeleteByTenant.
package digestsubscription

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrNotFound signals the tenant has no digest subscription.
var ErrNotFound = errors.New("digest subscription not found")

// Subscription is one digest_subscriptions row. Nullable columns are pointers:
// Byweekday (only meaningful for frequency='weekly'), Timezone (override; nil =>
// use the tenant's timezone), ThemePrompt (LLM naming override), NextRunAt /
// LastRunAt (scheduler cursor).
type Subscription struct {
	ID             uuid.UUID
	TenantID       string
	Enabled        bool
	Frequency      string // 'daily' | 'weekly'
	SendHour       int    // tenant-local hour 0..23
	Byweekday      *int   // Go time.Weekday (Sun=0); weekly only
	Timezone       *string
	LLMMinFeedback int
	SendOnEmpty    bool
	ThemePrompt    *string
	NextRunAt      *time.Time
	LastRunAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DueSubscription is a Subscription plus the resolved IANA timezone (the
// subscription override, else the tenant default) the scheduler needs.
type DueSubscription struct {
	Subscription
	ResolvedTimezone string
}

// Repo wraps the digest_subscriptions table.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

const selectColumns = `id, tenant_id, enabled, frequency, send_hour, byweekday, timezone,
	llm_min_feedback, send_on_empty, theme_prompt, next_run_at, last_run_at, created_at, updated_at`

func scanSubscription(row pgx.Row, s *Subscription) error {
	return row.Scan(
		&s.ID, &s.TenantID, &s.Enabled, &s.Frequency, &s.SendHour, &s.Byweekday, &s.Timezone,
		&s.LLMMinFeedback, &s.SendOnEmpty, &s.ThemePrompt, &s.NextRunAt, &s.LastRunAt,
		&s.CreatedAt, &s.UpdatedAt,
	)
}

// GetByTenant returns the tenant's digest subscription, or ErrNotFound.
func (r *Repo) GetByTenant(ctx context.Context, tenantID string) (*Subscription, error) {
	var s Subscription
	err := scanSubscription(
		r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM digest_subscriptions WHERE tenant_id = $1`, tenantID),
		&s, // ptrext:allow scan-target
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get digest subscription: %w", err)
	}
	return ptrext.Of(s), nil
}

// Upsert inserts or replaces the tenant's subscription (UNIQUE tenant_id). The
// caller computes NextRunAt (via service/digest.NextRun) so a fresh or rescheduled
// subscription fires at the right local time rather than immediately.
func (r *Repo) Upsert(ctx context.Context, s Subscription) (*Subscription, error) {
	var out Subscription
	err := scanSubscription(r.pool.QueryRow(ctx, `
		INSERT INTO digest_subscriptions
		 (tenant_id, enabled, frequency, send_hour, byweekday, timezone,
		  llm_min_feedback, send_on_empty, theme_prompt, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id) DO UPDATE SET
		 enabled = EXCLUDED.enabled,
		 frequency = EXCLUDED.frequency,
		 send_hour = EXCLUDED.send_hour,
		 byweekday = EXCLUDED.byweekday,
		 timezone = EXCLUDED.timezone,
		 llm_min_feedback = EXCLUDED.llm_min_feedback,
		 send_on_empty = EXCLUDED.send_on_empty,
		 theme_prompt = EXCLUDED.theme_prompt,
		 next_run_at = EXCLUDED.next_run_at,
		 updated_at = NOW()
		RETURNING `+selectColumns,
		s.TenantID, s.Enabled, s.Frequency, s.SendHour, s.Byweekday, s.Timezone,
		s.LLMMinFeedback, s.SendOnEmpty, s.ThemePrompt, s.NextRunAt,
	), &out) // ptrext:allow scan-target
	if err != nil {
		return nil, fmt.Errorf("upsert digest subscription: %w", err)
	}
	return ptrext.Of(out), nil
}

// DeleteByTenant removes the tenant's subscription. ErrNotFound when absent.
func (r *Repo) DeleteByTenant(ctx context.Context, tenantID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM digest_subscriptions WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return fmt.Errorf("delete digest subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FindDue returns enabled subscriptions whose next_run_at has passed (NULL =>
// never scheduled => due), each carrying the resolved IANA timezone.
func (r *Repo) FindDue(ctx context.Context, now time.Time) ([]DueSubscription, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.tenant_id, s.enabled, s.frequency, s.send_hour, s.byweekday, s.timezone,
		 s.llm_min_feedback, s.send_on_empty, s.theme_prompt, s.next_run_at, s.last_run_at,
		 s.created_at, s.updated_at, COALESCE(s.timezone, t.timezone) AS resolved_tz
		FROM digest_subscriptions s
		JOIN tenants t ON t.id = s.tenant_id
		WHERE s.enabled = TRUE AND (s.next_run_at IS NULL OR s.next_run_at <= $1)
		ORDER BY s.next_run_at NULLS FIRST`, now)
	if err != nil {
		return nil, fmt.Errorf("find due digest subscriptions: %w", err)
	}
	defer rows.Close()

	var out []DueSubscription
	for rows.Next() {
		var d DueSubscription
		s := &d.Subscription // ptrext:allow scan-target
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.Enabled, &s.Frequency, &s.SendHour, &s.Byweekday, &s.Timezone,
			&s.LLMMinFeedback, &s.SendOnEmpty, &s.ThemePrompt, &s.NextRunAt, &s.LastRunAt,
			&s.CreatedAt, &s.UpdatedAt, &d.ResolvedTimezone,
		); err != nil {
			return nil, fmt.Errorf("scan due digest subscription: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetResolved returns the tenant's subscription with the resolved IANA timezone
// (override else tenant default). Used by the worker to process a claimed run.
func (r *Repo) GetResolved(ctx context.Context, tenantID string) (*DueSubscription, error) {
	var d DueSubscription
	s := &d.Subscription // ptrext:allow scan-target
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.tenant_id, s.enabled, s.frequency, s.send_hour, s.byweekday, s.timezone,
		 s.llm_min_feedback, s.send_on_empty, s.theme_prompt, s.next_run_at, s.last_run_at,
		 s.created_at, s.updated_at, COALESCE(s.timezone, t.timezone) AS resolved_tz
		FROM digest_subscriptions s
		JOIN tenants t ON t.id = s.tenant_id
		WHERE s.tenant_id = $1`, tenantID).Scan(
		&s.ID, &s.TenantID, &s.Enabled, &s.Frequency, &s.SendHour, &s.Byweekday, &s.Timezone,
		&s.LLMMinFeedback, &s.SendOnEmpty, &s.ThemePrompt, &s.NextRunAt, &s.LastRunAt,
		&s.CreatedAt, &s.UpdatedAt, &d.ResolvedTimezone,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get resolved digest subscription: %w", err)
	}
	return ptrext.Of(d), nil
}

// BeginTx opens a transaction so the scheduler can claim a run and advance the
// cursor atomically.
func (r *Repo) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

// AdvanceCursorTx moves next_run_at forward (computed by the caller) and stamps
// last_run_at. Runs inside the same tx as the digest_runs claim so a duplicate
// tick or a second replica can't double-schedule.
func (r *Repo) AdvanceCursorTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, nextRunAt time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE digest_subscriptions
		SET next_run_at = $2, last_run_at = NOW(), updated_at = NOW()
		WHERE id = $1`, id, nextRunAt)
	if err != nil {
		return fmt.Errorf("advance digest cursor: %w", err)
	}
	return nil
}
