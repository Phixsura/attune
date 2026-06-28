// SPDX-License-Identifier: Apache-2.0

package gdpr

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

type RequestType string

const (
	RequestTypeExport RequestType = "export"
	RequestTypeDelete RequestType = "delete"
)

var ErrInvalidRequestCursor = errors.New("cursor must be gdpr request pagination token")

type RequestStatus string

const (
	RequestStatusQueued     RequestStatus = "queued"
	RequestStatusRunning    RequestStatus = "running"
	RequestStatusReady      RequestStatus = "ready"
	RequestStatusDownloaded RequestStatus = "downloaded"
	RequestStatusCompleted  RequestStatus = "completed"
	RequestStatusFailed     RequestStatus = "failed"
	RequestStatusExpired    RequestStatus = "expired"
	RequestStatusScheduled  RequestStatus = "scheduled"
	RequestStatusCancelled  RequestStatus = "cancelled"
	RequestStatusRevoked    RequestStatus = "revoked"
)

type Request struct {
	ID              string
	TenantID        string
	RequestType     RequestType
	Status          RequestStatus
	SubjectKey      string
	SubjectHash     string
	SubjectDisplay  string
	ArchiveFilename string
	Error           string
	CreatedByType   string
	CreatedBy       string
	Counts          Counts
	CreatedAt       time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	ExpiresAt       *time.Time
	DownloadedAt    *time.Time
	ExecuteAfter    *time.Time
	CancelledAt     *time.Time
	RevokedAt       *time.Time
}

type ListRequestFilter struct {
	TenantID    string
	Cursor      string
	Limit       int
	RequestType string
}

type ListRequestResult struct {
	Items      []Request
	NextCursor string
}

type OperationsSummary struct {
	QueuedRequestCount   int
	ActiveRequestCount   int
	ReadyExportCount     int
	ScheduledDeleteCount int
	NextExportExpiryAt   *time.Time
}

const requestCols = `id, tenant_id, request_type, status, subject_key, subject_hash, subject_display,
	feedback_count, tag_assignment_count, feedback_audit_count, llm_audit_count,
	archive_filename, COALESCE(error, ''), created_by_type, created_by, created_at, started_at, completed_at, expires_at, downloaded_at, execute_after, cancelled_at, revoked_at`

func scanRequest(row pgx.Row) (*Request, error) {
	var req Request
	if err := row.Scan(
		&req.ID,
		&req.TenantID,
		&req.RequestType,
		&req.Status,
		&req.SubjectKey,
		&req.SubjectHash,
		&req.SubjectDisplay,
		&req.Counts.FeedbackCount,
		&req.Counts.TagAssignmentCount,
		&req.Counts.FeedbackAuditCount,
		&req.Counts.LLMAuditCount,
		&req.ArchiveFilename,
		&req.Error,
		&req.CreatedByType,
		&req.CreatedBy,
		&req.CreatedAt,
		&req.StartedAt,
		&req.CompletedAt,
		&req.ExpiresAt,
		&req.DownloadedAt,
		&req.ExecuteAfter,
		&req.CancelledAt,
		&req.RevokedAt,
	); err != nil {
		return nil, err
	}
	return ptrext.Of(req), nil
}

func newRequestID() string {
	return uuid.NewString()
}

var ErrRequestNotFound = fmt.Errorf("gdpr request not found")

func (r *Repo) ListRequests(ctx context.Context, filter ListRequestFilter) (ListRequestResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	query := `SELECT ` + requestCols + ` FROM gdpr_requests WHERE tenant_id = $1`
	args := []any{filter.TenantID}
	if trimmed := strings.TrimSpace(filter.RequestType); trimmed != "" {
		args = append(args, trimmed)
		query += fmt.Sprintf(" AND request_type = $%d", len(args))
	}
	if trimmed := strings.TrimSpace(filter.Cursor); trimmed != "" {
		cursorTime, cursorID, err := parseRequestCursor(trimmed)
		if err != nil {
			return ListRequestResult{}, err
		}
		args = append(args, cursorTime, cursorID)
		query += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ListRequestResult{}, fmt.Errorf("list gdpr requests: %w", err)
	}
	defer rows.Close()

	items := make([]Request, 0, limit+1)
	for rows.Next() {
		item, err := scanRequest(rows)
		if err != nil {
			return ListRequestResult{}, fmt.Errorf("scan gdpr request: %w", err)
		}
		items = append(items, ptrext.Indirect(item))
	}
	if err := rows.Err(); err != nil {
		return ListRequestResult{}, fmt.Errorf("iterate gdpr requests: %w", err)
	}

	if len(items) <= limit {
		return ListRequestResult{Items: items}, nil
	}
	return ListRequestResult{
		Items:      items[:limit],
		NextCursor: formatRequestCursor(items[limit-1]),
	}, nil
}

