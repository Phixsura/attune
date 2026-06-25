// SPDX-License-Identifier: Apache-2.0

// Package workerdrain provides a reusable graceful shutdown drain helper
// for background workers. It tracks in-flight work and waits for completion
// with a configurable timeout.
package workerdrain

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// DefaultTimeout is the default drain timeout.
const DefaultTimeout = 30 * time.Second

// Drainer tracks in-flight work and provides graceful shutdown drain.
type Drainer struct {
	name     string
	wg       sync.WaitGroup
	inFlight atomic.Int64
	timeout  time.Duration
}

// New creates a new Drainer with the given worker name.
func New(name string) *Drainer {
	return ptrext.Of(Drainer{
		name:    name,
		timeout: DefaultTimeout,
	})
}

// SetTimeout configures the drain timeout.
func (d *Drainer) SetTimeout(timeout time.Duration) {
	if timeout > 0 {
		d.timeout = timeout
	}
}

// Enter marks the start of in-flight work. Call Leave when done.
func (d *Drainer) Enter() {
	d.wg.Add(1)
	d.inFlight.Add(1)
	metrics.WorkerInFlight.WithLabelValues(d.name).Set(float64(d.inFlight.Load()))
}

// Leave marks the end of in-flight work.
func (d *Drainer) Leave() {
	d.wg.Done()
	d.inFlight.Add(-1)
	metrics.WorkerInFlight.WithLabelValues(d.name).Set(float64(d.inFlight.Load()))
}

// InFlight returns the current number of in-flight items.
func (d *Drainer) InFlight() int64 {
	return d.inFlight.Load()
}

// Drain waits for in-flight work to complete with timeout.
// It logs and records metrics for the drain outcome.
func (d *Drainer) Drain(ctx context.Context) {
	inFlight := d.inFlight.Load()
	if inFlight == 0 {
		metrics.WorkerDrainTotal.WithLabelValues(d.name, "clean").Inc()
		logext.Infof(ctx, "[%s.drain] no in-flight work", d.name)
		return
	}
	logext.Infof(ctx, "[%s.drain] waiting for %d in-flight items", d.name, inFlight)

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		metrics.WorkerDrainTotal.WithLabelValues(d.name, "clean").Inc()
		logext.Infof(ctx, "[%s.drain] complete", d.name)
	case <-time.After(d.timeout):
		remaining := d.inFlight.Load()
		metrics.WorkerDrainTotal.WithLabelValues(d.name, "timeout").Inc()
		logext.Warnf(ctx, "[%s.drain] timeout,remaining:%d", d.name, remaining)
	}
}
