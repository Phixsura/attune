// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// TestWebhookHandle_EmitsLatencyAndSourceStateOnSuccess covers the T23
// observability wiring: a successful POST must increment Total{result=ok}
// AND observe attune_inbound_latency_seconds AND set
// attune_inbound_source_state{state="enabled"} to 1. Without those calls,
// the metric vectors would not surface for ops on /metrics.
func TestWebhookHandle_EmitsLatencyAndSourceStateOnSuccess(t *testing.T) {
	installDelayedWebhookClock(t)
	fixture := newWebhookMetricsFixture(t)

	w := serveWebhook(fixture.adapter, fixture.request)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if len(fixture.metrics.Totals) == 0 || !strings.HasSuffix(fixture.metrics.Totals[len(fixture.metrics.Totals)-1], "|ok") {
		t.Errorf("expected last Total call to end with |ok, got %+v", fixture.metrics.Totals)
	}
	if len(fixture.metrics.Latencies) != 1 {
		t.Errorf("expected exactly 1 Latency call, got %d (%+v)",
			len(fixture.metrics.Latencies), fixture.metrics.Latencies)
	}
	if len(fixture.metrics.LatencySeconds) != 1 || fixture.metrics.LatencySeconds[0] < 1.5 {
		t.Errorf("latency should include bind/auth/decode time; got %+v", fixture.metrics.LatencySeconds)
	}
	want := "webhook|tenant-acme|main|enabled=on"
	if len(fixture.metrics.StateCalls) != 1 || fixture.metrics.StateCalls[0] != want {
		t.Errorf("expected SetSourceState call %q, got %+v", want, fixture.metrics.StateCalls)
	}
}

func TestWebhookHandle_OversizeBodyUsesDispatcherLimitEnvelope(t *testing.T) {
	fixture := newWebhookMetricsFixture(t)
	fixture.request.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxBodyBytes+1)))
	w := serveWebhook(fixture.adapter, fixture.request)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status: got %d, want 413; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"BODY_TOO_LARGE"`) {
		t.Fatalf("want BODY_TOO_LARGE envelope, got %s", w.Body.String())
	}
	if len(fixture.metrics.Totals) != 1 || fixture.metrics.Totals[0] != "webhook|unknown|unknown|validate_err" {
		t.Fatalf("expected one unknown validate_err metric, got %+v", fixture.metrics.Totals)
	}
}

type webhookMetricsFixture struct {
	adapter *adapter
	request *http.Request
	metrics *inboundtest.FakeMetrics
	sources *inboundtest.FakeSources
	ingest  *inboundtest.FakeIngest
	source  inbound.Source
}

func installDelayedWebhookClock(t *testing.T) {
	t.Helper()
	oldNow := nowFn
	base := time.Now()
	calls := 0
	nowFn = func() time.Time {
		calls++
		if calls == 1 {
			return base.Add(-2 * time.Second)
		}
		return base
	}
	t.Cleanup(func() { nowFn = oldNow })
}

func newWebhookMetricsFixture(t *testing.T) webhookMetricsFixture {
	t.Helper()
	secrets := inboundtest.FakeSecrets{}
	plaintext := []byte("test-secret-32-bytes-padding-zzz")
	encSecret, err := secrets.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	innerCfg, err := json.Marshal(map[string]any{
		"version":                  1,
		"secret_current_encrypted": encSecret,
		"hmac_algo":                "sha256",
	})
	if err != nil {
		t.Fatalf("marshal inner cfg: %v", err)
	}
	encInner, err := secrets.Encrypt(innerCfg)
	if err != nil {
		t.Fatalf("encrypt inner cfg: %v", err)
	}

	sources := inboundtest.NewFakeSources()
	src := inbound.Source{
		ID:       "src-1",
		TenantID: "tenant-acme",
		Channel:  channelName,
		Slug:     "main",
		Name:     "Main webhook",
		Enabled:  true,
		Config:   encInner,
	}
	sources.Put("acme", src)

	ingest := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	mux := ptrext.Of(inboundtest.FakeMux{})
	metrics := ptrext.Of(inboundtest.FakeMetrics{})

	a := &adapter{ // ptrext:allow inbound-handle-identity
		deps: inbound.Deps{
			Mux:     mux,
			Ingest:  inbound.IngestFunc(ingest.Ingest),
			Sources: sources,
			Secrets: secrets,
			Metrics: metrics,
		},
	}

	// Sign a valid request.
	body := []byte(`{"content":"hello world"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := "sha256=" + computeSig(plaintext, ts, body)

	r := httptest.NewRequest(http.MethodPost,
		"/webhook/acme/main", strings.NewReader(string(body))).
		WithContext(context.Background())
	r.Header.Set(hdrTimestamp, ts)
	r.Header.Set(hdrSignature, sig)
	// chi route params are normally injected by the router; tests have to
	// thread them in via RouteContext.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant-slug", "acme")
	rctx.URLParams.Add("source-slug", "main")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	return webhookMetricsFixture{adapter: a, request: r, metrics: metrics, sources: sources, ingest: ingest, source: src}
}

