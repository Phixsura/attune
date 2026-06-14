// SPDX-License-Identifier: Apache-2.0

// feedback_batch.go — batch operations for user_feedback: batch tag updates,
// workflow transitions, soft/hard deletes, and filter-based ID listing.
// Split from feedback.go to honor the no-grab-bag-files guidance (#30).
package feedback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Batch operation limits.
const (
	MaxBatchSize      = 500
	MaxFilterIDsLimit = 10000
)

// Batch operation error codes returned in ItemFailure.Code.
const (
	BatchErrNotFound        = "not_found"
	BatchErrVersionConflict = "version_conflict"
	BatchErrDeleted         = "deleted"
	BatchErrInvalidState    = "invalid_state"
)

// BatchTagUpdate describes tag modifications for one feedback item.
type BatchTagUpdate struct {
	FeedbackID int64
	AddTags    []string // tag UUIDs to add
	RemoveTags []string // tag UUIDs to remove
}

// BatchResult summarizes a batch operation outcome.
type BatchResult struct {
	Succeeded int
	Skipped   int
	Failed    []ItemFailure
}

// ItemFailure describes why a single item in a batch failed.
type ItemFailure struct {
	FeedbackID int64
	Code       string
	Message    string
	UpdatedAt  *time.Time // current updated_at if version conflict
}

