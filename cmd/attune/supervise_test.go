package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/Phixsura/attune/internal/infra/metrics"
)

// counterValue reads the current value of a counter child without pulling in
// the prometheus testutil package (which adds an extra module dependency).
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil { // ptrext:allow out-param prometheus Collector.Write
		t.Fatalf("write metric: %v", err)
	}
	return m.GetCounter().GetValue()
}

func TestRunGuardedReportsPanic(t *testing.T) {
	if !runGuarded(context.Background(), "test_panic", func() { panic("boom") }) {
		t.Fatal("runGuarded should report panicked=true when fn panics")
	}
	if runGuarded(context.Background(), "test_clean", func() {}) {
		t.Fatal("runGuarded should report panicked=false on clean return")
	}
}

func TestRunGuardedCountsPanic(t *testing.T) {
	counter := metrics.WorkerPanics.WithLabelValues("test_count")
	before := counterValue(t, counter)
	runGuarded(context.Background(), "test_count", func() { panic("x") })
	if delta := counterValue(t, counter) - before; delta != 1 {
		t.Fatalf("WorkerPanics delta = %v, want 1", delta)
	}
}

func TestSafegoRestartsAfterPanic(t *testing.T) {
	// fn panics on its first run and signals on its second. The supervisor must
	// recover the panic and restart fn — a bare `go fn()` would crash the test
	// process instead.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	done := make(chan struct{})
	safego(ctx, "test_restart", func() {
		if calls.Add(1) == 1 {
			panic("first run panics")
		}
		close(done)
		<-ctx.Done()
	})

	select {
	case <-done:
		if calls.Load() < 2 {
			t.Fatalf("expected >=2 runs after restart, got %d", calls.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not restart after panic")
	}
}

func TestSafegoNoRestartOnCtxCancel(t *testing.T) {
	// A worker that returns once ctx is cancelled must not be restarted.
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	safego(ctx, "test_cancel", func() {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
	})

	<-started
	cancel()
	time.Sleep(300 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("expected exactly 1 run, got %d", calls.Load())
	}
}
