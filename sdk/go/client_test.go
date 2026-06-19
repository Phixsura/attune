package attune

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestClient returns a client pointed at srv with deterministic jitter and a
// no-op sleep that records the delays it was asked to wait.
func newTestClient(t *testing.T, srv *httptest.Server, opts ...Option) (*Client, *[]time.Duration) {
	t.Helper()
	c, err := New(srv.URL, "att_sk_test", opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var slept []time.Duration
	c.rand = func() float64 { return 0.5 }
	c.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	return c, &slept
}

func okHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "42", "enrichmentStatus": "pending"})
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New("https://x.example", ""); err == nil {
		t.Error("empty apiKey should error")
	}
	if _, err := New("https://x.example", "key\nwith-newline"); err == nil {
		t.Error("apiKey with newline should error")
	}
	if _, err := New("://bad", "k"); err == nil {
		t.Error("invalid baseURL should error")
	}
	if _, err := New("ftp://x.example", "k"); err == nil {
		t.Error("non-http(s) scheme should error")
	}
	if _, err := New("https://x.example", "k"); err != nil {
		t.Errorf("valid args should not error: %v", err)
	}
}

func TestIngestHappyPath(t *testing.T) {
	var gotReq *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "42", "enrichmentStatus": "pending"})
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	res, err := c.Ingest(context.Background(), IngestInput{Content: "hello", Source: "web"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.ID != "42" || res.EnrichmentStatus != "pending" {
		t.Errorf("result = %+v", res)
	}
	if gotReq.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", gotReq.Method)
	}
	if gotReq.URL.Path != "/v1/feedback/ingest" {
		t.Errorf("path = %s, want /v1/feedback/ingest", gotReq.URL.Path)
	}
	if gotReq.Header.Get("X-API-Key") != "att_sk_test" {
		t.Errorf("X-API-Key = %q", gotReq.Header.Get("X-API-Key"))
	}
	if ct := gotReq.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if ua := gotReq.Header.Get("User-Agent"); !strings.HasPrefix(ua, "attune-go/"+Version+" go/") {
		t.Errorf("User-Agent = %q, want prefix attune-go/%s go/", ua, Version)
	}
	if k := gotReq.Header.Get("Idempotency-Key"); !serverKeyShape.MatchString(k) {
		t.Errorf("Idempotency-Key %q does not match server shape", k)
	}
	// protojson output is not byte-stable; assert the decoded shape.
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("request body not valid JSON: %v (%s)", err, gotBody)
	}
	if len(body) != 2 || body["content"] != "hello" || body["source"] != "web" {
		t.Errorf("request body = %s, want {content:hello, source:web}", gotBody)
	}
}

func TestIngestErrorNotRetried(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{http.StatusUnauthorized, CodeUnauthorized},
		{http.StatusForbidden, CodeForbidden},
		{http.StatusConflict, CodeIdempotencyConflict},
		{http.StatusRequestEntityTooLarge, CodeBodyTooLarge},
	} {
		var hits int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits++
			w.WriteHeader(tc.status)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": tc.code, "message": "nope", "requestId": "rq"})
		}))
		c, _ := newTestClient(t, srv)
		_, err := c.Ingest(context.Background(), IngestInput{Content: "x"})
		srv.Close()

		ae, ok := err.(*AttuneError)
		if !ok {
			t.Fatalf("status %d: error type = %T, want *AttuneError", tc.status, err)
		}
		if ae.Code != tc.code || ae.Status != tc.status || ae.RequestID != "rq" {
			t.Errorf("status %d: got %+v", tc.status, ae)
		}
		if hits != 1 {
			t.Errorf("status %d: server hit %d times, want 1 (no retry)", tc.status, hits)
		}
	}
}

