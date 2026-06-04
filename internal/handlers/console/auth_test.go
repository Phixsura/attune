package console

import (
	"strings"
	"testing"
	"time"
)

const testKey = "test-signer-key-32-bytes-padding!" // unit-test only, never used in prod

func TestNewSigner_RejectsShortKey(t *testing.T) {
	if _, err := NewSigner("too-short", false); err == nil {
		t.Fatal("expected error for key < 32 bytes")
	}
}

func TestSigner_InsecureFlag(t *testing.T) {
	s, _ := NewSigner(testKey, true)
	if !s.Insecure() {
		t.Fatal("expected Insecure()=true when constructed with insecure=true")
	}
}

func TestSession_RoundTrip(t *testing.T) {
	s, err := NewSigner(testKey, false)
	if err != nil {
		t.Fatal(err)
	}
	p := sessionPayload{TenantID: "t1", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour).Unix()}
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
	s, _ := NewSigner(testKey, false)
	p := sessionPayload{TenantID: "t", UserID: "u", ExpiresAt: time.Now().Add(-time.Second).Unix()}
	tok, _ := s.SignSession(p)
	if _, err := s.VerifySession(tok); err == nil {
		t.Fatal("expected expired session to be rejected")
	}
}

func TestSession_RejectsTampered(t *testing.T) {
	s, _ := NewSigner(testKey, false)
	p := sessionPayload{TenantID: "t", UserID: "u", ExpiresAt: time.Now().Add(time.Hour).Unix()}
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
	s, _ := NewSigner(testKey, false)
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
