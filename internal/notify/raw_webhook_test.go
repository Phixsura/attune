package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo"
)

func snapshotFixture() domain.Snapshot {
	return domain.Snapshot{
		ID:         12345,
		TenantID:   "test-tenant",
		Content:    "剧本按钮点不动",
		Source:     "api",
		UserID:     "ext_abc:open_id_xxx",
		Title:      "剧本选择按钮无响应",
		Kind:       "bug",
		Severity:   "P1",
		Modules:    []string{"剧本"},
		Priority:   3.0,
		EnrichedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
	}
}

func mustVerifySig(t *testing.T, body []byte, secret, sigHeader string) {
	t.Helper()
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	expected := "sha256=" + hex.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sigHeader)) {
		t.Fatalf("signature mismatch: want %s, got %s", expected, sigHeader)
	}
}

func TestRawWebhook_HappyPath(t *testing.T) {
	var receivedBody []byte
	var receivedSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		receivedSig = r.Header.Get("X-Attune-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	secret := "test-secret-16-or-more-chars"
	router := NewRawWebhookRouter(
		NewTransport(nil, NoRetry()),
		[]repo.NotifyTarget{{
			TenantID:        "test-tenant",
			DestinationType: repo.DestRawWebhook,
			Audience:        repo.AudiencePool,
			URL:             srv.URL,
			Secret:          secret,
			TimeoutSeconds:  10,
		}},
	)
	if err := router.PushPool(context.Background(), snapshotFixture()); err != nil {
		t.Fatalf("PushPool: %v", err)
	}

	// Envelope shape contract
	var env map[string]any
	if err := json.Unmarshal(receivedBody, &env); err != nil {
		t.Fatalf("envelope JSON parse: %v", err)
	}
	if env["version"] != "1" {
		t.Errorf("envelope.version: want 1, got %v", env["version"])
	}
	if env["event_type"] != "feedback.enriched" {
		t.Errorf("envelope.event_type: want feedback.enriched, got %v", env["event_type"])
	}
	fb, ok := env["feedback"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.feedback missing or wrong type")
	}
	if fb["id"].(float64) != 12345 {
		t.Errorf("feedback.id: want 12345, got %v", fb["id"])
	}
	enriched := fb["enriched"].(map[string]any)
	if enriched["severity"] != "P1" {
		t.Errorf("enriched.severity: want P1, got %v", enriched["severity"])
	}

	// Signature contract
	mustVerifySig(t, receivedBody, secret, receivedSig)
}

func TestRawWebhook_UnknownTenantSilentNoOp(t *testing.T) {
	// snapshot has tenant_id 'unmapped'; router has no row for it.
	called := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	router := NewRawWebhookRouter(
		NewTransport(nil, NoRetry()),
		[]repo.NotifyTarget{{
			TenantID:        "different-tenant",
			DestinationType: repo.DestRawWebhook,
			Audience:        repo.AudiencePool,
			URL:             srv.URL,
			Secret:          "test-secret-16-or-more-chars",
		}},
	)
	s := snapshotFixture()
	s.TenantID = "unmapped"
	if err := router.PushPool(context.Background(), s); err != nil {
		t.Fatalf("want nil for unmapped tenant, got %v", err)
	}
	if called.Load() != 0 {
		t.Errorf("server should not have been called, got %d", called.Load())
	}
}

func TestRawWebhook_AudienceAllMatchesBothPushes(t *testing.T) {
	called := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	router := NewRawWebhookRouter(
		NewTransport(nil, NoRetry()),
		[]repo.NotifyTarget{{
			TenantID:        "test-tenant",
			DestinationType: repo.DestRawWebhook,
			Audience:        repo.AudienceAll,
			URL:             srv.URL,
			Secret:          "test-secret-16-or-more-chars",
		}},
	)
	s := snapshotFixture()
	_ = router.PushPool(context.Background(), s)
	_ = router.PushRadar(context.Background(), s)
	if called.Load() != 2 {
		t.Errorf("audience=all should accept both pool + radar, got %d calls",
			called.Load())
	}
}

func TestRawWebhook_4xxIsTerminal(t *testing.T) {
	attempts := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	router := NewRawWebhookRouter(
		NewTransport(nil, RetryPolicy{
			MaxAttempts: 5, BaseDelay: 1 * time.Millisecond, MaxDelay: 2 * time.Millisecond,
		}),
		[]repo.NotifyTarget{{
			TenantID:        "test-tenant",
			DestinationType: repo.DestRawWebhook,
			Audience:        repo.AudiencePool,
			URL:             srv.URL,
			Secret:          "test-secret-16-or-more-chars",
		}},
	)
	err := router.PushPool(context.Background(), snapshotFixture())
	if err == nil {
		t.Fatalf("400 should yield error")
	}
	if attempts.Load() != 1 {
		t.Errorf("4xx is terminal → 1 attempt only, got %d", attempts.Load())
	}
}

func TestRawWebhook_5xxRetries(t *testing.T) {
	attempts := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	router := NewRawWebhookRouter(
		NewTransport(nil, RetryPolicy{
			MaxAttempts: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 2 * time.Millisecond,
		}),
		[]repo.NotifyTarget{{
			TenantID:        "test-tenant",
			DestinationType: repo.DestRawWebhook,
			Audience:        repo.AudiencePool,
			URL:             srv.URL,
			Secret:          "test-secret-16-or-more-chars",
		}},
	)
	_ = router.PushPool(context.Background(), snapshotFixture())
	if attempts.Load() != 3 {
		t.Errorf("5xx should retry up to MaxAttempts=3, got %d", attempts.Load())
	}
}

func TestSignRawBody_Deterministic(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	a := signRawBody(body, "shared-secret")
	b := signRawBody(body, "shared-secret")
	if a != b {
		t.Fatalf("same body+secret should give same sig: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "sha256=") {
		t.Errorf("sig must start with sha256= prefix, got %q", a)
	}
}

func TestSignRawBody_DifferentSecretChangesSig(t *testing.T) {
	body := []byte(`{}`)
	a := signRawBody(body, "secret-a")
	b := signRawBody(body, "secret-b")
	if a == b {
		t.Fatalf("different secret must produce different sig")
	}
}
