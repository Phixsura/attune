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
