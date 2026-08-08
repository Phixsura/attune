// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/Phixsura/attune/internal/handlers/portal"
	"github.com/Phixsura/attune/internal/handlers/surveywebhook"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	surveyrepo "github.com/Phixsura/attune/internal/repo/survey"
	surveysvc "github.com/Phixsura/attune/internal/service/survey"
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

func TestPortalAnonymousLimiter_LimitsPerTenantAndClientIP(t *testing.T) {
	t.Parallel()

	limiter := newPortalAnonymousLimiter(60, 1, false, 0)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	first := portalLimiterRequest(limiter, next, "203.0.113.10:1234", "acme")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}

	second := portalLimiterRequest(limiter, next, "203.0.113.10:5678", "acme")
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

	otherTenant := portalLimiterRequest(limiter, next, "203.0.113.10:1234", "globex")
	if otherTenant.Code != http.StatusOK {
		t.Fatalf("other tenant status = %d, want %d", otherTenant.Code, http.StatusOK)
	}

	otherClient := portalLimiterRequest(limiter, next, "203.0.113.11:1234", "acme")
	if otherClient.Code != http.StatusOK {
		t.Fatalf("other client status = %d, want %d", otherClient.Code, http.StatusOK)
	}
}

func TestPortalTenantClientRateKeyHashesClientIP(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests/pricing", nil)
	req.RemoteAddr = "203.0.113.77:1234"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_slug", "acme")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	got := portalTenantClientRateKey(req, 0)
	if !strings.HasPrefix(got, "tenant:acme|client:") {
		t.Fatalf("key = %q, want tenant/client hash prefix", got)
	}
	if strings.Contains(got, "203.0.113.77") {
		t.Fatalf("key leaked raw client IP: %q", got)
	}
}

func TestPortalSurveyTokenLimiter_LimitsPerTokenAcrossClients(t *testing.T) {
	t.Parallel()

	limiter := newPortalSurveyTokenLimiter(60, 1, false)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	first := portalSurveyLimiterRequest(limiter, next, http.MethodPost, "/surveys/token-secret/responses", "203.0.113.10:1234", "token-secret")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}

	sameTokenOtherClient := portalSurveyLimiterRequest(limiter, next, http.MethodPost, "/surveys/token-secret/responses", "203.0.113.11:5678", "token-secret")
	if sameTokenOtherClient.Code != http.StatusTooManyRequests {
		t.Fatalf("same token status = %d, want %d", sameTokenOtherClient.Code, http.StatusTooManyRequests)
	}

	otherToken := portalSurveyLimiterRequest(limiter, next, http.MethodPost, "/surveys/other-token/responses", "203.0.113.10:1234", "other-token")
	if otherToken.Code != http.StatusOK {
		t.Fatalf("other token status = %d, want %d", otherToken.Code, http.StatusOK)
	}
}

func TestPortalSurveyTokenKeyAndLogPathRedactRawToken(t *testing.T) {
	t.Parallel()

	const rawToken = "top-secret-survey-token"
	req := httptest.NewRequest(http.MethodGet, "/surveys/"+rawToken+"?score=5", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", rawToken)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	key := portalSurveyTokenRateKey(req)
	if !strings.HasPrefix(key, "survey-token:") {
		t.Fatalf("key = %q, want survey token hash prefix", key)
	}
	if strings.Contains(key, rawToken) {
		t.Fatalf("key leaked raw survey token: %q", key)
	}

	path := portalSafePathForLog(req)
	if strings.Contains(path, rawToken) {
		t.Fatalf("log path leaked raw survey token: %q", path)
	}
	if path != "/surveys/<token>" {
		t.Fatalf("log path = %q, want redacted survey path", path)
	}
}

func TestPortalSurveyProviderEventRateKeyHashesSender(t *testing.T) {
	t.Parallel()

	senderID := uuid.NewString()
	req := httptest.NewRequest(http.MethodPost, "/v1/surveys/provider-events/tenant-1/"+senderID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant-1")
	rctx.URLParams.Add("sender_id", senderID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	got := portalSurveyProviderEventRateKey(req)
	if !strings.HasPrefix(got, "survey-provider-event:tenant:") {
		t.Fatalf("key = %q, want survey provider event hash prefix", got)
	}
	if strings.Contains(got, "tenant-1") || strings.Contains(got, senderID) {
		t.Fatalf("key leaked raw route identifiers: %q", got)
	}
}

func TestPortalNoStoreWrapsRateLimitRejections(t *testing.T) {
	t.Parallel()

	limiter := newPortalAnonymousLimiter(60, 1, false, 0)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := portal.NoStore(limiter.Middleware(next))

	first := portalNoStoreLimiterRequest(handler, "203.0.113.20:1234", "acme")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}

	second := portalNoStoreLimiterRequest(handler, "203.0.113.20:5678", "acme")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if second.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", second.Header().Get("Cache-Control"))
	}
}

func TestPortalLimiterKeyFallbacks(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/v1/portal/acme/requests", nil)
	req.RemoteAddr = "203.0.113.77:1234"

	var nilLimiter *portalAnonymousLimiter
	if got := nilLimiter.key(req); got != "" {
		t.Fatalf("nil limiter key = %q, want empty", got)
	}

	limiter := newPortalLimiter(60, 1, false, 0, nil)
	if got := limiter.key(req); got != "203.0.113.77" {
		t.Fatalf("fallback key = %q, want client IP", got)
	}
}

func TestPortalLimiterRetryAfterFallbacks(t *testing.T) {
	t.Parallel()

	limiter := newPortalAnonymousLimiter(60, 1, false, 0)
	if got := limiter.retryAfterSeconds(rate.NewLimiter(0, 0)); got != 1 {
		t.Fatalf("retry after without reservation = %d, want 1", got)
	}
	if got := limiter.retryAfterSeconds(rate.NewLimiter(rate.Inf, 1)); got != 1 {
		t.Fatalf("retry after for zero delay = %d, want 1", got)
	}
}

func TestPortalLimiterCleanupEvictsIdleClients(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	limiter := newPortalAnonymousLimiter(60, 1, false, 0)
	limiter.nowFunc = func() time.Time { return now }
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	first := portalLimiterRequest(limiter, next, "203.0.113.10:1234", "acme")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}
	if got := portalLimiterClientCount(limiter); got != 1 {
		t.Fatalf("client buckets = %d, want 1", got)
	}

	now = now.Add(11 * time.Minute)
	limiter.cleanup(10 * time.Minute)
	if got := portalLimiterClientCount(limiter); got != 0 {
		t.Fatalf("client buckets after cleanup = %d, want 0", got)
	}
}

