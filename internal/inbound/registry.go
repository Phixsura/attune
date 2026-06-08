// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Factory — adapter package's init() calls Register(channel, factory).
type Factory func() Adapter

// Entry — what Factories() returns. Named struct (not anonymous) so the
// public API is consumable: range, map, sort, etc.
type Entry struct {
	Channel string
	Factory Factory
}

// DefaultShutdownTimeout — applied when an Adapter does NOT implement
// ShutdownTimeouter. Set high enough for IMAP LOGOUT half-closes; webhook
// adapters that need immediate return implement ShutdownTimeouter and
// return 0.
const DefaultShutdownTimeout = 15 * time.Second

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register — called from each adapter package's init(). Panics on
// duplicate channel name (compile-time-equivalent guarantee).
func Register(channel string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := factories[channel]; exists {
		panic(fmt.Sprintf("inbound: channel %q already registered", channel))
	}
	factories[channel] = factory
}

// ResetForTest — clears the registry. Test fixtures use it to deduplicate
// across tests that import multiple adapter packages transitively.
// Documented test-only — production binaries should not call this.
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	factories = map[string]Factory{}
}

// Factories — snapshot for cmd/attune. Returns a sorted slice for
// deterministic startup order.
func Factories() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Entry, 0, len(factories))
	for ch, f := range factories {
		out = append(out, Entry{Channel: ch, Factory: f})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out
}

// Manager — orchestrates Start/Shutdown across all registered adapters.
type Manager struct {
	deps     Deps
	adapters []Adapter
}

// NewManager — constructor.
func NewManager(deps Deps) *Manager { return ptrext.Of(Manager{deps: deps}) }

// StartAll — starts every registered adapter in deterministic order. On
// any single failure, already-started adapters are shut down with their
// per-adapter deadline; the original error is returned.
func (m *Manager) StartAll(ctx context.Context) error {
	for _, entry := range Factories() {
		a := entry.Factory()
		if err := a.Start(ctx, m.deps); err != nil {
			_ = m.shutdownStarted(context.Background())
			return fmt.Errorf("inbound: start %q: %w", entry.Channel, err)
		}
		m.adapters = append(m.adapters, a)
	}
	return nil
}

// ShutdownAll — reverse order; per-adapter timeout; errors aggregated via
// errors.Join.
func (m *Manager) ShutdownAll(ctx context.Context) error {
	return m.shutdownStarted(ctx)
}

// shutdownStarted — each adapter gets its own deadline (DefaultShutdownTimeout
// or what ShutdownTimeouter declares). A wedged adapter does not steal the
// budget from the next one.
func (m *Manager) shutdownStarted(parent context.Context) error {
	var errs []error
	for i := len(m.adapters) - 1; i >= 0; i-- {
		a := m.adapters[i]
		budget := DefaultShutdownTimeout
		if t, ok := a.(ShutdownTimeouter); ok {
			budget = t.ShutdownTimeout()
		}
		ctx, cancel := context.WithTimeout(parent, budget)
		if err := a.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("inbound: shutdown adapter %d: %w", i, err))
		}
		cancel()
	}
	return errors.Join(errs...)
}
