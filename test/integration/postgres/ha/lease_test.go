// SPDX-License-Identifier: Apache-2.0
//go:build integration

package ha

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/replydraft"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestConcurrentClaim_NoDoubleClaim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.Pool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)

	repo := replydraft.NewDraftTaskRepo(pool)

	var mu sync.Mutex
	claimed := make(map[string]bool)
	var wg sync.WaitGroup

	// Simulate 5 workers competing for the same task
	for i := 0; i < 5; i++ {
		wg.Add(1)
		owner := "worker-" + string(rune('A'+i))
		go func(owner string) {
			defer wg.Done()
			task, err := repo.TryClaimWithOwner(ctx, 5*time.Minute, owner)
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
	pool := testdb.Pool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)

	repo := replydraft.NewDraftTaskRepo(pool)

	// Worker A claims the task
	task, err := repo.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)
	require.NotNil(t, task)

	// Simulate timeout: reset claim and let worker B claim
	_, err = pool.Exec(ctx, `
		UPDATE reply_draft_task
		SET claimed_at = NOW() - INTERVAL '10 minutes', claimed_by = NULL
		WHERE id = $1`, taskID)
	require.NoError(t, err)

	task2, err := repo.TryClaimWithOwner(ctx, 5*time.Minute, "worker-B")
	require.NoError(t, err)
	require.NotNil(t, task2)

	// Worker A tries to complete (should fail due to fencing)
	n, err := repo.MarkDone(ctx, taskID, "worker-A")
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "old owner should not be able to complete")

	// Worker B can complete
	n, err = repo.MarkDone(ctx, taskID, "worker-B")
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "current owner should complete successfully")
}

func TestHeartbeatRefresh_ExtendsLease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.Pool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)

	repo := replydraft.NewDraftTaskRepo(pool)

	// Claim the task
	_, err := repo.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Get initial claimed_at
	var initialClaim time.Time
	err = pool.QueryRow(ctx, `SELECT claimed_at FROM reply_draft_task WHERE id = $1`, taskID).Scan(&initialClaim)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// Refresh the claim
	n, err := repo.RefreshClaim(ctx, taskID, "worker-A")
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
	pool := testdb.Pool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)

	repo := replydraft.NewDraftTaskRepo(pool)

	// Worker A claims
	_, err := repo.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Simulate worker B stealing the claim
	_, err = pool.Exec(ctx, `
		UPDATE reply_draft_task SET claimed_by = 'worker-B', claimed_at = NOW()
		WHERE id = $1`, taskID)
	require.NoError(t, err)

	// Worker A's heartbeat should return 0
	n, err := repo.RefreshClaim(ctx, taskID, "worker-A")
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "heartbeat should detect lease lost")
}

func TestStaleClaimRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.Pool(t)

	tenantID := setupTestTenant(t, pool)
	taskID := createTestTask(t, pool, tenantID)

	repo := replydraft.NewDraftTaskRepo(pool)

	// Claim the task
	_, err := repo.TryClaimWithOwner(ctx, 5*time.Minute, "worker-A")
	require.NoError(t, err)

	// Make the claim stale
	_, err = pool.Exec(ctx, `
		UPDATE reply_draft_task
		SET claimed_at = NOW() - INTERVAL '10 minutes'
		WHERE id = $1`, taskID)
	require.NoError(t, err)

	// Reset stale claims
	reset, err := repo.ResetStaleClaims(ctx, 5*time.Minute)
	require.NoError(t, err)
	require.Equal(t, int64(1), reset, "should recover one stale claim")

	// Worker B can now claim
	task, err := repo.TryClaimWithOwner(ctx, 5*time.Minute, "worker-B")
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, taskID, task.ID)
}

func setupTestTenant(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	tenantID := "test-tenant-" + t.Name()
	_, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, slug, name, is_active)
		VALUES ($1, $1, $1, true)
		ON CONFLICT (id) DO NOTHING`, tenantID)
	require.NoError(t, err)
	return tenantID
}

func createTestTask(t *testing.T, pool *pgxpool.Pool, tenantID string) int64 {
	t.Helper()
	ctx := context.Background()

	// Create feedback first
	var feedbackID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, source, content, enrichment_status)
		VALUES ($1, 'test', 'test content', 'done')
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
