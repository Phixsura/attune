package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

// OutboxRepo owns the notify_outbox table — the at-least-once delivery
// queue. The transactional INSERT path is exposed as both Insert (for
// callers that already have a tx) and InsertOne (single-row convenience
// for tests). Worker reads use ClaimBatch (SKIP LOCKED).
type OutboxRepo struct {
	pool *pgxpool.Pool
}

func NewOutbox(pool *pgxpool.Pool) *OutboxRepo {
	return ptrext.Of(OutboxRepo{pool: pool})
}

// Outbox statuses — keep in lockstep with the CHECK constraint in
// migrations/005_notify_outbox.sql.
const (
	OutboxStatusPending   = "pending"
	OutboxStatusDelivered = "delivered"
	OutboxStatusFailed    = "failed"
	OutboxStatusDead      = "dead"
)

// OutboxRow is one queued (feedback × destination) entry. The dead-queue
// display fields (#33) are populated by ListByStatus / GetByID; the worker's
// ClaimBatch only fills the send-path fields and leaves the rest zero.
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

	// Dead-queue display fields (#33). Zero values when not loaded.
	DeadReason        string
	FailureKind       string // notify.FailureKind value; "" when unknown
	HTTPStatus        int    // upstream HTTP status; 0 = no response
	NextRetryAt       time.Time
	CreatedAt         time.Time
	DeliveredAt       *time.Time // nil until delivered
	ClaimedAt         *time.Time // non-nil = a worker holds the row in-flight
	LastManualRetryAt *time.Time
	RetriedBy         string // actor of the last manual retry; "" if never
	ManualRetryCount  int
}

// MaxListLimit caps ListByStatus page size. Exported so the handler can size
// its keyset cursor consistently.
const MaxListLimit = 200

// RetryOutcome is the result of a manual RetryOne. The handler maps it to
// 202 (Retried), 404 (!Found), or 409 (Found && !Retried — already
// delivered/pending, or InFlight).
type RetryOutcome struct {
	Found    bool
	Retried  bool
	InFlight bool      // claimed_at set: a worker is delivering it right now
	Status   string    // the row's status at decision time (for the 409 message)
	Snapshot OutboxRow // pre-retry state (audit "before" + full row for display)
	Updated  OutboxRow // post-retry re-armed state (audit "after" + the response body)
}

// ErrOutboxNotFound — used by tests / single-row lookups.
var ErrOutboxNotFound = errors.New("outbox row not found")

