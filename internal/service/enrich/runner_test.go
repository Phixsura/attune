package enrich

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestMemoryQueueSubmitReturnsFullWithoutBlocking(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(1)
	if err := q.Submit(context.Background(), Job{ID: 1}); err != nil {
		t.Fatalf("first submit err = %v", err)
	}
	if err := q.Submit(context.Background(), Job{ID: 2}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("second submit err = %v, want ErrQueueFull", err)
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

func TestClassifySkipsMarkFailedForRateLimitWaitCancellation(t *testing.T) {
	t.Parallel()
	if shouldMarkEnrichFailed(llmclient.ErrRateLimitWaitCanceled) {
		t.Fatal("rate-limit wait cancellation should not mark row failed")
	}
	if !shouldMarkEnrichFailed(errors.New("provider down")) {
		t.Fatal("ordinary provider failure should mark row failed")
	}
}
