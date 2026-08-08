//go:build integration

// SPDX-License-Identifier: Apache-2.0

package idempotency_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/idempotency"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPGIdempotencyFailedRetryHasSingleAcquirer(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "idempotency-race", "Idempotency Race")
	require.NoError(t, err)
	repo := idempotency.New(pool)
	const key = "retry-race-1"
	hash := []byte("request-payload")

	_, acquired, err := repo.Acquire(ctx, tenantID, key, hash, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, repo.Fail(ctx, tenantID, key))

	_, err = pool.Exec(ctx, `
		CREATE FUNCTION test_block_idempotency_retry() RETURNS trigger AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(236001);
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		CREATE TRIGGER test_block_idempotency_retry
		BEFORE UPDATE OF status ON idempotency_keys
		FOR EACH ROW
		WHEN (OLD.status = 'failed' AND NEW.status = 'pending')
		EXECUTE FUNCTION test_block_idempotency_retry()`)
	require.NoError(t, err)

	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(ctx) })
	_, err = blocker.Exec(ctx, "SELECT pg_advisory_xact_lock(236001)")
	require.NoError(t, err)

	type outcome struct {
		record   *idempotency.Key
		acquired bool
		err      error
	}
	const contenders = 2
	start := make(chan struct{})
	results := make(chan outcome, contenders)
	for range contenders {
		go func() {
			<-start
			record, didAcquire, acquireErr := repo.Acquire(ctx, tenantID, key, hash, time.Minute)
			results <- outcome{record: record, acquired: didAcquire, err: acquireErr}
		}()
	}
	close(start)

	require.Eventually(t, func() bool {
		var waiting int
		err := pool.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_stat_activity
			WHERE wait_event_type = 'Lock'
			  AND query LIKE '%UPDATE idempotency_keys%'`).Scan(&waiting)
		return err == nil && waiting >= contenders
	}, 5*time.Second, 25*time.Millisecond, "idempotency retry contenders did not reach the failed key transition")
	require.NoError(t, blocker.Commit(ctx))

	acquirers := 0
	for range contenders {
		select {
		case result := <-results:
			require.NoError(t, result.err)
			require.NotNil(t, result.record)
			require.Equal(t, idempotency.StatusPending, result.record.Status)
			if result.acquired {
				acquirers++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("idempotency retry contender did not resume after the row lock was released")
		}
	}
	require.Equal(t, 1, acquirers)
}

func TestPGIdempotencyDeleteExpiredRetainsFreshRetryKey(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "idempotency-expiry", "Idempotency Expiry")
	require.NoError(t, err)
	repo := idempotency.New(pool)
	const key = "expiry-race-1"
	hash := []byte("request-payload")

	_, acquired, err := repo.Acquire(ctx, tenantID, key, hash, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	_, err = pool.Exec(ctx, `
		UPDATE idempotency_keys
		SET expires_at = NOW() - INTERVAL '1 minute'
		WHERE tenant_id = $1 AND key = $2`, tenantID, key)
	require.NoError(t, err)

	deleted, err := repo.DeleteExpired(ctx, tenantID, key)
	require.NoError(t, err)
	require.True(t, deleted)
	_, acquired, err = repo.Acquire(ctx, tenantID, key, hash, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	deleted, err = repo.DeleteExpired(ctx, tenantID, key)
	require.NoError(t, err)
	require.False(t, deleted)
	stored, err := repo.Get(ctx, tenantID, key)
	require.NoError(t, err)
	require.Equal(t, idempotency.StatusPending, stored.Status)
}
