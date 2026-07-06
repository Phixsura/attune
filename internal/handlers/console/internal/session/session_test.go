package session

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const testKey = "test-signer-key-32-bytes-padding!" // unit-test only, never used in prod

func TestNewSigner_RejectsShortKey(t *testing.T) {
	if _, err := NewSigner("too-short"); err == nil {
		t.Fatal("expected error for key < 32 bytes")
	}
}

func TestSession_RoundTrip(t *testing.T) {
	s, err := NewSigner(testKey)
	if err != nil {
		t.Fatal(err)
	}
	p := Payload{TenantID: "t1", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	token, err := s.SignSession(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.VerifySession(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "t1" || got.UserID != "u1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestSession_RejectsExpired(t *testing.T) {
	s, _ := NewSigner(testKey)
	p := Payload{TenantID: "t", UserID: "u", ExpiresAt: time.Now().Add(-time.Second).Unix()}
	tok, _ := s.SignSession(p)
	if _, err := s.VerifySession(tok); err == nil {
		t.Fatal("expected expired session to be rejected")
	}
}

func TestSession_RejectsTampered(t *testing.T) {
	s, _ := NewSigner(testKey)
	p := Payload{TenantID: "t", UserID: "u", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	tok, _ := s.SignSession(p)
	// Flip a payload byte.
	parts := strings.SplitN(tok, ".", 2)
	tampered := flipFirstChar(parts[0]) + "." + parts[1]
	if _, err := s.VerifySession(tampered); err == nil {
		t.Fatal("expected tampered payload to be rejected")
	}
	// Wrong signature.
	if _, err := s.VerifySession(parts[0] + ".bogus"); err == nil {
		t.Fatal("expected bogus signature to be rejected")
	}
}

func TestCSRF_RoundTrip(t *testing.T) {
	s, _ := NewSigner(testKey)
	tok := s.CSRFToken("user-1")
	if !s.VerifyCSRF("user-1", tok) {
		t.Fatal("CSRF round-trip failed")
	}
	if s.VerifyCSRF("user-2", tok) {
		t.Fatal("CSRF token must be bound to user")
	}
	if s.VerifyCSRF("user-1", "") {
		t.Fatal("empty CSRF must be rejected")
	}
}

func TestRequireSession_MissingCookieUsesDispatcherEnvelope(t *testing.T) {
	s, _ := NewSigner(testKey)
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run without a session")
	})
	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/me", nil)
	rec := httptest.NewRecorder()

	s.RequireSession(next).ServeHTTP(rec, req)

	assertDispatcherEnvelope(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestRequireSession_InvalidCookieUsesDispatcherEnvelope(t *testing.T) {
	s, _ := NewSigner(testKey)
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run with an invalid session")
	})
	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/me", nil)
	req.AddCookie(ptrext.Of(http.Cookie{Name: SessionCookieName, Value: "not-a-valid-session"}))
	rec := httptest.NewRecorder()

	s.RequireSession(next).ServeHTTP(rec, req)

	assertDispatcherEnvelope(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestRequireSession_InvalidCSRFUsesDispatcherEnvelope(t *testing.T) {
	s, _ := NewSigner(testKey)
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run with invalid csrf")
	})
	token, err := s.SignSession(Payload{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/fb/v1/console/me/change-password", nil)
	req.AddCookie(ptrext.Of(http.Cookie{Name: SessionCookieName, Value: token}))
	req.Header.Set(CSRFHeader, "wrong-token")
	rec := httptest.NewRecorder()

	s.RequireSession(next).ServeHTTP(rec, req)

	assertDispatcherEnvelope(t, rec, http.StatusForbidden, "CSRF_INVALID")
}

func assertDispatcherEnvelope(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, status, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("want %s envelope, got %s", code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("legacy error field leaked: %s", rec.Body.String())
	}
}

// flipFirstChar swaps the leading base64 char to something different —
// used by the tamper test to guarantee a real mutation.
func flipFirstChar(s string) string {
	if s == "" {
		return "X"
	}
	if s[0] == 'A' {
		return "B" + s[1:]
	}
	return "A" + s[1:]
}

func TestFromContext_PanicsWhenNoAuth(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("FromContext should panic when AuthCtx is missing")
		}
	}()
	ctx := t.Context()
	FromContext(ctx) // should panic
}

func TestWithAuthCtx_RoundTrip(t *testing.T) {
	ctx := t.Context()
	auth := ptrext.Of(AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
		UserType: "oidc",
		ExpAt:    time.Now().Add(time.Hour),
	})
	ctx = WithAuthCtx(ctx, auth)

	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext returned nil after WithAuthCtx")
	}
	if got.TenantID != "tenant-1" || got.UserID != "user-1" || got.UserType != "oidc" {
		t.Fatalf("auth mismatch: %+v", got)
	}
}

