// SPDX-License-Identifier: Apache-2.0

// Package safegoroutine provides utilities for launching goroutines with
// automatic panic recovery and logging.
package safegoroutine

import (
	"context"
	"runtime/debug"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

// Go launches a goroutine with panic recovery. If the function panics,
// the panic is logged with stack trace and the worker panic metric is
// incremented. The goroutine does not propagate the panic.
func Go(ctx context.Context, name string, f func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logext.Errorf(ctx, "[safegoroutine] panic in %s: %v\n%s", name, r, debug.Stack())
				metrics.WorkerPanics.WithLabelValues(name).Inc()
			}
		}()
		f()
	}()
}

// GoWithContext launches a goroutine that receives the context. Same panic
// recovery as Go.
func GoWithContext(ctx context.Context, name string, f func(context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logext.Errorf(ctx, "[safegoroutine] panic in %s: %v\n%s", name, r, debug.Stack())
				metrics.WorkerPanics.WithLabelValues(name).Inc()
			}
		}()
		f(ctx)
	}()
}

// GoLoop launches a goroutine that runs f repeatedly until ctx is canceled.
// Each iteration has independent panic recovery — a panic in one iteration
// does not stop the loop.
func GoLoop(ctx context.Context, name string, f func(context.Context)) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			func() {
				defer func() {
					if r := recover(); r != nil {
						logext.Errorf(ctx, "[safegoroutine] panic in %s loop: %v\n%s", name, r, debug.Stack())
						metrics.WorkerPanics.WithLabelValues(name).Inc()
					}
				}()
				f(ctx)
			}()
		}
	}()
}
