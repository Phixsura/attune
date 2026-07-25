// SPDX-License-Identifier: Apache-2.0

// Package intercom is the inbound Intercom channel adapter. It polls an
// Intercom workspace via the conversations search API with an updated_at
// watermark and normalizes each conversation (with its thread parts)
// into the shared ingest shape.
package intercom

import (
	"context"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
)

// Channel is the registered channel name for Intercom inbound sources.
const Channel = "intercom"

const (
	channelName         = Channel
	defaultPollInterval = 60 * time.Second
)

func init() {
	inbound.Register(channelName, "Intercom", NewAdapter)
}

type adapter struct {
	mu            sync.Mutex
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	deps          inbound.Deps
	newClient     clientFactory
	lastSuccessAt map[string]time.Time
	failureCount  map[string]int // per-source consecutive failure counter
	syncNow       chan string    // receives source ID for immediate sync
}

// NewAdapter returns a fresh Intercom adapter instance.
func NewAdapter() inbound.Adapter {
	return &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		newClient:     newAPIClient,
		lastSuccessAt: map[string]time.Time{},
		failureCount:  map[string]int{},
		syncNow:       make(chan string, 1),
	}
}

// TriggerSync requests an immediate sync for the given source ID.
// Non-blocking: if a sync is already pending, the request is dropped.
func (a *adapter) TriggerSync(sourceID string) {
	select {
	case a.syncNow <- sourceID:
	default:
	}
}

// Channel reports the registered channel name.
func (a *adapter) Channel() string { return channelName }

// ShutdownTimeout gives the poll loop time to finish the current pass.
func (a *adapter) ShutdownTimeout() time.Duration { return 10 * time.Second }

// Start launches the poll loop in a background context.
func (a *adapter) Start(_ context.Context, deps inbound.Deps) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return nil
	}
	a.deps = deps
	runCtx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.wg.Add(1)
	go a.pollLoop(runCtx)
	return nil
}

// Shutdown cancels the poll loop and waits for it to drain.
func (a *adapter) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