func TestWebhookHandle_UsesAuthenticatedSourceFromBinder(t *testing.T) {
	fixture := newWebhookMetricsFixture(t)
	spy := ptrext.Of(mutatingSourceStore{
		inner: fixture.sources,
		replacement: inbound.Source{
			ID:       "src-mutated",
			TenantID: "tenant-mutated",
			Channel:  channelName,
			Slug:     "main",
			Name:     "Mutated webhook",
			Enabled:  true,
			Config:   fixture.source.Config,
		},
	})
	fixture.adapter.deps.Sources = spy

	w := serveWebhook(fixture.adapter, fixture.request)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if spy.GetBySlugsCalls != 1 {
		t.Fatalf("GetBySlugs calls: got %d, want 1", spy.GetBySlugsCalls)
	}
	if len(fixture.ingest.Calls) != 1 {
		t.Fatalf("ingest calls: got %d, want 1", len(fixture.ingest.Calls))
	}
	call := fixture.ingest.Calls[0]
	if call.TenantID != "tenant-acme" {
		t.Fatalf("tenant from authenticated source: got %q, want tenant-acme", call.TenantID)
	}
	if got := call.In.SourceMeta["inbound_source_id"]; got != "src-1" {
		t.Fatalf("source meta id: got %v, want src-1", got)
	}
}

type mutatingSourceStore struct {
	inner           *inboundtest.FakeSources
	replacement     inbound.Source
	GetBySlugsCalls int
}

func (s *mutatingSourceStore) List(ctx context.Context, channel string) ([]inbound.Source, error) {
	return s.inner.List(ctx, channel)
}

func (s *mutatingSourceStore) Get(ctx context.Context, id string) (inbound.Source, error) {
	return s.inner.Get(ctx, id)
}

func (s *mutatingSourceStore) GetBySlugs(ctx context.Context, tenantSlug, channel, sourceSlug string) (inbound.Source, error) {
	s.GetBySlugsCalls++
	src, err := s.inner.GetBySlugs(ctx, tenantSlug, channel, sourceSlug)
	if s.GetBySlugsCalls == 1 {
		s.inner.Put(tenantSlug, s.replacement)
	}
	return src, err
}

func (s *mutatingSourceStore) UpdateState(ctx context.Context, id string, state inbound.SourceState) error {
	return s.inner.UpdateState(ctx, id, state)
}

func (s *mutatingSourceStore) SetEnabled(ctx context.Context, id string, enabled bool, reason string) error {
	return s.inner.SetEnabled(ctx, id, enabled, reason)
}

func serveWebhook(a *adapter, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.webhook.handle",
		dispatcher.Empty(func() *attunev1.IngestRequest { return ptrext.Of(attunev1.IngestRequest{}) }),
		a.handle,
		dispatcher.WithAuth(a.bindRequest),
	)
	handler(w, r)
	return w
}
