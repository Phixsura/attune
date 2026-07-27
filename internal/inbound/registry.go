// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// channelShapeRE is the narrow allow-list for a channel name: ASCII lowercase
// letters, digits, underscore, and hyphen. Each excluded character class is a
// concrete defect vector at the persisted + wire boundary:
//   - control chars (NUL etc.): PG TEXT rejects NUL on INSERT, so a NUL-bearing
//     channel would crash every ingest with that source;
//   - whitespace / slash / dot: structural characters that confuse operator UIs,
//     URL routing, and audit logs;
//   - non-ASCII (zero-width space, bidi overrides, Unicode lookalikes): the
//     homoglyph / spoofing class on a persisted identity token.
//
// The lowercase invariant is also enforced by the regex (no [A-Z]). Adapters
// declare channels at compile time, so this is a developer-facing boot panic,
// not a hot-path check.
var channelShapeRE = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Factory — adapter package's init() calls Register(channel, display, factory).
type Factory func() Adapter

// Entry — what Factories() returns. Named struct (not anonymous) so the
// public API is consumable: range, map, sort, etc. Display is the human label
// the adapter declares at its Register call; cmd/attune folds it into the
// injected SourceSet so a channel's label travels with the channel (#95).
type Entry struct {
	Channel string
	Display string
	Factory Factory
}

// registration is the per-channel record the registry holds: the constructor
// plus the declared display label.
type registration struct {
	display string
	factory Factory
}

// DefaultShutdownTimeout — applied when an Adapter does NOT implement
// ShutdownTimeouter. Set high enough for IMAP LOGOUT half-closes; webhook
// adapters that need immediate return implement ShutdownTimeouter and
// return 0.
const DefaultShutdownTimeout = 15 * time.Second

var (
	mu        sync.RWMutex
	factories = map[string]registration{}
)

// Register — called from each adapter package's init(). channel is the source
// enum the adapter owns; display is its human label (folded into the injected
// SourceSet by cmd/attune). Panics on duplicate channel name
// (compile-time-equivalent guarantee).
func Register(channel, display string, factory Factory) {
	// Registration-time invariants (the database/sql.Register / gob.Register /
	// Caddy.RegisterModule discipline): channel is a FROZEN persisted + wire
	// token, so a malformed one is a permanent storage-contract defect, and a
	// nil factory would otherwise detonate as an unattributed nil-deref deep in
	// Manager.StartAll. These are init-time programmer errors → panic with
	// attribution to the bad call site.
	if factory == nil {
		panic("inbound: nil factory for channel " + channel)
	}
	if channel == "" {
		panic("inbound: empty channel")
	}
	// Narrow allow-list at the single write point: closes case-collision (no
	// uppercase), NUL / whitespace / structural-char persistence defects, and
	// the zero-width / bidi-override homoglyph class — all real adversarial-
	// review findings — with one boot-time check. See channelShapeRE.
	if !channelShapeRE.MatchString(channel) {
		panic(fmt.Sprintf("inbound: channel %q must match %s (source is a persisted + wire token)", channel, channelShapeRE.String()))
	}
	if strings.TrimSpace(display) == "" {
		panic(fmt.Sprintf("inbound: channel %q registered with empty or whitespace-only display", channel))
	}
	// Display invariants: NUL is rejected by PG TEXT on INSERT; newlines and
	// carriage-returns corrupt the GitHub-issue markdown table row (one row =
	// one line) that the renderers produce. Tab is included for the same
	// structural-corruption reason.
	if strings.ContainsAny(display, "\x00\n\r\t") {
		panic(fmt.Sprintf("inbound: channel %q display %q contains control / structural characters (NUL / CR / LF / tab)", channel, display))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := factories[channel]; exists {
		panic(fmt.Sprintf("inbound: channel %q already registered", channel))
	}
	factories[channel] = registration{display: display, factory: factory}
}

// ResetForTest — clears the registry. Test fixtures use it to deduplicate
// across tests that import multiple adapter packages transitively.
//
// Runtime-gated via `testing.Testing()` (Go 1.21+) so a production
// binary that accidentally reaches this function panics rather than
// silently nuking its adapter registry. Spec §Registry asks for
// "build-tag-gated"; we use Go's built-in test detector instead
// because (a) shipping a build tag would require every test caller
// to opt in via `-tags`, breaking conformance suites that depend on
// cross-package imports, and (b) testing.Testing() achieves the same
// "prod can't call this" guarantee at zero cost (G4 fix, #66).
func ResetForTest() {
	if !testing.Testing() {
		panic("inbound.ResetForTest must only be called from tests")
	}
	mu.Lock()
	defer mu.Unlock()
	factories = map[string]registration{}
}

// Factories — snapshot for cmd/attune. Returns a sorted slice for
// deterministic startup order.
func Factories() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Entry, 0, len(factories))
	for ch, r := range factories {
		out = append(out, Entry{Channel: ch, Display: r.display, Factory: r.factory})
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

// Triggerable is an optional interface for adapters that support on-demand
// sync. The Manager delegates TriggerSync calls to any adapter that implements it.
type Triggerable interface {
	TriggerSync(sourceID string)
}

// TriggerSync dispatches a sync-now request to any adapter that implements Triggerable.
func (m *Manager) TriggerSync(sourceID string) {
	for _, a := range m.adapters {
		if t, ok := a.(Triggerable); ok {
			t.TriggerSync(sourceID)
		}
	}
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