func TestIngestRetriesThenSucceeds(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "7", "enrichmentStatus": "pending"})
	}))
	defer srv.Close()

	c, slept := newTestClient(t, srv)
	res, err := c.Ingest(context.Background(), IngestInput{Content: "x"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.ID != "7" {
		t.Errorf("id = %s, want 7", res.ID)
	}
	if hits != 3 {
		t.Errorf("hits = %d, want 3", hits)
	}
	wantDelays := []time.Duration{200 * time.Millisecond, 400 * time.Millisecond}
	if len(*slept) != 2 || (*slept)[0] != wantDelays[0] || (*slept)[1] != wantDelays[1] {
		t.Errorf("slept = %v, want %v", *slept, wantDelays)
	}
}

func TestIngestRetriesExhausted(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Ingest(context.Background(), IngestInput{Content: "x"})
	ae, ok := err.(*AttuneError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ae.Status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", ae.Status)
	}
	if hits != 3 { // maxRetries default 2 => 3 attempts total
		t.Errorf("hits = %d, want 3", hits)
	}
}

func TestWithMaxRetriesNegativeClampsToZero(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv, WithMaxRetries(-5))
	if _, err := c.Ingest(context.Background(), IngestInput{Content: "x"}); err == nil {
		t.Fatal("expected error")
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1 (negative maxRetries must clamp to 0, not stay at default)", hits)
	}
}

func TestAttuneErrorCarriesResponseHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "rq-99")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "VALIDATION", "message": "bad"})
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Ingest(context.Background(), IngestInput{Content: "x"})
	ae, ok := err.(*AttuneError)
	if !ok {
		t.Fatalf("err type = %T", err)
	}
	if ae.Headers == nil || ae.Headers.Get("X-Request-Id") != "rq-99" {
		t.Errorf("AttuneError.Headers did not carry the response headers: %v", ae.Headers)
	}
}

func TestIngestHonorsRetryAfter(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "9", "enrichmentStatus": "pending"})
	}))
	defer srv.Close()

	c, slept := newTestClient(t, srv)
	if _, err := c.Ingest(context.Background(), IngestInput{Content: "x"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(*slept) != 1 || (*slept)[0] != 7*time.Second {
		t.Errorf("slept = %v, want [7s] from Retry-After", *slept)
	}
}

func TestIngestIdempotencyKeyStableAcrossRetries(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		n := len(keys)
		mu.Unlock()
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "1", "enrichmentStatus": "pending"})
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.Ingest(context.Background(), IngestInput{Content: "x"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("keys = %v", keys)
	}
	for _, k := range keys {
		if k != keys[0] {
			t.Errorf("idempotency key changed across retries: %v", keys)
		}
	}
}

func TestIngestWithIdempotencyKeyOverride(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "1", "enrichmentStatus": "pending"})
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.Ingest(context.Background(), IngestInput{Content: "x"}, WithIdempotencyKey("my-stable-key-123")); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if gotKey != "my-stable-key-123" {
		t.Errorf("Idempotency-Key = %q, want my-stable-key-123", gotKey)
	}
}

func TestIngestRejectsCRLFIdempotencyKey(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Ingest(context.Background(), IngestInput{Content: "x"}, WithIdempotencyKey("bad\r\nkey"))
	ae, ok := err.(*AttuneError)
	if !ok || ae.Code != CodeBadRequest {
		t.Fatalf("err = %v, want BAD_REQUEST", err)
	}
	if hits != 0 {
		t.Errorf("server was hit %d times, want 0 (client-side rejection)", hits)
	}
}

func TestWithDefaultHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "1", "enrichmentStatus": "pending"})
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv, WithDefaultHeaders(map[string]string{
		"X-Trace-Id": "trace-123",
		"X-API-Key":  "attacker-override", // reserved header: must NOT win
	}))
	if _, err := c.Ingest(context.Background(), IngestInput{Content: "x"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got.Get("X-Trace-Id") != "trace-123" {
		t.Errorf("custom default header not sent: %q", got.Get("X-Trace-Id"))
	}
	if got.Get("X-API-Key") != "att_sk_test" {
		t.Errorf("reserved X-API-Key was overridden by defaultHeaders: %q", got.Get("X-API-Key"))
	}
}

func TestWithDefaultHeadersCopiesMap(t *testing.T) {
	srv := httptest.NewServer(okHandler(t))
	defer srv.Close()
	m := map[string]string{"X-Trace-Id": "v1"}
	c, _ := newTestClient(t, srv, WithDefaultHeaders(m))
	m["X-Trace-Id"] = "mutated" // mutating the caller's map must not affect the client
	var got http.Header
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "1", "enrichmentStatus": "pending"})
	})
	if _, err := c.Ingest(context.Background(), IngestInput{Content: "x"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got.Get("X-Trace-Id") != "v1" {
		t.Errorf("default headers not copied: got %q, want v1", got.Get("X-Trace-Id"))
	}
}

func TestWithHTTPClientDoesNotMutateCaller(t *testing.T) {
	// A caller-supplied client with no CheckRedirect must not be mutated by New
	// (it may be shared elsewhere, e.g. http.DefaultClient).
	hc := &http.Client{}
	if _, err := New("https://x.example", "k", WithHTTPClient(hc)); err != nil {
		t.Fatalf("New: %v", err)
	}
	if hc.CheckRedirect != nil {
		t.Error("New mutated the caller's http.Client.CheckRedirect")
	}
}

func TestWithHTTPClientStillRefusesRedirect(t *testing.T) {
	// Even with a caller-supplied client, the no-redirect guard is enforced
	// (on the SDK's internal copy), so X-API-Key can't leak to a 3xx target.
	var targetHit bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHit = true
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "evil", "enrichmentStatus": "pending"})
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/feedback/ingest", http.StatusFound)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "att_sk_test", WithHTTPClient(&http.Client{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.rand = func() float64 { return 0.5 }
	c.sleep = func(_ context.Context, _ time.Duration) error { return nil }
	if _, err := c.Ingest(context.Background(), IngestInput{Content: "x"}); err == nil {
		t.Fatal("expected error on 3xx with caller-supplied client")
	}
	if targetHit {
		t.Error("redirect followed despite caller-supplied client — key would leak")
	}
}

func TestIngestDoesNotFollowRedirect(t *testing.T) {
	var targetHit bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHit = true
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "evil", "enrichmentStatus": "pending"})
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/feedback/ingest", http.StatusFound)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Ingest(context.Background(), IngestInput{Content: "x"})
	if err == nil {
		t.Fatal("expected error on 3xx, got nil")
	}
	if targetHit {
		t.Error("redirect was followed — X-API-Key would have leaked to the target host")
	}
}

func TestIngestResponseBodyCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A 200 whose body exceeds the 1 MiB cap: the truncated read can't parse,
		// proving the cap is active (the client never buffers the whole body).
		_, _ = w.Write([]byte(`{"id":"1","enrichmentStatus":"pending","pad":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", 2<<20)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Ingest(context.Background(), IngestInput{Content: "x"})
	ae, ok := err.(*AttuneError)
	if !ok || ae.Code != CodeInternal {
		t.Fatalf("err = %v, want *AttuneError INTERNAL on oversized body", err)
	}
	if !strings.Contains(ae.Message, "cap") {
		t.Errorf("message = %q, want it to mention the response-size cap", ae.Message)
	}
}

func TestIngestPerAttemptTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "1", "enrichmentStatus": "pending"})
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv, WithTimeout(30*time.Millisecond), WithMaxRetries(0))
	c.rand = func() float64 { return 0.5 }
	c.sleep = func(_ context.Context, _ time.Duration) error { return nil }
	_, err := c.Ingest(context.Background(), IngestInput{Content: "x"})
	ae, ok := err.(*AttuneError)
	if !ok || ae.Code != CodeTimeout {
		t.Fatalf("err = %v, want TIMEOUT", err)
	}
}

func TestIngestCallerCancel(t *testing.T) {
	srv := httptest.NewServer(okHandler(t))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Ingest(ctx, IngestInput{Content: "x"})
	ae, ok := err.(*AttuneError)
	if !ok || ae.Code != CodeAborted {
		t.Fatalf("err = %v, want ABORTED", err)
	}
}
