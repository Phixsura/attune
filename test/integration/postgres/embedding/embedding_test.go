// Package embedding contains integration tests for the embedding repository.
//
//go:build integration

package embedding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/testdb"
)

// setupTenant creates an isolated tenant for a single test.
func setupTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, clusteringEnabled bool) string {
	t.Helper()
	tenantID := uuid.NewString()
	_, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, slug, name, clustering_enabled, clustering_threshold)
		VALUES ($1, $2, $3, $4, 0.85)`,
		tenantID, "test-"+tenantID[:8], "Test Tenant", clusteringEnabled,
	)
	require.NoError(t, err)
	return tenantID
}

// createFeedback creates a test feedback row.
func createFeedback(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, content string) int64 {
	t.Helper()
	var feedbackID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, content, source, enrichment_status)
		VALUES ($1, 'u-test', $2, 'api', 'done')
		RETURNING id`, tenantID, content).Scan(&feedbackID)
	require.NoError(t, err)
	return feedbackID
}

func TestCreateTaskTx_OnlyWhenClusteringEnabled(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("creates_task_when_enabled", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "test content")

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		err = repo.CreateTaskTx(ctx, tx, feedbackID, tenantID)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))

		var count int
		err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM embedding_task WHERE feedback_id = $1`, feedbackID).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("skips_task_when_disabled", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, false)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "test content 2")

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		err = repo.CreateTaskTx(ctx, tx, feedbackID, tenantID)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))

		var count int
		err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM embedding_task WHERE feedback_id = $1`, feedbackID).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestTryClaim(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("claims_pending_task", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "claim test")

		_, err := pool.Exec(ctx, `
			INSERT INTO embedding_task (feedback_id, tenant_id, status)
			VALUES ($1, $2, 'pending')`, feedbackID, tenantID)
		require.NoError(t, err)

		task, err := repo.TryClaim(ctx, 5*time.Minute)
		require.NoError(t, err)
		require.NotNil(t, task)
		require.Equal(t, feedbackID, task.FeedbackID)
		require.Equal(t, 1, task.Attempts)
	})

	t.Run("skips_when_clustering_disabled", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, false)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "disabled test")

		_, err := pool.Exec(ctx, `
			INSERT INTO embedding_task (feedback_id, tenant_id, status)
			VALUES ($1, $2, 'pending')`, feedbackID, tenantID)
		require.NoError(t, err)

		// Claim all pending tasks from enabled tenants first
		for {
			task, err := repo.TryClaim(ctx, 5*time.Minute)
			if err == embedding.ErrNoTask || task == nil {
				break
			}
			require.NoError(t, err)
			// Mark done so we don't claim it again
			_ = repo.MarkDone(ctx, task.ID)
		}

		// Now try to claim - should get ErrNoTask since disabled tenant's task shouldn't be claimed
		task, err := repo.TryClaim(ctx, 5*time.Minute)
		require.ErrorIs(t, err, embedding.ErrNoTask)
		require.Nil(t, task)
	})

	t.Run("reclaims_stale_processing", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "stale test")

		// Insert task in stale processing state (10 minutes ago)
		_, err := pool.Exec(ctx, `
			INSERT INTO embedding_task (feedback_id, tenant_id, status, claimed_at, attempts)
			VALUES ($1, $2, 'processing', NOW() - INTERVAL '10 minutes', 1)`, feedbackID, tenantID)
		require.NoError(t, err)

		// Should reclaim with 5 minute stale duration
		task, err := repo.TryClaim(ctx, 5*time.Minute)
		require.NoError(t, err)
		require.NotNil(t, task)
		require.Equal(t, feedbackID, task.FeedbackID)
		require.Equal(t, 2, task.Attempts) // incremented from 1 to 2
	})
}