// BatchUpdateTags updates tags for multiple feedback items within a
// transaction. Each item is verified for tenant ownership and optionally
// checked against ifUnmodifiedSince for optimistic locking.
func (r *FeedbackRepo) BatchUpdateTags(
	ctx context.Context,
	tenantID string,
	updates []BatchTagUpdate,
	ifUnmodifiedSince *time.Time,
) (*BatchResult, error) {
	const where = "repo.FeedbackRepo.BatchUpdateTags"

	if len(updates) == 0 {
		return ptrext.Of(BatchResult{}), nil
	}
	if len(updates) > MaxBatchSize {
		return nil, fmt.Errorf("%s: batch size %d exceeds max %d", where, len(updates), MaxBatchSize)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin tx: %w", where, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := ptrext.Of(BatchResult{})

	for _, u := range updates {
		failure := r.processTagUpdate(ctx, tx, tenantID, u, ifUnmodifiedSince)
		if failure != nil {
			result.Failed = append(result.Failed, ptrext.Indirect(failure)) // ptrext:allow append-deref
		} else {
			result.Succeeded++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("%s: commit: %w", where, err)
	}

	return result, nil
}

func (r *FeedbackRepo) processTagUpdate(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	u BatchTagUpdate,
	ifUnmodifiedSince *time.Time,
) *ItemFailure {
	// Verify feedback exists, belongs to tenant, and check version/deleted status.
	var updatedAt time.Time
	var deletedAt *time.Time
	err := tx.QueryRow(ctx,
		`SELECT updated_at, deleted_at FROM user_feedback
		 WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		u.FeedbackID, tenantID,
	).Scan(&updatedAt, &deletedAt) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return ptrext.Of(ItemFailure{
			FeedbackID: u.FeedbackID,
			Code:       BatchErrNotFound,
			Message:    "feedback not found",
		})
	}
	if err != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: u.FeedbackID,
			Code:       BatchErrNotFound,
			Message:    fmt.Sprintf("query error: %v", err),
		})
	}

	if deletedAt != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: u.FeedbackID,
			Code:       BatchErrDeleted,
			Message:    "feedback is deleted",
		})
	}

	if ifUnmodifiedSince != nil && updatedAt.After(ptrext.Indirect(ifUnmodifiedSince)) {
		return ptrext.Of(ItemFailure{
			FeedbackID: u.FeedbackID,
			Code:       BatchErrVersionConflict,
			Message:    "feedback modified since specified time",
			UpdatedAt:  ptrext.Of(updatedAt),
		})
	}

	// Add tags via INSERT ... ON CONFLICT DO NOTHING.
	for _, tagID := range u.AddTags {
		_, err := tx.Exec(ctx,
			`INSERT INTO feedback_tag_assignments (feedback_id, tag_id, created_by)
			 SELECT $1, $2::uuid, 'batch'
			 WHERE EXISTS (SELECT 1 FROM tenant_feedback_tags WHERE id = $2::uuid AND tenant_id = $3)
			 ON CONFLICT (feedback_id, tag_id) DO NOTHING`,
			u.FeedbackID, tagID, tenantID,
		)
		if err != nil {
			return ptrext.Of(ItemFailure{
				FeedbackID: u.FeedbackID,
				Code:       BatchErrInvalidState,
				Message:    fmt.Sprintf("add tag %s: %v", tagID, err),
			})
		}
	}

	// Remove tags via DELETE.
	for _, tagID := range u.RemoveTags {
		_, err := tx.Exec(ctx,
			`DELETE FROM feedback_tag_assignments
			 WHERE feedback_id = $1 AND tag_id = $2::uuid`,
			u.FeedbackID, tagID,
		)
		if err != nil {
			return ptrext.Of(ItemFailure{
				FeedbackID: u.FeedbackID,
				Code:       BatchErrInvalidState,
				Message:    fmt.Sprintf("remove tag %s: %v", tagID, err),
			})
		}
	}

	// Touch updated_at (trigger handles this, but explicit update ensures it).
	_, err = tx.Exec(ctx,
		`UPDATE user_feedback SET updated_at = NOW() WHERE id = $1`,
		u.FeedbackID,
	)
	if err != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: u.FeedbackID,
			Code:       BatchErrInvalidState,
			Message:    fmt.Sprintf("touch updated_at: %v", err),
		})
	}

	return nil
}

// BatchUpdateWorkflow transitions multiple feedback items to a new workflow
// state within a transaction.
func (r *FeedbackRepo) BatchUpdateWorkflow(
	ctx context.Context,
	tenantID string,
	feedbackIDs []int64,
	toStateID string,
	comment string,
	ifUnmodifiedSince *time.Time,
) (*BatchResult, error) {
	const where = "repo.FeedbackRepo.BatchUpdateWorkflow"

	if len(feedbackIDs) == 0 {
		return ptrext.Of(BatchResult{}), nil
	}
	if len(feedbackIDs) > MaxBatchSize {
		return nil, fmt.Errorf("%s: batch size %d exceeds max %d", where, len(feedbackIDs), MaxBatchSize)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin tx: %w", where, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Validate target state exists and belongs to tenant.
	var stateExists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM tenant_workflow_states
			WHERE id = $1::uuid AND tenant_id = $2 AND archived_at IS NULL
		)`, toStateID, tenantID,
	).Scan(&stateExists) // ptrext:allow scan-target
	if err != nil {
		return nil, fmt.Errorf("%s: check state: %w", where, err)
	}
	if !stateExists {
		return nil, fmt.Errorf("%s: target state %s not found", where, toStateID)
	}

	result := ptrext.Of(BatchResult{})

	for _, fbID := range feedbackIDs {
		failure := r.processWorkflowUpdate(ctx, tx, tenantID, fbID, toStateID, comment, ifUnmodifiedSince)
		if failure != nil {
			result.Failed = append(result.Failed, ptrext.Indirect(failure)) // ptrext:allow append-deref
		} else {
			result.Succeeded++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("%s: commit: %w", where, err)
	}

	return result, nil
}

func (r *FeedbackRepo) processWorkflowUpdate(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	feedbackID int64,
	toStateID string,
	comment string,
	ifUnmodifiedSince *time.Time,
) *ItemFailure {
	var updatedAt time.Time
	var deletedAt *time.Time
	var currentStateID *string
	err := tx.QueryRow(ctx,
		`SELECT updated_at, deleted_at, workflow_state_id
		 FROM user_feedback
		 WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		feedbackID, tenantID,
	).Scan(&updatedAt, &deletedAt, &currentStateID) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrNotFound,
			Message:    "feedback not found",
		})
	}
	if err != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrNotFound,
			Message:    fmt.Sprintf("query error: %v", err),
		})
	}

	if deletedAt != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrDeleted,
			Message:    "feedback is deleted",
		})
	}

	if ifUnmodifiedSince != nil && updatedAt.After(ptrext.Indirect(ifUnmodifiedSince)) {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrVersionConflict,
			Message:    "feedback modified since specified time",
			UpdatedAt:  ptrext.Of(updatedAt),
		})
	}

	// Skip if already in target state.
	if currentStateID != nil && ptrext.Indirect(currentStateID) == toStateID {
		return nil
	}

	// Update workflow state.
	_, err = tx.Exec(ctx,
		`UPDATE user_feedback
		 SET workflow_state_id = $2::uuid, workflow_updated_at = NOW()
		 WHERE id = $1`,
		feedbackID, toStateID,
	)
	if err != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrInvalidState,
			Message:    fmt.Sprintf("update workflow state: %v", err),
		})
	}

	// Insert workflow transition audit record if transition history table exists.
	// This is optional - if the table doesn't exist or insert fails, we still proceed.
	if comment != "" && currentStateID != nil {
		_, _ = tx.Exec(ctx,
			`INSERT INTO feedback_workflow_transitions
			 (feedback_id, from_state_id, to_state_id, comment, created_by)
			 VALUES ($1, $2::uuid, $3::uuid, $4, 'batch')
			 ON CONFLICT DO NOTHING`,
			feedbackID, ptrext.Indirect(currentStateID), toStateID, comment,
		)
	}

	return nil
}

// BatchSoftDelete marks multiple feedback items as deleted (sets deleted_at).
func (r *FeedbackRepo) BatchSoftDelete(
	ctx context.Context,
	tenantID string,
	feedbackIDs []int64,
	ifUnmodifiedSince *time.Time,
) (*BatchResult, error) {
	const where = "repo.FeedbackRepo.BatchSoftDelete"

	if len(feedbackIDs) == 0 {
		return ptrext.Of(BatchResult{}), nil
	}
	if len(feedbackIDs) > MaxBatchSize {
		return nil, fmt.Errorf("%s: batch size %d exceeds max %d", where, len(feedbackIDs), MaxBatchSize)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin tx: %w", where, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := ptrext.Of(BatchResult{})

	for _, fbID := range feedbackIDs {
		failure := r.processSoftDelete(ctx, tx, tenantID, fbID, ifUnmodifiedSince)
		if failure != nil {
			if failure.Code == BatchErrDeleted {
				result.Skipped++
			} else {
				result.Failed = append(result.Failed, ptrext.Indirect(failure)) // ptrext:allow append-deref
			}
		} else {
			result.Succeeded++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("%s: commit: %w", where, err)
	}

	return result, nil
}

func (r *FeedbackRepo) processSoftDelete(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	feedbackID int64,
	ifUnmodifiedSince *time.Time,
) *ItemFailure {
	var updatedAt time.Time
	var deletedAt *time.Time
	err := tx.QueryRow(ctx,
		`SELECT updated_at, deleted_at FROM user_feedback
		 WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		feedbackID, tenantID,
	).Scan(&updatedAt, &deletedAt) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrNotFound,
			Message:    "feedback not found",
		})
	}
	if err != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrNotFound,
			Message:    fmt.Sprintf("query error: %v", err),
		})
	}

	if deletedAt != nil {
		// Already deleted - skip rather than fail.
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrDeleted,
			Message:    "feedback already deleted",
		})
	}

	if ifUnmodifiedSince != nil && updatedAt.After(ptrext.Indirect(ifUnmodifiedSince)) {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrVersionConflict,
			Message:    "feedback modified since specified time",
			UpdatedAt:  ptrext.Of(updatedAt),
		})
	}

	_, err = tx.Exec(ctx,
		`UPDATE user_feedback SET deleted_at = NOW() WHERE id = $1`,
		feedbackID,
	)
	if err != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrInvalidState,
			Message:    fmt.Sprintf("soft delete: %v", err),
		})
	}

	return nil
}

