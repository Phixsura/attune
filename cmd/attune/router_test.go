package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// TestMountHealth is a contract test for the liveness probe. The deploy kit
// (docker-compose) and external uptime checks probe /healthz, which must return
// 200 "ok". /health was removed in 0.2.0 (BREAKING) and must now 404 — asserting
// that here locks in the removal so a future re-add is a conscious change.
func TestMountHealth(t *testing.T) {
	r := chi.NewRouter()
	mountHealth(r, readyFunc(func(context.Context) error { return nil }))

	t.Run("/healthz returns 200 ok", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body, err := io.ReadAll(rec.Result().Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != "ok" {
			t.Fatalf("body = %q, want %q", got, "ok")
		}
	})

	t.Run("/health is gone (404)", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d (404)", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("/readyz returns 200 ok when database ping passes", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body, err := io.ReadAll(rec.Result().Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != "ok" {
			t.Fatalf("body = %q, want %q", got, "ok")
		}
	})

	t.Run("/readyz returns 503 when database ping fails", func(t *testing.T) {
		failRouter := chi.NewRouter()
		mountHealth(failRouter, readyFunc(func(context.Context) error {
			return errors.New("database unavailable")
		}))
		rec := httptest.NewRecorder()
		failRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("/readyz returns 503 while draining", func(t *testing.T) {
		draining := newDrainAwareReadiness(readyFunc(func(context.Context) error {
			return nil
		}))
		draining.BeginDrain()

		drainRouter := chi.NewRouter()
		mountHealth(drainRouter, draining)
		rec := httptest.NewRecorder()
		drainRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}

type readyFunc func(context.Context) error

func (f readyFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

func TestBuildRouterMinimalServerSurface(t *testing.T) {
	cfg := ptrext.Of(config.Config{
		RateLimitDisabled:  true,
		RateLimitPerMinute: 60,
		RateLimitBurst:     1,
	})
	ingestHandler := handlers.NewIngestHandler(nil, domain.DefaultSourceSet())

	r, err := buildRouter(
		context.Background(),
		cfg,
		ingestHandler,
		nil,
		nil,
		readyFunc(func(context.Context) error { return nil }),
		nil,
		chi.NewRouter(),
		nil,
		nil,
		nil,
		nil,
		nil,
		domain.DefaultSourceSet(),
		nil, // cohortSyncSvc
	)
	if err != nil {
		t.Fatalf("buildRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d, want %d", rec.Code, http.StatusOK)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want %d", rec.Code, http.StatusOK)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/mcp status = %d, want %d when MCP is disabled", rec.Code, http.StatusNotFound)
	}
}
