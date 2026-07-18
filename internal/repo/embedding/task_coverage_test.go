// SPDX-License-Identifier: Apache-2.0

package embedding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
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
	expectRepoErr(t, "RefreshClaim", func() error {
		_, err := r.RefreshClaim(ctx, 1, "worker-1")
		return err
	})
	expectRepoErr(t, "MarkDone", func() error {
		_, err := r.MarkDone(ctx, 1, "worker-1")
		return err
	})
	expectRepoErr(t, "MarkFailed", func() error {
		_, err := r.MarkFailed(ctx, 1, "worker-1", errors.New("failed"), 3)
		return err
	})
	expectRepoErr(t, "ResetStaleClaims", func() error {
		_, err := r.ResetStaleClaims(ctx, time.Minute)
		return err
	})
	expectRepoErr(t, "QueueDepth", func() error {
		_, err := r.QueueDepth(ctx, "tenant-1")
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

func TestCreateTaskTxUsesTransaction(t *testing.T) {
	t.Parallel()

	repo := TaskRepo{}
	ctx := context.Background()
	tx := ptrext.Of(fakeEmbeddingTx{})
	if err := repo.CreateTaskTx(ctx, tx, 42, "tenant-1"); err != nil {
		t.Fatalf("CreateTaskTx() error = %v", err)
	}
	if tx.execs != 1 {
		t.Fatalf("CreateTaskTx() execs = %d, want 1", tx.execs)
	}

	boom := errors.New("insert failed")
	err := repo.CreateTaskTx(ctx, ptrext.Of(fakeEmbeddingTx{execErr: boom}), 42, "tenant-1")
	if !errors.Is(err, boom) {
		t.Fatalf("CreateTaskTx(exec error) = %v, want %v", err, boom)
	}
}

func TestClusterQueryBuildersReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableTaskRepo(t)
	clusterID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
	cursorTime := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	cursorID := uuid.MustParse("bbbbbbbb-1111-2222-3333-cccccccccccc")

	expectRepoErr(t, "queryClusters latest", func() error {
		_, err := r.queryClusters(ctx, "tenant-1", normalizeClusterOpts(ClusterListOpts{Limit: 10}), time.Time{}, uuid.Nil)
		return err
	})
	expectRepoErr(t, "queryClusters count cursor query", func() error {
		_, err := r.queryClusters(ctx, "tenant-1", normalizeClusterOpts(ClusterListOpts{Limit: 10, Sort: "count", Query: "billing"}), cursorTime, cursorID)
		return err
	})
	expectRepoErr(t, "queryClusterMembers first page", func() error {
		_, err := r.queryClusterMembers(ctx, "tenant-1", clusterID, 10, time.Time{}, 0)
		return err
	})
	expectRepoErr(t, "queryClusterMembers cursor page", func() error {
		_, err := r.queryClusterMembers(ctx, "tenant-1", clusterID, 10, cursorTime, 42)
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

type fakeEmbeddingTx struct {
	execs   int
	execErr error
}

func (tx *fakeEmbeddingTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *fakeEmbeddingTx) Commit(context.Context) error          { return nil }
func (tx *fakeEmbeddingTx) Rollback(context.Context) error        { return nil }
func (tx *fakeEmbeddingTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *fakeEmbeddingTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *fakeEmbeddingTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }

func (tx *fakeEmbeddingTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *fakeEmbeddingTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.execs++
	if tx.execErr != nil {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *fakeEmbeddingTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call in fakeEmbeddingTx")
}

func (tx *fakeEmbeddingTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeEmbeddingRow{}
}
func (tx *fakeEmbeddingTx) Conn() *pgx.Conn { return nil }

type fakeEmbeddingRow struct{}

func (fakeEmbeddingRow) Scan(...any) error {
	return errors.New("unexpected QueryRow call in fakeEmbeddingTx")
}
