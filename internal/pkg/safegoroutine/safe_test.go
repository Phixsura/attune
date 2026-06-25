// SPDX-License-Identifier: Apache-2.0

package safegoroutine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGo_NoPanic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var called atomic.Bool
	Go(ctx, "test", func() {
		called.Store(true)
	})

	time.Sleep(10 * time.Millisecond)
	require.True(t, called.Load())
}

func TestGo_RecoversPanic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var called atomic.Bool
	Go(ctx, "test-panic", func() {
		called.Store(true)
		panic("test panic")
	})

	time.Sleep(10 * time.Millisecond)
	require.True(t, called.Load())
}

func TestGoWithContext_PassesContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var receivedCtx atomic.Value
	GoWithContext(ctx, "test", func(c context.Context) {
		receivedCtx.Store(c)
	})

	time.Sleep(10 * time.Millisecond)
	require.Equal(t, ctx, receivedCtx.Load())
}

func TestGoLoop_StopsOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	var iterations atomic.Int32
	GoLoop(ctx, "test-loop", func(context.Context) {
		iterations.Add(1)
		time.Sleep(5 * time.Millisecond)
	})

	time.Sleep(25 * time.Millisecond)
	cancel()
	time.Sleep(15 * time.Millisecond)

	count := iterations.Load()
	require.GreaterOrEqual(t, count, int32(2))

	time.Sleep(20 * time.Millisecond)
	require.Equal(t, count, iterations.Load(), "should stop after cancel")
}

func TestGoLoop_ContinuesAfterPanic(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var iterations atomic.Int32
	GoLoop(ctx, "test-panic-loop", func(context.Context) {
		iterations.Add(1)
		if iterations.Load() <= 2 {
			panic("test panic")
		}
		time.Sleep(5 * time.Millisecond)
	})

	time.Sleep(50 * time.Millisecond)
	require.GreaterOrEqual(t, iterations.Load(), int32(3), "should continue after panics")
}
