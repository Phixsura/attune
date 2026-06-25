// SPDX-License-Identifier: Apache-2.0

package problemdetails

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProblem_New(t *testing.T) {
	t.Parallel()
	p := New(400, "Bad Request")

	require.Equal(t, "about:blank", p.Type)
	require.Equal(t, "Bad Request", p.Title)
	require.Equal(t, 400, p.Status)
}

func TestProblem_WithDetail(t *testing.T) {
	t.Parallel()
	p := New(400, "Bad Request").WithDetail("Invalid email format")

	require.Equal(t, "Invalid email format", p.Detail)
}

func TestProblem_WithType(t *testing.T) {
	t.Parallel()
	p := New(400, "Bad Request").WithType("https://example.com/errors/validation")

	require.Equal(t, "https://example.com/errors/validation", p.Type)
}

func TestProblem_WithExtension(t *testing.T) {
	t.Parallel()
	p := New(400, "Bad Request").
		WithExtension("field", "email").
		WithExtension("code", "invalid_format")

	require.Equal(t, "email", p.Extensions["field"])
	require.Equal(t, "invalid_format", p.Extensions["code"])
}

func TestProblem_MarshalJSON(t *testing.T) {
	t.Parallel()
	p := New(400, "Bad Request").
		WithDetail("Invalid input").
		WithExtension("field", "email")

	data, err := json.Marshal(p)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.Equal(t, "about:blank", decoded["type"])
	require.Equal(t, "Bad Request", decoded["title"])
	require.Equal(t, float64(400), decoded["status"])
	require.Equal(t, "Invalid input", decoded["detail"])
	require.Equal(t, "email", decoded["field"])
}

func TestProblem_Write(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	BadRequest("Invalid input").Write(rec)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, ContentType, rec.Header().Get("Content-Type"))

	var p Problem
	err := json.NewDecoder(rec.Body).Decode(&p)
	require.NoError(t, err)
	require.Equal(t, "Bad Request", p.Title)
}

func TestBadRequest(t *testing.T) {
	t.Parallel()
	p := BadRequest("Invalid input")
	require.Equal(t, http.StatusBadRequest, p.Status)
	require.Equal(t, "Invalid input", p.Detail)
}

func TestUnauthorized(t *testing.T) {
	t.Parallel()
	p := Unauthorized("Token expired")
	require.Equal(t, http.StatusUnauthorized, p.Status)
}

func TestForbidden(t *testing.T) {
	t.Parallel()
	p := Forbidden("Access denied")
	require.Equal(t, http.StatusForbidden, p.Status)
}

func TestNotFound(t *testing.T) {
	t.Parallel()
	p := NotFound("Resource not found")
	require.Equal(t, http.StatusNotFound, p.Status)
}

func TestConflict(t *testing.T) {
	t.Parallel()
	p := Conflict("Resource already exists")
	require.Equal(t, http.StatusConflict, p.Status)
}

func TestTooManyRequests(t *testing.T) {
	t.Parallel()
	p := TooManyRequests("Rate limit exceeded", 60)
	require.Equal(t, http.StatusTooManyRequests, p.Status)
	require.Equal(t, 60, p.Extensions["retry_after"])
}

func TestInternalError(t *testing.T) {
	t.Parallel()
	p := InternalError("Database error")
	require.Equal(t, http.StatusInternalServerError, p.Status)
}

func TestServiceUnavailable(t *testing.T) {
	t.Parallel()
	p := ServiceUnavailable("Service is down")
	require.Equal(t, http.StatusServiceUnavailable, p.Status)
}
