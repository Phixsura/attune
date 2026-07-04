package enrich

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestMemoryQueueSubmitReturnsFullWithoutBlocking(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(1)
	defer drainQueue(t, q)

	if err := q.Submit(context.Background(), Job{ID: 1}); err != nil {
		t.Fatalf("first submit err = %v", err)
	}
	waitForQueueDepth(t, q, 0)

	if err := q.Submit(context.Background(), Job{ID: 2}); err != nil {
		t.Fatalf("second submit err = %v", err)
	}
	if err := q.Submit(context.Background(), Job{ID: 3}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("third submit err = %v, want ErrQueueFull", err)
	}
}

func waitForQueueDepth(t *testing.T, q *MemoryQueue, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if q.Len() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queue depth = %d, want %d", q.Len(), want)
}

func drainQueue(t *testing.T, q *MemoryQueue) {
	t.Helper()
	if err := q.Close(); err != nil {
		t.Fatalf("close queue: %v", err)
	}
	for range q.Consume() {
	}
}

func TestRunnerProcessBatchHonorsWorkerLimit(t *testing.T) {
	t.Parallel()
	var current atomic.Int32
	var maxSeen atomic.Int32
	done := make(chan struct{})

	runner := ptrext.Of(Runner{
		queue: NewMemoryQueue(10),
		cfg: RunnerConfig{
			QueueLen:      10,
			Workers:       3,
			BatchSize:     10,
			BatchWindow:   time.Millisecond,
			SweepInterval: time.Second,
		},
		execute: func(context.Context, Job) error {
			n := current.Add(1)
			for {
				seen := maxSeen.Load()
				if n <= seen || maxSeen.CompareAndSwap(seen, n) {
					break
				}
			}
			<-done
			current.Add(-1)
			return nil
		},
	})

	for i := 0; i < 6; i++ {
		if err := runner.Submit(context.Background(), Job{ID: int64(i + 1)}); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	_ = runner.queue.Close()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		runner.runProcessor(context.Background())
	}()

	for maxSeen.Load() < 3 {
		time.Sleep(time.Millisecond)
	}
	close(done)

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("processor did not finish")
	}

	if got := maxSeen.Load(); got != 3 {
		t.Fatalf("max concurrency = %d, want 3", got)
	}
}

// TestRunnerProcessBatchRecoversPanic proves a panic enriching one job (the
// documented malformed-LLM-response risk) is recovered: the processor survives,
// the panic is counted, and the other jobs in the batch still execute. A bare
// `go` launch would have crashed the whole test binary instead.
func TestRunnerProcessBatchRecoversPanic(t *testing.T) {
	t.Parallel()
	var ok atomic.Int32
	before := counterValue(t, metrics.WorkerPanics.WithLabelValues("enrich_job"))

	runner := ptrext.Of(Runner{
		queue: NewMemoryQueue(10),
		cfg: RunnerConfig{
			QueueLen: 10, Workers: 2, BatchSize: 10,
			BatchWindow: time.Millisecond, SweepInterval: time.Second,
		},
		execute: func(_ context.Context, job Job) error {
			if job.ID == 1 {
				panic("boom: malformed LLM response")
			}
			ok.Add(1)
			return nil
		},
	})

	for i := 1; i <= 3; i++ {
		if err := runner.Submit(context.Background(), Job{ID: int64(i)}); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	_ = runner.queue.Close()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		runner.runProcessor(context.Background())
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("processor did not finish — a panic likely escaped recovery")
	}

	if got := ok.Load(); got != 2 {
		t.Fatalf("non-panicking jobs executed = %d, want 2", got)
	}
	// >= 1 (not == 1): the counter is process-global and this test runs in
	// parallel, so another test panicking with the same label could bump it too.
	if delta := counterValue(t, metrics.WorkerPanics.WithLabelValues("enrich_job")) - before; delta < 1 {
		t.Fatalf("WorkerPanics delta = %v, want >= 1", delta)
	}
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil { // ptrext:allow out-param prometheus Collector.Write
		t.Fatalf("write metric: %v", err)
	}
	return m.GetCounter().GetValue()
}

func TestClassifySkipsMarkFailedForRateLimitWaitCancellation(t *testing.T) {
	t.Parallel()
	if shouldMarkEnrichFailed(llmclient.ErrRateLimitWaitCanceled) {
		t.Fatal("rate-limit wait cancellation should not mark row failed")
	}
	if !shouldMarkEnrichFailed(errors.New("provider down")) {
		t.Fatal("ordinary provider failure should mark row failed")
	}
}
