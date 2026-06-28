package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/apiversion"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type recordingVerifier struct {
	called bool
	scopes []domain.Scope
}

func (v *recordingVerifier) Lookup(context.Context, string) (string, uuid.UUID, error) {
	v.called = true
	return "tenant-1", uuid.MustParse("11111111-1111-1111-1111-111111111111"), nil
}

func (v *recordingVerifier) LookupWithScopes(context.Context, string) (string, uuid.UUID, []domain.Scope, error) {
	v.called = true
	return "tenant-1", uuid.MustParse("11111111-1111-1111-1111-111111111111"), v.scopes, nil
}

func (v *recordingVerifier) LookupWithScopesAndIP(context.Context, string, string) (string, uuid.UUID, []domain.Scope, *int, error) {
	v.called = true
	return "tenant-1", uuid.MustParse("11111111-1111-1111-1111-111111111111"), v.scopes, nil, nil
}

func TestBuildPublicIngestCORSConfig_DisabledByDefault(t *testing.T) {
	t.Parallel()

	cfg, ok := buildPublicIngestCORSConfig(ptrext.Of(config.Config{}))
	require.False(t, ok)
	require.Empty(t, cfg.AllowedOrigins)
}

func TestBuildPublicIngestCORSConfig_UsesNormalizedOrigins(t *testing.T) {
	t.Parallel()

	cfg, ok := buildPublicIngestCORSConfig(ptrext.Of(config.Config{
		IngestCORSAllowedOrigins: []string{"https://app.example.com", "http://localhost:5173"},
	}))
	require.True(t, ok)
	require.Equal(t, []string{"https://app.example.com", "http://localhost:5173"}, cfg.AllowedOrigins)
	require.Contains(t, cfg.AllowedHeaders, "X-API-Key")
	require.Contains(t, cfg.AllowedHeaders, "Idempotency-Key")
	require.Contains(t, cfg.AllowedHeaders, "X-Attune-Api-Version")
	require.Contains(t, cfg.ExposedHeaders, "X-Attune-Api-Version")
	require.Contains(t, cfg.ExposedHeaders, "Deprecation")
	require.Contains(t, cfg.ExposedHeaders, "Sunset")
}

func TestPublicIngestCORSPreflightBypassesAuth(t *testing.T) {
	t.Parallel()

	cfg := ptrext.Of(config.Config{IngestCORSAllowedOrigins: []string{"https://app.example.com"}})
	mw := publicIngestCORS(cfg)
	require.NotNil(t, mw)

	verifier := ptrext.Of(recordingVerifier{scopes: []domain.Scope{domain.ScopeIngestWrite}})
	router := chi.NewRouter()
	router.Use(mw)
	router.Use(apikey.Middleware(verifier))
	router.Use(apikey.RequireScope(domain.ScopeIngestWrite))
	router.Post("/v1/feedback/ingest", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/v1/feedback/ingest", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.False(t, verifier.called, "preflight must not hit api-key auth")
	require.Equal(t, "https://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "X-API-Key")
	require.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key")
	require.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "X-Attune-Api-Version")
	require.Contains(t, rec.Header().Values("Vary"), "Origin")
}

func TestPublicIngestCORSSetsHeadersOnActualRequest(t *testing.T) {
	t.Parallel()

	cfg := ptrext.Of(config.Config{IngestCORSAllowedOrigins: []string{"https://app.example.com"}})
	mw := publicIngestCORS(cfg)
	require.NotNil(t, mw)

	verifier := ptrext.Of(recordingVerifier{scopes: []domain.Scope{domain.ScopeIngestWrite}})
	router := chi.NewRouter()
	router.Use(mw)
	router.Use(apikey.Middleware(verifier))
	router.Use(apikey.RequireScope(domain.ScopeIngestWrite))
	router.Post("/v1/feedback/ingest", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/feedback/ingest", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"publishable")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.True(t, verifier.called)
	require.Equal(t, "https://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, rec.Header().Get("Access-Control-Expose-Headers"), "X-Attune-Api-Version")
	require.Contains(t, rec.Header().Get("Access-Control-Expose-Headers"), "Deprecation")
	require.Contains(t, rec.Header().Get("Access-Control-Expose-Headers"), "Sunset")
}

func TestPublicIngestCORSOmitsHeadersForDisallowedOrigin(t *testing.T) {
	t.Parallel()

	cfg := ptrext.Of(config.Config{IngestCORSAllowedOrigins: []string{"https://app.example.com"}})
	mw := publicIngestCORS(cfg)
	require.NotNil(t, mw)

	router := chi.NewRouter()
	router.Use(mw)
	router.Post("/v1/feedback/ingest", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/feedback/ingest", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestPublicIngestCORSKeepsVersionErrorsReadableInBrowser(t *testing.T) {
	t.Parallel()

	cfg := ptrext.Of(config.Config{IngestCORSAllowedOrigins: []string{"https://app.example.com"}})
	mw := publicIngestCORS(cfg)
	require.NotNil(t, mw)

	router := chi.NewRouter()
	router.Route("/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(mw)
			r.Use(apiversion.Middleware(apiversion.DefaultConfig()))
			r.Post("/feedback/ingest", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			})
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/feedback/ingest", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("X-Attune-Api-Version", "2025-01-01")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "https://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, rec.Header().Get("Access-Control-Expose-Headers"), "X-Attune-Api-Version")
	require.Contains(t, rec.Header().Get("Access-Control-Expose-Headers"), "Deprecation")
	require.Contains(t, rec.Header().Get("Access-Control-Expose-Headers"), "Sunset")
	require.Contains(t, rec.Header().Values("Vary"), "Origin")
	require.Contains(t, rec.Header().Values("Vary"), "X-Attune-Api-Version")
}
