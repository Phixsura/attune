// SPDX-License-Identifier: Apache-2.0

package oidc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/crypto"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/service/oidcauth"
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

func TestNewHandler_ProviderMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		providerName string
		oidcOnly     bool
	}{
		{name: "local login available", providerName: "Okta", oidcOnly: false},
		{name: "oidc only", providerName: "Acme SSO", oidcOnly: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newMetadataHandler(t, tt.providerName, tt.oidcOnly)

			require.NotNil(t, h)
			assert.Equal(t, tt.providerName, h.ProviderName())
			assert.Equal(t, tt.oidcOnly, h.OIDCOnly())
			assert.Equal(t, "https://console.example.test", h.baseURL)
		})
	}
}

func TestStartSetsStateCookieAndRedirects(t *testing.T) {
	t.Parallel()

	h := newMetadataHandler(t, "Okta", false)
	req := httptest.NewRequest(http.MethodGet, "/start?return_url=/console/settings", nil)
	rec := httptest.NewRecorder()

	h.Start(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, oidcStateCookie, cookies[0].Name)

	reqWithCookie := httptest.NewRequest(http.MethodGet, "/callback", nil)
	reqWithCookie.AddCookie(cookies[0])
	storedState, err := h.getStateCookie(reqWithCookie)
	require.NoError(t, err)
	assert.Equal(t, "/console/settings", storedState.ReturnURL)
	assert.NotEmpty(t, storedState.State)
	assert.NotEmpty(t, storedState.PKCEVerifier)
	assert.NotEmpty(t, storedState.Nonce)
	assert.Greater(t, storedState.ExpiresAt, time.Now().Unix())

	redirectURL, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/authorize", redirectURL.Path)

	query := redirectURL.Query()
	assert.Equal(t, "client-1", query.Get("client_id"))
	assert.Equal(t, "code", query.Get("response_type"))
	assert.Equal(t, "S256", query.Get("code_challenge_method"))
	assert.Equal(t, storedState.State, query.Get("state"))
	assert.Equal(t, storedState.Nonce, query.Get("nonce"))
	assert.NotEmpty(t, query.Get("code_challenge"))
}

func TestStartDropsInvalidReturnURL(t *testing.T) {
	t.Parallel()

	h := newMetadataHandler(t, "Okta", false)
	req := httptest.NewRequest(http.MethodGet, "/start?return_url=https://evil.example/phish", nil)
	rec := httptest.NewRecorder()

	h.Start(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)

	reqWithCookie := httptest.NewRequest(http.MethodGet, "/callback", nil)
	reqWithCookie.AddCookie(cookies[0])
	storedState, err := h.getStateCookie(reqWithCookie)
	require.NoError(t, err)
	assert.Empty(t, storedState.ReturnURL)
}

func newMetadataHandler(t *testing.T, providerName string, oidcOnly bool) *Handler {
	t.Helper()

	server := newOIDCMetadataServer(t)
	cfg := ptrext.Of(config.OIDCConfig{
		Enabled:            true,
		IssuerURL:          server.URL,
		ClientID:           "client-1",
		ClientSecret:       "secret-1",
		RedirectURI:        "https://console.example.test/fb/v1/console/auth/oidc/callback",
		Scopes:             []string{"openid", "email"},
		UserClaim:          "email",
		GroupsClaim:        "groups",
		ProviderName:       providerName,
		OIDCOnly:           oidcOnly,
		InsecureSkipVerify: true,
	})
	svc, err := oidcauth.NewService(t.Context(), cfg, nil, nil)
	require.NoError(t, err)

	signer, err := session.NewSigner("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	aead, err := crypto.NewAEAD([]byte("test-key-handler-metadata"))
	require.NoError(t, err)

	return NewHandler(svc, signer, aead, "https://console.example.test/")
}

func newOIDCMetadataServer(t *testing.T) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": %q,
			"userinfo_endpoint": %q
		}`, server.URL, server.URL+"/authorize", server.URL+"/token", server.URL+"/keys", server.URL+"/userinfo")
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
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