// BatchHardDelete permanently removes multiple feedback items.
func (r *FeedbackRepo) BatchHardDelete(
	ctx context.Context,
	tenantID string,
	feedbackIDs []int64,
	ifUnmodifiedSince *time.Time,
) (*BatchResult, error) {
	const where = "repo.FeedbackRepo.BatchHardDelete"

	if len(feedbackIDs) == 0 {
		return ptrext.Of(BatchResult{}), nil
	}
	if len(feedbackIDs) > MaxBatchSize {
		return nil, fmt.Errorf("%s: batch size %d exceeds max %d", where, len(feedbackIDs), MaxBatchSize)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin tx: %w", where, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := ptrext.Of(BatchResult{})

	for _, fbID := range feedbackIDs {
		failure := r.processHardDelete(ctx, tx, tenantID, fbID, ifUnmodifiedSince)
		if failure != nil {
			result.Failed = append(result.Failed, ptrext.Indirect(failure)) // ptrext:allow append-deref
		} else {
			result.Succeeded++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("%s: commit: %w", where, err)
	}

	return result, nil
}

func (r *FeedbackRepo) processHardDelete(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	feedbackID int64,
	ifUnmodifiedSince *time.Time,
) *ItemFailure {
	// Check existence and version before delete.
	var updatedAt time.Time
	err := tx.QueryRow(ctx,
		`SELECT updated_at FROM user_feedback
		 WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		feedbackID, tenantID,
	).Scan(&updatedAt) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrNotFound,
			Message:    "feedback not found",
		})
	}
	if err != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrNotFound,
			Message:    fmt.Sprintf("query error: %v", err),
		})
	}

	if ifUnmodifiedSince != nil && updatedAt.After(ptrext.Indirect(ifUnmodifiedSince)) {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrVersionConflict,
			Message:    "feedback modified since specified time",
			UpdatedAt:  ptrext.Of(updatedAt),
		})
	}

	// Delete tag assignments first (foreign key).
	_, err = tx.Exec(ctx,
		`DELETE FROM feedback_tag_assignments WHERE feedback_id = $1`,
		feedbackID,
	)
	if err != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrInvalidState,
			Message:    fmt.Sprintf("delete tag assignments: %v", err),
		})
	}

	// Delete embedding task if exists.
	_, _ = tx.Exec(ctx,
		`DELETE FROM embedding_task WHERE feedback_id = $1`,
		feedbackID,
	)

	// Delete the feedback row.
	ct, err := tx.Exec(ctx,
		`DELETE FROM user_feedback WHERE id = $1 AND tenant_id = $2`,
		feedbackID, tenantID,
	)
	if err != nil {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrInvalidState,
			Message:    fmt.Sprintf("hard delete: %v", err),
		})
	}
	if ct.RowsAffected() == 0 {
		return ptrext.Of(ItemFailure{
			FeedbackID: feedbackID,
			Code:       BatchErrNotFound,
			Message:    "feedback disappeared during delete",
		})
	}

	return nil
}

// FeedbackFilter defines criteria for filtering feedback items.
// Used by CountByFilter and ListIDsByFilter for query-based batch operations.
type FeedbackFilter struct {
	Attrs            []AttrFilter // per-dim JSONB containment filters
	Urgent           *bool        // nil = no filter
	Q                string       // ILIKE on content/titles
	TagIDs           []string     // require all these tag UUIDs
	WorkflowStateIDs []string     // any of these workflow state UUIDs
	WorkflowCategory *string      // "open"/"active"/"closed"
	IncludeDeleted   bool         // include soft-deleted items (default false)
	CreatedAfter     *time.Time   // created_at >= this
	CreatedBefore    *time.Time   // created_at < this
}

// CountByFilter returns count of feedback matching the filter.
func (r *FeedbackRepo) CountByFilter(
	ctx context.Context,
	tenantID string,
	filter *FeedbackFilter,
) (int64, error) {
	const where = "repo.FeedbackRepo.CountByFilter"

	qb := newQueryBuilder(tenantID)
	if err := r.applyBatchFilters(qb, filter); err != nil {
		return 0, fmt.Errorf("%s: %w", where, err)
	}

	query := "SELECT COUNT(*) FROM user_feedback " + qb.where
	var count int64
	err := r.pool.QueryRow(ctx, query, qb.args...).Scan(&count) // ptrext:allow scan-target
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return 0, fmt.Errorf("%s: %w", where, err)
	}
	return count, nil
}

// ListIDsByFilter returns feedback IDs matching the filter.
// Limit is required and capped at MaxFilterIDsLimit for safety.
func (r *FeedbackRepo) ListIDsByFilter(
	ctx context.Context,
	tenantID string,
	filter *FeedbackFilter,
	limit int,
) ([]int64, error) {
	const where = "repo.FeedbackRepo.ListIDsByFilter"

	if limit <= 0 {
		limit = 100
	}
	if limit > MaxFilterIDsLimit {
		limit = MaxFilterIDsLimit
	}

	qb := newQueryBuilder(tenantID)
	if err := r.applyBatchFilters(qb, filter); err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}

	query := "SELECT id FROM user_feedback " + qb.where +
		" ORDER BY id DESC LIMIT " + qb.addArg(limit)

	rows, err := r.pool.Query(ctx, query, qb.args...)
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("%s: %w", where, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil { // ptrext:allow scan-target
			return nil, fmt.Errorf("%s: scan: %w", where, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *FeedbackRepo) applyBatchFilters(qb *queryBuilder, filter *FeedbackFilter) error {
	// Include/exclude deleted items.
	if filter == nil || !filter.IncludeDeleted {
		qb.and("deleted_at IS NULL")
	}

	if filter == nil {
		return nil
	}

	// Attribute containment filters.
	for _, f := range filter.Attrs {
		if f.Dim == "" || f.Value == "" {
			continue
		}
		clause, err := containmentClause(f)
		if err != nil {
			return err
		}
		qb.and("enriched_attrs @> " + qb.addArg(clause) + "::jsonb")
	}

	// Urgency filter.
	if filter.Urgent != nil {
		qb.and("is_urgent = " + qb.addArg(ptrext.Indirect(filter.Urgent)))
	}

	// Text search.
	if filter.Q != "" {
		p := qb.addArg("%" + filter.Q + "%")
		qb.and("(content ILIKE " + p +
			" OR enriched_title ILIKE " + p +
			" OR enriched_display_title ILIKE " + p + ")")
	}

	// Tag filters - require all specified tags.
	for _, tagID := range filter.TagIDs {
		qb.and("EXISTS (SELECT 1 FROM feedback_tag_assignments fta WHERE fta.feedback_id = id AND fta.tag_id = " + qb.addArg(tagID) + "::uuid)")
	}

	// Workflow state filters.
	if len(filter.WorkflowStateIDs) > 0 {
		qb.and("workflow_state_id = ANY(" + qb.addArg(filter.WorkflowStateIDs) + "::uuid[])")
	}

	// Workflow category filter.
	if filter.WorkflowCategory != nil {
		qb.and("workflow_state_id IN (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND category = " + qb.addArg(ptrext.Indirect(filter.WorkflowCategory)) + ")")
	}

	// Time range filters.
	if filter.CreatedAfter != nil {
		qb.and("created_at >= " + qb.addArg(ptrext.Indirect(filter.CreatedAfter)))
	}
	if filter.CreatedBefore != nil {
		qb.and("created_at < " + qb.addArg(ptrext.Indirect(filter.CreatedBefore)))
	}

	return nil
}

// RestoreSoftDeleted restores soft-deleted feedback items.
func (r *FeedbackRepo) RestoreSoftDeleted(
	ctx context.Context,
	tenantID string,
	feedbackIDs []int64,
) (*BatchResult, error) {
	const where = "repo.FeedbackRepo.RestoreSoftDeleted"

	if len(feedbackIDs) == 0 {
		return ptrext.Of(BatchResult{}), nil
	}
	if len(feedbackIDs) > MaxBatchSize {
		return nil, fmt.Errorf("%s: batch size %d exceeds max %d", where, len(feedbackIDs), MaxBatchSize)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin tx: %w", where, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := ptrext.Of(BatchResult{})

	for _, fbID := range feedbackIDs {
		ct, err := tx.Exec(ctx,
			`UPDATE user_feedback SET deleted_at = NULL
			 WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NOT NULL`,
			fbID, tenantID,
		)
		if err != nil {
			result.Failed = append(result.Failed, ItemFailure{
				FeedbackID: fbID,
				Code:       BatchErrInvalidState,
				Message:    fmt.Sprintf("restore: %v", err),
			})
			continue
		}
		if ct.RowsAffected() == 0 {
			// Either not found or not deleted.
			var exists bool
			_ = tx.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM user_feedback WHERE id = $1 AND tenant_id = $2)`,
				fbID, tenantID,
			).Scan(&exists) // ptrext:allow scan-target
			if !exists {
				result.Failed = append(result.Failed, ItemFailure{
					FeedbackID: fbID,
					Code:       BatchErrNotFound,
					Message:    "feedback not found",
				})
			} else {
				result.Skipped++ // Not deleted, nothing to restore.
			}
		} else {
			result.Succeeded++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return nil, fmt.Errorf("%s: commit: %w", where, err)
	}

	return result, nil
}