func TestMountV1PortalRoutesSeparatesReadAndWriteBuckets(t *testing.T) {
	t.Parallel()

	readLimiter := newPortalAnonymousLimiter(60, 1, false, 0)
	writeLimiter := newPortalSubmissionLimiter(60, 1, false, 0)
	surveyReadLimiter := newPortalSurveyTokenLimiter(60, 1, false)
	surveyWriteLimiter := newPortalSurveyTokenLimiter(60, 1, false)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		mountV1PortalRoutes(
			r,
			portal.NewHandler(nil, nil, nil),
			func(next http.Handler) http.Handler { return next },
			readLimiter,
			writeLimiter,
			surveyReadLimiter,
			surveyWriteLimiter,
		)
	})

	readFirst := portalRouteRequest(r, http.MethodGet, "/v1/portal/acme/requests", "203.0.113.30:1234")
	if readFirst.Code != http.StatusNotImplemented {
		t.Fatalf("first read status = %d, want %d", readFirst.Code, http.StatusNotImplemented)
	}

	writeAllowed := portalRouteRequest(r, http.MethodPost, "/v1/portal/acme/requests/pricing/votes", "203.0.113.30:5678")
	if writeAllowed.Code != http.StatusNotImplemented {
		t.Fatalf("write status = %d, want %d", writeAllowed.Code, http.StatusNotImplemented)
	}

	readSecond := portalRouteRequest(r, http.MethodGet, "/v1/portal/acme/requests", "203.0.113.30:9999")
	if readSecond.Code != http.StatusTooManyRequests {
		t.Fatalf("second read status = %d, want %d", readSecond.Code, http.StatusTooManyRequests)
	}
}

func TestMountV1PortalRoutesAppliesSurveyTokenLimiter(t *testing.T) {
	t.Parallel()

	readLimiter := newPortalAnonymousLimiter(60, 100, false, 0)
	writeLimiter := newPortalSubmissionLimiter(60, 100, false, 0)
	surveyReadLimiter := newPortalSurveyTokenLimiter(60, 1, false)
	surveyWriteLimiter := newPortalSurveyTokenLimiter(60, 1, false)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		mountV1PortalRoutes(
			r,
			portal.NewHandler(nil, nil, nil),
			func(next http.Handler) http.Handler { return next },
			readLimiter,
			writeLimiter,
			surveyReadLimiter,
			surveyWriteLimiter,
		)
	})

	first := portalRouteRequest(r, http.MethodGet, "/v1/surveys/token-1", "203.0.113.10:1234")
	if first.Code != http.StatusNotImplemented {
		t.Fatalf("first survey read status = %d, want %d", first.Code, http.StatusNotImplemented)
	}

	sameTokenOtherClient := portalRouteRequest(r, http.MethodGet, "/v1/surveys/token-1", "203.0.113.11:1234")
	if sameTokenOtherClient.Code != http.StatusTooManyRequests {
		t.Fatalf("same token survey read status = %d, want %d", sameTokenOtherClient.Code, http.StatusTooManyRequests)
	}

	otherToken := portalRouteRequest(r, http.MethodGet, "/v1/surveys/token-2", "203.0.113.10:1234")
	if otherToken.Code != http.StatusNotImplemented {
		t.Fatalf("other token survey read status = %d, want %d", otherToken.Code, http.StatusNotImplemented)
	}
}

