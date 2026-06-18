package enrich

import (
	"context"
	"errors"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// recoverWorker recovers a panic in an enrichment goroutine so a single bad job
// (e.g. a malformed LLM response tripping a nil-deref deep in EnrichOne) can't
// unwind to the top of the goroutine and crash the whole process — which a bare
// `go` launch would. It counts the panic in attune_worker_panics_total{worker}
// and logs a stack. The enrich runner is NOT wrapped by cmd/attune.safego
// (its Run does `defer close(r.done)`, so a restart would double-close); these
// per-goroutine recovers are the equivalent protection for the enrich path.
func recoverWorker(ctx context.Context, worker string) {
	if rec := recover(); rec != nil {
		metrics.WorkerPanics.WithLabelValues(worker).Inc()
		logext.Errorf(ctx, "[service.enrich] worker panicked — recovered,worker:%s,panic:%v,stack:%s",
			worker, rec, string(debug.Stack()))
	}
}

type enrichExecutor interface {
	EnrichOne(ctx context.Context, id int64) error
}

type pendingLister interface {
	ListPending(ctx context.Context, n int) ([]int64, error)
}

type RunnerConfig struct {
	QueueLen      int
	Workers       int
	BatchSize     int
	BatchWindow   time.Duration
	SweepInterval time.Duration
}

type RunnerSnapshot struct {
	QueueDepth             int
	QueueCapacityTarget    int
	QueueCapacityEffective int
	QueueResizePending     bool
	Workers                int
	BatchSize              int
	BatchWindow            time.Duration
	SweepInterval          time.Duration
	InFlight               int32
}

type Runner struct {
	queue   *MemoryQueue
	execute func(context.Context, Job) error
	list    func(context.Context, int) ([]int64, error)

	mu            sync.RWMutex
	cfg           RunnerConfig
	inFlight      atomic.Int32
	lastAppliedAt atomic.Int64

	done chan struct{}
}

func NewRunner(repo pendingLister, executor enrichExecutor, cfg RunnerConfig) *Runner {
	queue := NewMemoryQueue(cfg.QueueLen)
	return ptrext.Of(Runner{
		queue: queue,
		execute: func(ctx context.Context, job Job) error {
			traceID := job.TraceID
			if traceID == "" {
				traceID = trace.New()
			}
			jobCtx := trace.WithID(context.WithoutCancel(ctx), traceID)
			return executor.EnrichOne(jobCtx, job.ID)
		},
		list: repo.ListPending,
		cfg:  normalizeRunnerConfig(cfg),
		done: make(chan struct{}),
	})
}

func normalizeRunnerConfig(cfg RunnerConfig) RunnerConfig {
	if cfg.QueueLen <= 0 {
		cfg.QueueLen = 1
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1
	}
	if cfg.BatchWindow <= 0 {
		cfg.BatchWindow = time.Millisecond
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = time.Second
	}
	return cfg
}

func (r *Runner) Configure(cfg RunnerConfig) error {
	cfg = normalizeRunnerConfig(cfg)
	r.mu.Lock()
	r.cfg = cfg
	r.mu.Unlock()
	r.queue.Configure(cfg.QueueLen)
	r.lastAppliedAt.Store(time.Now().UTC().UnixNano())
	return nil
}

func (r *Runner) Snapshot() RunnerSnapshot {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()
	queue := r.queue.Status()
	return RunnerSnapshot{
		QueueDepth:             queue.Depth,
		QueueCapacityTarget:    queue.CapacityTarget,
		QueueCapacityEffective: queue.CapacityEffective,
		QueueResizePending:     queue.ResizePending,
		Workers:                cfg.Workers,
		BatchSize:              cfg.BatchSize,
		BatchWindow:            cfg.BatchWindow,
		SweepInterval:          cfg.SweepInterval,
		InFlight:               r.inFlight.Load(),
	}
}

func (r *Runner) Submit(ctx context.Context, job Job) error {
	if r == nil || r.queue == nil {
		return ErrQueueClosed
	}
	return r.queue.Submit(ctx, job)
}

func (r *Runner) Run(ctx context.Context) {
	const where = "service.enrich.Runner.Run"
	defer close(r.done)
	snap := r.Snapshot()
	logext.Infof(ctx, "[%s] started,queue_len:%d,workers:%d,batch_size:%d,batch_window:%s,sweep_interval:%s",
		where, snap.QueueCapacityTarget, snap.Workers, snap.BatchSize, snap.BatchWindow, snap.SweepInterval)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer recoverWorker(ctx, "enrich_processor")
		r.runProcessor(ctx)
	}()
	go func() {
		defer wg.Done()
		defer recoverWorker(ctx, "enrich_sweeper")
		r.runSweeper(ctx)
	}()
	<-ctx.Done()
	_ = r.queue.Close()
	wg.Wait()
}