// Insert writes one outbox row inside an existing transaction. enricher
// MUST call this in the same tx as MarkDone so the row + the enrichment
// state flip are atomic — see service/enrich/enricher.go EnrichOne for the
// canonical pattern.
//
// payload is the fully-built envelope JSON ready to POST (modulo the
// per-attempt signature header). Today's worker stores the envelope at
// insertion time so a destination URL/secret change doesn't change
// what gets resent.
func (r *OutboxRepo) Insert(
	ctx context.Context,
	tx pgx.Tx,
	row OutboxRow,
) (int64, error) {
	const where = "repo.OutboxRepo.Insert"
	var id int64
	err := tx.QueryRow(
		ctx, `
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
// marks them with claimed_at = NOW(). FOR UPDATE SKIP LOCKED prevents two
// concurrent claims from grabbing the same row within the lock window, and the
// `claimed_at IS NULL OR claimed_at < NOW() - 10m` predicate prevents a SECOND
// worker replica from re-claiming a row that is already in-flight (the lock is
// released once this UPDATE commits, before the delivery attempt finishes) —
// without it, running >1 outbox worker would double-deliver. The 10-minute
// window matches ResetStaleClaims so a worker that crashes mid-delivery still
// gets its row retried. The active worker calls RefreshClaims periodically while
// draining a batch, so a long batch (serial sends to slow destinations) keeps
// its lease fresh and never ages past the window mid-flight. Mark{Delivered,
// Failed,Dead} all clear claimed_at, so a row's own retry is never blocked.
//
// Returns the claimed rows; len(0) means nothing to send right now.
// Caller must call MarkDelivered / MarkFailed / MarkDead per row after
// the delivery attempt.
func (r *OutboxRepo) ClaimBatch(ctx context.Context, n int, owner string) ([]OutboxRow, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE notify_outbox
		 SET claimed_at = NOW(), claimed_by = $2
		 WHERE id IN (
		 SELECT id FROM notify_outbox
		 WHERE status IN ('pending', 'failed')
		 AND next_retry_at <= NOW()
		 AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
		 ORDER BY next_retry_at ASC
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, feedback_id, tenant_id, destination_type,
		 destination_target, audience, payload, status,
		 attempts, trace_id, COALESCE(last_error, '')`,
		n, owner)
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
// MarkDelivered marks a row as successfully delivered. Returns 0 rows if
// another worker re-claimed the row (fencing token check via claimed_by).
func (r *OutboxRepo) MarkDelivered(ctx context.Context, id int64, owner string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notify_outbox
		 SET status = 'delivered',
		 delivered_at = NOW(),
		 claimed_at = NULL,
		 claimed_by = NULL,
		 last_error = NULL
		 WHERE id = $1 AND claimed_by = $2`, id, owner)
	if err != nil {
		return 0, fmt.Errorf("mark delivered %d: %w", id, err)
	}
	return tag.RowsAffected(), nil
}

// MarkFailed records a retryable failure: increment attempts, schedule
// next_retry_at by exponential backoff, clear claimed_at so a worker
// can pick it up again. Returns 0 rows if another worker re-claimed the row.
//
// Prior version used `($3 || ' seconds')::INTERVAL` for the delay —
// pgx5 strict-mode rejects binding int into a TEXT parameter
// ("cannot find encode plan"), so MarkFailed silently failed on prod
// and outbox rows accumulated forever in 'pending' state (see
// memory/feedback_outbox_lag_stale.md). make_interval(secs => $3)
// binds int natively, no string concat.
func (r *OutboxRepo) MarkFailed(
	ctx context.Context,
	id int64,
	owner string,
	errMsg, failureKind string,
	httpStatus int,
	nextDelay time.Duration,
) (int64, error) {
	const where = "repo.OutboxRepo.MarkFailed"
	tag, err := r.pool.Exec(ctx, `
		UPDATE notify_outbox
		 SET status = 'failed',
		 attempts = attempts + 1,
		 last_error = $2,
		 failure_kind = $5,
		 http_status = $6,
		 next_retry_at = NOW() + make_interval(secs => $3),
		 claimed_at = NULL,
		 claimed_by = NULL
		 WHERE id = $1 AND claimed_by = $4`,
		id, pgxutil.Truncate(errMsg, 1000), int(nextDelay.Seconds()), owner,
		nullStr(failureKind), nullInt(httpStatus))
	if err != nil {
		logext.Errorf(ctx, "[%s] mark failed,id:%d,err:%+v", where, id, err.Error())
		return 0, fmt.Errorf("mark failed %d: %w", id, err)
	}
	return tag.RowsAffected(), nil
}

// MarkDead writes a terminal failure: status='dead', stores the reason
// so the console can review. Caller invokes this on
// ErrTerminal from the notifier OR when attempts exceeds the max.
// Returns 0 rows if another worker re-claimed the row.
func (r *OutboxRepo) MarkDead(
	ctx context.Context,
	id int64,
	owner string,
	reason, failureKind string,
	httpStatus int,
) (int64, error) {
	const where = "repo.OutboxRepo.MarkDead"
	tag, err := r.pool.Exec(ctx, `
		UPDATE notify_outbox
		 SET status = 'dead',
		 dead_reason = $2,
		 failure_kind = $4,
		 http_status = $5,
		 claimed_at = NULL,
		 claimed_by = NULL
		 WHERE id = $1 AND claimed_by = $3`, id, pgxutil.Truncate(reason, 1000), owner,
		nullStr(failureKind), nullInt(httpStatus))
	if err != nil {
		logext.Errorf(ctx, "[%s] mark dead failed,id:%d,err:%+v", where, id, err.Error())
		return 0, fmt.Errorf("mark dead %d: %w", id, err)
	}
	logext.Infof(ctx, "[%s] OK,id:%d,reason:%s", where, id, pgxutil.Truncate(reason, 200))
	return tag.RowsAffected(), nil
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
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE notify_outbox
		 SET status = 'dead',
		 dead_reason = $2,
		 claimed_at = NULL
		 WHERE status IN ('pending', 'failed')
		 AND created_at < $1`,
		before, pgxutil.Truncate(reason, 1000),
	)
	if err != nil {
		return 0, fmt.Errorf("prune stale pending: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RefreshClaims re-stamps claimed_at = NOW() for the still-unsent rows in ids
// that THIS worker (owner) still holds, renewing the lease so a long-running
// batch can't age past the ClaimBatch 10-minute window (which would let a second
// replica re-claim an in-flight row). The claimed_by = owner filter means a
// worker never renews a row another replica has since re-claimed; the status
// filter leaves already-delivered/dead rows alone. Called periodically by the
// outbox worker while it drains a batch.
func (r *OutboxRepo) RefreshClaims(ctx context.Context, ids []int64, owner string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE notify_outbox
		 SET claimed_at = NOW()
		 WHERE id = ANY($1)
		 AND claimed_by = $2
		 AND claimed_at IS NOT NULL
		 AND status IN ('pending', 'failed')`, ids, owner)
	if err != nil {
		return 0, fmt.Errorf("refresh claims: %w", err)
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
// attune_outbox_lag_seconds. Returns 0 when the queue is empty.
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
	return time.Duration(ptrext.Indirect(ageSec) * float64(time.Second)), nil
}

// DeadCount returns the number of rows currently in the terminal 'dead' state.
// Feeds the attune_outbox_dead_rows gauge sampled in cmd/attune.
func (r *OutboxRepo) DeadCount(ctx context.Context) (int64, error) {
	var n int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notify_outbox WHERE status = 'dead'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("dead count: %w", err)
	}
	return n, nil
}

// deadCols is the shared SELECT list for the dead-queue display rows. Note:
// payload is intentionally omitted — the list view doesn't need the full
// envelope, only the metadata.
const deadCols = `id, feedback_id, tenant_id, destination_type, destination_target,
	audience, status, attempts, trace_id,
	COALESCE(last_error, ''), COALESCE(dead_reason, ''), COALESCE(failure_kind, ''),
	http_status, next_retry_at, created_at, delivered_at, claimed_at,
	last_manual_retry_at, COALESCE(retried_by, ''), manual_retry_count`

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface{ Scan(dest ...any) error }

func scanDeadRow(s rowScanner) (OutboxRow, error) {
	var row OutboxRow
	var httpStatus *int16
	var deliveredAt, claimedAt, lastManualRetryAt *time.Time
	if err := s.Scan(
		&row.ID, &row.FeedbackID, &row.TenantID, &row.DestinationType, &row.DestinationTarget,
		&row.Audience, &row.Status, &row.Attempts, &row.TraceID,
		&row.LastError, &row.DeadReason, &row.FailureKind,
		&httpStatus, &row.NextRetryAt, &row.CreatedAt, &deliveredAt, &claimedAt,
		&lastManualRetryAt, &row.RetriedBy, &row.ManualRetryCount,
	); err != nil {
		return OutboxRow{}, err
	}
	row.HTTPStatus = int(ptrext.Indirect(httpStatus))
	row.DeliveredAt = deliveredAt
	row.ClaimedAt = claimedAt
	row.LastManualRetryAt = lastManualRetryAt
	return row, nil
}

// ListByStatus returns dead-queue display rows for one tenant filtered by
// status, newest first, keyset-paginated on id (beforeID == 0 → first page).
// limit is clamped to [1, 200].
func (r *OutboxRepo) ListByStatus(
	ctx context.Context,
	tenantID string,
	statuses []string,
	limit int,
	beforeID int64,
) ([]OutboxRow, error) {
	if limit <= 0 || limit > MaxListLimit {
		limit = MaxListLimit
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+deadCols+`
		 FROM notify_outbox
		 WHERE tenant_id = $1 AND status = ANY($2)
		 AND ($3 = 0 OR id < $3)
		 ORDER BY id DESC
		 LIMIT $4`,
		tenantID, statuses, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("list outbox by status: %w", err)
	}
	defer rows.Close()

	var out []OutboxRow
	for rows.Next() {
		row, err := scanDeadRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan dead row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetByID loads one dead-queue display row scoped to the tenant. Returns
// ErrOutboxNotFound when the row is missing or owned by another tenant.
func (r *OutboxRepo) GetByID(ctx context.Context, tenantID string, id int64) (*OutboxRow, error) {
	row, err := scanDeadRow(r.pool.QueryRow(ctx,
		`SELECT `+deadCols+` FROM notify_outbox WHERE id = $1 AND tenant_id = $2`,
		id, tenantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOutboxNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get outbox row %d: %w", id, err)
	}
	return ptrext.Of(row), nil
}

// RetryOne re-arms a single dead/failed row for redelivery (retry-in-place):
// reset to pending, attempts=0, clear the terminal/failure fields, and stamp
// the manual-retry bookkeeping. Concurrency-safe: the row is SELECT-ed
// FOR UPDATE so a worker's SKIP-LOCKED claim can't race it, and only
// dead/failed rows with no live claim are eligible — anything else comes back
// as a non-retried outcome for the handler to turn into 404/409.
//
// auditFn (optional) runs inside the same transaction after the re-arm, before
// commit, with the before/after rows. If it returns an error the whole retry
// rolls back — so a destructive retry can never commit without its audit trail
// (#33). The before (Snapshot) and after (Updated) rows are returned for the
// response and audit.
func (r *OutboxRepo) RetryOne(
	ctx context.Context,
	tenantID string,
	id int64,
	actor string,
	auditFn func(ctx context.Context, tx pgx.Tx, before, after OutboxRow) error,
) (RetryOutcome, error) {
	const where = "repo.OutboxRepo.RetryOne"
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RetryOutcome{}, fmt.Errorf("begin retry tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := scanDeadRow(tx.QueryRow(ctx,
		`SELECT `+deadCols+` FROM notify_outbox WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		id, tenantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return RetryOutcome{Found: false}, nil
	}
	if err != nil {
		return RetryOutcome{}, fmt.Errorf("select for retry %d: %w", id, err)
	}

	retryable := (before.Status == OutboxStatusDead || before.Status == OutboxStatusFailed) &&
		before.ClaimedAt == nil
	if !retryable {
		return RetryOutcome{
			Found: true, Retried: false, InFlight: before.ClaimedAt != nil, Status: before.Status,
		}, nil
	}

	var nextRetryAt time.Time
	var lastManualRetryAt *time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE notify_outbox
		 SET status = 'pending', attempts = 0, next_retry_at = NOW(), claimed_at = NULL,
		 last_error = NULL, dead_reason = NULL, failure_kind = NULL, http_status = NULL,
		 last_manual_retry_at = NOW(), retried_by = $2,
		 manual_retry_count = manual_retry_count + 1
		 WHERE id = $1
		 RETURNING next_retry_at, last_manual_retry_at`,
		id, nullStr(actor)).Scan(&nextRetryAt, &lastManualRetryAt); err != nil {
		return RetryOutcome{}, fmt.Errorf("reset for retry %d: %w", id, err)
	}

	after := reArm(before, actor, nextRetryAt, lastManualRetryAt)
	if auditFn != nil {
		if err := auditFn(ctx, tx, before, after); err != nil {
			// Deferred rollback undoes the re-arm: retry + audit are atomic.
			return RetryOutcome{}, fmt.Errorf("retry audit %d: %w", id, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return RetryOutcome{}, fmt.Errorf("commit retry %d: %w", id, err)
	}
	logext.Infof(ctx, "[%s] re-armed,id:%d,tenant:%s,prev_status:%s,actor:%s",
		where, id, tenantID, before.Status, actor)
	return RetryOutcome{
		Found: true, Retried: true, Status: before.Status, Snapshot: before, Updated: after,
	}, nil
}

// reArm returns the post-retry state of a row: pending with a fresh attempt
// budget, terminal/failure fields cleared, manual-retry bookkeeping stamped. The
// timestamps come from the UPDATE's RETURNING (the authoritative DB NOW()), so
// the returned row exactly matches what was persisted.
func reArm(before OutboxRow, actor string, nextRetryAt time.Time, lastManualRetryAt *time.Time) OutboxRow {
	after := before
	after.Status = OutboxStatusPending
	after.Attempts = 0
	after.LastError = ""
	after.DeadReason = ""
	after.FailureKind = ""
	after.HTTPStatus = 0
	after.ClaimedAt = nil
	after.NextRetryAt = nextRetryAt
	after.LastManualRetryAt = lastManualRetryAt
	after.RetriedBy = actor
	after.ManualRetryCount = before.ManualRetryCount + 1
	return after
}

// nullStr / nullInt map Go zero values to SQL NULL so empty failure_kind and
// a 0 http_status (no response) are stored as NULL rather than ” / 0.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
