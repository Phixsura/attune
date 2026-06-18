package enrich

import (
	"context"
	"errors"
	"sync"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var (
	ErrQueueFull   = errors.New("enrichment queue full")
	ErrQueueClosed = errors.New("enrichment queue closed")
)

type Job struct {
	ID      int64
	TraceID string
}

type Submitter interface {
	Submit(ctx context.Context, job Job) error
}

type QueueStatus struct {
	Depth             int
	CapacityTarget    int
	CapacityEffective int
	ResizePending     bool
}

// MemoryQueue owns a brokered in-memory queue with a stable consumer channel.
// Capacity changes update internal admission rules without swapping the worker-
// facing consume channel.
type MemoryQueue struct {
	mu     *sync.Mutex
	cond   *sync.Cond
	buf    []Job
	out    chan Job
	closed bool
	target int
	eff    int
}

func NewMemoryQueue(capacity int) *MemoryQueue {
	q := ptrext.Of(MemoryQueue{
		mu:     ptrext.Of(sync.Mutex{}),
		buf:    make([]Job, 0, maxInt(capacity, 1)),
		out:    make(chan Job),
		target: maxInt(capacity, 1),
		eff:    maxInt(capacity, 1),
	})
	q.cond = sync.NewCond(q.mu)
	go q.broker()
	return q
}

func (q *MemoryQueue) Submit(_ context.Context, job Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrQueueClosed
	}
	if len(q.buf) >= q.eff {
		metrics.EnrichQueueFullTotal.Inc()
		q.observeDepthLocked()
		return ErrQueueFull
	}
	q.buf = append(q.buf, job)
	q.observeDepthLocked()
	q.recomputeEffectiveLocked()
	q.cond.Signal()
	return nil
}

func (q *MemoryQueue) Consume() <-chan Job {
	return q.out
}

func (q *MemoryQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.buf)
}

func (q *MemoryQueue) Status() QueueStatus {
	q.mu.Lock()
	defer q.mu.Unlock()
	return QueueStatus{
		Depth:             len(q.buf),
		CapacityTarget:    q.target,
		CapacityEffective: q.eff,
		ResizePending:     q.eff != q.target,
	}
}

func (q *MemoryQueue) Configure(capacity int) QueueStatus {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.target = maxInt(capacity, 1)
	q.recomputeEffectiveLocked()
	q.cond.Broadcast()
	return QueueStatus{
		Depth:             len(q.buf),
		CapacityTarget:    q.target,
		CapacityEffective: q.eff,
		ResizePending:     q.eff != q.target,
	}
}

func (q *MemoryQueue) observeDepth() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.observeDepthLocked()
}

func (q *MemoryQueue) observeDepthLocked() {
	metrics.EnrichQueueDepth.Set(float64(len(q.buf)))
}

func (q *MemoryQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil
	}
	q.closed = true
	q.cond.Broadcast()
	return nil
}

func (q *MemoryQueue) broker() {
	for {
		q.mu.Lock()
		for len(q.buf) == 0 && !q.closed {
			q.cond.Wait()
		}
		if len(q.buf) == 0 && q.closed {
			q.observeDepthLocked()
			q.mu.Unlock()
			close(q.out)
			return
		}
		job := q.buf[0]
		q.buf = q.buf[1:]
		q.recomputeEffectiveLocked()
		q.observeDepthLocked()
		q.mu.Unlock()
		q.out <- job
	}
}

func (q *MemoryQueue) recomputeEffectiveLocked() {
	if len(q.buf) > q.target {
		q.eff = len(q.buf)
		return
	}
	q.eff = q.target
}

func maxInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}
