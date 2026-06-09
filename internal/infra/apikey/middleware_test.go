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
)

// stubVerifier is the smallest Verifier that lets us drive every
// branch of Middleware without spinning up a real service.APIKeys.
type stubVerifier struct {
	tid string
	kid uuid.UUID
	err error
}

func (s stubVerifier) Lookup(ctx context.Context, raw string) (string, uuid.UUID, error) {
	return s.tid, s.kid, s.err
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
