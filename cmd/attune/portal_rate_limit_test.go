// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/handlers/portal"
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

func TestPortalNoStoreWrapsRateLimitRejections(t *testing.T) {
	t.Parallel()

	limiter := newPortalAnonymousLimiter(60, 1, false, 0)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := portal.NoStore(limiter.Middleware(next))

	first := portalNoStoreLimiterRequest(handler, "203.0.113.20:1234")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}

	second := portalNoStoreLimiterRequest(handler, "203.0.113.20:5678")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if second.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", second.Header().Get("Cache-Control"))
	}
}

func TestPortalSubmissionLimiter_LimitsPerTenantAndIP(t *testing.T) {
	t.Parallel()

	limiter := newPortalSubmissionLimiter(60, 1, false, 0)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	first := portalSubmissionLimiterRequest(limiter, next, "203.0.113.10:1234", "acme")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}

	sameTenant := portalSubmissionLimiterRequest(limiter, next, "203.0.113.10:5678", "acme")
	if sameTenant.Code != http.StatusTooManyRequests {
		t.Fatalf("same tenant status = %d, want %d", sameTenant.Code, http.StatusTooManyRequests)
	}

	otherTenant := portalSubmissionLimiterRequest(limiter, next, "203.0.113.10:1234", "globex")
	if otherTenant.Code != http.StatusOK {
		t.Fatalf("other tenant status = %d, want %d", otherTenant.Code, http.StatusOK)
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

func portalNoStoreLimiterRequest(handler http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests/pricing", nil)
	req.RemoteAddr = remoteAddr
	handler.ServeHTTP(w, req)
	return w
}

func portalSubmissionLimiterRequest(
	limiter *portalAnonymousLimiter,
	next http.Handler,
	remoteAddr string,
	tenantSlug string,
) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/portal/"+tenantSlug+"/submissions", nil)
	req.RemoteAddr = remoteAddr
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_slug", tenantSlug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	limiter.Middleware(next).ServeHTTP(w, req)
	return w
}
