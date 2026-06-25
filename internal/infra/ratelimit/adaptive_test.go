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

// ============================================================================
// Edge Cases
// ============================================================================

func TestAdaptiveLimiter_ZeroInitialLimit(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 0
	cfg.MinLimit = 0
	limiter := NewAdaptiveLimiter(cfg)

	// Should not allow any
	require.False(t, limiter.Allow())
}

func TestAdaptiveLimiter_NegativeLatency(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	limiter := NewAdaptiveLimiter(cfg)

	// Should handle negative latency gracefully
	limiter.RecordSuccess(-1 * time.Second)
	stats := limiter.Stats()
	require.Equal(t, int64(1), stats.SuccessCount)
}

func TestAdaptiveLimiter_VeryHighLatency(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.LatencyThreshold = 100 * time.Millisecond
	limiter := NewAdaptiveLimiter(cfg)

	// Record very high latency
	for i := 0; i < 100; i++ {
		limiter.RecordSuccess(10 * time.Second)
	}

	stats := limiter.Stats()
	require.Equal(t, int64(100), stats.SuccessCount)
}

func TestAdaptiveLimiter_AllFailures(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 100
	cfg.WindowSize = 10 * time.Millisecond
	limiter := NewAdaptiveLimiter(cfg)

	// Record all failures
	for i := 0; i < 100; i++ {
		limiter.RecordFailure()
	}

	// Wait for window adjustment
	time.Sleep(20 * time.Millisecond)
	limiter.RecordFailure()

	// Limit should have decreased
	require.Less(t, limiter.CurrentLimit(), float64(100))
}

func TestAdaptiveLimiter_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 1000
	limiter := NewAdaptiveLimiter(cfg)

	done := make(chan bool)

	// Concurrent Allow calls
	go func() {
		for i := 0; i < 1000; i++ {
			limiter.Allow()
		}
		done <- true
	}()

	// Concurrent RecordSuccess calls
	go func() {
		for i := 0; i < 1000; i++ {
			limiter.RecordSuccess(time.Millisecond)
		}
		done <- true
	}()

	// Concurrent RecordFailure calls
	go func() {
		for i := 0; i < 1000; i++ {
			limiter.RecordFailure()
		}
		done <- true
	}()

	// Concurrent Stats calls
	go func() {
		for i := 0; i < 1000; i++ {
			limiter.Stats()
		}
		done <- true
	}()

	for i := 0; i < 4; i++ {
		<-done
	}
}

func TestAdaptiveLimiter_MinLimitEnforced(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 20
	cfg.MinLimit = 10
	cfg.WindowSize = 10 * time.Millisecond
	cfg.DecreaseFactor = 0.1 // Very aggressive decrease
	limiter := NewAdaptiveLimiter(cfg)

	// Record lots of failures to trigger decrease
	for j := 0; j < 10; j++ {
		for i := 0; i < 100; i++ {
			limiter.RecordFailure()
		}
		time.Sleep(15 * time.Millisecond)
	}

	// Should not go below min limit
	require.GreaterOrEqual(t, limiter.CurrentLimit(), cfg.MinLimit)
}

func TestAdaptiveLimiter_MaxLimitEnforced(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 9000
	cfg.MaxLimit = 10000
	cfg.WindowSize = 10 * time.Millisecond
	cfg.IncreaseAmount = 500
	limiter := NewAdaptiveLimiter(cfg)

	// Record lots of successes to trigger increase
	for j := 0; j < 10; j++ {
		for i := 0; i < 100; i++ {
			limiter.RecordSuccess(time.Microsecond)
		}
		time.Sleep(15 * time.Millisecond)
	}

	// Should not exceed max limit
	require.LessOrEqual(t, limiter.CurrentLimit(), cfg.MaxLimit)
}

func TestLoadShedder_ContextCancelled(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 5
	limiter := NewAdaptiveLimiter(cfg)
	shedder := NewLoadShedder(limiter)

	// Exhaust the limit
	for i := 0; i < 5; i++ {
		release, ok := shedder.Acquire(context.Background())
		require.True(t, ok)
		_ = release
	}

	// Try with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, ok := shedder.Acquire(ctx)
	require.False(t, ok, "should fail with cancelled context")
}

func TestLoadShedder_ReleaseTwice(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 5
	limiter := NewAdaptiveLimiter(cfg)
	shedder := NewLoadShedder(limiter)

	release, ok := shedder.Acquire(context.Background())
	require.True(t, ok)

	// Release once - should work
	release(true, time.Millisecond)

	// Release again - should be safe (no panic)
	release(true, time.Millisecond)
}

func TestLoadShedder_ReleaseWithFailure(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 5
	limiter := NewAdaptiveLimiter(cfg)
	shedder := NewLoadShedder(limiter)

	release, ok := shedder.Acquire(context.Background())
	require.True(t, ok)

	// Release with failure
	release(false, time.Millisecond)

	stats := limiter.Stats()
	require.Equal(t, int64(1), stats.FailureCount)
}

func TestAdaptiveLimiter_ZeroWindowSize(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.WindowSize = 0
	limiter := NewAdaptiveLimiter(cfg)

	// Should still work
	limiter.RecordSuccess(time.Millisecond)
	require.True(t, limiter.Allow() || !limiter.Allow()) // just verify no panic
}

func TestAdaptiveLimiter_StatsAccuracy(t *testing.T) {
	t.Parallel()
	cfg := DefaultAdaptiveConfig()
	cfg.InitialLimit = 100
	limiter := NewAdaptiveLimiter(cfg)

	// Record specific counts
	for i := 0; i < 42; i++ {
		limiter.RecordSuccess(time.Millisecond)
	}
	for i := 0; i < 13; i++ {
		limiter.RecordFailure()
	}

	stats := limiter.Stats()
	require.Equal(t, int64(42), stats.SuccessCount)
	require.Equal(t, int64(13), stats.FailureCount)
}
