// SPDX-License-Identifier: Apache-2.0

package cors

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiddleware_NoOrigin(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://example.com"}

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestMiddleware_AllowedOrigin(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://example.com"}

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.True(t, varyContains(rec.Header(), "Origin"))
}

func TestMiddleware_DisallowedOrigin(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://example.com"}

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	require.True(t, varyContains(rec.Header(), "Origin"))
}

func TestMiddleware_Wildcard(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"*"}

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://any.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestMiddleware_Preflight(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://example.com"}

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"))
	require.NotEmpty(t, rec.Header().Get("Access-Control-Max-Age"))
	require.True(t, varyContains(rec.Header(), "Origin"))
	require.True(t, varyContains(rec.Header(), "Access-Control-Request-Method"))
	require.True(t, varyContains(rec.Header(), "Access-Control-Request-Headers"))
}

func TestMiddleware_PreflightEchoesRequestedCustomHeaders(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://example.com"}

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Headers", "x-trace-id, x-sdk-flavor, X-API-Key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	allowHeaders := rec.Header().Get("Access-Control-Allow-Headers")
	require.Contains(t, allowHeaders, "X-API-Key")
	require.Contains(t, allowHeaders, "x-trace-id")
	require.Contains(t, allowHeaders, "x-sdk-flavor")
}

func TestMiddleware_PreflightIgnoresInvalidRequestedHeaderTokens(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://example.com"}

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Headers", "x-trace-id, bad header, x-bad:evil")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	allowHeaders := rec.Header().Get("Access-Control-Allow-Headers")
	require.Contains(t, allowHeaders, "x-trace-id")
	require.NotContains(t, allowHeaders, "bad header")
	require.NotContains(t, allowHeaders, "x-bad:evil")
}

func TestMiddleware_Credentials(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://example.com"}
	cfg.AllowCredentials = true

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	require.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.True(t, varyContains(rec.Header(), "Origin"))
}

func TestMiddleware_WildcardWithCredentialsReflectsOrigin(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"*"}
	cfg.AllowCredentials = true

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://any.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, "https://any.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	require.True(t, varyContains(rec.Header(), "Origin"))
}

func TestDefaultConfig_IncludesPublicAPIVersionHeaders(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	require.True(t, slices.Contains(cfg.AllowedHeaders, "X-API-Key"))
	require.True(t, slices.Contains(cfg.AllowedHeaders, "Idempotency-Key"))
	require.True(t, slices.Contains(cfg.AllowedHeaders, "X-Attune-Api-Version"))
	require.True(t, slices.Contains(cfg.ExposedHeaders, "X-Attune-Api-Version"))
	require.True(t, slices.Contains(cfg.ExposedHeaders, "Deprecation"))
	require.True(t, slices.Contains(cfg.ExposedHeaders, "Sunset"))
}

func varyContains(h http.Header, want string) bool {
	for _, value := range h.Values("Vary") {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	}
	return false
}
