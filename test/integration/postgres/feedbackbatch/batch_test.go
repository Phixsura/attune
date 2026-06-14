//go:build integration

// Package feedbackbatch_test — integration tests for batch operations (#30).
// Tests the full stack: repo layer batch operations against a real PostgreSQL database.
package feedbackbatch_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/testdb"
)

type env struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	feedback *feedback.FeedbackRepo
	tags     *feedbacktag.Repo
	tenantID string
}

func setup(t *testing.T) env {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()

	// Create tenant.
	var tenantID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('batch-test','Batch Test Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	return env{
		ctx:      ctx,
		pool:     pool,
		feedback: feedback.NewFeedback(pool),
		tags:     feedbacktag.New(pool),
		tenantID: tenantID,
	}
}

func (e env) insertFeedback(t *testing.T, content string) int64 {
	t.Helper()
	var id int64
	// Use same default workflow state selection as repo.Insert.
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO user_feedback (tenant_id, user_id, source, content, workflow_state_id)
		 VALUES ($1, 'u1', 'api', $2,
		   (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))
		 RETURNING id`,
		e.tenantID, content,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert feedback: %v", err)
	}
	return id
}

func (e env) insertFeedbackWithAttrs(t *testing.T, content string, attrs map[string]any) int64 {
	t.Helper()
	var id int64
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_attrs, enrichment_status, is_urgent)
		 VALUES ($1, 'u1', 'api', $2, $3::jsonb, 'done', COALESCE($3->>'is_urgent', 'false')::boolean) RETURNING id`,
		e.tenantID, content, attrs,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert feedback with attrs: %v", err)
	}
	return id
}

func (e env) createTag(t *testing.T, name string) *feedbacktag.Tag {
	t.Helper()
	tag, err := e.tags.Create(e.ctx, feedbacktag.Tag{
		TenantID: e.tenantID,
		Name:     name,
		Color:    "#0000ff", // Must be lowercase hex.
	})
	if err != nil {
		t.Fatalf("create tag %q: %v", name, err)
	}
	return tag
}

