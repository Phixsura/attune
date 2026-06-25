// SPDX-License-Identifier: Apache-2.0

// Package configreload provides configuration hot-reload support.
package configreload

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Reloader watches for reload signals and notifies subscribers.
type Reloader struct {
	mu        sync.RWMutex
	callbacks []func()
	debounce  time.Duration
	lastCall  time.Time
}

// New creates a new Reloader with the given debounce duration.
func New(debounce time.Duration) *Reloader {
	if debounce <= 0 {
		debounce = 1 * time.Second
	}
	return ptrext.Of(Reloader{
		debounce: debounce,
	})
}

// OnReload registers a callback to be called on reload.
// Callbacks are called in registration order.
func (r *Reloader) OnReload(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbacks = append(r.callbacks, fn)
}

// Trigger manually triggers a reload.
func (r *Reloader) Trigger() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if now.Sub(r.lastCall) < r.debounce {
		return
	}
	r.lastCall = now

	for _, fn := range r.callbacks {
		fn()
	}
}

// WatchSignal watches for SIGHUP and triggers reload.
// Blocks until context is cancelled.
func (r *Reloader) WatchSignal(ctx context.Context) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigCh:
			r.Trigger()
		}
	}
}

// Value wraps a value that can be atomically updated on reload.
type Value[T any] struct {
	mu    sync.RWMutex
	value T
}

// NewValue creates a new reloadable value with initial value.
func NewValue[T any](initial T) *Value[T] {
	return &Value[T]{value: initial} // ptrext:allow generic type
}

// Get returns the current value.
func (v *Value[T]) Get() T {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.value
}

// Set updates the value.
func (v *Value[T]) Set(val T) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.value = val
}

// Update applies a function to update the value.
func (v *Value[T]) Update(fn func(T) T) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.value = fn(v.value)
}
