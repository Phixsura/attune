// SPDX-License-Identifier: Apache-2.0

package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAdaptiveLimiter_Allow(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 10
	limiter := NewAdaptiveLimiter(cfg)

	// Should allow up to initial limit
	allowed := 0
	for i := 0; i < 20; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	require.Equal(t, 10, allowed)
}

func TestAdaptiveLimiter_RecordSuccess(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	limiter := NewAdaptiveLimiter(cfg)

	limiter.RecordSuccess(10 * time.Millisecond)
	limiter.RecordSuccess(20 * time.Millisecond)

	stats := limiter.Stats()
	require.Equal(t, int64(2), stats.SuccessCount)
}

func TestAdaptiveLimiter_RecordFailure(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	limiter := NewAdaptiveLimiter(cfg)

	limiter.RecordFailure()
	limiter.RecordFailure()

	stats := limiter.Stats()
	require.Equal(t, int64(2), stats.FailureCount)
}

func TestAdaptiveLimiter_CurrentLimit(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 50
	limiter := NewAdaptiveLimiter(cfg)

	require.Equal(t, float64(50), limiter.CurrentLimit())
}

func TestAdaptiveLimiter_Adjustment(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 100
	cfg.WindowSize = 10 * time.Millisecond
	cfg.IncreaseAmount = 10
	limiter := NewAdaptiveLimiter(cfg)

	// Record successes
	for i := 0; i < 20; i++ {
		limiter.RecordSuccess(1 * time.Millisecond)
	}

	// Wait for window to rotate
	time.Sleep(20 * time.Millisecond)
	limiter.RecordSuccess(1 * time.Millisecond)

	// Limit should have increased
	require.GreaterOrEqual(t, limiter.CurrentLimit(), float64(100))
}

func TestLoadShedder_Acquire(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 5
	limiter := NewAdaptiveLimiter(cfg)
	shedder := NewLoadShedder(limiter)

	ctx := context.Background()

	// Should allow first 5
	allowed := 0
	for i := 0; i < 10; i++ {
		release, ok := shedder.Acquire(ctx)
		if ok {
			allowed++
			release(true, time.Millisecond)
		}
	}

	require.Equal(t, 5, allowed)
}

func TestDefaultAdaptiveConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()

	require.Equal(t, float64(10), cfg.MinLimit)
	require.Equal(t, float64(10000), cfg.MaxLimit)
	require.Equal(t, float64(100), cfg.InitialLimit)
	require.Equal(t, float64(0.5), cfg.DecreaseFactor)
}
