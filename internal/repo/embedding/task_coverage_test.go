// SPDX-License-Identifier: Apache-2.0

package embedding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTaskQueueMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableTaskRepo(t)

	expectRepoErr(t, "CreateTask", func() error {
		_, err := r.CreateTask(ctx, 42, "tenant-1")
		return err
	})
	expectRepoErr(t, "BackfillTasks", func() error {
		_, err := r.BackfillTasks(ctx, "tenant-1", 100, true)
		return err
	})
	expectRepoErr(t, "TryClaim", func() error {
		_, err := r.TryClaim(ctx, time.Minute)
		return err
	})
	expectRepoErr(t, "TryClaimWithOwner", func() error {
		_, err := r.TryClaimWithOwner(ctx, time.Minute, "worker-1")
		return err
	})
	expectRepoErr(t, "MarkFailed", func() error {
		_, err := r.MarkFailed(ctx, 1, "worker-1", errors.New("failed"), 3)
		return err
	})
	expectRepoErr(t, "QueueDepthByTenant", func() error {
		_, err := r.QueueDepthByTenant(ctx)
		return err
	})
}

func TestEmbeddingClusterMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableTaskRepo(t)
	clusterID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")

	expectRepoErr(t, "UpdateEmbedding", func() error {
		return r.UpdateEmbedding(ctx, 42, FeedbackEmbedding{Embedding: []float32{0.1, 0.2}, EmbeddingModel: "model", EmbeddingDims: 2, ClusterID: clusterID})
	})
	expectRepoErr(t, "FindSimilar", func() error {
		_, err := r.FindSimilar(ctx, FindSimilarOpts{TenantID: "tenant-1", Embedding: []float32{0.1}, EmbeddingModel: "model", Threshold: 0.8})
		return err
	})
	expectRepoErr(t, "GetFeedbackContent", func() error {
		_, err := r.GetFeedbackContent(ctx, 42)
		return err
	})
	expectRepoErr(t, "GetClusterInfo", func() error {
		_, err := r.GetClusterInfo(ctx, "tenant-1", clusterID)
		return err
	})
	expectRepoErr(t, "GetClusterTitles", func() error {
		_, err := r.GetClusterTitles(ctx, "tenant-1", clusterID, 3)
		return err
	})
	expectRepoErr(t, "UpdateClusterLabel", func() error {
		return r.UpdateClusterLabel(ctx, "tenant-1", clusterID, "Billing")
	})
}

func TestEmbeddingListAndDigestMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableTaskRepo(t)
	clusterID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
	now := time.Now()

	expectRepoErr(t, "ListClusters", func() error {
		_, err := r.ListClusters(ctx, "tenant-1", ClusterListOpts{Limit: 10})
		return err
	})
	expectRepoErr(t, "GetClusterMembers", func() error {
		_, err := r.GetClusterMembers(ctx, "tenant-1", clusterID, ClusterMembersOpts{Limit: 10})
		return err
	})
	expectRepoErr(t, "IsClusteringEnabled", func() error {
		_, err := r.IsClusteringEnabled(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "GetClusteringConfig", func() error {
		_, err := r.GetClusteringConfig(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "EmbeddingsInWindow", func() error {
		_, err := r.EmbeddingsInWindow(ctx, "tenant-1", now.Add(-time.Hour), now, 100)
		return err
	})
	expectRepoErr(t, "TopClustersInWindow", func() error {
		_, err := r.TopClustersInWindow(ctx, "tenant-1", now.Add(-time.Hour), now, 10)
		return err
	})
	expectRepoErr(t, "ClusterExamplesInWindow", func() error {
		_, err := r.ClusterExamplesInWindow(ctx, "tenant-1", clusterID, now.Add(-time.Hour), now, 3)
		return err
	})
}

func newUnreachableTaskRepo(t *testing.T) *TaskRepo {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return NewTaskRepo(pool)
}

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
