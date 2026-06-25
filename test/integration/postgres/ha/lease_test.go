// SPDX-License-Identifier: Apache-2.0
//go:build integration

package ha

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/taskoutbox"
	"github.com/Phixsura/attune/internal/testdb"
)

// ============================================================================
// Core HA Lease Tests
// ============================================================================

func TestConcurrentClaim_NoDoubleClaim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	var mu sync.Mutex
	claimed := make(map[string]bool)
	var wg sync.WaitGroup

	// Simulate 10 workers competing for the same task
	for i := 0; i < 10; i++ {
		wg.Add(1)
		owner := "worker-" + string(rune('A'+i))
		go func(owner string) {
			defer wg.Done()
			task, err := q.TryClaimWithOwner(ctx, 5*time.Minute, owner)
			if err == nil && task != nil && task.ID == taskID {
				mu.Lock()
				claimed[owner] = true
				mu.Unlock()
			}
		}(owner)
	}

	wg.Wait()

	// Exactly one worker should have claimed the task
	require.Len(t, claimed, 1, "exactly one worker should claim the task")
}

func TestFencing_OldOwnerCannotComplete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Worker A claims the task
	task, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, taskID, task.ID)

	// Simulate timeout: reset claim and let worker B claim
	_, err = pool.Exec(ctx, `
		UPDATE reply_draft_task
		SET claimed_at = NOW() - INTERVAL '10 minutes', claimed_by = NULL
		WHERE id = $1`, taskID)
	require.NoError(t, err)

	// Worker B claims the task
	task2, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-B")
	require.NoError(t, err)
	require.NotNil(t, task2)
	require.Equal(t, taskID, task2.ID)

	// Worker A tries to complete (should fail due to fencing)
	n, err := q.MarkDone(ctx, taskID, "worker-A")
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "old owner should not be able to complete")

	// Worker B can complete
	n, err = q.MarkDone(ctx, taskID, "worker-B")
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "current owner should complete successfully")
}

func TestHeartbeatRefresh_ExtendsLease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim the task
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Get initial claimed_at
	var initialClaim time.Time
	err = pool.QueryRow(ctx, `SELECT claimed_at FROM reply_draft_task WHERE id = $1`, taskID).Scan(&initialClaim)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Refresh the claim
	n, err := q.RefreshClaim(ctx, taskID, "worker-A")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	// Verify claimed_at was updated
	var newClaim time.Time
	err = pool.QueryRow(ctx, `SELECT claimed_at FROM reply_draft_task WHERE id = $1`, taskID).Scan(&newClaim)
	require.NoError(t, err)
	require.True(t, newClaim.After(initialClaim), "heartbeat should extend lease")
}

func TestHeartbeatRefresh_DetectsLeaseLost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Worker A claims
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Simulate worker B stealing the claim
	_, err = pool.Exec(ctx, `
		UPDATE reply_draft_task SET claimed_by = 'worker-B', claimed_at = NOW()
		WHERE id = $1`, taskID)
	require.NoError(t, err)

	// Worker A's heartbeat should return 0
	n, err := q.RefreshClaim(ctx, taskID, "worker-A")
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "heartbeat should detect lease lost")
}

func TestStaleClaimRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim the task
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Make the claim stale
	_, err = pool.Exec(ctx, `
		UPDATE reply_draft_task
		SET claimed_at = NOW() - INTERVAL '10 minutes'
		WHERE id = $1`, taskID)
	require.NoError(t, err)

	// Reset stale claims
	reset, err := q.ResetStaleClaims(ctx, 5*time.Minute)
	require.NoError(t, err)
	require.Equal(t, int64(1), reset, "should recover one stale claim")

	// Worker B can now claim
	task, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-B")
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, taskID, task.ID)
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestClaim_EmptyOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim with empty owner (legacy mode)
	task, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "")
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, taskID, task.ID)
	require.Equal(t, "", task.ClaimedBy)
}

func TestRefreshClaim_NonExistentTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Refresh a non-existent task
	n, err := q.RefreshClaim(ctx, 999999, "worker-A")
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "should return 0 for non-existent task")
}

func TestRefreshClaim_WrongOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim with worker-A
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Try to refresh with worker-B
	n, err := q.RefreshClaim(ctx, taskID, "worker-B")
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "should not refresh with wrong owner")
}

func TestMarkDone_NonExistentTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Mark done a non-existent task
	n, err := q.MarkDone(ctx, 999999, "worker-A")
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "should return 0 for non-existent task")
}

func TestMarkDone_AlreadyDone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim and complete
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	n, err := q.MarkDone(ctx, taskID, "worker-A")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	// Try to complete again
	n, err = q.MarkDone(ctx, taskID, "worker-A")
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "should return 0 for already completed task")
}

func TestMarkFailed_RetryBackoff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Mark failed with retries allowed
	n, err := q.MarkFailed(ctx, taskID, "worker-A", nil, 3)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	// Check next_retry_at is set
	var nextRetry *time.Time
	err = pool.QueryRow(ctx, `SELECT next_retry_at FROM reply_draft_task WHERE id = $1`, taskID).Scan(&nextRetry)
	require.NoError(t, err)
	require.NotNil(t, nextRetry, "next_retry_at should be set for retryable task")
}

