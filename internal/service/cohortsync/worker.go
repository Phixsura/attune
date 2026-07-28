// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

// DefaultCleanupInterval is the default interval for stale membership cleanup.
const DefaultCleanupInterval = 24 * time.Hour

// staleRunTimeout is the maximum duration a run can stay in "running" status
// before the cleanup worker marks it as failed. Prevents permanently stuck runs
// from blocking future syncs via the partial unique index.
const staleRunTimeout = 30 * time.Minute

// RunCleanupLoop runs the stale membership cleanup on a fixed interval.
// It acquires an advisory lock so only one replica runs the cleanup.
// It also recovers stuck "running" sync runs that exceed staleRunTimeout.
func (s *Service) RunCleanupLoop(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultCleanupInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCleanupTick(ctx, pool)
		}
	}
}

func (s *Service) runCleanupTick(ctx context.Context, pool *pgxpool.Pool) {
	lock, acquired, err := database.TryAdvisoryLock(ctx, pool, database.LockCohortSyncCleanup)
	if err != nil {
		metrics.AdvisoryLockTotal.WithLabelValues("cohort_sync_cleanup", "error").Inc()
		logext.Errorf(ctx, "[cohortsync.cleanup] advisory lock error,err:%s", err.Error())
		return
	}
	if !acquired {
		metrics.AdvisoryLockTotal.WithLabelValues("cohort_sync_cleanup", "busy").Inc()
		return
	}
	metrics.AdvisoryLockTotal.WithLabelValues("cohort_sync_cleanup", "acquired").Inc()
	defer func() { _ = lock.Release(ctx) }()

	// Recover stuck runs first.
	staleRuns, recoverErr := s.repo.RecoverStaleRuns(ctx, staleRunTimeout)
	if recoverErr != nil {
		logext.Errorf(ctx, "[cohortsync.cleanup] recover stale runs failed,err:%s", recoverErr.Error())
	} else if staleRuns > 0 {
		logext.Warnf(ctx, "[cohortsync.cleanup] recovered %d stale running sync runs", staleRuns)
	}

	// Clean expired memberships.
	cleaned, cleanErr := s.CleanExpired(ctx)
	if cleanErr != nil {
		logext.Errorf(ctx, "[cohortsync.cleanup] failed,err:%s", cleanErr.Error())
	} else if cleaned > 0 {
		logext.Infof(ctx, "[cohortsync.cleanup] cleaned %d expired memberships", cleaned)
		metrics.CohortSyncStaleCleanedTotal.Add(float64(cleaned))
	}

	metrics.WorkerHeartbeatTotal.WithLabelValues("cohort_sync_cleanup", "ok").Inc()
}
