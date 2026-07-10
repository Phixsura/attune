// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPortalAnonymousLimiter_DisabledBypasses(t *testing.T) {
	t.Parallel()

	limiter := newPortalAnonymousLimiter(1, 1, true, 0)
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	})

	for range 3 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests/pricing", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		limiter.Middleware(next).ServeHTTP(w, req)
		if w.Result().StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", w.Result().StatusCode, http.StatusNoContent)
		}
	}
	if called != 3 {
		t.Fatalf("next handler calls = %d, want 3", called)
	}
}

func TestPortalAnonymousLimiter_LimitsPerClientIP(t *testing.T) {
	t.Parallel()

	limiter := newPortalAnonymousLimiter(60, 1, false, 0)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	first := portalLimiterRequest(limiter, next, "203.0.113.10:1234")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}

	second := portalLimiterRequest(limiter, next, "203.0.113.10:5678")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if second.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", second.Header().Get("Retry-After"))
	}
	var body map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, second.Body.String())
	}
	if body["code"] != "RATE_LIMITED" {
		t.Fatalf("code = %v, want RATE_LIMITED; body=%s", body["code"], second.Body.String())
	}

	otherClient := portalLimiterRequest(limiter, next, "203.0.113.11:1234")
	if otherClient.Code != http.StatusOK {
		t.Fatalf("other client status = %d, want %d", otherClient.Code, http.StatusOK)
	}
}

func portalLimiterRequest(
	limiter *portalAnonymousLimiter,
	next http.Handler,
	remoteAddr string,
) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests/pricing", nil)
	req.RemoteAddr = remoteAddr
	limiter.Middleware(next).ServeHTTP(w, req)
	return w
}
