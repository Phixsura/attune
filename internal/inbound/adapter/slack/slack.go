// SPDX-License-Identifier: Apache-2.0

// Package slack is the inbound Slack channel adapter. It polls a
// single readable channel per source and normalizes each message into
// the shared ingest shape.
package slack

import (
	"context"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
)

const ChannelName = "slack"

const (
	channelName         = ChannelName
	defaultPollInterval = 60 * time.Second
	syncLookback        = 2 * time.Minute
)

func init() {
	inbound.Register(channelName, "Slack", NewAdapter)
}

type adapter struct {
	mu            sync.Mutex
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	deps          inbound.Deps
	newClient     clientFactory
	lastSuccessAt map[string]time.Time
}

// NewAdapter returns a fresh Slack adapter instance.
func NewAdapter() inbound.Adapter {
	return &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		newClient:     newAPIClient,
		lastSuccessAt: map[string]time.Time{},
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
