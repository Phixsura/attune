// SPDX-License-Identifier: Apache-2.0

package requestvalidation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestContentTypeJSON_Valid(t *testing.T) {
	t.Parallel()
	handler := ContentTypeJSON(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestContentTypeJSON_Invalid(t *testing.T) {
	t.Parallel()
	handler := ContentTypeJSON(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("data"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

func TestContentTypeJSON_GET(t *testing.T) {
	t.Parallel()
	handler := ContentTypeJSON(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireContentLength_Present(t *testing.T) {
	t.Parallel()
	handler := RequireContentLength(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("body"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestMaxContentLength_UnderLimit(t *testing.T) {
	t.Parallel()
	handler := MaxContentLength(1000)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestMaxContentLength_OverLimit(t *testing.T) {
	t.Parallel()
	handler := MaxContentLength(10)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("this is a longer body"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestRequireHeaders_Present(t *testing.T) {
	t.Parallel()
	handler := RequireHeaders("X-API-Key", "X-Request-ID")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "key")
	req.Header.Set("X-Request-ID", "123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireHeaders_Missing(t *testing.T) {
	t.Parallel()
	handler := RequireHeaders("X-API-Key")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "X-API-Key")
}

func TestAcceptJSON_Valid(t *testing.T) {
	t.Parallel()
	handler := AcceptJSON(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAcceptJSON_Wildcard(t *testing.T) {
	t.Parallel()
	handler := AcceptJSON(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "*/*")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAcceptJSON_Invalid(t *testing.T) {
	t.Parallel()
	handler := AcceptJSON(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotAcceptable, rec.Code)
}

func TestMethods_Allowed(t *testing.T) {
	t.Parallel()
	handler := Methods("GET", "POST")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestMethods_NotAllowed(t *testing.T) {
	t.Parallel()
	handler := Methods("GET", "POST")(okHandler())

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Contains(t, rec.Header().Get("Allow"), "GET")
}