func (e env) insertWorkflowState(t *testing.T, name, category string, isDefault bool) string {
	t.Helper()
	id := uuid.NewString()
	// Use INSERT ... RETURNING with ON CONFLICT ... DO UPDATE to always get the id.
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO tenant_workflow_states (id, tenant_id, name, color, category, position, is_default)
		 VALUES ($1::uuid, $2, $3, '#666666', $4, 0, $5)
		 ON CONFLICT (tenant_id, name) WHERE archived_at IS NULL
		 DO UPDATE SET name = EXCLUDED.name -- no-op update to trigger RETURNING
		 RETURNING id::text`,
		id, e.tenantID, name, category, isDefault,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert workflow state %q: %v", name, err)
	}
	return id
}

func (e env) feedbackIsDeleted(t *testing.T, id int64) bool {
	t.Helper()
	var deleted bool
	err := e.pool.QueryRow(e.ctx,
		`SELECT deleted_at IS NOT NULL FROM user_feedback WHERE id = $1`,
		id,
	).Scan(&deleted) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("check deleted: %v", err)
	}
	return deleted
}

func (e env) feedbackExists(t *testing.T, id int64) bool {
	t.Helper()
	var exists bool
	err := e.pool.QueryRow(e.ctx,
		`SELECT EXISTS(SELECT 1 FROM user_feedback WHERE id = $1)`,
		id,
	).Scan(&exists) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("check exists: %v", err)
	}
	return exists
}

func (e env) feedbackWorkflowState(t *testing.T, id int64) *string {
	t.Helper()
	var stateID *string
	err := e.pool.QueryRow(e.ctx,
		`SELECT workflow_state_id::text FROM user_feedback WHERE id = $1`,
		id,
	).Scan(&stateID) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("get workflow state: %v", err)
	}
	return stateID
}

func (e env) feedbackHasTag(t *testing.T, feedbackID int64, tagID string) bool {
	t.Helper()
	var exists bool
	err := e.pool.QueryRow(e.ctx,
		`SELECT EXISTS(SELECT 1 FROM feedback_tag_assignments WHERE feedback_id = $1 AND tag_id = $2::uuid)`,
		feedbackID, tagID,
	).Scan(&exists) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("check tag assignment: %v", err)
	}
	return exists
}

// TestPG_BatchTagAdd tests adding tags to multiple feedback items.
func TestPG_BatchTagAdd(t *testing.T) {
	e := setup(t)

	// Create feedback items.
	fb1 := e.insertFeedback(t, "feedback one")
	fb2 := e.insertFeedback(t, "feedback two")

	// Create a tag.
	tag := e.createTag(t, "important")

	// Batch add tag.
	updates := []feedback.BatchTagUpdate{
		{FeedbackID: fb1, AddTags: []string{tag.ID.String()}},
		{FeedbackID: fb2, AddTags: []string{tag.ID.String()}},
	}
	result, err := e.feedback.BatchUpdateTags(e.ctx, e.tenantID, updates, nil)
	if err != nil {
		t.Fatalf("BatchUpdateTags: %v", err)
	}

	if result.Succeeded != 2 {
		t.Errorf("succeeded = %d, want 2", result.Succeeded)
	}
	if len(result.Failed) != 0 {
		t.Errorf("failed = %d, want 0", len(result.Failed))
	}

	// Verify tags were added.
	if !e.feedbackHasTag(t, fb1, tag.ID.String()) {
		t.Error("fb1 should have tag")
	}
	if !e.feedbackHasTag(t, fb2, tag.ID.String()) {
		t.Error("fb2 should have tag")
	}
}

// TestPG_BatchTagRemove tests removing tags from multiple feedback items.
func TestPG_BatchTagRemove(t *testing.T) {
	e := setup(t)

	// Create feedback and tag.
	fb1 := e.insertFeedback(t, "feedback one")
	fb2 := e.insertFeedback(t, "feedback two")
	tag := e.createTag(t, "to-remove")

	// Add tag first.
	addUpdates := []feedback.BatchTagUpdate{
		{FeedbackID: fb1, AddTags: []string{tag.ID.String()}},
		{FeedbackID: fb2, AddTags: []string{tag.ID.String()}},
	}
	_, err := e.feedback.BatchUpdateTags(e.ctx, e.tenantID, addUpdates, nil)
	if err != nil {
		t.Fatalf("add tags: %v", err)
	}

	// Verify tags are there.
	if !e.feedbackHasTag(t, fb1, tag.ID.String()) || !e.feedbackHasTag(t, fb2, tag.ID.String()) {
		t.Fatal("tags should be added")
	}

	// Batch remove tag.
	removeUpdates := []feedback.BatchTagUpdate{
		{FeedbackID: fb1, RemoveTags: []string{tag.ID.String()}},
		{FeedbackID: fb2, RemoveTags: []string{tag.ID.String()}},
	}
	result, err := e.feedback.BatchUpdateTags(e.ctx, e.tenantID, removeUpdates, nil)
	if err != nil {
		t.Fatalf("BatchUpdateTags remove: %v", err)
	}

	if result.Succeeded != 2 {
		t.Errorf("succeeded = %d, want 2", result.Succeeded)
	}

	// Verify tags were removed.
	if e.feedbackHasTag(t, fb1, tag.ID.String()) {
		t.Error("fb1 should not have tag")
	}
	if e.feedbackHasTag(t, fb2, tag.ID.String()) {
		t.Error("fb2 should not have tag")
	}
}

// TestPG_BatchWorkflowTransition tests workflow state transitions.
func TestPG_BatchWorkflowTransition(t *testing.T) {
	e := setup(t)

	// Create workflow states.
	openState := e.insertWorkflowState(t, "New", "open", true)
	activeState := e.insertWorkflowState(t, "In Progress", "active", false)

	// Create feedback items (they get the default state).
	fb1 := e.insertFeedback(t, "feedback one")
	fb2 := e.insertFeedback(t, "feedback two")

	// Verify initial state.
	if s := e.feedbackWorkflowState(t, fb1); s == nil || *s != openState {
		t.Errorf("fb1 initial state = %v, want %s", s, openState)
	}

	// Batch transition to active.
	// Note: passing empty comment to skip workflow transition audit (table may not exist).
	result, err := e.feedback.BatchUpdateWorkflow(e.ctx, e.tenantID, []int64{fb1, fb2}, activeState, "", nil)
	if err != nil {
		t.Fatalf("BatchUpdateWorkflow: %v", err)
	}

	if result.Succeeded != 2 {
		t.Errorf("succeeded = %d, want 2", result.Succeeded)
	}

	// Verify workflow state changed.
	if s := e.feedbackWorkflowState(t, fb1); s == nil || *s != activeState {
		t.Errorf("fb1 state = %v, want %s", s, activeState)
	}
	if s := e.feedbackWorkflowState(t, fb2); s == nil || *s != activeState {
		t.Errorf("fb2 state = %v, want %s", s, activeState)
	}
}

// TestPG_BatchSoftDelete tests soft delete of multiple feedback items.
func TestPG_BatchSoftDelete(t *testing.T) {
	e := setup(t)

	fb1 := e.insertFeedback(t, "feedback one")
	fb2 := e.insertFeedback(t, "feedback two")

	// Soft delete.
	result, err := e.feedback.BatchSoftDelete(e.ctx, e.tenantID, []int64{fb1, fb2}, nil)
	if err != nil {
		t.Fatalf("BatchSoftDelete: %v", err)
	}

	if result.Succeeded != 2 {
		t.Errorf("succeeded = %d, want 2", result.Succeeded)
	}

	// Verify items are soft deleted.
	if !e.feedbackIsDeleted(t, fb1) {
		t.Error("fb1 should be deleted")
	}
	if !e.feedbackIsDeleted(t, fb2) {
		t.Error("fb2 should be deleted")
	}

	// Verify items still exist in DB (soft delete).
	if !e.feedbackExists(t, fb1) {
		t.Error("fb1 should still exist")
	}
}

// TestPG_BatchHardDelete tests permanent deletion of multiple feedback items.
func TestPG_BatchHardDelete(t *testing.T) {
	e := setup(t)

	fb1 := e.insertFeedback(t, "feedback one")
	fb2 := e.insertFeedback(t, "feedback two")

	// Hard delete.
	result, err := e.feedback.BatchHardDelete(e.ctx, e.tenantID, []int64{fb1, fb2}, nil)
	if err != nil {
		t.Fatalf("BatchHardDelete: %v", err)
	}

	if result.Succeeded != 2 {
		t.Errorf("succeeded = %d, want 2", result.Succeeded)
	}

	// Verify items are gone.
	if e.feedbackExists(t, fb1) {
		t.Error("fb1 should not exist")
	}
	if e.feedbackExists(t, fb2) {
		t.Error("fb2 should not exist")
	}
}

// TestPG_BatchCountAndListByFilter tests filter-based counting and listing.
func TestPG_BatchCountAndListByFilter(t *testing.T) {
	e := setup(t)

	// Create feedback with different attributes.
	_ = e.insertFeedbackWithAttrs(t, "urgent feedback", map[string]any{"priority": "high"})
	id2 := e.insertFeedbackWithAttrs(t, "normal feedback", map[string]any{"priority": "low"})
	id3 := e.insertFeedbackWithAttrs(t, "another normal", map[string]any{"priority": "low"})

	// Mark id2 as urgent directly.
	_, err := e.pool.Exec(e.ctx, `UPDATE user_feedback SET is_urgent = true WHERE id = $1`, id2)
	if err != nil {
		t.Fatalf("mark urgent: %v", err)
	}

	// Count urgent items.
	urgentFilter := ptrext.Of(feedback.FeedbackFilter{Urgent: ptrext.Of(true)})
	count, err := e.feedback.CountByFilter(e.ctx, e.tenantID, urgentFilter)
	if err != nil {
		t.Fatalf("CountByFilter urgent: %v", err)
	}
	if count != 1 {
		t.Errorf("urgent count = %d, want 1", count)
	}

	// Count non-urgent items.
	nonUrgentFilter := ptrext.Of(feedback.FeedbackFilter{Urgent: ptrext.Of(false)})
	count, err = e.feedback.CountByFilter(e.ctx, e.tenantID, nonUrgentFilter)
	if err != nil {
		t.Fatalf("CountByFilter non-urgent: %v", err)
	}
	if count != 2 {
		t.Errorf("non-urgent count = %d, want 2", count)
	}

	// List IDs by filter.
	ids, err := e.feedback.ListIDsByFilter(e.ctx, e.tenantID, nonUrgentFilter, 100)
	if err != nil {
		t.Fatalf("ListIDsByFilter: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("non-urgent ids = %d, want 2", len(ids))
	}
	// Results should include id3 (we don't check id2 since it's now urgent).
	found := false
	for _, id := range ids {
		if id == id3 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("id3 should be in results: got %v", ids)
	}
}

// TestPG_BatchVersionConflict tests optimistic locking with if_unmodified_since.
func TestPG_BatchVersionConflict(t *testing.T) {
	e := setup(t)

	fb1 := e.insertFeedback(t, "feedback one")

	// Get the current updated_at.
	var updatedAt time.Time
	err := e.pool.QueryRow(e.ctx, `SELECT updated_at FROM user_feedback WHERE id = $1`, fb1).Scan(&updatedAt) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("get updated_at: %v", err)
	}

	// Try to soft delete with a time before the update.
	pastTime := updatedAt.Add(-1 * time.Hour)
	result, err := e.feedback.BatchSoftDelete(e.ctx, e.tenantID, []int64{fb1}, ptrext.Of(pastTime))
	if err != nil {
		t.Fatalf("BatchSoftDelete with version: %v", err)
	}

	// Should fail with version conflict.
	if result.Succeeded != 0 {
		t.Errorf("succeeded = %d, want 0", result.Succeeded)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("failed = %d, want 1", len(result.Failed))
	}
	if result.Failed[0].Code != feedback.BatchErrVersionConflict {
		t.Errorf("error code = %s, want %s", result.Failed[0].Code, feedback.BatchErrVersionConflict)
	}

	// Verify item is not deleted.
	if e.feedbackIsDeleted(t, fb1) {
		t.Error("fb1 should not be deleted")
	}
}

// TestPG_BatchNotFound tests handling of non-existent feedback items.
func TestPG_BatchNotFound(t *testing.T) {
	e := setup(t)

	fb1 := e.insertFeedback(t, "real feedback")
	nonExistent := int64(999999999)

	result, err := e.feedback.BatchSoftDelete(e.ctx, e.tenantID, []int64{fb1, nonExistent}, nil)
	if err != nil {
		t.Fatalf("BatchSoftDelete: %v", err)
	}

	if result.Succeeded != 1 {
		t.Errorf("succeeded = %d, want 1", result.Succeeded)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("failed = %d, want 1", len(result.Failed))
	}
	if result.Failed[0].FeedbackID != nonExistent {
		t.Errorf("failed id = %d, want %d", result.Failed[0].FeedbackID, nonExistent)
	}
	if result.Failed[0].Code != feedback.BatchErrNotFound {
		t.Errorf("error code = %s, want %s", result.Failed[0].Code, feedback.BatchErrNotFound)
	}
}

// TestPG_BatchSoftDeleteSkipsAlreadyDeleted tests that re-deleting is skipped.
func TestPG_BatchSoftDeleteSkipsAlreadyDeleted(t *testing.T) {
	e := setup(t)

	fb1 := e.insertFeedback(t, "feedback")

	// Soft delete once.
	_, err := e.feedback.BatchSoftDelete(e.ctx, e.tenantID, []int64{fb1}, nil)
	if err != nil {
		t.Fatalf("first delete: %v", err)
	}

	// Try to delete again.
	result, err := e.feedback.BatchSoftDelete(e.ctx, e.tenantID, []int64{fb1}, nil)
	if err != nil {
		t.Fatalf("second delete: %v", err)
	}

	// Should be skipped, not succeeded or failed.
	if result.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", result.Skipped)
	}
	if result.Succeeded != 0 {
		t.Errorf("succeeded = %d, want 0", result.Succeeded)
	}
}

// TestPG_BatchRestoreSoftDeleted tests restoring soft-deleted items.
func TestPG_BatchRestoreSoftDeleted(t *testing.T) {
	e := setup(t)

	fb1 := e.insertFeedback(t, "feedback")

	// Soft delete.
	_, err := e.feedback.BatchSoftDelete(e.ctx, e.tenantID, []int64{fb1}, nil)
	if err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if !e.feedbackIsDeleted(t, fb1) {
		t.Fatal("should be deleted")
	}

	// Restore.
	result, err := e.feedback.RestoreSoftDeleted(e.ctx, e.tenantID, []int64{fb1})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	if result.Succeeded != 1 {
		t.Errorf("succeeded = %d, want 1", result.Succeeded)
	}

	// Verify restored.
	if e.feedbackIsDeleted(t, fb1) {
		t.Error("should not be deleted after restore")
	}
}

// TestPG_BatchEmptyInput tests that empty input returns empty result.
func TestPG_BatchEmptyInput(t *testing.T) {
	e := setup(t)

	result, err := e.feedback.BatchSoftDelete(e.ctx, e.tenantID, []int64{}, nil)
	if err != nil {
		t.Fatalf("empty delete: %v", err)
	}
	if result.Succeeded != 0 || result.Skipped != 0 || len(result.Failed) != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}