func TestListClusters(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("respects_recency_days", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		clusterID := uuid.New()

		// Create old feedback (40 days ago) - outside default 30 day window
		_, err := pool.Exec(ctx, `
			INSERT INTO user_feedback (tenant_id, user_id, content, source, enrichment_status, cluster_id, created_at)
			VALUES ($1, 'u-test', 'old feedback 1', 'api', 'done', $2, NOW() - INTERVAL '40 days'),
			       ($1, 'u-test', 'old feedback 2', 'api', 'done', $2, NOW() - INTERVAL '40 days')`,
			tenantID, clusterID)
		require.NoError(t, err)

		// Default 30 days should NOT include these
		clusters, err := repo.ListClusters(ctx, tenantID, embedding.ClusterListOpts{})
		require.NoError(t, err)
		found := false
		for _, c := range clusters.Items {
			if c.ClusterID == clusterID {
				found = true
			}
		}
		require.False(t, found, "should not find 40-day old cluster with default 30 day recency")

		// 60 days SHOULD include these
		clusters, err = repo.ListClusters(ctx, tenantID, embedding.ClusterListOpts{RecencyDays: 60})
		require.NoError(t, err)
		found = false
		for _, c := range clusters.Items {
			if c.ClusterID == clusterID {
				found = true
				require.Equal(t, 2, c.Count)
			}
		}
		require.True(t, found, "should find cluster with 60 day recency")
	})

	t.Run("respects_min_count", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		clusterID := uuid.New()

		// Create cluster with only 1 member
		_, err := pool.Exec(ctx, `
			INSERT INTO user_feedback (tenant_id, user_id, content, source, enrichment_status, cluster_id)
			VALUES ($1, 'u-test', 'single feedback', 'api', 'done', $2)`,
			tenantID, clusterID)
		require.NoError(t, err)

		// Default min_count=2 should NOT include single-member cluster
		clusters, err := repo.ListClusters(ctx, tenantID, embedding.ClusterListOpts{})
		require.NoError(t, err)
		for _, c := range clusters.Items {
			require.NotEqual(t, clusterID, c.ClusterID, "should not find single-member cluster")
		}

		// min_count=1 SHOULD include it
		clusters, err = repo.ListClusters(ctx, tenantID, embedding.ClusterListOpts{MinCount: 1})
		require.NoError(t, err)
		found := false
		for _, c := range clusters.Items {
			if c.ClusterID == clusterID {
				found = true
			}
		}
		require.True(t, found, "should find cluster with min_count=1")
	})
}

func TestGetClusterMembers(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("pagination", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		clusterID := uuid.New()

		// Create 5 feedbacks in cluster
		for i := 0; i < 5; i++ {
			_, err := pool.Exec(ctx, `
				INSERT INTO user_feedback (tenant_id, user_id, content, source, enrichment_status, cluster_id)
				VALUES ($1, 'u-test', $2, 'api', 'done', $3)`,
				tenantID, "pagination test "+string(rune('A'+i)), clusterID)
			require.NoError(t, err)
		}

		// Get first 2
		result, err := repo.GetClusterMembers(ctx, tenantID, clusterID, embedding.ClusterMembersOpts{Limit: 2})
		require.NoError(t, err)
		require.Equal(t, 5, result.TotalCount)
		require.Len(t, result.Items, 2)
		require.NotEmpty(t, result.NextCursor, "should have next cursor")

		// Get next 2 using cursor
		result2, err := repo.GetClusterMembers(ctx, tenantID, clusterID, embedding.ClusterMembersOpts{Limit: 2, Cursor: result.NextCursor})
		require.NoError(t, err)
		require.Equal(t, 5, result2.TotalCount)
		require.Len(t, result2.Items, 2)

		// IDs should be different
		require.NotEqual(t, result.Items[0].ID, result2.Items[0].ID)
	})

	t.Run("wrong_tenant_returns_empty", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		clusterID := uuid.New()

		_, err := pool.Exec(ctx, `
			INSERT INTO user_feedback (tenant_id, user_id, content, source, enrichment_status, cluster_id)
			VALUES ($1, 'u-test', 'wrong tenant test', 'api', 'done', $2)`,
			tenantID, clusterID)
		require.NoError(t, err)

		// Different tenant should get empty result
		otherTenantID := setupTenant(t, ctx, pool, true)
		result, err := repo.GetClusterMembers(ctx, otherTenantID, clusterID, embedding.ClusterMembersOpts{})
		require.NoError(t, err)
		require.Equal(t, 0, result.TotalCount)
		require.Empty(t, result.Items)
	})
}

func TestIsClusteringEnabled(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("returns_true_when_enabled", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		enabled, err := repo.IsClusteringEnabled(ctx, tenantID)
		require.NoError(t, err)
		require.True(t, enabled)
	})

	t.Run("returns_false_when_disabled", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, false)
		enabled, err := repo.IsClusteringEnabled(ctx, tenantID)
		require.NoError(t, err)
		require.False(t, enabled)
	})

	t.Run("returns_false_for_nonexistent", func(t *testing.T) {
		enabled, err := repo.IsClusteringEnabled(ctx, uuid.NewString())
		require.NoError(t, err)
		require.False(t, enabled)
	})
}

