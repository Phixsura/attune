// SPDX-License-Identifier: Apache-2.0

// Package inboundtest provides shared fakes and a conformance suite for
// inbound adapters. Mirrors stdlib's httptest / iotest / fstest pattern:
// production code never imports this package.
package inboundtest

import (
	"context"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// FakeIngest — records IngestInput by tenant/source. Concurrent-safe.
type FakeIngest struct {
	mu      sync.Mutex
	Calls   []FakeIngestCall
	NextErr error
}

// FakeIngestCall captures one Ingest invocation for assertion.
type FakeIngestCall struct {
	TenantID string
	KeyID    uuid.UUID
	In       domain.IngestInput
}

// Ingest satisfies inbound.IngestPort.
func (f *FakeIngest) Ingest(_ context.Context, tenant string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.NextErr; err != nil {
		f.NextErr = nil
		return 0, err
	}
	f.Calls = append(f.Calls, FakeIngestCall{tenant, keyID, in})
	return int64(len(f.Calls)), nil
}

// FakeSources — in-memory SourceStore.
type FakeSources struct {
	mu     sync.RWMutex
	bySlug map[string]inbound.Source // key = "<tenantSlug>|<channel>|<sourceSlug>"
	byID   map[string]inbound.Source
}

// NewFakeSources — empty store ready for Put.
func NewFakeSources() *FakeSources {
	return &FakeSources{ // ptrext:allow mutex-identity
		bySlug: map[string]inbound.Source{},
		byID:   map[string]inbound.Source{},
	}
}

// Put inserts/replaces a source. tenantSlug is used only for the
// GetBySlugs lookup key (Source.TenantID still holds the canonical id).
func (f *FakeSources) Put(tenantSlug string, s inbound.Source) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bySlug[tenantSlug+"|"+s.Channel+"|"+s.Slug] = s
	f.byID[s.ID] = s
}

// List satisfies inbound.SourceStore.
func (f *FakeSources) List(_ context.Context, channel string) ([]inbound.Source, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := []inbound.Source{}
	for _, s := range f.byID {
		if s.Channel == channel {
			out = append(out, s)
		}
	}
	return out, nil
}

// Get satisfies inbound.SourceStore.
func (f *FakeSources) Get(_ context.Context, id string) (inbound.Source, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.byID[id]
	if !ok {
		return inbound.Source{}, errNotFound
	}
	return s, nil
}

// GetBySlugs satisfies inbound.SourceStore.
func (f *FakeSources) GetBySlugs(_ context.Context, tenantSlug, channel, sourceSlug string) (inbound.Source, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.bySlug[tenantSlug+"|"+channel+"|"+sourceSlug]
	if !ok {
		return inbound.Source{}, errNotFound
	}
	return s, nil
}

// UpdateState satisfies inbound.SourceStore.
func (f *FakeSources) UpdateState(_ context.Context, id string, state inbound.SourceState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byID[id]
	if !ok {
		return errNotFound
	}
	s.State = state
	f.byID[id] = s
	return nil
}

// SetEnabled satisfies inbound.SourceStore.
func (f *FakeSources) SetEnabled(_ context.Context, id string, enabled bool, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byID[id]
	if !ok {
		return errNotFound
	}
	s.Enabled = enabled
	f.byID[id] = s
	return nil
}

// FakeSecrets — deterministic test-only envelope; tests don't need real
// encryption.
type FakeSecrets struct{}

// Encrypt prepends a tiny marker to plaintext. Caps
// input at 1 MiB so the `2+len(b)` capacity computation cannot overflow
// int on 32-bit builds (CodeQL #30) — fixtures never approach this.
func (FakeSecrets) Encrypt(b []byte) ([]byte, error) {
	const maxLen = 1 << 20
	if len(b) > maxLen {
		return nil, errCrypto
	}
	out := make([]byte, 0, 2+len(b))
	out = append(out, 0x01, 0x00)
	out = append(out, b...)
	return out, nil
}

// Decrypt strips the test marker.
func (FakeSecrets) Decrypt(b []byte) ([]byte, error) {
	if len(b) < 2 {
		return nil, errCrypto
	}
	return b[2:], nil
}

// FakeMetrics — recorder; tests can introspect every emitted call to
// confirm the adapter wired the four standard inbound metrics.
type FakeMetrics struct {
	Totals         []string
	Latencies      []string
	LatencySeconds []float64
	StateCalls     []string
	PollLags       []string
}

// Total appends "channel|tenant|source|result" for later assertions.
func (f *FakeMetrics) Total(channel, tenant, sourceSlug, result string) {
	f.Totals = append(f.Totals, channel+"|"+tenant+"|"+sourceSlug+"|"+result)
}

// Latency appends "channel|tenant|source" plus the observed seconds for tests
// that need to pin measurement boundaries.
func (f *FakeMetrics) Latency(channel, tenant, sourceSlug string, seconds float64) {
	f.Latencies = append(f.Latencies, channel+"|"+tenant+"|"+sourceSlug)
	f.LatencySeconds = append(f.LatencySeconds, seconds)
}

// SetSourceState appends "channel|tenant|source|state=on|off" so tests
// can assert both the state name and the on/off transition.
func (f *FakeMetrics) SetSourceState(channel, tenant, sourceSlug, state string, on bool) {
	v := "off"
	if on {
		v = "on"
	}
	f.StateCalls = append(f.StateCalls, channel+"|"+tenant+"|"+sourceSlug+"|"+state+"="+v)
}

// SetPollLag appends "channel|tenant|source" (the value is wall-clock
// jitter; tests assert presence + order, not magnitude).
func (f *FakeMetrics) SetPollLag(channel, tenant, sourceSlug string, _ float64) {
	f.PollLags = append(f.PollLags, channel+"|"+tenant+"|"+sourceSlug)
}

// FakeMux — collects mounted routes for assertions.
type FakeMux struct {
	Routes []FakeRoute
}

// FakeRoute is one entry in FakeMux.Routes.
type FakeRoute struct {
	Method  string
	Pattern string
	Handler http.Handler
}

// Method satisfies inbound.Mux.
func (m *FakeMux) Method(method, pattern string, h http.Handler) {
	m.Routes = append(m.Routes, FakeRoute{method, pattern, h})
}

// DepsFor — convenience: a Deps wired to in-memory fakes. Pass nil for
// any of the first three arguments to use freshly-constructed defaults.
func DepsFor(ingest *FakeIngest, sources *FakeSources, mux *FakeMux) inbound.Deps {
	if ingest == nil {
		ingest = &FakeIngest{} // ptrext:allow mutex-identity
	}
	if sources == nil {
		sources = NewFakeSources()
	}
	if mux == nil {
		mux = ptrext.Of(FakeMux{})
	}
	return inbound.Deps{
		Mux:     mux,
		Ingest:  inbound.IngestFunc(ingest.Ingest),
		Sources: sources,
		Secrets: FakeSecrets{},
		Metrics: ptrext.Of(FakeMetrics{}),
	}
}

// internal sentinel errors used by the fakes.
var (
	errNotFound = stringErr("not found")
	errCrypto   = stringErr("crypto")
)

type stringErr string

func (s stringErr) Error() string { return string(s) }

// Compile-time interface assertions.
var (
	_ inbound.SourceStore    = (*FakeSources)(nil)
	_ inbound.SecretStore    = FakeSecrets{}
	_ inbound.InboundMetrics = (*FakeMetrics)(nil)
	_ inbound.Mux            = (*FakeMux)(nil)
)
