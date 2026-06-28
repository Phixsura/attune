// SPDX-License-Identifier: Apache-2.0

// Package auditevidence owns audit evidence export job persistence.
package auditevidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

var (
	ErrJobNotFound        = errors.New("audit evidence export job not found")
	ErrJobNotDownloadable = errors.New("audit evidence export job not downloadable")
)

type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobExpired   JobStatus = "expired"
)

type ExportJob struct {
	ID              string
	TenantID        string
	Status          JobStatus
	FilterJSON      json.RawMessage
	TotalEvents     int
	Archive         []byte
	ArchiveFilename string
	Error           string
	CreatedByType   string
	CreatedBy       string
	CreatedAt       time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	ExpiresAt       *time.Time
	DownloadedAt    *time.Time
	ClaimedBy       string
	ClaimedAt       *time.Time
	LastHeartbeat   *time.Time
}

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

const jobCols = `id, tenant_id, status, filter_json, total_events,
	archive, archive_filename, error, created_by_type, created_by, created_at,
	started_at, completed_at, expires_at, downloaded_at,
	claimed_by, claimed_at, last_heartbeat`

func scanJob(row pgx.Row) (*ExportJob, error) {
	var job ExportJob
	err := row.Scan(
		&job.ID, &job.TenantID, &job.Status, &job.FilterJSON, &job.TotalEvents,
		&job.Archive, &job.ArchiveFilename, &job.Error, &job.CreatedByType, &job.CreatedBy, &job.CreatedAt,
		&job.StartedAt, &job.CompletedAt, &job.ExpiresAt, &job.DownloadedAt,
		&job.ClaimedBy, &job.ClaimedAt, &job.LastHeartbeat,
	)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(job), nil
}

func (r *Repo) CreateJob(
	ctx context.Context,
	tenantID, createdByType, createdBy string,
	filterJSON json.RawMessage,
) (*ExportJob, error) {
	id := uuid.NewString()
	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO audit_evidence_export (id, tenant_id, status, filter_json, created_by_type, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+jobCols,
		id, tenantID, JobQueued, filterJSON, createdByType, createdBy,
	)
	job, err := scanJob(row)
	if err != nil {
		return nil, fmt.Errorf("create audit evidence export job: %w", err)
	}
	return job, nil
}

func (r *Repo) GetJob(ctx context.Context, tenantID, jobID string) (*ExportJob, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT `+jobCols+` FROM audit_evidence_export WHERE id = $1 AND tenant_id = $2`,
		jobID, tenantID,
	)
	job, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get audit evidence export job: %w", err)
	}
	return job, nil
}

func (r *Repo) ClaimNextJob(ctx context.Context, owner string) (*ExportJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin evidence claim tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		SELECT `+jobCols+`
		FROM audit_evidence_export
		WHERE status = 'queued'
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`)
	job, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim evidence job select: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE audit_evidence_export
		SET status = 'running', started_at = NOW(), claimed_at = NOW(),
		    claimed_by = $2, last_heartbeat = NOW()
		WHERE id = $1`,
		job.ID, owner,
	)
	if err != nil {
		return nil, fmt.Errorf("claim evidence job update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit evidence claim tx: %w", err)
	}

	now := time.Now().UTC()
	job.Status = JobRunning
	job.StartedAt = ptrext.Of(now)
	job.ClaimedAt = ptrext.Of(now)
	job.LastHeartbeat = ptrext.Of(now)
	job.ClaimedBy = owner
	return job, nil
}

func (r *Repo) HeartbeatJob(ctx context.Context, jobID, owner string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE audit_evidence_export SET last_heartbeat = NOW()
		WHERE id = $1 AND status = 'running' AND claimed_by = $2`,
		jobID, owner,
	)
	if err != nil {
		return 0, fmt.Errorf("heartbeat evidence job: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) CompleteJob(
	ctx context.Context,
	jobID, owner, archiveFilename string,
	archive []byte,
	totalEvents int,
	expiresAt time.Time,
) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE audit_evidence_export
		SET status = 'completed', archive = $2, archive_filename = $3,
		    total_events = $4, completed_at = NOW(), expires_at = $5,
		    claimed_at = NULL, claimed_by = '', error = ''
		WHERE id = $1 AND status = 'running' AND claimed_by = $6`,
		jobID, archive, archiveFilename, totalEvents, expiresAt.UTC(), owner,
	)
	if err != nil {
		return 0, fmt.Errorf("complete evidence job: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) FailJob(ctx context.Context, jobID, owner, errMsg string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE audit_evidence_export
		SET status = 'failed', error = $2, completed_at = NOW(),
		    claimed_at = NULL, claimed_by = ''
		WHERE id = $1 AND status = 'running' AND claimed_by = $3`,
		jobID, pgxutil.Truncate(errMsg, 1000), owner,
	)
	if err != nil {
		return 0, fmt.Errorf("fail evidence job: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) ExpireJobs(ctx context.Context, now time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE audit_evidence_export
		SET status = 'expired', archive = NULL, archive_filename = ''
		WHERE status = 'completed' AND expires_at IS NOT NULL AND expires_at <= $1`,
		now.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("expire evidence jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) RequeueStaleJobs(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-staleThreshold)
	tag, err := r.pool.Exec(ctx, `
		UPDATE audit_evidence_export
		SET status = 'queued', started_at = NULL, claimed_by = '', claimed_at = NULL, last_heartbeat = NULL
		WHERE status = 'running' AND last_heartbeat < $1`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("requeue stale evidence jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repo) MarkDownloaded(ctx context.Context, tenantID, jobID string) (*ExportJob, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE audit_evidence_export
		SET downloaded_at = COALESCE(downloaded_at, NOW())
		WHERE id = $1
		  AND tenant_id = $2
		  AND status = 'completed'
		  AND archive IS NOT NULL
		  AND expires_at IS NOT NULL
		  AND expires_at > NOW()
		RETURNING `+jobCols,
		jobID, tenantID,
	)
	job, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists int
		_ = r.pool.QueryRow(ctx, `SELECT 1 FROM audit_evidence_export WHERE id = $1 AND tenant_id = $2`, jobID, tenantID).Scan(&exists)
		if exists == 0 {
			return nil, ErrJobNotFound
		}
		return nil, ErrJobNotDownloadable
	}
	if err != nil {
		return nil, fmt.Errorf("mark evidence job downloaded: %w", err)
	}
	return job, nil
}
