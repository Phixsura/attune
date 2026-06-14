package feedbackjob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

// Repo is the PostgreSQL implementation of Store.
type Repo struct {
	pool *pgxpool.Pool
}

// New creates a new Repo backed by the given connection pool.
func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

const selectCols = `id, tenant_id, status, request, total, progress,
	result, error, created_by, created_at, started_at, completed_at,
	claimed_at, last_heartbeat`

func scanJob(row pgx.Row) (*Job, error) {
	var j Job
	// ptrext:allow scan-target
	err := row.Scan(
		&j.ID, &j.TenantID, &j.Status, &j.Request, &j.Total, &j.Progress,
		&j.Result, &j.Error, &j.CreatedBy, &j.CreatedAt, &j.StartedAt,
		&j.CompletedAt, &j.ClaimedAt, &j.HeartbeatAt,
	)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(j), nil
}

func scanJobs(rows pgx.Rows) ([]*Job, error) {
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		var j Job
		// ptrext:allow scan-target
		if err := rows.Scan(
			&j.ID, &j.TenantID, &j.Status, &j.Request, &j.Total, &j.Progress,
			&j.Result, &j.Error, &j.CreatedBy, &j.CreatedAt, &j.StartedAt,
			&j.CompletedAt, &j.ClaimedAt, &j.HeartbeatAt,
		); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		out = append(out, ptrext.Of(j))
	}
	return out, rows.Err()
}

// Create creates a new job in queued status.
func (r *Repo) Create(ctx context.Context, tenantID, createdBy string, request []byte, total int) (*Job, error) {
	const where = "repo.feedbackjob.Create"
	id := uuid.NewString()
	row := r.pool.QueryRow(
		ctx, `
		INSERT INTO batch_jobs (id, tenant_id, status, request, total, created_by)
		VALUES ($1, $2, 'queued', $3, $4, $5)
		RETURNING `+selectCols,
		id, tenantID, request, total, createdBy,
	)
	job, err := scanJob(row)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("create batch job: %w", err)
	}
	return job, nil
}

// Get retrieves a job by ID, validating tenant ownership.
func (r *Repo) Get(ctx context.Context, tenantID, jobID string) (*Job, error) {
	const where = "repo.feedbackjob.Get"
	row := r.pool.QueryRow(
		ctx,
		"SELECT "+selectCols+" FROM batch_jobs WHERE id = $1 AND tenant_id = $2",
		jobID, tenantID,
	)
	job, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,job_id:%s,err:%+v", where, jobID, err.Error())
		return nil, fmt.Errorf("get batch job: %w", err)
	}
	return job, nil
}

// List returns jobs for a tenant with optional status filter.
func (r *Repo) List(ctx context.Context, tenantID string, status *Status, limit int) ([]*Job, error) {
	const where = "repo.feedbackjob.List"

	// Cap limit to 100 for safety
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	var rows pgx.Rows
	var err error
	if status != nil {
		rows, err = r.pool.Query(
			ctx,
			"SELECT "+selectCols+" FROM batch_jobs WHERE tenant_id = $1 AND status = $2 ORDER BY created_at DESC LIMIT $3",
			tenantID, string(ptrext.Indirect(status)), limit,
		)
	} else {
		rows, err = r.pool.Query(
			ctx,
			"SELECT "+selectCols+" FROM batch_jobs WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT $2",
			tenantID, limit,
		)
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("list batch jobs: %w", err)
	}
	return scanJobs(rows)
}

// Claim attempts to claim the next queued job for processing.
// Uses SELECT FOR UPDATE SKIP LOCKED to handle concurrent workers.
// Returns nil if no jobs available.
func (r *Repo) Claim(ctx context.Context) (*Job, error) {
	const where = "repo.feedbackjob.Claim"

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// SELECT one queued job with FOR UPDATE SKIP LOCKED
	row := tx.QueryRow(
		ctx, `
		SELECT `+selectCols+`
		FROM batch_jobs
		WHERE status = 'queued'
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`,
	)
	job, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// No jobs available
		return nil, nil
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] select failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("claim batch job select: %w", err)
	}

	// Update to running status
	_, err = tx.Exec(
		ctx, `
		UPDATE batch_jobs
		SET status = 'running', started_at = NOW(), claimed_at = NOW(), last_heartbeat = NOW()
		WHERE id = $1`,
		job.ID,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,job_id:%s,err:%+v", where, job.ID, err.Error())
		return nil, fmt.Errorf("claim batch job update: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim tx: %w", err)
	}

	// Update local job state to reflect the claim
	now := time.Now()
	job.Status = StatusRunning
	job.StartedAt = ptrext.Of(now)
	job.ClaimedAt = ptrext.Of(now)
	job.HeartbeatAt = ptrext.Of(now)

	return job, nil
}