func (r *Repo) GetOperationsSummary(ctx context.Context, tenantID string) (*OperationsSummary, error) {
	summary := ptrext.Of(OperationsSummary{})
	if err := r.pool.QueryRow(
		ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'queued') AS queued_count,
			COUNT(*) FILTER (WHERE status IN ('queued', 'running', 'scheduled')) AS active_count,
			COUNT(*) FILTER (WHERE request_type = 'export' AND status IN ('ready', 'downloaded')) AS ready_export_count,
			COUNT(*) FILTER (WHERE request_type = 'delete' AND status = 'scheduled') AS scheduled_delete_count,
			MIN(expires_at) FILTER (WHERE request_type = 'export' AND status IN ('ready', 'downloaded') AND expires_at IS NOT NULL) AS next_export_expiry_at
		FROM gdpr_requests
		WHERE tenant_id = $1`,
		tenantID,
	).Scan(
		&summary.QueuedRequestCount,
		&summary.ActiveRequestCount,
		&summary.ReadyExportCount,
		&summary.ScheduledDeleteCount,
		&summary.NextExportExpiryAt,
	); err != nil {
		return nil, fmt.Errorf("get gdpr operations summary: %w", err)
	}
	return summary, nil
}

func (r *Repo) CreateDeleteRequest(
	ctx context.Context,
	tenantID, subjectKey, subjectHash, createdByType, createdBy string,
	executeAfter time.Time,
) (*DeleteResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	info, err := subjectInfoTx(ctx, tx, tenantID, subjectKey)
	if err != nil {
		return nil, err
	}

	var counts Counts
	if err := tx.QueryRow(
		ctx, `
		SELECT
			(SELECT COUNT(*) FROM feedback_tag_assignments WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM feedback_audit_log WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM llm_audit WHERE feedback_id = ANY($1))`,
		info.feedbackIDs,
	).Scan(&counts.TagAssignmentCount, &counts.FeedbackAuditCount, &counts.LLMAuditCount); err != nil {
		return nil, fmt.Errorf("count subject-linked rows: %w", err)
	}
	counts.FeedbackCount = len(info.feedbackIDs)

	requestID := newRequestID()
	if _, err := tx.Exec(
		ctx, `
		INSERT INTO gdpr_requests (
			id, tenant_id, request_type, status, subject_key, subject_hash, subject_display,
			feedback_count, tag_assignment_count, feedback_audit_count, llm_audit_count,
			created_by_type, created_by, execute_after
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		requestID, tenantID, RequestTypeDelete, RequestStatusScheduled, subjectKey, subjectHash, info.subjectDisplay,
		counts.FeedbackCount, counts.TagAssignmentCount, counts.FeedbackAuditCount, counts.LLMAuditCount,
		createdByType, createdBy, executeAfter.UTC(),
	); err != nil {
		return nil, fmt.Errorf("insert gdpr delete request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ptrext.Of(DeleteResult{
		RequestID:    requestID,
		SubjectKey:   subjectKey,
		Counts:       counts,
		ExecuteAfter: ptrext.Of(executeAfter.UTC()),
		Status:       RequestStatusScheduled,
	}), nil
}

var ErrDeleteRequestNotCancellable = fmt.Errorf("gdpr delete request not cancellable")

func (r *Repo) CancelDeleteRequest(ctx context.Context, tenantID, requestID string) (*Request, error) {
	row := r.pool.QueryRow(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'cancelled', cancelled_at = NOW(), completed_at = NOW()
		WHERE id = $1
		  AND tenant_id = $2
		  AND request_type = 'delete'
		  AND status = 'scheduled'
		RETURNING `+requestCols,
		requestID, tenantID,
	)
	req, err := scanRequest(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDeleteRequestNotCancellable
		}
		return nil, fmt.Errorf("cancel gdpr delete request: %w", err)
	}
	return req, nil
}

func (r *Repo) ClaimNextDeleteRequest(ctx context.Context, now time.Time) (*Request, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin delete claim tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(
		ctx, `
		SELECT `+requestCols+`
		FROM gdpr_requests
		WHERE request_type = 'delete'
		  AND status = 'scheduled'
		  AND execute_after IS NOT NULL
		  AND execute_after <= $1
		ORDER BY execute_after ASC, created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`,
		now.UTC(),
	)
	req, err := scanRequest(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select gdpr delete request: %w", err)
	}
	if _, err := tx.Exec(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'running', started_at = NOW(), error = ''
		WHERE id = $1`,
		req.ID,
	); err != nil {
		return nil, fmt.Errorf("claim gdpr delete request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit delete claim tx: %w", err)
	}
	nowUTC := now.UTC()
	req.Status = RequestStatusRunning
	req.StartedAt = ptrext.Of(nowUTC)
	return req, nil
}

func (r *Repo) CompleteDeleteRequest(ctx context.Context, requestID string, counts Counts) error {
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'completed',
		    feedback_count = $2,
		    tag_assignment_count = $3,
		    feedback_audit_count = $4,
		    llm_audit_count = $5,
		    completed_at = NOW(),
		    error = ''
		WHERE id = $1 AND request_type = 'delete'`,
		requestID, counts.FeedbackCount, counts.TagAssignmentCount, counts.FeedbackAuditCount, counts.LLMAuditCount,
	)
	if err != nil {
		return fmt.Errorf("complete gdpr delete request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRequestNotFound
	}
	return nil
}

func (r *Repo) FailDeleteRequest(ctx context.Context, requestID, errMsg string) error {
	tag, err := r.pool.Exec(
		ctx, `
		UPDATE gdpr_requests
		SET status = 'failed', error = $2, completed_at = NOW()
		WHERE id = $1 AND request_type = 'delete'`,
		requestID, pgxutil.Truncate(errMsg, 1000),
	)
	if err != nil {
		return fmt.Errorf("fail gdpr delete request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRequestNotFound
	}
	return nil
}

func formatRequestCursor(item Request) string {
	return fmt.Sprintf("%d:%s", item.CreatedAt.UTC().UnixNano(), item.ID)
}

func parseRequestCursor(raw string) (time.Time, string, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("%w", ErrInvalidRequestCursor)
	}
	unixNanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w", ErrInvalidRequestCursor)
	}
	if strings.TrimSpace(parts[1]) == "" {
		return time.Time{}, "", fmt.Errorf("%w", ErrInvalidRequestCursor)
	}
	return time.Unix(0, unixNanos).UTC(), parts[1], nil
}
