// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"context"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

// DefaultCleanupInterval is the default interval for stale membership cleanup.
const DefaultCleanupInterval = 24 * time.Hour

// RunCleanupLoop runs the stale membership cleanup on a fixed interval.
// It deletes cohort_memberships rows whose expires_at has passed.
func (s *Service) RunCleanupLoop(ctx context.Context, interval time.Duration) {
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
			cleaned, err := s.CleanExpired(ctx)
			if err != nil {
				logext.Errorf(ctx, "[cohortsync.cleanup] failed,err:%s", err.Error())
				continue
			}
			if cleaned > 0 {
				logext.Infof(ctx, "[cohortsync.cleanup] cleaned %d expired memberships", cleaned)
				metrics.CohortSyncStaleCleanedTotal.Add(float64(cleaned))
			}
		}
	}
}
