// SPDX-License-Identifier: Apache-2.0

package oidc

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/pkg/crypto"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestNewHandler_NilDependencies(t *testing.T) {
	t.Parallel()

	// All nil -> nil
	h := NewHandler(nil, nil, nil, "")
	if h != nil {
		t.Error("expected nil handler when svc is nil")
	}

	// NewHandler returns nil if any required dependency is nil
	// We can't easily construct real dependencies here, so we just verify
	// the nil check path works.
}

func TestStateCookie_RoundTrip(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-for-oidc-state-cookie"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	original := OIDCState{
		State:        "random-state-value",
		PKCEVerifier: "pkce-verifier-43chars",
		Nonce:        "nonce-for-replay-protection",
		ReturnURL:    "/console/feedback",
		ExpiresAt:    time.Now().Add(10 * time.Minute).Unix(),
	}

	// Set cookie
	rec := httptest.NewRecorder()
	err = h.setStateCookie(rec, original)
	require.NoError(t, err)

	// Extract cookie from response
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, oidcStateCookie, cookies[0].Name)
	assert.True(t, cookies[0].HttpOnly)
	assert.True(t, cookies[0].Secure)

	// Create request with cookie
	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	req.AddCookie(cookies[0])

	// Get and decrypt cookie
	decoded, err := h.getStateCookie(req)
	require.NoError(t, err)

	assert.Equal(t, original.State, decoded.State)
	assert.Equal(t, original.PKCEVerifier, decoded.PKCEVerifier)
	assert.Equal(t, original.Nonce, decoded.Nonce)
	assert.Equal(t, original.ReturnURL, decoded.ReturnURL)
	assert.Equal(t, original.ExpiresAt, decoded.ExpiresAt)
}

func TestGetStateCookie_NoCookie(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})
	req := httptest.NewRequest(http.MethodGet, "/callback", nil)

	_, err = h.getStateCookie(req)
	assert.Error(t, err)
}

func TestGetStateCookie_InvalidCiphertext(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})
	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	req.AddCookie(ptrext.Of(http.Cookie{
		Name:  oidcStateCookie,
		Value: "invalid-ciphertext",
	}))

	_, err = h.getStateCookie(req)
	assert.Error(t, err)
}

func TestClearStateCookie(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{})
	rec := httptest.NewRecorder()

	h.clearStateCookie(rec)

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, oidcStateCookie, cookies[0].Name)
	assert.Equal(t, "", cookies[0].Value)
	assert.Equal(t, -1, cookies[0].MaxAge)
}

func TestHealth(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"healthy"`)
}

func TestIsValidReturnURL(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{})

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"relative path", "/console/feedback", true},
		{"relative with query", "/console/feedback?id=123", true},
		{"root path", "/", true},
		{"empty string", "", false},
		{"absolute http", "http://evil.com/", false},
		{"absolute https", "https://evil.com/", false},
		{"protocol relative", "//evil.com/path", false},
		{"no leading slash", "console/feedback", false},
		{"javascript protocol", "javascript:alert(1)", false},
		{"data protocol", "data:text/html,<script>", false},
		{"unparseable url (control char)", "/foo\x7fbar", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, h.isValidReturnURL(tt.url))
		})
	}
}

func TestRedirectError(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{})
	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	req.Header.Set("X-Request-Id", "req-123")
	rec := httptest.NewRecorder()

	h.redirectError(rec, req, "bad_state")

	assert.Equal(t, http.StatusFound, rec.Code)
	loc := rec.Header().Get("Location")
	assert.Contains(t, loc, "/console/login/error")
	assert.Contains(t, loc, "code=bad_state")
	assert.Contains(t, loc, "request_id=req-123")
}

func TestRedirectError_IncludesTraceID(t *testing.T) {
	t.Parallel()

	tid, err := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	sid, err := trace.SpanIDFromHex("0123456789abcdef")
	require.NoError(t, err)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
	})

	h := ptrext.Of(Handler{})
	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	req = req.WithContext(trace.ContextWithSpanContext(req.Context(), sc))
	rec := httptest.NewRecorder()

	h.redirectError(rec, req, "bad_state")

	assert.Contains(t, rec.Header().Get("Location"), "trace_id=0123456789abcdef0123456789abcdef")
}

func TestResponseWriterAdapter_SetCookie(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	adapter := responseWriterAdapter{w: rec}

	adapter.SetCookie(ptrext.Of(http.Cookie{Name: "x", Value: "y"}))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "x", cookies[0].Name)
	assert.Equal(t, "y", cookies[0].Value)
}
