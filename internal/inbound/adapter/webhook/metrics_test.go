// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// TestWebhookHandle_EmitsLatencyAndSourceStateOnSuccess covers the T23
// observability wiring: a successful POST must increment Total{result=ok}
// AND observe attune_inbound_latency_seconds AND set
// attune_inbound_source_state{state="enabled"} to 1. Without those calls,
// the metric vectors would not surface for ops on /metrics.
func TestWebhookHandle_EmitsLatencyAndSourceStateOnSuccess(t *testing.T) {
	// Build an in-memory source with a known plaintext secret. The
	// FakeSecrets envelope is identity-passthrough (version + key_id +
	// payload), so we can hand-roll the wire shape parseConfig expects.
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
		stubSecret: ProcessStubSecret(),
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

	w := httptest.NewRecorder()
	a.handle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if len(metrics.Totals) == 0 || !strings.HasSuffix(metrics.Totals[len(metrics.Totals)-1], "|ok") {
		t.Errorf("expected last Total call to end with |ok, got %+v", metrics.Totals)
	}
	if len(metrics.Latencies) != 1 {
		t.Errorf("expected exactly 1 Latency call, got %d (%+v)",
			len(metrics.Latencies), metrics.Latencies)
	}
	want := "webhook|tenant-acme|main|enabled=on"
	if len(metrics.StateCalls) != 1 || metrics.StateCalls[0] != want {
		t.Errorf("expected SetSourceState call %q, got %+v", want, metrics.StateCalls)
	}
}
