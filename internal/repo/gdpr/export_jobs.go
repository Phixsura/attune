// SPDX-License-Identifier: Apache-2.0

package gdpr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

var (
	ErrExportJobNotFound        = errors.New("gdpr export job not found")
	ErrExportJobNotDownloadable = errors.New("gdpr export job not downloadable")
	ErrExportJobNotRevocable    = errors.New("gdpr export job not revocable")
)

type ExportJobStatus string

const (
	ExportJobQueued    ExportJobStatus = "queued"
	ExportJobRunning   ExportJobStatus = "running"
	ExportJobCompleted ExportJobStatus = "completed"
	ExportJobFailed    ExportJobStatus = "failed"
	ExportJobExpired   ExportJobStatus = "expired"
	ExportJobRevoked   ExportJobStatus = "revoked"
)

type ExportJob struct {
	ID              string
	TenantID        string
	SubjectKey      string
	SubjectHash     string
	SubjectDisplay  string
	Status          ExportJobStatus
	Archive         []byte
	ArchiveFilename string
	Counts          Counts
	Error           string
	CreatedByType   string
	CreatedBy       string
	CreatedAt       time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	ExpiresAt       *time.Time
	DownloadedAt    *time.Time
	RevokedAt       *time.Time
	ClaimedAt       *time.Time
	HeartbeatAt     *time.Time
}

const exportJobCols = `id, tenant_id, subject_key, subject_hash, subject_display, status,
	archive, archive_filename, feedback_count, tag_assignment_count, feedback_audit_count,
	llm_audit_count, COALESCE(error, ''), created_by_type, created_by, created_at, started_at, completed_at,
	expires_at, downloaded_at, revoked_at, claimed_at, last_heartbeat`

func scanExportJob(row pgx.Row) (*ExportJob, error) {
	var job ExportJob
	err := row.Scan(
		&job.ID, &job.TenantID, &job.SubjectKey, &job.SubjectHash, &job.SubjectDisplay, &job.Status,
		&job.Archive, &job.ArchiveFilename, &job.Counts.FeedbackCount, &job.Counts.TagAssignmentCount,
		&job.Counts.FeedbackAuditCount, &job.Counts.LLMAuditCount, &job.Error, &job.CreatedByType, &job.CreatedBy,
		&job.CreatedAt, &job.StartedAt, &job.CompletedAt, &job.ExpiresAt, &job.DownloadedAt, &job.RevokedAt,
		&job.ClaimedAt, &job.HeartbeatAt,
	)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(job), nil
}

func (r *Repo) CreateExportJob(
	ctx context.Context,
	tenantID, subjectKey, subjectHash, createdByType, createdBy string,
) (*ExportJob, error) {
	const where = "repo.gdpr.CreateExportJob"
	id := uuid.NewString()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin export create tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(
		ctx, `
		INSERT INTO gdpr_requests (
			id, tenant_id, request_type, status, subject_key, subject_hash, created_by_type, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, tenantID, RequestTypeExport, RequestStatusQueued, subjectKey, subjectHash, createdByType, createdBy,
	); err != nil {
		logext.Errorf(ctx, "[%s] insert request failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("create gdpr request: %w", err)
	}
	row := tx.QueryRow(
		ctx, `
		INSERT INTO gdpr_export_jobs (id, request_id, tenant_id, subject_key, subject_hash, status, created_by_type, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+exportJobCols,
		id, id, tenantID, subjectKey, subjectHash, ExportJobQueued, createdByType, createdBy,
	)
	job, err := scanExportJob(row)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert job failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("create gdpr export job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit export create tx: %w", err)
	}
	return job, nil
}

