// SPDX-License-Identifier: Apache-2.0

// Package email is the inbound IMAP-poll adapter (#66 Plan T14). It
// registers via init() on package import; cmd/attune blank-imports
// this package to wire it into the inbound framework.
package email

import (
	"context"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
)

const channelName = "email"

func init() {
	inbound.Register(channelName, NewAdapter)
}

type adapter struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
	deps   inbound.Deps
	// lastSuccessAt — slug-keyed last successful pollSource finish, used
	// to emit attune_inbound_poll_lag_seconds. Reads/writes happen on
	// the pollLoop goroutine only, but the mu guard keeps it safe
	// against Shutdown winding the goroutine down concurrently.
	lastSuccessAt map[string]time.Time
}

// NewAdapter — exposed constructor. Production registration runs via
// init(); external callers (tests, one-off wiring) use NewAdapter
// directly.
func NewAdapter() inbound.Adapter {
	return &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		lastSuccessAt: map[string]time.Time{},
	}
}

// Channel reports the registered channel name.
func (a *adapter) Channel() string { return channelName }

// ShutdownTimeout — IMAP LOGOUT can block on a half-closed socket. Give
// the pollLoop 10 seconds to drain in-flight work before the framework
// cancels harder.
func (a *adapter) ShutdownTimeout() time.Duration { return 10 * time.Second }

// Start spawns the pollLoop goroutine in a fresh background context
// (independent of the Start ctx, which is request-scoped). Shutdown
// cancels that context and waits on the WaitGroup.
func (a *adapter) Start(_ context.Context, deps inbound.Deps) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		// Already started — defensive; Manager would not call us twice.
		return nil
	}
	a.deps = deps
	runCtx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.wg.Add(1)
	go a.pollLoop(runCtx)
	return nil
}

// Shutdown cancels the pollLoop and waits for it to drain. Idempotent.
func (a *adapter) Shutdown(_ context.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.wg.Wait()
	return nil
}
