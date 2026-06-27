package apikey

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// stubVerifier is the smallest Verifier that lets us drive every
// branch of Middleware without spinning up a real service.APIKeys.
type stubVerifier struct {
	tid    string
	kid    uuid.UUID
	scopes []domain.Scope
	err    error
}

func (s stubVerifier) Lookup(ctx context.Context, raw string) (string, uuid.UUID, error) {
	return s.tid, s.kid, s.err
}

func (s stubVerifier) LookupWithScopes(ctx context.Context, raw string) (string, uuid.UUID, []domain.Scope, error) {
	return s.tid, s.kid, s.scopes, s.err
}

func (s stubVerifier) LookupWithScopesAndIP(ctx context.Context, raw, clientIP string) (string, uuid.UUID, []domain.Scope, *int, error) {
	return s.tid, s.kid, s.scopes, nil, s.err
}

// ipCapturingVerifier records the client IP the middleware resolved and passed
// to LookupWithScopesAndIP — lets the XFF tests assert on the actual value.
type ipCapturingVerifier struct {
	gotIP string
}

func (v *ipCapturingVerifier) Lookup(context.Context, string) (string, uuid.UUID, error) {
	return "t", uuid.Nil, nil
}

func (v *ipCapturingVerifier) LookupWithScopes(context.Context, string) (string, uuid.UUID, []domain.Scope, error) {
	return "t", uuid.Nil, nil, nil
}

func (v *ipCapturingVerifier) LookupWithScopesAndIP(_ context.Context, _, clientIP string) (string, uuid.UUID, []domain.Scope, *int, error) {
	v.gotIP = clientIP
	return "t", uuid.Nil, nil, nil, nil
}

// TestMiddlewareWithProxies_ResolvesClientIP locks the XFF trust model: with no
// trusted proxy a forged X-Forwarded-For must NOT change the resolved client IP
// (it stays the direct peer), and with N trusted proxies the IP comes from N
// entries in from the right. Regression guard for the IP-allowlist bypass: the
// router must also NOT run chi's middleware.RealIP, which would rewrite
// RemoteAddr from these same headers upstream and defeat this.
func TestMiddlewareWithProxies_ResolvesClientIP(t *testing.T) {
	tests := []struct {
		name string
		hops int
		xff  string
		want string
	}{
		{"no proxy ignores forged xff", 0, "203.0.113.50", "198.51.100.7"},
		{"no proxy no xff uses peer", 0, "", "198.51.100.7"},
		{"one trusted proxy honored", 1, "203.0.113.50", "203.0.113.50"},
		{"attacker prepend ignored", 1, "9.9.9.9, 203.0.113.50", "203.0.113.50"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ptrext.Of(ipCapturingVerifier{})
			h := MiddlewareWithProxies(v, tt.hops)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodPost, "/v1/feedback/ingest", nil)
			req.Header.Set("X-API-Key", domain.APIKeyPrefix+"abc")
			req.RemoteAddr = "198.51.100.7:54321"
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			if v.gotIP != tt.want {
				t.Fatalf("resolved client IP = %q, want %q", v.gotIP, tt.want)
			}
		})
	}
}

// decode is shared by the envelope assertions — keeps each test focused.
func decode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, body)
	}
	return m
}

// wrap installs chi's RequestID middleware so dispatcher-owned errors include
// a non-empty requestId field — mirrors production wiring.
func wrap(h http.Handler) http.Handler {
	return middleware.RequestID(h)
}

// next is a sentinel "success" handler that should NEVER fire under
// any rejection-path test.
func next(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called on a rejected request")
		w.WriteHeader(http.StatusOK)
	})
}

// TestMiddleware_MissingHeader_UnifiedEnvelope pins the contract that
// the public ingest path returns the same {code, message, requestId}
// shape as every console endpoint — surfaced by E2E smoke when the
// previous {"error":"..."} leak slipped past review.
func TestMiddleware_MissingHeader_UnifiedEnvelope(t *testing.T) {
	mw := Middleware(stubVerifier{})
	srv := httptest.NewServer(wrap(mw(next(t))))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/feedback/ingest")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want JSON", ct)
	}

	body, _ := readAll(resp)
	m := decode(t, body)
	if m["code"] != "UNAUTHORIZED" {
		t.Errorf("code = %v, want UNAUTHORIZED; body=%s", m["code"], body)
	}
	if m["message"] != "missing or malformed api key" {
		t.Errorf("message = %v; body=%s", m["message"], body)
	}
	if rid, _ := m["requestId"].(string); rid == "" {
		t.Errorf("requestId must be non-empty (chi RequestID middleware); body=%s", body)
	}
	// Negative: the OLD shape's "error" key must NOT appear, otherwise
	// some caller is bypassing the dispatcher envelope again.
	if _, leak := m["error"]; leak {
		t.Errorf("legacy {\"error\":...} envelope still present: %s", body)
	}
}