func (r *Runner) Wait(ctx context.Context) error {
	if r == nil {
		return nil
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) runSweeper(ctx context.Context) {
	const where = "service.enrich.Runner.runSweeper"
	for {
		cfg := r.loadConfig()
		timer := time.NewTimer(cfg.SweepInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		ids, err := r.list(ctx, cfg.BatchSize)
		if err != nil {
			logext.Errorf(ctx, "[%s] list pending failed,err:%+v", where, err.Error())
			continue
		}
		for _, id := range ids {
			if err := r.Submit(ctx, Job{ID: id, TraceID: trace.New()}); err != nil {
				if errors.Is(err, ErrQueueFull) || errors.Is(err, ErrQueueClosed) {
					break
				}
				logext.Warnf(ctx, "[%s] submit failed,id:%d,err:%+v", where, id, err.Error())
				continue
			}
			metrics.EnrichSweepSubmittedTotal.Inc()
		}
	}
}

func (r *Runner) runProcessor(ctx context.Context) {
	const where = "service.enrich.Runner.runProcessor"
	ch := r.queue.Consume()
	for {
		batch, open := r.collectBatch(ctx, ch)
		if len(batch) > 0 {
			metrics.EnrichBatchSize.Observe(float64(len(batch)))
			r.processBatch(ctx, batch)
		}
		if !open {
			logext.Infof(ctx, "[%s] stopped", where)
			return
		}
	}
}

func (r *Runner) collectBatch(ctx context.Context, ch <-chan Job) ([]Job, bool) {
	cfg := r.loadConfig()
	select {
	case <-ctx.Done():
		return nil, false
	case job, ok := <-ch:
		if !ok {
			return nil, false
		}
		r.queue.observeDepth()
		batch := []Job{job}
		timer := time.NewTimer(cfg.BatchWindow)
		defer timer.Stop()
		for len(batch) < cfg.BatchSize {
			select {
			case job, ok := <-ch:
				if !ok {
					return batch, false
				}
				r.queue.observeDepth()
				batch = append(batch, job)
			case <-timer.C:
				return batch, true
			case <-ctx.Done():
				return batch, false
			}
		}
		return batch, true
	}
}

func (r *Runner) processBatch(ctx context.Context, batch []Job) {
	cfg := r.loadConfig()
	sem := make(chan struct{}, cfg.Workers)
	var wg sync.WaitGroup
	for _, job := range batch {
		sem <- struct{}{}
		wg.Add(1)
		go func(job Job) {
			r.inFlight.Add(1)
			defer func() {
				r.inFlight.Add(-1)
				<-sem
				wg.Done()
			}()
			// Per-job recover: a panic enriching one row (the documented
			// malformed-LLM-response risk) must not crash the process or the
			// processor — count it, log it, and move on to the next job.
			defer recoverWorker(ctx, "enrich_job")
			if err := r.execute(ctx, job); err != nil {
				logext.Warnf(ctx, "[service.enrich.Runner.processBatch] enrich failed,id:%d,err:%+v", job.ID, err.Error())
			}
		}(job)
	}
	wg.Wait()
}

func (r *Runner) loadConfig() RunnerConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}
