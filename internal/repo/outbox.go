package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/listen/internal/logext"
)

// OutboxRepo owns the notify_outbox table — the at-least-once delivery
// queue. The transactional INSERT path is exposed as both Insert (for
// callers that already have a tx) and InsertOne (single-row convenience
// for tests). Worker reads use ClaimBatch (SKIP LOCKED).
type OutboxRepo struct {
	pool *pgxpool.Pool
}

func NewOutbox(pool *pgxpool.Pool) *OutboxRepo {
	return &OutboxRepo{pool: pool}
}

// Outbox statuses — keep in lockstep with the CHECK constraint in
// migrations/005_notify_outbox.sql.
const (
	OutboxStatusPending   = "pending"
	OutboxStatusDelivered = "delivered"
	OutboxStatusFailed    = "failed"
	OutboxStatusDead      = "dead"
)

// OutboxRow is one queued (feedback × destination) entry.
type OutboxRow struct {
	ID                int64
	FeedbackID        int64
	TenantID          string
	DestinationType   string
	DestinationTarget string
	Audience          string
	Payload           []byte // pre-built envelope JSON (signature added per-attempt)
	Status            string
	Attempts          int
	TraceID           string
	LastError         string
}

// ErrOutboxNotFound — used by tests / single-row lookups.
var ErrOutboxNotFound = errors.New("outbox row not found")

