// SPDX-License-Identifier: Apache-2.0

package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteJSON(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusOK, map[string]string{"key": "value"})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var result map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &result) // ptrext:allow unmarshal-out-param
	require.NoError(t, err)
	require.Equal(t, "value", result["key"])
}

func TestWriteError(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, "invalid_input", "Bad request")

	require.Equal(t, http.StatusBadRequest, rec.Code)

	var result map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &result) // ptrext:allow unmarshal-out-param
	require.NoError(t, err)
	require.Equal(t, "invalid_input", result["error"])
	require.Equal(t, "Bad request", result["message"])
}

func TestWriteNoContent(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	WriteNoContent(rec)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestIsSuccess(t *testing.T) {
	t.Parallel()
	require.True(t, IsSuccess(200))
	require.True(t, IsSuccess(201))
	require.True(t, IsSuccess(204))
	require.False(t, IsSuccess(199))
	require.False(t, IsSuccess(300))
	require.False(t, IsSuccess(400))
}

func TestIsClientError(t *testing.T) {
	t.Parallel()
	require.True(t, IsClientError(400))
	require.True(t, IsClientError(404))
	require.True(t, IsClientError(499))
	require.False(t, IsClientError(399))
	require.False(t, IsClientError(500))
}

func TestIsServerError(t *testing.T) {
	t.Parallel()
	require.True(t, IsServerError(500))
	require.True(t, IsServerError(503))
	require.False(t, IsServerError(499))
	require.False(t, IsServerError(600))
}

func TestIsRetryable(t *testing.T) {
	t.Parallel()
	require.True(t, IsRetryable(429))
	require.True(t, IsRetryable(503))
	require.True(t, IsRetryable(504))
	require.True(t, IsRetryable(502))
	require.False(t, IsRetryable(500))
	require.False(t, IsRetryable(400))
}

func TestStatusText(t *testing.T) {
	t.Parallel()
	require.Equal(t, "OK", StatusText(200))
	require.Equal(t, "Not Found", StatusText(404))
}