func TestMountV1SurveyProviderWebhookRoutesAppliesSenderLimiter(t *testing.T) {
	t.Parallel()

	writeLimiter := newPortalSubmissionLimiter(60, 100, false, 0)
	providerLimiter := newPortalSurveyProviderEventLimiter(60, 1, false)
	service := ptrext.Of(routeSurveyWebhookService{})
	handler := surveywebhook.NewHandler(service)
	r := chi.NewRouter()
	mountV1SurveyProviderWebhookRoutes(r, handler, writeLimiter, providerLimiter)
	senderID := uuid.New()

	first := surveyProviderWebhookRouteRequest(r, "/surveys/provider-events/tenant-1/"+senderID.String(), "203.0.113.10:1234")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first provider event status = %d body = %s; want %d", first.Code, first.Body.String(), http.StatusAccepted)
	}

	sameSenderOtherClient := surveyProviderWebhookRouteRequest(r, "/surveys/provider-events/tenant-1/"+senderID.String(), "203.0.113.11:1234")
	if sameSenderOtherClient.Code != http.StatusTooManyRequests {
		t.Fatalf("same sender provider event status = %d, want %d", sameSenderOtherClient.Code, http.StatusTooManyRequests)
	}

	otherSender := surveyProviderWebhookRouteRequest(r, "/surveys/provider-events/tenant-1/"+uuid.NewString(), "203.0.113.10:1234")
	if otherSender.Code != http.StatusAccepted {
		t.Fatalf("other sender provider event status = %d body = %s; want %d", otherSender.Code, otherSender.Body.String(), http.StatusAccepted)
	}
}

func TestPortalSubmissionLimiter_LimitsPortalWritesPerTenantAndIP(t *testing.T) {
	t.Parallel()

	limiter := newPortalSubmissionLimiter(60, 1, false, 0)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	first := portalWriteLimiterRequest(limiter, next, http.MethodPost, "/v1/portal/acme/requests/pricing/votes", "203.0.113.10:1234", "acme")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}

	sameTenant := portalWriteLimiterRequest(limiter, next, http.MethodPost, "/v1/portal/acme/requests/pricing/comments", "203.0.113.10:5678", "acme")
	if sameTenant.Code != http.StatusTooManyRequests {
		t.Fatalf("same tenant write status = %d, want %d", sameTenant.Code, http.StatusTooManyRequests)
	}

	if sameTenant.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", sameTenant.Header().Get("Retry-After"))
	}

	otherTenant := portalWriteLimiterRequest(limiter, next, http.MethodPost, "/v1/portal/globex/submissions", "203.0.113.10:1234", "globex")
	if otherTenant.Code != http.StatusOK {
		t.Fatalf("other tenant status = %d, want %d", otherTenant.Code, http.StatusOK)
	}
}

func portalLimiterRequest(
	limiter *portalAnonymousLimiter,
	next http.Handler,
	remoteAddr string,
	tenantSlug string,
) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/"+tenantSlug+"/requests/pricing", nil)
	req.RemoteAddr = remoteAddr
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_slug", tenantSlug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	limiter.Middleware(next).ServeHTTP(w, req)
	return w
}

func portalNoStoreLimiterRequest(handler http.Handler, remoteAddr string, tenantSlug string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/portal/"+tenantSlug+"/requests/pricing", nil)
	req.RemoteAddr = remoteAddr
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_slug", tenantSlug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	handler.ServeHTTP(w, req)
	return w
}

func portalSurveyLimiterRequest(
	limiter *portalAnonymousLimiter,
	next http.Handler,
	method string,
	path string,
	remoteAddr string,
	token string,
) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remoteAddr
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	limiter.Middleware(next).ServeHTTP(w, req)
	return w
}

func portalWriteLimiterRequest(
	limiter *portalAnonymousLimiter,
	next http.Handler,
	method string,
	path string,
	remoteAddr string,
	tenantSlug string,
) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remoteAddr
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_slug", tenantSlug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	limiter.Middleware(next).ServeHTTP(w, req)
	return w
}

func portalRouteRequest(r http.Handler, method, path, remoteAddr string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remoteAddr
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_slug", "acme")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	r.ServeHTTP(w, req)
	return w
}

func surveyProviderWebhookRouteRequest(r http.Handler, path string, remoteAddr string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.RemoteAddr = remoteAddr
	r.ServeHTTP(w, req)
	return w
}

func portalLimiterClientCount(limiter *portalAnonymousLimiter) int {
	limiter.mu.RLock()
	defer limiter.mu.RUnlock()
	return len(limiter.clients)
}

type routeSurveyWebhookService struct{}

func (routeSurveyWebhookService) RecordSignedProviderEvent(context.Context, surveysvc.SignedProviderEventInput) (surveyrepo.Invitation, error) {
	return surveyrepo.Invitation{ID: uuid.New()}, nil
}