// Insert writes one outbox row inside an existing transaction. enricher
// MUST call this in the same tx as MarkDone so the row + the enrichment
// state flip are atomic — see service/enricher.go EnrichOne for the
// canonical pattern.
//
// payload is the fully-built envelope JSON ready to POST (modulo the
// per-attempt signature header). Wave 1.2 stores the envelope at
// insertion time so a destination URL/secret change doesn't change
// what gets resent.
func (r *OutboxRepo) Insert(
	ctx context.Context,
	tx pgx.Tx,
	row OutboxRow,
) (int64, error) {
	const where = "repo.OutboxRepo.Insert"
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO notify_outbox
		  (feedback_id, tenant_id, destination_type, destination_target,
		   audience, payload, status, trace_id)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7)
		RETURNING id`,
		row.FeedbackID, row.TenantID, row.DestinationType,
		row.DestinationTarget, row.Audience, row.Payload, row.TraceID,
	).Scan(&id)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,feedback_id:%d,dest_type:%s,err:%+v",
			where, row.FeedbackID, row.DestinationType, err.Error())
		return 0, fmt.Errorf("insert outbox: %w", err)
	}
	return id, nil
}

// ClaimBatch atomically pulls up to n rows that are ready to send and
// marks them with claimed_at = NOW(). Uses FOR UPDATE SKIP LOCKED so
// multiple workers (in one process or many) never grab the same row.
//
// Returns the claimed rows; len(0) means nothing to send right now.
// Caller must call MarkDelivered / MarkFailed / MarkDead per row after
// the delivery attempt.
func (r *OutboxRepo) ClaimBatch(ctx context.Context, n int) ([]OutboxRow, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE notify_outbox
		   SET claimed_at = NOW()
		 WHERE id IN (
		     SELECT id FROM notify_outbox
		      WHERE status IN ('pending', 'failed')
		        AND next_retry_at <= NOW()
		      ORDER BY next_retry_at ASC
		      LIMIT $1
		      FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, feedback_id, tenant_id, destination_type,
		           destination_target, audience, payload, status,
		           attempts, trace_id, COALESCE(last_error, '')`,
		n)
	if err != nil {
		return nil, fmt.Errorf("claim outbox batch: %w", err)
	}
	defer rows.Close()

	var out []OutboxRow
	for rows.Next() {
		var row OutboxRow
		if err := rows.Scan(
			&row.ID, &row.FeedbackID, &row.TenantID, &row.DestinationType,
			&row.DestinationTarget, &row.Audience, &row.Payload, &row.Status,
			&row.Attempts, &row.TraceID, &row.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan outbox row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// MarkDelivered flips a row to status='delivered' and stamps
// delivered_at. Idempotent: a second call on the same id is a no-op.
func (r *OutboxRepo) MarkDelivered(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE notify_outbox
		   SET status = 'delivered',
		       delivered_at = NOW(),
		       claimed_at = NULL,
		       last_error = NULL
		 WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark delivered %d: %w", id, err)
	}
	return nil
}

// MarkFailed records a retryable failure: increment attempts, schedule
// next_retry_at by exponential backoff, clear claimed_at so a worker
// can pick it up again.
//
// Prior version used `($3 || ' seconds')::INTERVAL` for the delay —
// pgx5 strict-mode rejects binding int into a TEXT parameter
// ("cannot find encode plan"), so MarkFailed silently failed on prod
// and outbox rows accumulated forever in 'pending' state (see
// memory/feedback_outbox_lag_stale.md). make_interval(secs => $3)
// binds int natively, no string concat.
func (r *OutboxRepo) MarkFailed(ctx context.Context, id int64, errMsg string, nextDelay time.Duration) error {
	const where = "repo.OutboxRepo.MarkFailed"
	_, err := r.pool.Exec(ctx, `
		UPDATE notify_outbox
		   SET status = 'failed',
		       attempts = attempts + 1,
		       last_error = $2,
		       next_retry_at = NOW() + make_interval(secs => $3),
		       claimed_at = NULL
		 WHERE id = $1`,
		id, truncate(errMsg, 1000), int(nextDelay.Seconds()))
	if err != nil {
		logext.Errorf(ctx, "[%s] mark failed,id:%d,err:%+v", where, id, err.Error())
		return fmt.Errorf("mark failed %d: %w", id, err)
	}
	return nil
}

// MarkDead writes a terminal failure: status='dead', stores the reason
// so Wave 2 console / ops can review. Caller invokes this on
// ErrTerminal from the notifier OR when attempts exceeds the max.
func (r *OutboxRepo) MarkDead(ctx context.Context, id int64, reason string) error {
	const where = "repo.OutboxRepo.MarkDead"
	_, err := r.pool.Exec(ctx, `
		UPDATE notify_outbox
		   SET status = 'dead',
		       dead_reason = $2,
		       claimed_at = NULL
		 WHERE id = $1`, id, truncate(reason, 1000))
	if err != nil {
		logext.Errorf(ctx, "[%s] mark dead failed,id:%d,err:%+v", where, id, err.Error())
		return fmt.Errorf("mark dead %d: %w", id, err)
	}
	logext.Infof(ctx, "[%s] OK,id:%d,reason:%s", where, id, truncate(reason, 200))
	return nil
}

// PruneStalePending is a one-shot ops cleanup: mark every pending/failed
// row older than `before` as dead with a synthesized reason. Idempotent —
// already-dead/delivered rows are ignored.
//
// Used to clean up the pre-fix backlog (see memory/feedback_outbox_lag_stale.md)
// — rows stuck in pending forever because the pgx encode bug in MarkFailed
// prevented attempts from ever advancing past 0. After fix `2054e71`, new
// rows reach max_attempts → dead normally; only legacy rows need this.
//
// Returns the number of rows marked dead.
func (r *OutboxRepo) PruneStalePending(ctx context.Context, before time.Time, reason string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notify_outbox
		   SET status      = 'dead',
		       dead_reason = $2,
		       claimed_at  = NULL
		 WHERE status IN ('pending', 'failed')
		   AND created_at < $1`,
		before, truncate(reason, 1000),
	)
	if err != nil {
		return 0, fmt.Errorf("prune stale pending: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ResetStaleClaims releases rows that a crashed worker left stuck with
// a non-NULL claimed_at. Should be called once at startup. 10 minutes
// is generous enough that a slow LLM gateway can't trigger it on legit
// in-flight work.
func (r *OutboxRepo) ResetStaleClaims(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notify_outbox
		   SET claimed_at = NULL
		 WHERE claimed_at IS NOT NULL
		   AND claimed_at < NOW() - INTERVAL '10 minutes'
		   AND status IN ('pending', 'failed')`)
	if err != nil {
		return 0, fmt.Errorf("reset stale claims: %w", err)
	}
	return tag.RowsAffected(), nil
}

// OldestPendingAge returns the wall-clock age of the oldest unsent
// outbox row (status pending or failed). Used by metric
// listen_outbox_lag_seconds. Returns 0 when the queue is empty.
func (r *OutboxRepo) OldestPendingAge(ctx context.Context) (time.Duration, error) {
	var ageSec *float64
	err := r.pool.QueryRow(ctx, `
		SELECT EXTRACT(EPOCH FROM (NOW() - MIN(created_at)))
		  FROM notify_outbox
		 WHERE status IN ('pending', 'failed')`).Scan(&ageSec)
	if err != nil {
		return 0, fmt.Errorf("oldest pending age: %w", err)
	}
	if ageSec == nil {
		return 0, nil
	}
	return time.Duration(*ageSec * float64(time.Second)), nil
}
