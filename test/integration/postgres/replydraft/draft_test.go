//go:build integration

package replydraft

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/replydraft"
	"github.com/Phixsura/attune/internal/testdb"
)

func setupTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, enabled bool, minConfidence float64) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, slug, name, reply_draft_enabled, reply_draft_min_confidence)
		VALUES ($1, $2, $3, $4, $5)`,
		id, "test-"+id[:8], "Test Tenant", enabled, minConfidence)
	require.NoError(t, err)
	return id
}

func createEnrichedFeedback(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback
		  (tenant_id, user_id, content, source, enriched_title, enriched_attrs, language, enrichment_status)
		VALUES ($1, 'u-test', $2, 'api', $3, $4::jsonb, $5, 'done')
		RETURNING id`,
		tenantID, "the app keeps crashing on login", "Login crash",
		`{"severity":"critical","sentiment":"frustrated"}`, "en").Scan(&id)
	require.NoError(t, err)
	return id
}

func countTasks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, feedbackID int64) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM reply_draft_task WHERE feedback_id = $1`, feedbackID).Scan(&n))
	return n
}

func enqueue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *replydraft.DraftTaskRepo, fbID int64, tenantID string, conf *float64) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateTaskTx(ctx, tx, fbID, tenantID, conf, "trace-test"))
	require.NoError(t, tx.Commit(ctx))
}

func TestCreateTaskTx_Gate(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraft.NewDraftTaskRepo(pool)

	t.Run("enabled_no_threshold_admits_nil_confidence", func(t *testing.T) {
		tn := setupTenant(t, ctx, pool, true, 0)
		fb := createEnrichedFeedback(t, ctx, pool, tn)
		enqueue(t, ctx, pool, repo, fb, tn, nil)
		require.Equal(t, 1, countTasks(t, ctx, pool, fb))
	})

	t.Run("disabled_admits_nothing", func(t *testing.T) {
		tn := setupTenant(t, ctx, pool, false, 0)
		fb := createEnrichedFeedback(t, ctx, pool, tn)
		enqueue(t, ctx, pool, repo, fb, tn, ptrext.Of(0.9))
		require.Equal(t, 0, countTasks(t, ctx, pool, fb))
	})

	t.Run("threshold_admits_high_confidence", func(t *testing.T) {
		tn := setupTenant(t, ctx, pool, true, 0.6)
		fb := createEnrichedFeedback(t, ctx, pool, tn)
		enqueue(t, ctx, pool, repo, fb, tn, ptrext.Of(0.8))
		require.Equal(t, 1, countTasks(t, ctx, pool, fb))
	})

	t.Run("threshold_rejects_low_confidence", func(t *testing.T) {
		tn := setupTenant(t, ctx, pool, true, 0.6)
		fb := createEnrichedFeedback(t, ctx, pool, tn)
		enqueue(t, ctx, pool, repo, fb, tn, ptrext.Of(0.4))
		require.Equal(t, 0, countTasks(t, ctx, pool, fb))
	})

	t.Run("threshold_rejects_nil_confidence", func(t *testing.T) {
		tn := setupTenant(t, ctx, pool, true, 0.6)
		fb := createEnrichedFeedback(t, ctx, pool, tn)
		enqueue(t, ctx, pool, repo, fb, tn, nil)
		require.Equal(t, 0, countTasks(t, ctx, pool, fb))
	})
}

func TestLoadForDraft_And_UpdateRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraft.NewDraftTaskRepo(pool)

	tn := setupTenant(t, ctx, pool, true, 0)
	fb := createEnrichedFeedback(t, ctx, pool, tn)

	in, err := repo.LoadForDraft(ctx, fb, tn)
	require.NoError(t, err)
	require.Equal(t, "the app keeps crashing on login", in.Content)
	require.Equal(t, "Login crash", in.EnrichedTitle)
	require.Equal(t, "en", in.Language)
	require.Equal(t, "frustrated", in.Attrs["sentiment"])
	require.Equal(t, tn, in.TenantID)

	gotTS, err := repo.UpdateReplyDraft(ctx, fb, tn, "Sorry to hear that — we're on it.")
	require.NoError(t, err)
	require.False(t, gotTS.IsZero()) // DB-stamped time returned

	var draft string
	var generatedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT reply_draft, reply_draft_generated_at FROM user_feedback WHERE id = $1`, fb).
		Scan(&draft, &generatedAt))
	require.Equal(t, "Sorry to hear that — we're on it.", draft)
	require.NotNil(t, generatedAt)
}