func TestIssueSessionCookie_SetsCookie(t *testing.T) {
	s, _ := NewSigner(testKey)
	rec := httptest.NewRecorder()

	err := s.IssueSessionCookie(cookieRecorder{rec}, "tenant-1", "user-1")
	if err != nil {
		t.Fatalf("IssueSessionCookie: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	assertSessionCookiePolicy(t, cookies[0])
}

func TestIssueSessionCookieWithType_SetsUserType(t *testing.T) {
	s, _ := NewSigner(testKey)
	rec := httptest.NewRecorder()

	err := s.IssueSessionCookieWithType(cookieRecorder{rec}, "tenant-1", "user-1", "oidc")
	if err != nil {
		t.Fatalf("IssueSessionCookieWithType: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	assertSessionCookiePolicy(t, cookies[0])

	// Verify the cookie can be decoded and contains the right user type
	p, err := s.VerifySession(cookies[0].Value)
	if err != nil {
		t.Fatalf("VerifySession: %v", err)
	}
	if p.UserType != "oidc" {
		t.Fatalf("UserType = %s, want oidc", p.UserType)
	}
}

func TestClearSessionCookie_ExpiresImmediately(t *testing.T) {
	s, _ := NewSigner(testKey)
	rec := httptest.NewRecorder()

	s.ClearSessionCookie(cookieRecorder{rec})

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	assertSessionCookiePolicy(t, cookies[0])
	if cookies[0].MaxAge != -1 {
		t.Fatalf("MaxAge = %d, want -1", cookies[0].MaxAge)
	}
}

func assertSessionCookiePolicy(t *testing.T, c *http.Cookie) {
	t.Helper()
	if c.Name != SessionCookieName {
		t.Fatalf("cookie name = %s, want %s", c.Name, SessionCookieName)
	}
	if !c.HttpOnly || !c.Secure {
		t.Fatal("cookie should be HttpOnly and Secure")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", c.SameSite)
	}
}

func TestRefreshStepUpCookie_UpdatesStepUpTimestamp(t *testing.T) {
	s, _ := NewSigner(testKey)
	rec := httptest.NewRecorder()
	expAt := time.Now().Add(time.Hour)

	err := s.RefreshStepUpCookie(cookieRecorder{rec}, ptrext.Of(AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
		UserType: "admin",
		IssuedAt: time.Now().Add(-10 * time.Minute),
		ExpAt:    expAt,
	}))
	if err != nil {
		t.Fatalf("RefreshStepUpCookie: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	assertSessionCookiePolicy(t, cookies[0])
	payload, err := s.VerifySession(cookies[0].Value)
	if err != nil {
		t.Fatalf("VerifySession: %v", err)
	}
	if payload.StepUpAt == 0 {
		t.Fatal("expected step_up timestamp in refreshed session")
	}
	if payload.ExpiresAt != expAt.Unix() {
		t.Fatalf("ExpiresAt = %d, want %d", payload.ExpiresAt, expAt.Unix())
	}
}

type cookieRecorder struct {
	*httptest.ResponseRecorder
}

func (c cookieRecorder) SetCookie(cookie *http.Cookie) {
	http.SetCookie(c.ResponseRecorder, cookie)
}
