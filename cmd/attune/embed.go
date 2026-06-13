package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	embeddingrepo "github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// runEmbed dispatches `attune embed <subcmd> [flags]`.
//
//	attune embed backfill --tenant <slug>  Queue all unembedded feedback for embedding
//	attune embed reset --tenant <slug> --since <date>  Clear embeddings and re-queue
//	attune embed status --tenant <slug>  Show embedding queue depth and stats
func runEmbed(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: attune embed backfill|reset|status --tenant <slug>")
	}
	switch args[0] {
	case "backfill":
		return runEmbedBackfill(args[1:])
	case "reset":
		return runEmbedReset(args[1:])
	case "status":
		return runEmbedStatus(args[1:])
	default:
		return fmt.Errorf("unknown embed subcommand %q (try: backfill, reset, status)", args[0])
	}
}

func runEmbedBackfill(args []string) error {
	fs := flag.NewFlagSet("embed backfill", flag.ContinueOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug (required)")
	batchSize := fs.Int("batch", 1000, "batch size for queueing")
	force := fs.Bool("force", false, "queue even if already embedded (re-embed)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if ptrext.Indirect(tenantSlug) == "" {
		return fmt.Errorf("--tenant is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()

	tenantRepo := tenant.NewTenant(pool)
	tenantID, err := tenantRepo.ResolveSlug(ctx, ptrext.Indirect(tenantSlug))
	if err != nil {
		return fmt.Errorf("resolve tenant %q: %w", ptrext.Indirect(tenantSlug), err)
	}

	taskRepo := embeddingrepo.NewTaskRepo(pool)
	count, err := taskRepo.BackfillTasks(ctx, tenantID, ptrext.Indirect(batchSize), ptrext.Indirect(force))
	if err != nil {
		return fmt.Errorf("backfill: %w", err)
	}

	fmt.Printf("queued %d feedback items for embedding (tenant=%s, force=%t)\n",
		count, ptrext.Indirect(tenantSlug), ptrext.Indirect(force))
	return nil
}

func runEmbedReset(args []string) error {
	fs := flag.NewFlagSet("embed reset", flag.ContinueOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug (required)")
	since := fs.String("since", "", "reset embeddings created after this date (YYYY-MM-DD)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if ptrext.Indirect(tenantSlug) == "" {
		return fmt.Errorf("--tenant is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()

	tenantRepo := tenant.NewTenant(pool)
	tenantID, err := tenantRepo.ResolveSlug(ctx, ptrext.Indirect(tenantSlug))
	if err != nil {
		return fmt.Errorf("resolve tenant %q: %w", ptrext.Indirect(tenantSlug), err)
	}

	var sinceDate time.Time
	if ptrext.Indirect(since) != "" {
		sinceDate, err = time.Parse("2006-01-02", ptrext.Indirect(since))
		if err != nil {
			return fmt.Errorf("invalid --since date %q: use YYYY-MM-DD", ptrext.Indirect(since))
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	resetQuery := `
		UPDATE user_feedback
		SET embedding = NULL,
		    embedding_model = '',
		    embedding_dims = NULL,
		    embedded_at = NULL,
		    cluster_id = NULL,
		    cluster_label = NULL,
		    cluster_assigned_at = NULL
		WHERE tenant_id = $1`
	resetArgs := []any{tenantID}

	if !sinceDate.IsZero() {
		resetQuery += " AND created_at >= $2"
		resetArgs = append(resetArgs, sinceDate)
	}

	result, err := tx.Exec(ctx, resetQuery, resetArgs...)
	if err != nil {
		return fmt.Errorf("reset embeddings: %w", err)
	}
	resetCount := result.RowsAffected()

	deleteQuery := `DELETE FROM embedding_task WHERE tenant_id = $1`
	deleteArgs := []any{tenantID}
	if !sinceDate.IsZero() {
		deleteQuery += " AND created_at >= $2"
		deleteArgs = append(deleteArgs, sinceDate)
	}
	_, err = tx.Exec(ctx, deleteQuery, deleteArgs...)
	if err != nil {
		return fmt.Errorf("delete tasks: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	fmt.Printf("reset %d feedback embeddings (tenant=%s)\n", resetCount, ptrext.Indirect(tenantSlug))
	fmt.Println("run 'attune embed backfill --tenant <slug>' to regenerate")
	return nil
}

func runEmbedStatus(args []string) error {
	fs := flag.NewFlagSet("embed status", flag.ContinueOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if ptrext.Indirect(tenantSlug) == "" {
		return fmt.Errorf("--tenant is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()

	tenantRepo := tenant.NewTenant(pool)
	tenantID, err := tenantRepo.ResolveSlug(ctx, ptrext.Indirect(tenantSlug))
	if err != nil {
		return fmt.Errorf("resolve tenant %q: %w", ptrext.Indirect(tenantSlug), err)
	}

	taskRepo := embeddingrepo.NewTaskRepo(pool)
	depth, err := taskRepo.QueueDepth(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("queue depth: %w", err)
	}

	var stats struct {
		Total       int64
		Embedded    int64
		ClusterID   int64
		UniqueModel int64
	}
	err = pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE embedding IS NOT NULL) AS embedded,
			COUNT(DISTINCT cluster_id) FILTER (WHERE cluster_id IS NOT NULL) AS clusters,
			COUNT(DISTINCT embedding_model) FILTER (WHERE embedding_model <> '') AS models
		FROM user_feedback
		WHERE tenant_id = $1 AND enrichment_status = 'done'`,
		tenantID,
	).Scan(&stats.Total, &stats.Embedded, &stats.ClusterID, &stats.UniqueModel)
	if err != nil {
		return fmt.Errorf("stats query: %w", err)
	}

	var clusteringEnabled bool
	var threshold float64
	err = pool.QueryRow(ctx, `
		SELECT clustering_enabled, clustering_threshold
		FROM tenants WHERE id = $1`,
		tenantID,
	).Scan(&clusteringEnabled, &threshold)
	if err != nil {
		logext.Warnf(ctx, "failed to get tenant config: %v", err)
	}

	fmt.Printf("Embedding status for tenant %s:\n", ptrext.Indirect(tenantSlug))
	fmt.Printf("  Clustering enabled: %t\n", clusteringEnabled)
	fmt.Printf("  Threshold:          %.2f\n", threshold)
	fmt.Printf("  Queue depth:        %d pending tasks\n", depth)
	fmt.Printf("  Total feedback:     %d\n", stats.Total)
	fmt.Printf("  Embedded:           %d (%.1f%%)\n", stats.Embedded, ptrext.Indirect(safePercent(stats.Embedded, stats.Total)))
	fmt.Printf("  Clusters:           %d\n", stats.ClusterID)
	fmt.Printf("  Embedding models:   %d\n", stats.UniqueModel)
	return nil
}

func safePercent(n, total int64) *float64 {
	if total == 0 {
		return ptrext.Of(0.0)
	}
	return ptrext.Of(float64(n) / float64(total) * 100)
}