func TestLoadAndUpdate_TenantScoped(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraft.NewDraftTaskRepo(pool)

	owner := setupTenant(t, ctx, pool, true, 0)
	other := setupTenant(t, ctx, pool, true, 0)
	fb := createEnrichedFeedback(t, ctx, pool, owner)

	// Cross-tenant load is denied at the SQL layer, not just the handler.
	_, err := repo.LoadForDraft(ctx, fb, other)
	require.ErrorIs(t, err, replydraft.ErrNotFound)

	// Cross-tenant update writes nothing and reports not-found.
	_, err = repo.UpdateReplyDraft(ctx, fb, other, "hijacked")
	require.ErrorIs(t, err, replydraft.ErrNotFound)
	var draft *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT reply_draft FROM user_feedback WHERE id=$1`, fb).Scan(&draft))
	require.Nil(t, draft) // owner's row untouched

	in, err := repo.LoadForDraft(ctx, fb, owner)
	require.NoError(t, err)
	require.Equal(t, owner, in.TenantID)
}

func TestDraftPrecheck(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraft.NewDraftTaskRepo(pool)

	tn := setupTenant(t, ctx, pool, true, 0)
	fb := createEnrichedFeedback(t, ctx, pool, tn)

	status, enabled, lastGen, err := repo.DraftPrecheck(ctx, fb, tn)
	require.NoError(t, err)
	require.Equal(t, "done", status)
	require.True(t, enabled)
	require.Nil(t, lastGen) // never drafted yet → no cooldown

	// Cross-tenant precheck → not found.
	other := setupTenant(t, ctx, pool, false, 0)
	_, _, _, err = repo.DraftPrecheck(ctx, fb, other)
	require.ErrorIs(t, err, replydraft.ErrNotFound)
}

func TestTraceID_And_QueueDepthByTenant(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraft.NewDraftTaskRepo(pool)

	tn := setupTenant(t, ctx, pool, true, 0)
	fb := createEnrichedFeedback(t, ctx, pool, tn)
	enqueue(t, ctx, pool, repo, fb, tn, nil) // stamps inbound_trace_id="trace-test"

	var taskID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM reply_draft_task WHERE feedback_id=$1`, fb).Scan(&taskID))
	traceID, err := repo.TaskTraceID(ctx, taskID)
	require.NoError(t, err)
	require.Equal(t, "trace-test", traceID)

	depths, err := repo.QueueDepthByTenant(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), depths[tn])
}

// TestRetryBackoff_NotReclaimableUntilDue is the regression guard for the
// taskoutbox backoff bug: a just-failed (non-exhausted) task must NOT be
// re-claimable until its next_retry_at window passes. Before the fix, MarkFailed
// returned the row to 'pending' and TryClaim's pending branch ignored
// next_retry_at, so the worker re-hit a failing provider every poll with zero
// backoff.
func TestRetryBackoff_NotReclaimableUntilDue(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraft.NewDraftTaskRepo(pool)

	tn := setupTenant(t, ctx, pool, true, 0)
	fb := createEnrichedFeedback(t, ctx, pool, tn)
	enqueue(t, ctx, pool, repo, fb, tn, nil)

	claimed, err := repo.TryClaim(ctx, time.Minute) // attempts -> 1
	require.NoError(t, err)
	require.Equal(t, fb, claimed.FeedbackID)
	_, err = repo.MarkFailed(ctx, claimed.ID, "test-worker", errors.New("transient"), 5)
	require.NoError(t, err)

	// Backoff in effect: not immediately re-claimable.
	_, err = repo.TryClaim(ctx, time.Minute)
	require.ErrorIs(t, err, replydraft.ErrNoTask, "failed task must wait out its backoff window")

	// Force the window into the past → claimable again, attempts increments.
	_, err = pool.Exec(ctx,
		`UPDATE reply_draft_task SET next_retry_at = NOW() - INTERVAL '1 second' WHERE id=$1`, claimed.ID)
	require.NoError(t, err)
	reclaimed, err := repo.TryClaim(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, claimed.ID, reclaimed.ID)
	require.Equal(t, 2, reclaimed.Attempts)
}

// TestRetryBackoff_ExhaustedIsTerminal verifies an exhausted task stays 'failed'
// with a NULL next_retry_at and is never re-claimed (the predicate never matches
// a NULL comparison).
func TestRetryBackoff_ExhaustedIsTerminal(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	defer pool.Close()
	repo := replydraft.NewDraftTaskRepo(pool)

	tn := setupTenant(t, ctx, pool, true, 0)
	fb := createEnrichedFeedback(t, ctx, pool, tn)
	enqueue(t, ctx, pool, repo, fb, tn, nil)

	claimed, err := repo.TryClaim(ctx, time.Minute) // attempts -> 1
	require.NoError(t, err)
	// maxAttempts=1 → this failure exhausts it.
	_, err = repo.MarkFailed(ctx, claimed.ID, "test-worker", errors.New("permanent"), 1)
	require.NoError(t, err)

	var status string
	var nextRetryAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, next_retry_at FROM reply_draft_task WHERE id=$1`, claimed.ID).Scan(&status, &nextRetryAt))
	require.Equal(t, "failed", status)
	require.Nil(t, nextRetryAt, "exhausted task has no next_retry_at (terminal)")

	_, err = repo.TryClaim(ctx, time.Minute)
	require.ErrorIs(t, err, replydraft.ErrNoTask, "exhausted task is terminal")
}
