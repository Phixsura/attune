// SPDX-License-Identifier: Apache-2.0

// Package inboundtest provides shared fakes and a conformance suite for
// inbound adapters. Mirrors stdlib's httptest / iotest / fstest pattern:
// production code never imports this package.
package inboundtest

import (
	"context"
	"net/http"
	"sync"
	"time"

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

// FakeSecrets — identity passthrough; tests don't need real encryption.
// Layout matches inbound's real envelope (version + key_id bytes) so
// fixtures can be reasoned about.
type FakeSecrets struct{}

// Encrypt prepends version + key_id (0x01 0x00) to plaintext.
func (FakeSecrets) Encrypt(b []byte) ([]byte, error) {
	out := make([]byte, 0, 2+len(b))
	out = append(out, 0x01, 0x00)
	out = append(out, b...)
	return out, nil
}

// Decrypt strips version + key_id.
func (FakeSecrets) Decrypt(b []byte) ([]byte, error) {
	if len(b) < 2 {
		return nil, errCrypto
	}
	return b[2:], nil
}

// FakeMetrics — no-op recorder; tests can introspect Totals.
type FakeMetrics struct{ Totals []string }

// Total appends "channel|tenant|source|result" for later assertions.
func (f *FakeMetrics) Total(channel, tenant, sourceSlug, result string) {
	f.Totals = append(f.Totals, channel+"|"+tenant+"|"+sourceSlug+"|"+result)
}

// Latency discards.
func (FakeMetrics) Latency(string, string, string, float64) {}

// SetSourceState discards.
func (FakeMetrics) SetSourceState(string, string, string, string, bool) {}

// SetPollLag discards.
func (FakeMetrics) SetPollLag(string, string, string, float64) {}

// FakeLogger — discards.
type FakeLogger struct{}

// Infof discards.
func (FakeLogger) Infof(context.Context, string, ...any) {}

// Warnf discards.
func (FakeLogger) Warnf(context.Context, string, ...any) {}

// Errorf discards.
func (FakeLogger) Errorf(context.Context, string, ...any) {}

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
		Logger:  FakeLogger{},
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
	_ inbound.Logger         = FakeLogger{}
	_ inbound.Mux            = (*FakeMux)(nil)
)

var _ = time.Second // retain time import for future expansion
