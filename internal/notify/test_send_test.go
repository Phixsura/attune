// ptrext:file-allow test fixtures construct stub adapters inline.
package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

func TestTestSend_EmptyURL(t *testing.T) {
	t.Parallel()
	result := TestSend(context.Background(), notifytarget.NotifyTarget{
		ID:              uuid.New(),
		DestinationType: "raw-webhook",
	})
	if result.OK {
		t.Fatal("empty URL should fail")
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "url is empty") {
		t.Fatalf("unexpected error: %v", result.Err)
	}
}

func TestTestSend_UnknownDestType(t *testing.T) {
	t.Parallel()
	result := TestSend(context.Background(), notifytarget.NotifyTarget{
		ID:              uuid.New(),
		DestinationType: "carrier-pigeon",
		URL:             "https://example.com/hook",
	})
	if result.OK {
		t.Fatal("unknown dest type should fail")
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "not implemented") {
		t.Fatalf("unexpected error: %v", result.Err)
	}
}

func TestTestSend_StubAdapter_HappyPath(t *testing.T) {
	t.Parallel()
	registerStubAdapter(t, "test-stub-happy")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	result := TestSend(t.Context(), notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: "test-stub-happy",
		URL:             srv.URL,
		TimeoutSeconds:  5,
	})
	if !result.OK {
		t.Fatalf("expected OK, got err=%v status=%d", result.Err, result.StatusCode)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", result.StatusCode)
	}
	if result.LatencyMs < 0 {
		t.Errorf("latency_ms = %d, want >= 0", result.LatencyMs)
	}
}

func TestTestSend_StubAdapter_ServerError(t *testing.T) {
	t.Parallel()
	registerStubAdapter(t, "test-stub-5xx")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	t.Cleanup(srv.Close)

	result := TestSend(t.Context(), notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: "test-stub-5xx",
		URL:             srv.URL,
		TimeoutSeconds:  5,
	})
	if result.OK {
		t.Fatal("500 should not be OK")
	}
	if result.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", result.StatusCode)
	}
}

func TestTestSend_DefaultTimeout(t *testing.T) {
	t.Parallel()
	registerStubAdapter(t, "test-stub-timeout")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	result := TestSend(t.Context(), notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: "test-stub-timeout",
		URL:             srv.URL,
		TimeoutSeconds:  0,
	})
	if !result.OK {
		t.Fatalf("default timeout should work, err=%v", result.Err)
	}
}

// registerStubAdapter registers a minimal EventChannel for testing and
// cleans it up after the test. Uses UnregisterForTest to remove only this
// specific adapter, which is safe for parallel tests.
func registerStubAdapter(t *testing.T, id string) {
	t.Helper()
	outbound.Register(&stubChannel{id: id})
	t.Cleanup(func() { outbound.UnregisterForTest(id) })
}

type stubChannel struct {
	id string
}

func (s *stubChannel) ID() string { return s.id }

func (s *stubChannel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, strings.NewReader(`{"test":true}`))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			return req, nil
		},
		Check: func(ctx context.Context, status int, body []byte) error {
			if status >= 200 && status < 300 {
				return nil
			}
			return outbound.ErrTerminal
		},
	}, nil
}

// A transport-level failure (the *url.Error from http.Client.Do embeds the full
// request URL) must have its token-in-path webhook URL redacted before it is
// returned to the operator (API response + audit log).
func TestTestSend_TransportError_RedactsURL(t *testing.T) {
	t.Parallel()
	registerStubAdapter(t, "test-stub-transport-err")

	// Start then immediately close a server so the dial is refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()

	leakURL := base + "/webhooks/123/SUPER_SECRET_TOKEN"
	result := TestSend(t.Context(), notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: "test-stub-transport-err",
		URL:             leakURL,
		TimeoutSeconds:  2,
	})
	if result.OK || result.Err == nil {
		t.Fatalf("expected a transport error, got ok=%v err=%v", result.OK, result.Err)
	}
	if strings.Contains(result.Err.Error(), "SUPER_SECRET_TOKEN") {
		t.Errorf("returned error leaked the token: %q", result.Err.Error())
	}
}
