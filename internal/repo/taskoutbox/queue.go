// SPDX-License-Identifier: Apache-2.0

// Package taskoutbox is a generic per-feedback outbox queue. It owns the
// claim/retry/stale-reset machinery shared by every enrichment-style async
// task table (embedding_task, reply_draft_task, …) so the SQL is written
// once. Domain-specific enqueue and result writes stay in the owning repo.
package taskoutbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrNoTask is returned by TryClaim when no eligible task is available.
var ErrNoTask = errors.New("no task to claim")

// Task is one outbox row. All task tables share this shape; the owning repo
// may alias it (`type Task = taskoutbox.Task`) so its callers stays unchanged.
type Task struct {
	ID          int64
	FeedbackID  int64
	TenantID    string
	Status      string
	Attempts    int
	ClaimedAt   *time.Time
	ClaimedBy   string // worker instance that holds the claim; empty if unclaimed
	NextRetryAt *time.Time
	LastError   string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

// Queue wraps one outbox table. table and enableColumn are controlled
// internal constants supplied by the owning repo — never user input — and
// are interpolated into SQL with fmt.Sprintf. enableColumn is a boolean
// column on `tenants`; only tasks whose tenant has it TRUE are claimable.
type Queue struct {
	pool         *pgxpool.Pool
	table        string
	enableColumn string
}

func New(pool *pgxpool.Pool, table, enableColumn string) *Queue {
	return ptrext.Of(Queue{pool: pool, table: table, enableColumn: enableColumn})
}

// TryClaim atomically claims one pending (or retry-due, or stale-processing)
// task whose tenant has the enable column set. Returns ErrNoTask when none
// is available. Uses FOR UPDATE SKIP LOCKED so concurrent workers don't
// contend.
func (q *Queue) TryClaim(ctx context.Context, staleDuration time.Duration) (*Task, error) {
	return q.TryClaimWithOwner(ctx, staleDuration, "")
}

// TryClaimWithOwner is like TryClaim but also sets claimed_by to owner.
// This enables heartbeat refresh to only touch rows this worker instance
// still holds — preventing replica A from accidentally extending a row
// that replica B legitimately re-claimed after A's lease expired.
func (q *Queue) TryClaimWithOwner(ctx context.Context, staleDuration time.Duration, owner string) (*Task, error) {
	sql := fmt.Sprintf(`
		UPDATE %[1]s
		SET status = 'processing',
		    claimed_at = NOW(),
		    claimed_by = $2,
		    attempts = attempts + 1
		WHERE id = (
			SELECT t.id FROM %[1]s t
			JOIN tenants tn ON tn.id = t.tenant_id AND tn.%[2]s = TRUE
			WHERE (t.status = 'pending' OR (t.status = 'failed' AND t.next_retry_at <= NOW()))
			  OR (t.status = 'processing' AND t.claimed_at < NOW() - make_interval(secs => $1))
			ORDER BY t.created_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, feedback_id, tenant_id, status, attempts, claimed_at,
		          COALESCE(claimed_by, ''), next_retry_at, last_error, created_at, completed_at`,
		q.table, q.enableColumn)
	task, err := scanTask(q.pool.QueryRow(ctx, sql, staleDuration.Seconds(), owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoTask
	}
	if err != nil {
		return nil, fmt.Errorf("try claim %s: %w", q.table, err)
	}
	return ptrext.Of(task), nil
}

// MarkDone marks a task completed and releases the claim.
// Returns the number of rows updated (0 if the task was re-claimed by another worker).
func (q *Queue) MarkDone(ctx context.Context, taskID int64, owner string) (int64, error) {
	sql := fmt.Sprintf(`
		UPDATE %s
		SET status = 'done', completed_at = NOW(), last_error = '',
		    claimed_at = NULL, claimed_by = NULL
		WHERE id = $1 AND claimed_by = $2`, q.table)
	tag, err := q.pool.Exec(ctx, sql, taskID, owner)
	if err != nil {
		return 0, fmt.Errorf("mark %s done: %w", q.table, err)
	}
	return tag.RowsAffected(), nil
}

// MarkFailed records an attempt failure and releases the claim. The task stays
// in 'failed': while attempts < maxAttempts it carries a future next_retry_at
// (exponential backoff 30s · 2^(attempts-1)) so TryClaim's `status='failed' AND
// next_retry_at<=NOW()` branch re-claims it only once the backoff window passes;
// once exhausted it keeps status='failed' with next_retry_at=NULL, which that
// predicate never matches (a NULL comparison is never true) — making it terminal.
// claimed_at and claimed_by are cleared so a failed row no longer carries a
// stale claim.
//
// Retries deliberately stay 'failed' rather than returning to 'pending': the
// pending claim branch is unconditional (no next_retry_at gate), so a 'pending'
// retry would be re-claimed on the very next poll and the backoff would never
// take effect. This mirrors the proven enrichment retry path in
// internal/repo/feedback.
//
// Returns the number of rows updated (0 if the task was re-claimed by another worker).
func (q *Queue) MarkFailed(ctx context.Context, taskID int64, owner string, lastErr error, maxAttempts int) (int64, error) {
	errMsg := ""
	if lastErr != nil {
		errMsg = lastErr.Error()
	}
	sql := fmt.Sprintf(`
		UPDATE %s
		SET status = 'failed',
		    next_retry_at = CASE
		        WHEN attempts >= $4 THEN NULL
		        ELSE NOW() + (INTERVAL '30 seconds' * POWER(2, attempts - 1))
		    END,
		    claimed_at = NULL,
		    claimed_by = NULL,
		    last_error = $2
		WHERE id = $1 AND claimed_by = $3`, q.table)
	tag, err := q.pool.Exec(ctx, sql, taskID, errMsg, owner, maxAttempts)
	if err != nil {
		return 0, fmt.Errorf("mark %s failed: %w", q.table, err)
	}
	return tag.RowsAffected(), nil
}

// ResetStaleClaims returns tasks stuck in 'processing' past staleDuration to
// 'pending'. Called on worker boot to recover from crashed claims.
func (q *Queue) ResetStaleClaims(ctx context.Context, staleDuration time.Duration) (int64, error) {
	sql := fmt.Sprintf(`
		UPDATE %s
		SET status = 'pending', claimed_at = NULL, claimed_by = NULL
		WHERE status = 'processing'
		  AND claimed_at < NOW() - make_interval(secs => $1)`, q.table)
	tag, err := q.pool.Exec(ctx, sql, staleDuration.Seconds())
	if err != nil {
		return 0, fmt.Errorf("reset stale %s claims: %w", q.table, err)
	}
	return tag.RowsAffected(), nil
}

// RefreshClaim extends the lease on the given task by updating claimed_at.
// The claimed_by filter ensures this worker only extends its own claims —
// if another replica re-claimed the task after this worker's lease expired,
// this call safely no-ops (returns 0 rows affected). Returns the number of
// rows actually refreshed (0 or 1).
func (q *Queue) RefreshClaim(ctx context.Context, taskID int64, owner string) (int64, error) {
	sql := fmt.Sprintf(`
		UPDATE %s
		SET claimed_at = NOW()
		WHERE id = $1
		  AND claimed_by = $2
		  AND status = 'processing'`, q.table)
	tag, err := q.pool.Exec(ctx, sql, taskID, owner)
	if err != nil {
		return 0, fmt.Errorf("refresh %s claim: %w", q.table, err)
	}
	return tag.RowsAffected(), nil
}

// QueueDepthByTenant returns outstanding (pending/processing/failed) task counts
// grouped by tenant — feeds the per-tenant queue-depth gauge for this table.
func (q *Queue) QueueDepthByTenant(ctx context.Context) (map[string]int64, error) {
	sql := fmt.Sprintf(`
		SELECT tenant_id, COUNT(*) FROM %s
		WHERE status IN ('pending', 'processing', 'failed')
		GROUP BY tenant_id`, q.table)
	rows, err := q.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("%s queue depth by tenant: %w", q.table, err)
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var tenant string
		var n int64
		if err := rows.Scan(&tenant, &n); err != nil {
			return nil, fmt.Errorf("scan %s queue depth: %w", q.table, err)
		}
		out[tenant] = n
	}
	return out, rows.Err()
}

// QueueDepth counts outstanding (pending/processing/failed) tasks for a tenant.
func (q *Queue) QueueDepth(ctx context.Context, tenantID string) (int64, error) {
	sql := fmt.Sprintf(`
		SELECT COUNT(*) FROM %s
		WHERE tenant_id = $1 AND status IN ('pending', 'processing', 'failed')`, q.table)
	var count int64
	if err := q.pool.QueryRow(ctx, sql, tenantID).Scan(&count); err != nil {
		return 0, fmt.Errorf("%s queue depth: %w", q.table, err)
	}
	return count, nil
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(row taskScanner) (Task, error) {
	var t Task
	err := row.Scan(
		&t.ID, &t.FeedbackID, &t.TenantID, &t.Status, &t.Attempts,
		&t.ClaimedAt, &t.ClaimedBy, &t.NextRetryAt, &t.LastError, &t.CreatedAt, &t.CompletedAt,
	)
	return t, err
}
