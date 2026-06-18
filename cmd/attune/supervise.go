// supervise.go — panic-safe supervision for long-running background workers.
//
// Every background worker used to be launched as a bare `go X.Run(ctx)`. A
// single unrecovered panic in any of them (e.g. a malformed LLM response that
// trips a nil-deref deep in a handler) would unwind to the top of the
// goroutine and crash the whole process — taking the HTTP server and every
// other worker down with it. safego wraps each worker so a panic is recovered,
// logged with a stack, counted in attune_worker_panics_total, and the worker is
// restarted with capped backoff. Supervision ends only when ctx is cancelled
// (graceful shutdown) or the worker returns on its own.
package main

import (
	"context"
	"runtime/debug"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

const (
	supervisorInitialBackoff = 200 * time.Millisecond
	supervisorMaxBackoff     = 30 * time.Second
)

// safego runs fn in a supervised goroutine. fn is expected to be a long-running
// worker loop that returns when ctx is cancelled. If fn panics, the panic is
// recovered (so it never crashes the process), logged, counted, and fn is
// restarted after a backoff that grows up to supervisorMaxBackoff. A clean
// return from fn (ctx cancelled) ends supervision without a restart.
func safego(ctx context.Context, name string, fn func()) {
	go superviseLoop(ctx, name, fn)
}

func superviseLoop(ctx context.Context, name string, fn func()) {
	backoff := supervisorInitialBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		if !runGuarded(ctx, name, fn) {
			return // fn returned cleanly — nothing to restart
		}
		// fn panicked: wait (capped) before restarting, unless we're shutting down.
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < supervisorMaxBackoff {
			backoff *= 2
			if backoff > supervisorMaxBackoff {
				backoff = supervisorMaxBackoff
			}
		}
	}
}

// runGuarded runs fn once under a recover(). It returns true if fn panicked (the
// supervisor should restart it) and false if fn returned normally.
func runGuarded(ctx context.Context, name string, fn func()) (panicked bool) {
	const where = "main.safego"
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			metrics.WorkerPanics.WithLabelValues(name).Inc()
			logext.Errorf(
				ctx,
				"[%s] worker panicked — recovered and restarting,worker:%s,panic:%v,stack:%s",
				where, name, r, string(debug.Stack()),
			)
		}
	}()
	fn()
	return false
}
