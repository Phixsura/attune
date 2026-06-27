// SPDX-License-Identifier: Apache-2.0

package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestMemoryTokenBucketLimiter_AllowsBurstThenBlocks(t *testing.T) {
	t.Parallel()

	limiter := NewMemoryTokenBucketLimiter()
	ctx := context.Background()

	allowed, info, err := limiter.AllowWithInfo(ctx, "client:1", 1, 2)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 1, info.Limit)
	require.Equal(t, 1, info.Remaining)

	allowed, info, err = limiter.AllowWithInfo(ctx, "client:1", 1, 2)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 0, info.Remaining)

	allowed, info, err = limiter.AllowWithInfo(ctx, "client:1", 1, 2)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, 0, info.Remaining)
	require.Greater(t, info.Reset, 0*time.Second)
}

func TestMemoryTokenBucketLimiter_ReconfiguresExistingKey(t *testing.T) {
	t.Parallel()

	limiter := NewMemoryTokenBucketLimiter()
	ctx := context.Background()

	allowed, _, err := limiter.AllowWithInfo(ctx, "client:2", 1, 1)
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, _, err = limiter.AllowWithInfo(ctx, "client:2", 1, 1)
	require.NoError(t, err)
	require.False(t, allowed)

	allowed, info, err := limiter.AllowWithInfo(ctx, "client:2", 120, 3)
	require.NoError(t, err)
	require.True(t, allowed)
	require.GreaterOrEqual(t, info.Remaining, 1)
}

func TestMemoryTokenBucketLimiter_ZeroPerMinuteAlwaysAllows(t *testing.T) {
	t.Parallel()

	limiter := NewMemoryTokenBucketLimiter()
	ctx := context.Background()

	allowed, info, err := limiter.AllowWithInfo(ctx, "k", 0, 5)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 0, info.Limit)
}

func TestMemoryTokenBucketLimiter_EmptyKeyAlwaysAllows(t *testing.T) {
	t.Parallel()

	limiter := NewMemoryTokenBucketLimiter()
	ctx := context.Background()

	allowed, _, err := limiter.AllowWithInfo(ctx, "", 60, 5)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestMemoryTokenBucketLimiter_NegativeBurstDefaultsToOne(t *testing.T) {
	t.Parallel()

	limiter := NewMemoryTokenBucketLimiter()
	ctx := context.Background()

	allowed, _, err := limiter.AllowWithInfo(ctx, "k1", 60, -1)
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, _, err = limiter.AllowWithInfo(ctx, "k1", 60, -1)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestMemoryTokenBucketLimiter_Cleanup(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	limiter := ptrext.Of(MemoryTokenBucketLimiter{
		nowFunc: func() time.Time { return now },
	})
	ctx := context.Background()

	_, _, _ = limiter.AllowWithInfo(ctx, "active", 60, 5)
	_, _, _ = limiter.AllowWithInfo(ctx, "stale", 60, 5)

	now = now.Add(2 * time.Hour)
	limiter.nowFunc = func() time.Time { return now }
	_, _, _ = limiter.AllowWithInfo(ctx, "active", 60, 5)

	limiter.cleanup(1 * time.Hour)

	_, loaded := limiter.buckets.Load("active")
	require.True(t, loaded)
	_, loaded = limiter.buckets.Load("stale")
	require.False(t, loaded)
}

func TestMemoryTokenBucketLimiter_ReconfigStricter(t *testing.T) {
	t.Parallel()

	limiter := NewMemoryTokenBucketLimiter()
	ctx := context.Background()

	_, _, _ = limiter.AllowWithInfo(ctx, "k", 120, 10)

	allowed, info, err := limiter.AllowWithInfo(ctx, "k", 30, 2)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 30, info.Limit)
}

func TestClampTokens(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0, clampTokens(-1.5, 10))
	require.Equal(t, 5, clampTokens(5.9, 10))
	require.Equal(t, 10, clampTokens(15.0, 10))
	require.Equal(t, 3, clampTokens(3.0, 5))
}

func TestRatePerSecond(t *testing.T) {
	t.Parallel()

	r := ratePerSecond(60)
	require.InDelta(t, 1.0, float64(r), 0.001)
	r = ratePerSecond(120)
	require.InDelta(t, 2.0, float64(r), 0.001)
}
