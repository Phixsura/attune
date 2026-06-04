package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllow_Burst(t *testing.T) {
	l := New(60, 5, false, nil)
	// First 5 are within burst, allowed.
	for i := 0; i < 5; i++ {
		if !l.Allow("t1") {
			t.Fatalf("call %d unexpectedly denied", i+1)
		}
	}
	// 6th in same instant exceeds burst.
	if l.Allow("t1") {
		t.Fatal("6th call should have been rate-limited")
	}
}

func TestAllow_PerTenant(t *testing.T) {
	l := New(60, 2, false, nil)
	l.Allow("a")
	l.Allow("a")
	if l.Allow("a") {
		t.Fatal("tenant a should be limited")
	}
	// Different tenant has its own bucket.
	if !l.Allow("b") {
		t.Fatal("tenant b should still be allowed")
	}
}

func TestAllow_Disabled(t *testing.T) {
	l := New(60, 1, true, nil)
	for i := 0; i < 1000; i++ {
		if !l.Allow("any") {
			t.Fatal("disabled limiter should let everything through")
		}
	}
}

func TestAllow_EmptyTenant(t *testing.T) {
	l := New(60, 1, false, nil)
	// No tenant id (anonymous req) — bypass; api-key middleware will 401.
	if !l.Allow("") {
		t.Fatal("empty tenant should bypass (api-key middleware handles auth)")
	}
}

func TestAllow_OnLimitHook(t *testing.T) {
	var hits []string
	l := New(60, 1, false, func(tid string) { hits = append(hits, tid) })
	l.Allow("t1") // ok
	l.Allow("t1") // limited
	l.Allow("t1") // limited
	if len(hits) != 2 {
		t.Fatalf("expected 2 on-limit hits, got %d (%v)", len(hits), hits)
	}
	for _, h := range hits {
		if h != "t1" {
			t.Fatalf("expected hit for t1, got %q", h)
		}
	}
}

func TestMiddleware_BypassEmptyTenant(t *testing.T) {
	l := New(60, 1, false, nil)
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	r := httptest.NewRequest(http.MethodPost, "/v1/feedback/ingest", nil)
	w := httptest.NewRecorder()
	l.Middleware(next).ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("empty-tenant request should 200, got %d", w.Result().StatusCode)
	}
	if called != 1 {
		t.Fatalf("next handler should have run once")
	}
}
