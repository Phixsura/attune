// SPDX-License-Identifier: Apache-2.0

package cors

import (
	"net/http"
	"net/http/httptest"
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
}
