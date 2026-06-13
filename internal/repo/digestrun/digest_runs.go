// SPDX-License-Identifier: Apache-2.0

// Package digestrun owns the digest_runs ledger (#27) — the exactly-once guard
// for daily digests, one row per (tenant, local run_date). ClaimDay is the
// scheduler's "win the day" insert; TryClaim / Mark* / ResetStaleClaims are the
// taskoutbox-shaped processing machinery the digest worker drains.
package digestrun

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

// ErrNoRun is returned by TryClaim when no run is ready to process.
var ErrNoRun = errors.New("no digest run to claim")

// Run is one digest_runs row (the fields the worker needs).
type Run struct {
	ID             int64
	TenantID       string
	SubscriptionID uuid.UUID
	RunDate        time.Time
	Status         string
	Attempts       int
}

// Repo wraps the digest_runs table.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

// ClaimDay inserts a pending run for (tenant, runDate) inside tx. Returns
// (id, true) when this caller won the local day; (0, false) on conflict (another
// tick or replica already claimed it). runDate must be the date in the tenant's
// local timezone.
func (r *Repo) ClaimDay(
	ctx context.Context, tx pgx.Tx, tenantID string, subID uuid.UUID, runDate time.Time,
) (int64, bool, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO digest_runs (tenant_id, subscription_id, run_date, status)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (tenant_id, run_date) DO NOTHING
		RETURNING id`, tenantID, subID, runDate).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("claim digest day: %w", err)
	}
	return id, true, nil
}

// TryClaim atomically claims one runnable digest_run — pending, retry-due failed,
// or stale processing — and flips it to 'processing'. ErrNoRun when none.
// FOR UPDATE SKIP LOCKED keeps concurrent workers/replicas off the same row.
func (r *Repo) TryClaim(ctx context.Context, staleDuration time.Duration) (*Run, error) {
	var run Run
	err := r.pool.QueryRow(ctx, `
		UPDATE digest_runs
		SET status = 'processing', claimed_at = NOW(), attempts = attempts + 1
		WHERE id = (
			SELECT id FROM digest_runs
			WHERE status = 'pending'
			   OR (status = 'failed' AND next_retry_at <= NOW())
			   OR (status = 'processing' AND claimed_at < NOW() - make_interval(secs => $1))
			ORDER BY created_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, tenant_id, subscription_id, run_date, status, attempts`,
		staleDuration.Seconds(),
	).Scan(&run.ID, &run.TenantID, &run.SubscriptionID, &run.RunDate, &run.Status, &run.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoRun
	}
	if err != nil {
		return nil, fmt.Errorf("claim digest run: %w", err)
	}
	return ptrext.Of(run), nil
}

// MarkSent records a delivered digest with its window counts.
func (r *Repo) MarkSent(ctx context.Context, id int64, feedbackCount, themeCount int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE digest_runs
		SET status = 'sent', feedback_count = $2, theme_count = $3,
		    completed_at = NOW(), last_error = ''
		WHERE id = $1`, id, feedbackCount, themeCount)
	if err != nil {
		return fmt.Errorf("mark digest run sent: %w", err)
	}
	return nil
}

// MarkSkippedEmpty records a run that had nothing to send (below threshold and
// not opted into empty digests). It still completes the day so it never re-fires.
func (r *Repo) MarkSkippedEmpty(ctx context.Context, id int64, feedbackCount int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE digest_runs
		SET status = 'skipped_empty', feedback_count = $2,
		    completed_at = NOW(), last_error = ''
		WHERE id = $1`, id, feedbackCount)
	if err != nil {
		return fmt.Errorf("mark digest run skipped: %w", err)
	}
	return nil
}

// MarkFailed records an attempt failure with the taskoutbox exponential backoff
// (30s · 2^(attempts-1)); once attempts hit maxAttempts it stays failed with a
// NULL next_retry_at, which TryClaim never re-selects (terminal).
func (r *Repo) MarkFailed(ctx context.Context, id int64, lastErr error, maxAttempts int) error {
	errMsg := ""
	if lastErr != nil {
		errMsg = lastErr.Error()
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE digest_runs
		SET status = 'failed',
		    next_retry_at = CASE
		        WHEN attempts >= $3 THEN NULL
		        ELSE NOW() + (INTERVAL '30 seconds' * POWER(2, attempts - 1))
		    END,
		    claimed_at = NULL,
		    last_error = $2
		WHERE id = $1`, id, errMsg, maxAttempts)
	if err != nil {
		return fmt.Errorf("mark digest run failed: %w", err)
	}
	return nil
}

// ResetStaleClaims returns runs stuck in 'processing' past staleDuration to
// 'pending'. Called on worker boot to recover crashed claims.
func (r *Repo) ResetStaleClaims(ctx context.Context, staleDuration time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE digest_runs
		SET status = 'pending', claimed_at = NULL
		WHERE status = 'processing' AND claimed_at < NOW() - make_interval(secs => $1)`,
		staleDuration.Seconds())
	if err != nil {
		return 0, fmt.Errorf("reset stale digest claims: %w", err)
	}
	return tag.RowsAffected(), nil
}