func TestMarkFailed_ExhaustedRetries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Manually set attempts to maxAttempts
	_, err = pool.Exec(ctx, `UPDATE reply_draft_task SET attempts = 3 WHERE id = $1`, taskID)
	require.NoError(t, err)

	// Mark failed with no retries left
	n, err := q.MarkFailed(ctx, taskID, "worker-A", nil, 3)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	// Check next_retry_at is NULL (terminal state)
	var nextRetry *time.Time
	err = pool.QueryRow(ctx, `SELECT next_retry_at FROM reply_draft_task WHERE id = $1`, taskID).Scan(&nextRetry)
	require.NoError(t, err)
	require.Nil(t, nextRetry, "next_retry_at should be NULL for exhausted task")
}

func TestTimeBoundary_JustExpired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Set claimed_at to exactly 5 minutes + 1 second ago
	_, err = pool.Exec(ctx, `
		UPDATE reply_draft_task
		SET claimed_at = NOW() - INTERVAL '5 minutes' - INTERVAL '1 second'
		WHERE id = $1`, taskID)
	require.NoError(t, err)

	// Worker B should be able to claim (just expired)
	task, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-B")
	require.NoError(t, err)
	require.NotNil(t, task, "should claim just-expired task")
	require.Equal(t, taskID, task.ID)
}

func TestTimeBoundary_NotYetExpired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Set claimed_at to 4 minutes 59 seconds ago (not yet expired)
	_, err = pool.Exec(ctx, `
		UPDATE reply_draft_task
		SET claimed_at = NOW() - INTERVAL '4 minutes' - INTERVAL '59 seconds'
		WHERE id = $1`, taskID)
	require.NoError(t, err)

	// Worker B should NOT be able to claim
	task, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-B")
	require.ErrorIs(t, err, taskoutbox.ErrNoTask)
	require.Nil(t, task, "should not claim not-yet-expired task")
}

func TestLongOwnerString(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Use a very long owner string (256 chars)
	longOwner := ""
	for i := 0; i < 256; i++ {
		longOwner += "x"
	}

	// Should still work
	task, err := q.TryClaimWithOwner(ctx, 5*time.Minute, longOwner)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, taskID, task.ID)

	// Refresh should work
	n, err := q.RefreshClaim(ctx, taskID, longOwner)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

// ============================================================================
// Extreme Concurrency Tests
// ============================================================================

func TestExtremeConcurrency_100Workers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	numTasks := 50

	// Create multiple tasks
	taskIDs := make([]int64, numTasks)
	for i := 0; i < numTasks; i++ {
		taskIDs[i] = createTestTask(t, pool, tenantID)
	}

	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	var claimedCount atomic.Int64
	var wg sync.WaitGroup

	// 100 workers competing
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			owner := "worker-" + string(rune('A'+workerID%26)) + "-" + string(rune('0'+workerID/26))

			// Each worker tries to claim multiple times
			for j := 0; j < 10; j++ {
				task, err := q.TryClaimWithOwner(ctx, 5*time.Minute, owner)
				if err == nil && task != nil {
					claimedCount.Add(1)
					// Complete it
					_, _ = q.MarkDone(ctx, task.ID, owner)
				}
			}
		}(i)
	}

	wg.Wait()

	// Should have claimed all tasks (each task once)
	require.Equal(t, int64(numTasks), claimedCount.Load(), "all tasks should be claimed exactly once")
}

func TestHeartbeatRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Race: multiple heartbeats from the same worker
	var successCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := q.RefreshClaim(ctx, taskID, "worker-A")
			if err == nil && n == 1 {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	// All refreshes should succeed (no double-claim, just concurrent refresh)
	require.Equal(t, int64(20), successCount.Load(), "all concurrent refreshes should succeed")
}

func TestClaimAfterComplete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Claim and complete
	_, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	_, err = q.MarkDone(ctx, taskID, "worker-A")
	require.NoError(t, err)

	// Try to claim again - should fail
	task, err := q.TryClaimWithOwner(ctx, 5*time.Minute, "worker-B")
	require.ErrorIs(t, err, taskoutbox.ErrNoTask)
	require.Nil(t, task, "should not be able to claim completed task")
}

func TestQueueDepthByTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.NewPool(t)

	tenantID := setupTestTenant(t, pool)
	q := taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled")

	// Create 5 tasks
	for i := 0; i < 5; i++ {
		createTestTask(t, pool, tenantID)
	}

	// Check queue depth
	depths, err := q.QueueDepthByTenant(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(5), depths[tenantID])
}

// ============================================================================
// Test Helpers
// ============================================================================

func setupTestTenant(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	tenantID := "test-tenant-" + t.Name()
	_, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, slug, name, is_active, reply_draft_enabled)
		VALUES ($1, $1, $1, true, true)
		ON CONFLICT (id) DO UPDATE SET reply_draft_enabled = true`, tenantID)
	require.NoError(t, err)
	return tenantID
}

func createTestTask(t *testing.T, pool *pgxpool.Pool, tenantID string) int64 {
	t.Helper()
	ctx := context.Background()

	// Create feedback first
	var feedbackID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, source, content, enrichment_status, user_id)
		VALUES ($1, 'test', 'test content', 'done', 'test-user')
		RETURNING id`, tenantID).Scan(&feedbackID)
	require.NoError(t, err)

	// Create task
	var taskID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO reply_draft_task (tenant_id, feedback_id, status)
		VALUES ($1, $2, 'pending')
		RETURNING id`, tenantID, feedbackID).Scan(&taskID)
	require.NoError(t, err)

	return taskID
}
