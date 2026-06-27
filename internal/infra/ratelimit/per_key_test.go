package ratelimit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// rpmVerifier returns a FIXED key id (so the per-key bucket is shared across
// requests) and a configurable rate_limit_rpm.
type rpmVerifier struct {
	tenantID string
	keyID    uuid.UUID
	rpm      *int
}

func (v rpmVerifier) Lookup(context.Context, string) (string, uuid.UUID, error) {
	return v.tenantID, v.keyID, nil
}

func (v rpmVerifier) LookupWithScopes(context.Context, string) (string, uuid.UUID, []domain.Scope, error) {
	return v.tenantID, v.keyID, domain.AllScopes, nil
}

func (v rpmVerifier) LookupWithScopesAndIP(context.Context, string, string) (string, uuid.UUID, []domain.Scope, *int, error) {
	return v.tenantID, v.keyID, domain.AllScopes, v.rpm, nil
}

func TestPerKeyBurst_NegativeRPM(t *testing.T) {
	t.Parallel()
	if perKeyBurst(-1) != 1 {
		t.Fatal("negative rpm should clamp to 1")
	}
	if perKeyBurst(0) != 1 {
		t.Fatal("zero rpm should clamp to 1")
	}
	if perKeyBurst(5) != 5 {
		t.Fatal("positive rpm should be returned as-is")
	}
}

func TestPerKey_RetryAfterSeconds(t *testing.T) {
	t.Parallel()
	l := NewPerKey(false, nil)
	k := uuid.New()

	if l.retryAfterSeconds(k, 0) != 0 {
		t.Fatal("rpm=0 should return 0")
	}
	if l.retryAfterSeconds(uuid.Nil, 5) != 0 {
		t.Fatal("nil key should return 0")
	}

	l.Allow(k, 1, "t")
	l.Allow(k, 1, "t")
	got := l.retryAfterSeconds(k, 1)
	if got < 1 {
		t.Fatalf("should have positive retry-after, got %d", got)
	}
}

func TestPerKey_RetryAfterSeconds_Disabled(t *testing.T) {
	t.Parallel()
	l := NewPerKey(true, nil)
	if l.retryAfterSeconds(uuid.New(), 5) != 0 {
		t.Fatal("disabled limiter should return 0")
	}
}

func TestPerKey_Allow(t *testing.T) {
	l := NewPerKey(false, nil)
	k := uuid.New()
	// rpm=2 → burst 2: two pass, third in the same instant is limited.
	if !l.Allow(k, 2, "t1") {
		t.Fatal("first within burst should pass")
	}
	if !l.Allow(k, 2, "t1") {
		t.Fatal("second within burst should pass")
	}
	if l.Allow(k, 2, "t1") {
		t.Fatal("third should be limited")
	}
	// A different key has its own bucket.
	if !l.Allow(uuid.New(), 2, "t1") {
		t.Fatal("distinct key should pass")
	}
	// rpm<=0 or nil key → no per-key limit.
	other := uuid.New()
	for i := 0; i < 100; i++ {
		if !l.Allow(other, 0, "t1") {
			t.Fatal("rpm<=0 should bypass")
		}
	}
	if !l.Allow(uuid.Nil, 5, "t1") {
		t.Fatal("nil key should bypass")
	}
}

func TestPerKey_Disabled(t *testing.T) {
	l := NewPerKey(true, nil)
	k := uuid.New()
	for i := 0; i < 1000; i++ {
		if !l.Allow(k, 1, "t1") {
			t.Fatal("disabled limiter allows everything")
		}
	}
}

func TestPerKey_OnLimitHook(t *testing.T) {
	var hits []string
	l := NewPerKey(false, func(tid string) { hits = append(hits, tid) })
	k := uuid.New()
	l.Allow(k, 1, "t1") // ok
	l.Allow(k, 1, "t1") // limited
	l.Allow(k, 1, "t1") // limited
	if len(hits) != 2 || hits[0] != "t1" {
		t.Fatalf("expected 2 hits for t1, got %v", hits)
	}
}

func TestPerKeyMiddleware_NoRPMPasses(t *testing.T) {
	l := NewPerKey(false, nil)
	v := rpmVerifier{tenantID: "t1", keyID: uuid.New(), rpm: nil} // no per-key limit
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	h := apikey.Middleware(v)(l.Middleware(next))
	for i := 0; i < 50; i++ {
		r := httptest.NewRequest(http.MethodPost, "/v1/feedback/ingest", nil)
		r.Header.Set("X-API-Key", domain.APIKeyPrefix+"test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Result().StatusCode != http.StatusOK {
			t.Fatalf("req %d: status = %d, want 200 (no rpm → unlimited)", i, w.Result().StatusCode)
		}
	}
	if called != 50 {
		t.Fatalf("handler ran %d times, want 50", called)
	}
}

func TestPerKeyMiddleware_Enforces(t *testing.T) {
	l := NewPerKey(false, nil)
	v := rpmVerifier{tenantID: "t1", keyID: uuid.New(), rpm: ptrext.Of(1)} // 1 rpm, burst 1
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := apikey.Middleware(v)(l.Middleware(next))

	do := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/feedback/ingest", nil)
		r.Header.Set("X-API-Key", domain.APIKeyPrefix+"test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if got := do().Result().StatusCode; got != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", got)
	}
	w := do() // second within the same instant exceeds burst 1
	if w.Result().StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", w.Result().StatusCode)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After on per-key 429")
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, w.Body.String())
	}
	if body["code"] != "RATE_LIMITED" {
		t.Fatalf("code = %v, want RATE_LIMITED; body=%s", body["code"], w.Body.String())
	}
}
