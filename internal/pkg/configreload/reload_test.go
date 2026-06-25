// SPDX-License-Identifier: Apache-2.0

package configreload

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReloader_OnReload(t *testing.T) {
	t.Parallel()
	r := New(0)

	var called int32
	r.OnReload(func() {
		atomic.AddInt32(&called, 1) // ptrext:allow atomic out-param
	})

	r.Trigger()
	require.Equal(t, int32(1), atomic.LoadInt32(&called)) // ptrext:allow atomic out-param
}

func TestReloader_Debounce(t *testing.T) {
	t.Parallel()
	r := New(100 * time.Millisecond)

	var called int32
	r.OnReload(func() {
		atomic.AddInt32(&called, 1) // ptrext:allow atomic out-param
	})

	r.Trigger()
	r.Trigger()
	r.Trigger()

	require.Equal(t, int32(1), atomic.LoadInt32(&called)) // ptrext:allow atomic out-param

	time.Sleep(150 * time.Millisecond)
	r.Trigger()

	require.Equal(t, int32(2), atomic.LoadInt32(&called)) // ptrext:allow atomic out-param
}

func TestReloader_MultipleCallbacks(t *testing.T) {
	t.Parallel()
	r := New(0)

	var order []int
	r.OnReload(func() { order = append(order, 1) })
	r.OnReload(func() { order = append(order, 2) })
	r.OnReload(func() { order = append(order, 3) })

	r.Trigger()
	require.Equal(t, []int{1, 2, 3}, order)
}

func TestReloader_WatchSignal_Cancel(t *testing.T) {
	t.Parallel()
	r := New(0)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		r.WatchSignal(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WatchSignal did not return after cancel")
	}
}

func TestValue_GetSet(t *testing.T) {
	t.Parallel()
	v := NewValue(42)

	require.Equal(t, 42, v.Get())

	v.Set(100)
	require.Equal(t, 100, v.Get())
}

func TestValue_Update(t *testing.T) {
	t.Parallel()
	v := NewValue(10)

	v.Update(func(n int) int { return n * 2 })
	require.Equal(t, 20, v.Get())
}

func TestValue_Concurrent(t *testing.T) {
	t.Parallel()
	v := NewValue(0)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			v.Set(i)
		}
		close(done)
	}()

	for i := 0; i < 1000; i++ {
		_ = v.Get()
	}

	<-done
}