func TestUpdateEmbedding(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("stores_embedding_and_cluster", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "embedding update test")

		// Generate fake embedding (256 dims)
		vec := make([]float32, 256)
		for i := range vec {
			vec[i] = float32(i) / 256.0
		}

		clusterID := uuid.New()
		err := repo.UpdateEmbedding(ctx, feedbackID, embedding.FeedbackEmbedding{
			Embedding:      vec,
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDims:  256,
			ClusterID:      clusterID,
		})
		require.NoError(t, err)

		// Verify
		var dims int
		var model string
		var cid uuid.UUID
		err = pool.QueryRow(ctx, `
			SELECT embedding_dims, embedding_model, cluster_id
			FROM user_feedback WHERE id = $1`, feedbackID).Scan(&dims, &model, &cid)
		require.NoError(t, err)
		require.Equal(t, 256, dims)
		require.Equal(t, "text-embedding-3-small", model)
		require.Equal(t, clusterID, cid)
	})
}

func TestFindSimilar(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("no_match_high_threshold", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "similarity test")

		// Set embedding - unit vector along first axis
		vec1 := make([]float32, 256)
		vec1[0] = 1.0
		clusterID := uuid.New()
		err := repo.UpdateEmbedding(ctx, feedbackID, embedding.FeedbackEmbedding{
			Embedding:      vec1,
			EmbeddingModel: "test-model",
			EmbeddingDims:  256,
			ClusterID:      clusterID,
		})
		require.NoError(t, err)

		// Search with orthogonal vector - cosine similarity should be ~0
		vec2 := make([]float32, 256)
		vec2[1] = 1.0 // orthogonal to vec1

		result, err := repo.FindSimilar(ctx, embedding.FindSimilarOpts{
			TenantID:       tenantID,
			Embedding:      vec2,
			Threshold:      0.5, // Won't match orthogonal vectors
			RecencyDays:    30,
			EmbeddingModel: "test-model",
			EfSearch:       100,
		})
		require.NoError(t, err)
		require.Nil(t, result, "orthogonal vectors should not match")
	})

	t.Run("finds_similar_vector", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "similar test")

		// Set embedding
		vec1 := make([]float32, 256)
		for i := range vec1 {
			vec1[i] = 1.0 / 16.0 // normalized
		}
		clusterID := uuid.New()
		err := repo.UpdateEmbedding(ctx, feedbackID, embedding.FeedbackEmbedding{
			Embedding:      vec1,
			EmbeddingModel: "test-model",
			EmbeddingDims:  256,
			ClusterID:      clusterID,
		})
		require.NoError(t, err)

		// Search with same vector - should find it
		result, err := repo.FindSimilar(ctx, embedding.FindSimilarOpts{
			TenantID:       tenantID,
			Embedding:      vec1,
			Threshold:      0.9, // High threshold - same vector should match
			RecencyDays:    30,
			EmbeddingModel: "test-model",
			EfSearch:       100,
		})
		require.NoError(t, err)
		require.NotNil(t, result, "same vector should match")
		require.Equal(t, clusterID, result.ClusterID)
		require.Greater(t, result.Similarity, 0.99) // Should be ~1.0
	})

	t.Run("respects_embedding_model_filter", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "model filter test")

		vec := make([]float32, 256)
		for i := range vec {
			vec[i] = 1.0 / 16.0
		}
		clusterID := uuid.New()
		err := repo.UpdateEmbedding(ctx, feedbackID, embedding.FeedbackEmbedding{
			Embedding:      vec,
			EmbeddingModel: "model-A",
			EmbeddingDims:  256,
			ClusterID:      clusterID,
		})
		require.NoError(t, err)

		// Search with different model - should not find
		result, err := repo.FindSimilar(ctx, embedding.FindSimilarOpts{
			TenantID:       tenantID,
			Embedding:      vec,
			Threshold:      0.5,
			RecencyDays:    30,
			EmbeddingModel: "model-B", // different model
			EfSearch:       100,
		})
		require.NoError(t, err)
		require.Nil(t, result, "different embedding model should not match")
	})
}

