// SPDX-License-Identifier: Apache-2.0

package oidc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/crypto"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestNewHandler_NilSigner(t *testing.T) {
	t.Parallel()

	// NewHandler checks svc == nil || signer == nil || aead == nil,
	// so we cannot isolate the signer-nil branch without a real *oidcauth.Service.
	// Instead we verify that the combined guard returns nil when all are nil
	// (already covered) and document the gap.
	h := NewHandler(nil, nil, nil, "http://example.com")
	assert.Nil(t, h, "any nil dependency must produce nil handler")
}

func TestCallback_NoCookie_RedirectsStateExpired(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-callback-no-cookie"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	rec := httptest.NewRecorder()

	h.Callback(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	loc := rec.Header().Get("Location")
	assert.Contains(t, loc, "/console/login/error")
	assert.Contains(t, loc, "code=state_expired")
}

func TestCallback_ExpiredState_RedirectsStateExpired(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-callback-expired"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	// Write a cookie with an expiry well in the past (beyond clockSkewTolerance).
	rec := httptest.NewRecorder()
	err = h.setStateCookie(rec, OIDCState{
		State:        "test-state-abc",
		PKCEVerifier: "test-verifier",
		Nonce:        "test-nonce",
		ReturnURL:    "/console/",
		ExpiresAt:    time.Now().Add(-2 * time.Hour).Unix(),
	})
	require.NoError(t, err)

	// Build a request carrying that cookie.
	req := httptest.NewRequest(http.MethodGet, "/callback?state=test-state-abc", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	rec2 := httptest.NewRecorder()
	h.Callback(rec2, req)

	require.Equal(t, http.StatusFound, rec2.Code)
	loc := rec2.Header().Get("Location")
	assert.Contains(t, loc, "code=state_expired")
}

func TestCallback_WrongKey_RedirectsStateExpired(t *testing.T) {
	t.Parallel()

	// Encrypt with key A.
	aeadA, err := crypto.NewAEAD([]byte("key-A-for-encrypt"))
	require.NoError(t, err)

	hA := ptrext.Of(Handler{aead: aeadA})

	rec := httptest.NewRecorder()
	err = hA.setStateCookie(rec, OIDCState{
		State:     "state-xyz",
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	require.NoError(t, err)

	// Decrypt with key B: decryption fails, handler redirects state_expired.
	aeadB, err := crypto.NewAEAD([]byte("key-B-for-decrypt"))
	require.NoError(t, err)

	hB := ptrext.Of(Handler{aead: aeadB})

	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	rec2 := httptest.NewRecorder()
	hB.Callback(rec2, req)

	require.Equal(t, http.StatusFound, rec2.Code)
	assert.Contains(t, rec2.Header().Get("Location"), "code=state_expired")
}

func TestOIDCState_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := OIDCState{
		State:        "rs-1234",
		PKCEVerifier: "pkce-verif-abcdef",
		Nonce:        "nonce-ghijkl",
		ReturnURL:    "/console/settings",
		ExpiresAt:    1750000000,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded OIDCState
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original, decoded)
}

func TestOIDCState_JSONFieldTags(t *testing.T) {
	t.Parallel()

	st := OIDCState{
		State:        "s-val",
		PKCEVerifier: "v-val",
		Nonce:        "n-val",
		ReturnURL:    "r-val",
		ExpiresAt:    42,
	}

	data, err := json.Marshal(st)
	require.NoError(t, err)

	raw := make(map[string]json.RawMessage)
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	// Verify the short JSON tags are used (keeps cookie size small).
	for _, key := range []string{"s", "v", "n", "r", "e"} {
		assert.Contains(t, raw, key, "expected JSON key %q", key)
	}
}

func TestSetStateCookie_Attributes(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-cookie-attrs"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	rec := httptest.NewRecorder()
	err = h.setStateCookie(rec, OIDCState{
		State:     "attr-test",
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	require.NoError(t, err)

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)

	c := cookies[0]
	assert.Equal(t, oidcStateCookie, c.Name)
	assert.True(t, c.HttpOnly, "cookie must be HttpOnly")
	assert.True(t, c.Secure, "cookie must be Secure")
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite, "SameSite must be Lax")
	assert.Equal(t, "/", c.Path, "Path must be /")
	assert.Equal(t, int(stateTTL.Seconds()), c.MaxAge, "MaxAge must match stateTTL")
}

func TestGetStateCookie_CorruptedJSON(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-corrupted-json"))
	require.NoError(t, err)

	// Encrypt valid bytes that are NOT valid JSON.
	ciphertext, err := aead.Encrypt([]byte("this is not json"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	req.AddCookie(ptrext.Of(http.Cookie{
		Name:  oidcStateCookie,
		Value: ciphertext,
	}))

	_, err = h.getStateCookie(req)
	assert.Error(t, err, "decrypted non-JSON should cause unmarshal error")
}

func TestCallback_CorruptedJSON_RedirectsStateExpired(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-callback-corrupted"))
	require.NoError(t, err)

	// Encrypt non-JSON so decryption succeeds but unmarshal fails.
	ciphertext, err := aead.Encrypt([]byte("{invalid"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	req := httptest.NewRequest(http.MethodGet, "/callback?state=whatever", nil)
	req.AddCookie(ptrext.Of(http.Cookie{
		Name:  oidcStateCookie,
		Value: ciphertext,
	}))

	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "code=state_expired")
}

func TestCallback_StateWithinClockSkew_PassesExpiryCheck(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-clock-skew"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	// ExpiresAt is 10 seconds in the past — within the 30-second tolerance.
	rec := httptest.NewRecorder()
	err = h.setStateCookie(rec, OIDCState{
		State:     "skew-state",
		ExpiresAt: time.Now().Add(-10 * time.Second).Unix(),
	})
	require.NoError(t, err)

	// Query state matches cookie state; no code param.
	// ValidateState uses subtle.ConstantTimeCompare (no receiver fields),
	// so it works with a nil svc. After passing expiry + state validation,
	// the handler falls through to the "missing code" branch.
	req := httptest.NewRequest(http.MethodGet, "/callback?state=skew-state", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	rec2 := httptest.NewRecorder()
	h.Callback(rec2, req)

	require.Equal(t, http.StatusFound, rec2.Code)
	loc := rec2.Header().Get("Location")
	// "auth_failed" from the missing-code branch proves the expiry check was passed.
	assert.Contains(t, loc, "code=auth_failed")
}

func TestCallback_StateMismatch_Redirects(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-state-mismatch"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	rec := httptest.NewRecorder()
	err = h.setStateCookie(rec, OIDCState{
		State:     "expected-state",
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	require.NoError(t, err)

	// Query state does NOT match the cookie state.
	req := httptest.NewRequest(http.MethodGet, "/callback?state=wrong-state", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	rec2 := httptest.NewRecorder()
	h.Callback(rec2, req)

	require.Equal(t, http.StatusFound, rec2.Code)
	assert.Contains(t, rec2.Header().Get("Location"), "code=state_mismatch")
}

func TestCallback_IdPError_AccessDenied(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-idp-error"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	state := "idp-err-state"
	rec := httptest.NewRecorder()
	err = h.setStateCookie(rec, OIDCState{
		State:     state,
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/callback?state="+state+"&error=access_denied&error_description=user+denied", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	rec2 := httptest.NewRecorder()
	h.Callback(rec2, req)

	require.Equal(t, http.StatusFound, rec2.Code)
	assert.Contains(t, rec2.Header().Get("Location"), "code=user_cancelled")
}

func TestCallback_IdPError_Generic(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-idp-generic"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	state := "idp-generic-state"
	rec := httptest.NewRecorder()
	err = h.setStateCookie(rec, OIDCState{
		State:     state,
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/callback?state="+state+"&error=server_error&error_description=something+broke", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	rec2 := httptest.NewRecorder()
	h.Callback(rec2, req)

	require.Equal(t, http.StatusFound, rec2.Code)
	assert.Contains(t, rec2.Header().Get("Location"), "code=auth_failed")
}

func TestCallback_MissingCode_Redirects(t *testing.T) {
	t.Parallel()

	aead, err := crypto.NewAEAD([]byte("test-key-missing-code"))
	require.NoError(t, err)

	h := ptrext.Of(Handler{aead: aead})

	state := "no-code-state"
	rec := httptest.NewRecorder()
	err = h.setStateCookie(rec, OIDCState{
		State:     state,
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	require.NoError(t, err)

	// State matches, no error param, no code param.
	req := httptest.NewRequest(http.MethodGet, "/callback?state="+state, nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	rec2 := httptest.NewRecorder()
	h.Callback(rec2, req)

	require.Equal(t, http.StatusFound, rec2.Code)
	assert.Contains(t, rec2.Header().Get("Location"), "code=auth_failed")
}
