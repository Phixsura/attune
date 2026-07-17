// SPDX-License-Identifier: Apache-2.0

package workerdrain

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDrainer_EnterLeave(t *testing.T) {
	t.Parallel()
	d := New("test")

	require.Equal(t, int64(0), d.InFlight())

	d.Enter()
	require.Equal(t, int64(1), d.InFlight())

	d.Enter()
	require.Equal(t, int64(2), d.InFlight())

	d.Leave()
	require.Equal(t, int64(1), d.InFlight())

	d.Leave()
	require.Equal(t, int64(0), d.InFlight())
}

func TestDrainer_SetTimeoutIgnoresNonPositiveDuration(t *testing.T) {
	t.Parallel()
	d := New("test-timeout-default")

	d.SetTimeout(0)
	require.Equal(t, DefaultTimeout, d.timeout)

	d.SetTimeout(-time.Second)
	require.Equal(t, DefaultTimeout, d.timeout)
}

func TestDrainer_DrainClean(t *testing.T) {
	t.Parallel()
	d := New("test-clean")
	d.SetTimeout(100 * time.Millisecond)

	ctx := context.Background()

	// No in-flight work - should return immediately
	start := time.Now()
	d.Drain(ctx)
	require.Less(t, time.Since(start), 50*time.Millisecond)

	// With in-flight work that completes quickly
	d.Enter()
	go func() {
		time.Sleep(10 * time.Millisecond)
		d.Leave()
	}()
	start = time.Now()
	d.Drain(ctx)
	require.Less(t, time.Since(start), 50*time.Millisecond)
}

func TestDrainer_DrainTimeout(t *testing.T) {
	t.Parallel()
	d := New("test-timeout")
	d.SetTimeout(50 * time.Millisecond)

	ctx := context.Background()

	// Enter but never leave - should timeout
	d.Enter()
	start := time.Now()
	d.Drain(ctx)
	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
	require.Less(t, elapsed, 100*time.Millisecond)
	require.Equal(t, int64(1), d.InFlight()) // Still in-flight after timeout

	// Cleanup
	d.Leave()
}

func TestDrainer_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	d := New("test-concurrent")

	const workers = 100
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			d.Enter()
			time.Sleep(time.Millisecond)
			d.Leave()
		}()
	}

	wg.Wait()
	require.Equal(t, int64(0), d.InFlight())
}
