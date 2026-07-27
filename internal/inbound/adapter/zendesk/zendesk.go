// SPDX-License-Identifier: Apache-2.0

// Package zendesk is the inbound Zendesk channel adapter. It polls a
// Zendesk instance via the incremental ticket export API and normalizes
// each ticket (with its public comments) into the shared ingest shape.
package zendesk

import (
	"context"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
)

// Channel is the registered channel name for Zendesk inbound sources.
const Channel = "zendesk"

const (
	channelName         = Channel
	defaultPollInterval = 60 * time.Second
)

func init() {
	inbound.Register(channelName, "Zendesk", NewAdapter)
}

type adapter struct {
	mu            sync.Mutex
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	deps          inbound.Deps
	newClient     clientFactory
	lastSuccessAt map[string]time.Time
	lastAttemptAt map[string]time.Time
	failureCount  map[string]int // per-source consecutive failure counter
	// processedTickets remembers ticket snapshots already handled on a
	// page whose cursor advance was NOT persisted (mid-page stop on
	// comment budget or transient failure). The re-fetched page skips
	// them for free — no comment fetch, no ingest. Cleared whenever the
	// cursor advances (the export stream never re-lists those
	// snapshots). Memory-only: a restart costs one idempotency-deduped
	// re-pass of a single page.
	processedTickets map[string]map[string]struct{} // sourceID → ticket@genTS
	syncNow          chan string                    // receives source ID for immediate sync
}

// NewAdapter returns a fresh Zendesk adapter instance.
func NewAdapter() inbound.Adapter {
	return &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		newClient:        newAPIClient,
		lastSuccessAt:    map[string]time.Time{},
		lastAttemptAt:    map[string]time.Time{},
		failureCount:     map[string]int{},
		processedTickets: map[string]map[string]struct{}{},
		syncNow:          make(chan string, 1),
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