func (r *Repo) GetExportJob(ctx context.Context, tenantID, jobID string) (*ExportJob, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT `+exportJobCols+` FROM gdpr_export_jobs WHERE id = $1 AND tenant_id = $2`,
		jobID, tenantID,
	)
	job, err := scanExportJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExportJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get gdpr export job: %w", err)
	}
	return job, nil
}

func (r *Repo) ClaimNextExportJob(ctx context.Context) (*ExportJob, error) {
	return r.ClaimNextExportJobWithOwner(ctx, "")
}

// ClaimNextExportJobWithOwner claims the next queued export job with owner for fencing.
func (r *Repo) ClaimNextExportJobWithOwner(ctx context.Context, owner string) (*ExportJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin export claim tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		SELECT `+exportJobCols+`
		FROM gdpr_export_jobs
		WHERE status = 'queued'
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`)
	job, err := scanExportJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim gdpr export job select: %w", err)
	}

	_, err = tx.Exec(
		ctx, `
		UPDATE gdpr_export_jobs
		SET status = 'running', started_at = NOW(), claimed_at = NOW(),
		    claimed_by = $2, last_heartbeat = NOW()
		WHERE id = $1`,
		job.ID, owner,
	)
	if err != nil {
		return nil, fmt.Errorf("claim gdpr export job update: %w", err)
	}
	if _, err := tx.Exec(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'running', started_at = NOW(), claimed_at = NOW(), claimed_by = $2
		WHERE id = $1`,
		job.ID, owner,
	); err != nil {
		return nil, fmt.Errorf("claim gdpr request update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit export claim tx: %w", err)
	}

	now := time.Now().UTC()
	job.Status = ExportJobRunning
	job.StartedAt = ptrext.Of(now)
	job.ClaimedAt = ptrext.Of(now)
	job.HeartbeatAt = ptrext.Of(now)
	return job, nil
}

func (r *Repo) HeartbeatExportJob(ctx context.Context, jobID string) error {
	_, err := r.HeartbeatExportJobWithOwner(ctx, jobID, "")
	return err
}

// HeartbeatExportJobWithOwner refreshes the heartbeat with owner fencing.
// Returns rows affected (0 if job was re-claimed by another worker).
func (r *Repo) HeartbeatExportJobWithOwner(ctx context.Context, jobID, owner string) (int64, error) {
	var sql string
	var args []any
	if owner == "" {
		sql = `UPDATE gdpr_export_jobs SET last_heartbeat = NOW()
		       WHERE id = $1 AND status = 'running'`
		args = []any{jobID}
	} else {
		sql = `UPDATE gdpr_export_jobs SET last_heartbeat = NOW()
		       WHERE id = $1 AND status = 'running' AND claimed_by = $2`
		args = []any{jobID, owner}
	}
	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("heartbeat gdpr export job: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) CompleteExportJob(
	ctx context.Context,
	jobID, subjectDisplay, archiveFilename string,
	archive []byte,
	counts Counts,
	expiresAt time.Time,
) error {
	_, err := r.CompleteExportJobWithOwner(ctx, jobID, "", subjectDisplay, archiveFilename, archive, counts, expiresAt)
	return err
}

// CompleteExportJobWithOwner completes with owner fencing. Returns rows affected.
func (r *Repo) CompleteExportJobWithOwner(
	ctx context.Context,
	jobID, owner, subjectDisplay, archiveFilename string,
	archive []byte,
	counts Counts,
	expiresAt time.Time,
) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin complete export tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sql string
	var args []any
	if owner == "" {
		sql = `UPDATE gdpr_export_jobs
		       SET status = 'completed', subject_display = $2, archive = $3, archive_filename = $4,
		           feedback_count = $5, tag_assignment_count = $6, feedback_audit_count = $7,
		           llm_audit_count = $8, completed_at = NOW(), expires_at = $9,
		           claimed_at = NULL, claimed_by = NULL, error = ''
		       WHERE id = $1 AND status = 'running'`
		args = []any{
			jobID, subjectDisplay, archive, archiveFilename,
			counts.FeedbackCount, counts.TagAssignmentCount, counts.FeedbackAuditCount, counts.LLMAuditCount,
			expiresAt.UTC(),
		}
	} else {
		sql = `UPDATE gdpr_export_jobs
		       SET status = 'completed', subject_display = $2, archive = $3, archive_filename = $4,
		           feedback_count = $5, tag_assignment_count = $6, feedback_audit_count = $7,
		           llm_audit_count = $8, completed_at = NOW(), expires_at = $9,
		           claimed_at = NULL, claimed_by = NULL, error = ''
		       WHERE id = $1 AND status = 'running' AND claimed_by = $10`
		args = []any{
			jobID, subjectDisplay, archive, archiveFilename,
			counts.FeedbackCount, counts.TagAssignmentCount, counts.FeedbackAuditCount, counts.LLMAuditCount,
			expiresAt.UTC(), owner,
		}
	}
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("complete gdpr export job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'ready',
		    subject_display = $2,
		    archive_filename = $3,
		    feedback_count = $4,
		    tag_assignment_count = $5,
		    feedback_audit_count = $6,
		    llm_audit_count = $7,
		    completed_at = NOW(),
		    expires_at = $8,
		    claimed_at = NULL, claimed_by = NULL,
		    error = ''
		WHERE id = $1`,
		jobID, subjectDisplay, archiveFilename,
		counts.FeedbackCount, counts.TagAssignmentCount, counts.FeedbackAuditCount, counts.LLMAuditCount,
		expiresAt.UTC(),
	); err != nil {
		return 0, fmt.Errorf("complete gdpr request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit complete export tx: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) FailExportJob(ctx context.Context, jobID, errMsg string) error {
	_, err := r.FailExportJobWithOwner(ctx, jobID, "", errMsg)
	return err
}

// FailExportJobWithOwner fails with owner fencing. Returns rows affected.
func (r *Repo) FailExportJobWithOwner(ctx context.Context, jobID, owner, errMsg string) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin fail export tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sql string
	var args []any
	if owner == "" {
		sql = `UPDATE gdpr_export_jobs
		       SET status = 'failed', error = $2, completed_at = NOW(),
		           claimed_at = NULL, claimed_by = NULL
		       WHERE id = $1 AND status = 'running'`
		args = []any{jobID, pgxutil.Truncate(errMsg, 1000)}
	} else {
		sql = `UPDATE gdpr_export_jobs
		       SET status = 'failed', error = $2, completed_at = NOW(),
		           claimed_at = NULL, claimed_by = NULL
		       WHERE id = $1 AND status = 'running' AND claimed_by = $3`
		args = []any{jobID, pgxutil.Truncate(errMsg, 1000), owner}
	}
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("fail gdpr export job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'failed', error = $2, completed_at = NOW(),
		    claimed_at = NULL, claimed_by = NULL
		WHERE id = $1`,
		jobID, pgxutil.Truncate(errMsg, 1000),
	); err != nil {
		return 0, fmt.Errorf("fail gdpr request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit fail export tx: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) ExpireReadyExportJobs(ctx context.Context, now time.Time) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin expire export tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(
		ctx, `
		UPDATE gdpr_export_jobs
		SET status = 'expired', archive = NULL, archive_filename = ''
		WHERE status = 'completed' AND expires_at IS NOT NULL AND expires_at <= $1`,
		now.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("expire gdpr export jobs: %w", err)
	}
	if _, err := tx.Exec(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'expired', archive_filename = ''
		WHERE request_type = 'export'
		  AND status IN ('ready', 'downloaded')
		  AND expires_at IS NOT NULL
		  AND expires_at <= $1`,
		now.UTC(),
	); err != nil {
		return 0, fmt.Errorf("expire gdpr requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit expire export tx: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) MarkExportJobDownloaded(ctx context.Context, tenantID, jobID string) (*ExportJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin download export tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(
		ctx, `
		UPDATE gdpr_export_jobs
		SET downloaded_at = COALESCE(downloaded_at, NOW())
		WHERE id = $1
		  AND tenant_id = $2
		  AND status = 'completed'
		  AND archive IS NOT NULL
		  AND expires_at IS NOT NULL
		  AND expires_at > NOW()
		RETURNING `+exportJobCols,
		jobID, tenantID,
	)
	job, err := scanExportJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExportJobNotDownloadable
	}
	if err != nil {
		return nil, fmt.Errorf("mark gdpr export job downloaded: %w", err)
	}
	if _, err := tx.Exec(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'downloaded', downloaded_at = COALESCE(downloaded_at, NOW())
		WHERE id = $1`,
		jobID,
	); err != nil {
		return nil, fmt.Errorf("mark gdpr request downloaded: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit download export tx: %w", err)
	}
	return job, nil
}

func (r *Repo) RevokeExportJob(ctx context.Context, tenantID, jobID string) (*ExportJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin revoke export tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(
		ctx, `
		UPDATE gdpr_export_jobs
		SET status = 'revoked',
		    archive = NULL,
		    revoked_at = NOW()
		WHERE id = $1
		  AND tenant_id = $2
		  AND status = 'completed'
		  AND archive IS NOT NULL
		  AND expires_at IS NOT NULL
		  AND expires_at > NOW()
		RETURNING `+exportJobCols,
		jobID, tenantID,
	)
	job, err := scanExportJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExportJobNotRevocable
	}
	if err != nil {
		return nil, fmt.Errorf("revoke gdpr export job: %w", err)
	}
	if _, err := tx.Exec(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'revoked', revoked_at = COALESCE(revoked_at, NOW())
		WHERE id = $1`,
		jobID,
	); err != nil {
		return nil, fmt.Errorf("revoke gdpr request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit revoke export tx: %w", err)
	}
	return job, nil
}