// UpdateProgress updates the progress counter and heartbeat.
func (r *Repo) UpdateProgress(ctx context.Context, jobID string, progress int) error {
	const where = "repo.feedbackjob.UpdateProgress"
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE batch_jobs
		SET progress = $2, last_heartbeat = NOW()
		WHERE id = $1 AND status = 'running'`,
		jobID, progress,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,job_id:%s,err:%+v", where, jobID, err.Error())
		return fmt.Errorf("update batch job progress: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Complete marks a job as completed with the result.
func (r *Repo) Complete(ctx context.Context, jobID string, result []byte) error {
	const where = "repo.feedbackjob.Complete"
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE batch_jobs
		SET status = 'completed', result = $2, completed_at = NOW(), claimed_at = NULL
		WHERE id = $1 AND status = 'running'`,
		jobID, result,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,job_id:%s,err:%+v", where, jobID, err.Error())
		return fmt.Errorf("complete batch job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	logext.Infof(ctx, "[%s] OK,job_id:%s", where, jobID)
	return nil
}

// Fail marks a job as failed with an error message.
func (r *Repo) Fail(ctx context.Context, jobID string, errMsg string) error {
	const where = "repo.feedbackjob.Fail"
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE batch_jobs
		SET status = 'failed', error = $2, completed_at = NOW(), claimed_at = NULL
		WHERE id = $1 AND status = 'running'`,
		jobID, pgxutil.Truncate(errMsg, 1000),
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,job_id:%s,err:%+v", where, jobID, err.Error())
		return fmt.Errorf("fail batch job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	logext.Infof(ctx, "[%s] OK,job_id:%s,error:%s", where, jobID, pgxutil.Truncate(errMsg, 200))
	return nil
}

// Cancel attempts to cancel a job.
// Only queued or running jobs can be cancelled.
func (r *Repo) Cancel(ctx context.Context, tenantID, jobID string) error {
	const where = "repo.feedbackjob.Cancel"
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE batch_jobs
		SET status = 'cancelled', completed_at = NOW(), claimed_at = NULL
		WHERE id = $1 AND tenant_id = $2 AND status IN ('queued', 'running')`,
		jobID, tenantID,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,job_id:%s,err:%+v", where, jobID, err.Error())
		return fmt.Errorf("cancel batch job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Check if job exists to differentiate not found vs not cancellable
		var exists bool
		err := r.pool.QueryRow(
			ctx,
			"SELECT EXISTS(SELECT 1 FROM batch_jobs WHERE id = $1 AND tenant_id = $2)",
			jobID, tenantID,
		).Scan(&exists) // ptrext:allow scan-target
		if err != nil {
			return fmt.Errorf("check job exists: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
		return ErrJobNotCancellable
	}
	logext.Infof(ctx, "[%s] OK,job_id:%s", where, jobID)
	return nil
}

// Heartbeat updates the heartbeat timestamp for a running job.
func (r *Repo) Heartbeat(ctx context.Context, jobID string) error {
	const where = "repo.feedbackjob.Heartbeat"
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE batch_jobs
		SET last_heartbeat = NOW()
		WHERE id = $1 AND status = 'running'`,
		jobID,
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,job_id:%s,err:%+v", where, jobID, err.Error())
		return fmt.Errorf("heartbeat batch job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecoverStuck finds and requeues jobs with stale heartbeats.
// Returns count of recovered jobs.
func (r *Repo) RecoverStuck(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	const where = "repo.feedbackjob.RecoverStuck"
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE batch_jobs
		SET status = 'queued', started_at = NULL, claimed_at = NULL, last_heartbeat = NULL
		WHERE status = 'running'
		  AND last_heartbeat < NOW() - make_interval(secs => $1)`,
		int(staleThreshold.Seconds()),
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,err:%+v", where, err.Error())
		return 0, fmt.Errorf("recover stuck batch jobs: %w", err)
	}
	count := tag.RowsAffected()
	if count > 0 {
		logext.Infof(ctx, "[%s] recovered %d stuck jobs", where, count)
	}
	return count, nil
}

// Ensure Repo implements Store.
var _ Store = (*Repo)(nil)
