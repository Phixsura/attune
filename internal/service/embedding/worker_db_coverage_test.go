// SPDX-License-Identifier: Apache-2.0

package embedding

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	embeddingrepo "github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/service/llmrouter"
)

func TestWorkerDatabaseErrorPaths(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnreachableEmbeddingWorker(t)
	task := ptrext.Of(embeddingrepo.Task{ID: 1, TenantID: "tenant-1", FeedbackID: 100})

	w.ProcessOnce(ctx)

	_, _, _, ok := w.fetchEmbedding(ctx, ctx, task)
	if ok {
		t.Fatalf("fetchEmbedding() ok = true, want false for content load error")
	}

	if _, _, ok := w.assignCluster(ctx, task, []float32{0.1, 0.2}, "model-1"); ok {
		t.Fatalf("assignCluster() ok = true, want false for config load error")
	}

	w.completeTask(ctx, task, llmrouter.EmbeddingResponse{}, uuid.New(), true, time.Now())
	w.maybeGenerateClusterLabel(ctx, "tenant-1", uuid.New())
	w.processTask(ctx, task)
}

func TestWorkerRunStopsWithCancelledContext(t *testing.T) {
	t.Parallel()

	w := newUnreachableEmbeddingWorker(t)
	w.Configure(time.Millisecond, 0, 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.Run(ctx)
}

func newUnreachableEmbeddingWorker(t *testing.T) *Worker {
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
	return NewWorker(embeddingrepo.NewTaskRepo(pool), nil, nil, nil)
}