// TestMiddleware_InvalidKey_UnifiedEnvelope covers the second 401
// branch (key prefix OK, verifier rejects it).
func TestMiddleware_InvalidKey_UnifiedEnvelope(t *testing.T) {
	mw := Middleware(stubVerifier{err: domain.ErrInvalidAPIKey})
	srv := httptest.NewServer(wrap(mw(next(t))))
	defer srv.Close()

	rawKey := domain.APIKeyPrefix + "deadbeef"
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/feedback/ingest", nil)
	req.Header.Set("X-API-Key", rawKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	body, _ := readAll(resp)
	m := decode(t, body)
	if m["code"] != "UNAUTHORIZED" || m["message"] != "invalid api key" {
		t.Errorf("envelope wrong: %s", body)
	}
	if strings.Contains(string(body), rawKey) {
		t.Fatalf("raw api key leaked in response body: %s", body)
	}
}

// TestMiddleware_LookupFailure_500 covers the third branch: the
// verifier returned a non-ErrInvalidAPIKey error (DB down, etc.).
// Must return 500 + code=INTERNAL under the unified envelope.
func TestMiddleware_LookupFailure_500(t *testing.T) {
	mw := Middleware(stubVerifier{err: errors.New("db timeout")})
	srv := httptest.NewServer(wrap(mw(next(t))))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/feedback/ingest", nil)
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	body, _ := readAll(resp)
	m := decode(t, body)
	if m["code"] != "INTERNAL" || m["message"] != "api key lookup failed" {
		t.Errorf("envelope wrong: %s", body)
	}
}

// TestMiddleware_HappyPath_NextCalled proves we still forward authed
// requests AND that the tenant/key are reachable via the ctx helpers.
func TestMiddleware_HappyPath_NextCalled(t *testing.T) {
	want := uuid.New()
	mw := Middleware(stubVerifier{tid: "tenant-x", kid: want})

	called := false
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		tid, ok := TenantIDFromContext(r.Context())
		if !ok || tid != "tenant-x" {
			t.Errorf("tenant id missing in ctx: %q ok=%v", tid, ok)
		}
		kid, ok := KeyIDFromContext(r.Context())
		if !ok || kid != want {
			t.Errorf("key id missing in ctx: %v ok=%v", kid, ok)
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(wrap(mw(final)))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/feedback/ingest", nil)
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if !called {
		t.Error("next handler must be called on success")
	}
}

func readAll(resp *http.Response) ([]byte, error) {
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 512)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if errors.Is(err, http.ErrBodyReadAfterClose) {
				return buf, nil
			}
			break
		}
	}
	return buf, nil
}

// TestRequireScope_HasScope verifies that a request with the required scope passes.
func TestRequireScope_HasScope(t *testing.T) {
	kid := uuid.New()
	mw := Middleware(stubVerifier{
		tid:    "tenant-x",
		kid:    kid,
		scopes: []domain.Scope{domain.ScopeIngestWrite},
	})
	scope := RequireScope(domain.ScopeIngestWrite)

	called := false
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(wrap(mw(scope(final))))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/feedback/ingest", nil)
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if !called {
		t.Error("handler should be called when scope matches")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestIsBrowserUserAgent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ua   string
		want bool
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)", true},
		{"chrome/120.0.0.0", true},
		{"attune-sdk/1.0", false},
		{"curl/7.88.1", false},
		{"", false},
		{"Safari/605.1.15", true},
		{"Firefox/120.0", true},
		{"Edge/120", true},
		{"Go-http-client/1.1", false},
	}
	for _, c := range cases {
		if got := IsBrowserUserAgent(c.ua); got != c.want {
			t.Errorf("IsBrowserUserAgent(%q) = %v, want %v", c.ua, got, c.want)
		}
	}
}

func TestFromContextSafe_MissingCtx(t *testing.T) {
	t.Parallel()
	auth, ok := FromContextSafe(context.Background())
	if ok || auth != nil {
		t.Errorf("expected nil/false from empty context, got %v %v", auth, ok)
	}
}

func TestWithAuthForTest(t *testing.T) {
	t.Parallel()
	scopes := []domain.Scope{domain.ScopeIngestWrite, domain.ScopeFeedbackRead}
	ctx := WithAuthForTest(context.Background(), "tenant-1", uuid.Nil.String(), scopes)
	auth, ok := FromContextSafe(ctx)
	if !ok || auth == nil {
		t.Fatal("auth should be present")
	}
	if auth.TenantID != "tenant-1" {
		t.Errorf("tenant = %s, want tenant-1", auth.TenantID)
	}
	if len(auth.Scopes) != 2 {
		t.Errorf("scopes = %d, want 2", len(auth.Scopes))
	}
}

// TestRequireScope_MissingScope verifies that a request without the required scope is rejected.
func TestRequireScope_MissingScope(t *testing.T) {
	kid := uuid.New()
	mw := Middleware(stubVerifier{
		tid:    "tenant-x",
		kid:    kid,
		scopes: []domain.Scope{domain.ScopeFeedbackRead},
	})
	scope := RequireScope(domain.ScopeIngestWrite)

	srv := httptest.NewServer(wrap(mw(scope(next(t)))))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/feedback/ingest", nil)
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	body, _ := readAll(resp)
	m := decode(t, body)
	if m["code"] != "FORBIDDEN" {
		t.Errorf("code = %v, want FORBIDDEN; body=%s", m["code"], body)
	}
}

func TestMiddleware_ExpiredKey_401(t *testing.T) {
	t.Parallel()
	mw := Middleware(stubVerifier{err: domain.ErrAPIKeyExpired})
	srv := httptest.NewServer(wrap(mw(next(t))))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/feedback/ingest", nil)
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	body, _ := readAll(resp)
	m := decode(t, body)
	if m["message"] != "api key expired" {
		t.Errorf("message = %v, want 'api key expired'; body=%s", m["message"], body)
	}
}

func TestMiddleware_IPNotAllowed_403(t *testing.T) {
	t.Parallel()
	mw := Middleware(stubVerifier{err: domain.ErrIPNotAllowed})
	srv := httptest.NewServer(wrap(mw(next(t))))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/feedback/ingest", nil)
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	body, _ := readAll(resp)
	m := decode(t, body)
	if m["code"] != "FORBIDDEN" || m["message"] != "ip not in allowlist" {
		t.Errorf("envelope wrong: %s", body)
	}
}

func TestFromContext_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from FromContext with empty context")
		}
	}()
	FromContext(context.Background())
}