func TestQueueDepth(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("counts_pending_tasks", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)

		// Create 3 pending tasks
		for i := 0; i < 3; i++ {
			feedbackID := createFeedback(t, ctx, pool, tenantID, "depth test "+string(rune('A'+i)))
			_, err := pool.Exec(ctx, `
				INSERT INTO embedding_task (feedback_id, tenant_id, status)
				VALUES ($1, $2, 'pending')`, feedbackID, tenantID)
			require.NoError(t, err)
		}

		depth, err := repo.QueueDepth(ctx, tenantID)
		require.NoError(t, err)
		require.Equal(t, int64(3), depth)
	})
}

func TestMarkDoneAndFailed(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("mark_done", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "mark done test")

		var taskID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO embedding_task (feedback_id, tenant_id, status)
			VALUES ($1, $2, 'processing')
			RETURNING id`, feedbackID, tenantID).Scan(&taskID)
		require.NoError(t, err)

		err = repo.MarkDone(ctx, taskID)
		require.NoError(t, err)

		var status string
		err = pool.QueryRow(ctx, `SELECT status FROM embedding_task WHERE id = $1`, taskID).Scan(&status)
		require.NoError(t, err)
		require.Equal(t, "done", status)
	})

	t.Run("mark_failed_with_retries_left", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "mark failed retry test")

		var taskID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO embedding_task (feedback_id, tenant_id, status, attempts)
			VALUES ($1, $2, 'processing', 1)
			RETURNING id`, feedbackID, tenantID).Scan(&taskID)
		require.NoError(t, err)

		// attempts(1) < maxAttempts(3), should set to 'pending' for retry
		err = repo.MarkFailed(ctx, taskID, errors.New("transient error"), 3)
		require.NoError(t, err)

		var status, lastErr string
		var nextRetryAt *time.Time
		err = pool.QueryRow(ctx, `SELECT status, last_error, next_retry_at FROM embedding_task WHERE id = $1`, taskID).Scan(&status, &lastErr, &nextRetryAt)
		require.NoError(t, err)
		require.Equal(t, "pending", status, "should be pending when retries remain")
		require.Equal(t, "transient error", lastErr)
		require.NotNil(t, nextRetryAt, "next_retry_at should be set for retry")
	})

	t.Run("mark_failed_permanently", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "mark failed permanent test")

		var taskID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO embedding_task (feedback_id, tenant_id, status, attempts)
			VALUES ($1, $2, 'processing', 3)
			RETURNING id`, feedbackID, tenantID).Scan(&taskID)
		require.NoError(t, err)

		// attempts(3) >= maxAttempts(3), should set to 'failed' permanently
		err = repo.MarkFailed(ctx, taskID, errors.New("permanent error"), 3)
		require.NoError(t, err)

		var status, lastErr string
		var nextRetryAt *time.Time
		err = pool.QueryRow(ctx, `SELECT status, last_error, next_retry_at FROM embedding_task WHERE id = $1`, taskID).Scan(&status, &lastErr, &nextRetryAt)
		require.NoError(t, err)
		require.Equal(t, "failed", status, "should be failed when max attempts reached")
		require.Equal(t, "permanent error", lastErr)
		require.Nil(t, nextRetryAt, "next_retry_at should be NULL for permanent failure")
	})
}

func TestResetStaleClaims(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := embedding.NewTaskRepo(pool)

	t.Run("resets_stale_processing_tasks", func(t *testing.T) {
		tenantID := setupTenant(t, ctx, pool, true)
		feedbackID := createFeedback(t, ctx, pool, tenantID, "reset test")

		// Insert stale processing task
		_, err := pool.Exec(ctx, `
			INSERT INTO embedding_task (feedback_id, tenant_id, status, claimed_at)
			VALUES ($1, $2, 'processing', NOW() - INTERVAL '10 minutes')`, feedbackID, tenantID)
		require.NoError(t, err)

		// Reset with 5 minute duration
		count, err := repo.ResetStaleClaims(ctx, 5*time.Minute)
		require.NoError(t, err)
		require.GreaterOrEqual(t, count, int64(1))

		// Verify reset
		var status string
		err = pool.QueryRow(ctx, `SELECT status FROM embedding_task WHERE feedback_id = $1`, feedbackID).Scan(&status)
		require.NoError(t, err)
		require.Equal(t, "pending", status)
	})
}
