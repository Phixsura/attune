// SPDX-License-Identifier: Apache-2.0

package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
