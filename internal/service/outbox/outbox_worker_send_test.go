// ptrext:file-allow test fixtures construct stub adapters and targets inline.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

// --- wrapCheck tests ---

func TestWrapCheck_TranslatesOutboundTerminalToNotifyTerminal(t *testing.T) {
	t.Parallel()
	adapterCheck := func(_ context.Context, status int, _ []byte) error {
		return fmt.Errorf("%w: slack status=403 body=invalid_token", outbound.ErrTerminal)
	}
	row := outboxrepo.OutboxRow{ID: 42, TenantID: "t1", TraceID: "trace-1"}
	wrapped := wrapCheck(adapterCheck, row)

	err := wrapped(context.Background(), 403, []byte("invalid_token"))
	require.Error(t, err)
	require.ErrorIs(t, err, notify.ErrTerminal)
}

func TestWrapCheck_NonTerminalPassesThrough(t *testing.T) {
	t.Parallel()
	adapterCheck := func(_ context.Context, _ int, _ []byte) error {
		return fmt.Errorf("slack retryable status=429")
	}
	row := outboxrepo.OutboxRow{ID: 43, TenantID: "t1"}
	wrapped := wrapCheck(adapterCheck, row)

	err := wrapped(context.Background(), 429, []byte("rate limited"))
	require.Error(t, err)
	require.False(t, errors.Is(err, notify.ErrTerminal), "retryable error should not wrap notify.ErrTerminal")
	require.False(t, errors.Is(err, outbound.ErrTerminal), "retryable error should not wrap outbound.ErrTerminal")
}

func TestWrapCheck_SuccessReturnsNil(t *testing.T) {
	t.Parallel()
	adapterCheck := func(_ context.Context, _ int, _ []byte) error { return nil }
	row := outboxrepo.OutboxRow{ID: 44, TenantID: "t1", TraceID: "trace-2"}
	wrapped := wrapCheck(adapterCheck, row)

	err := wrapped(context.Background(), 200, []byte("ok"))
	require.NoError(t, err)
}

// --- unmarshalEnvelope tests ---

// TestUnmarshalEnvelope_OutboxShape proves that the real outbox JSON
// (produced by enricher_outbox.go buildOutboxEnvelope) round-trips
// correctly through unmarshalEnvelope -- timestamp, tenant_id, and
// nested enriched fields all land where the adapter expects them.
func TestUnmarshalEnvelope_OutboxShape(t *testing.T) {
	t.Parallel()

	outboxJSON, _ := json.Marshal(map[string]any{
		"version":      "2",
		"event_type":   "feedback.enriched",
		"delivered_at": "2026-06-20T12:00:00Z",
		"trace_id":     "abc-123",
		"feedback": map[string]any{
			"id":           42,
			"tenant_id":    "tenant-x",
			"content":      "Payment failed on checkout",
			"source":       "api",
			"user_id":      "u-1",
			"submitted_at": "2026-06-20T11:55:00Z",
			"enriched": map[string]any{
				"title":       "Checkout Payment Failure",
				"is_urgent":   true,
				"rationale":   "Payment flow broken",
				"enriched_at": "2026-06-20T12:00:00Z",
				"attrs": map[string]any{
					"severity": "high",
					"category": "payments",
				},
			},
		},
	})

	env, err := unmarshalEnvelope(outboxJSON)
	require.NoError(t, err)
	require.Equal(t, "2", env.Version)
	require.Equal(t, "2026-06-20T12:00:00Z", env.Timestamp)
	require.Equal(t, "tenant-x", env.TenantID)

	fb := env.Feedback
	enriched, _ := fb["enriched"].(map[string]any)
	require.NotNil(t, enriched)

	title, _ := enriched["title"].(string)
	require.Equal(t, "Checkout Payment Failure", title)
	isUrgent, _ := enriched["is_urgent"].(bool)
	require.True(t, isUrgent)

	attrs, _ := enriched["attrs"].(map[string]any)
	require.NotNil(t, attrs)
	sev, _ := attrs["severity"].(string)
	require.Equal(t, "high", sev)
	cat, _ := attrs["category"].(string)
	require.Equal(t, "payments", cat)
}

func TestUnmarshalEnvelope_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := unmarshalEnvelope([]byte(`{not valid json}`))
	require.Error(t, err)
}

func TestUnmarshalEnvelope_EmptyPayload(t *testing.T) {
	t.Parallel()
	_, err := unmarshalEnvelope([]byte(``))
	require.Error(t, err)
}

func TestUnmarshalEnvelope_TimestampFieldDirect(t *testing.T) {
	t.Parallel()
	// When "timestamp" is set directly, delivered_at fallback should not override it.
	payload, _ := json.Marshal(map[string]any{
		"version":      "2",
		"timestamp":    "2026-01-01T00:00:00Z",
		"delivered_at": "2026-06-20T12:00:00Z",
		"feedback":     map[string]any{},
	})
	env, err := unmarshalEnvelope(payload)
	require.NoError(t, err)
	require.Equal(t, "2026-01-01T00:00:00Z", env.Timestamp)
}

func TestUnmarshalEnvelope_TenantIDDirect(t *testing.T) {
	t.Parallel()
	// When top-level tenant_id is set, it takes precedence over feedback.tenant_id.
	payload, _ := json.Marshal(map[string]any{
		"version":   "2",
		"timestamp": "2026-01-01T00:00:00Z",
		"tenant_id": "top-level-tenant",
		"feedback": map[string]any{
			"tenant_id": "nested-tenant",
		},
	})
	env, err := unmarshalEnvelope(payload)
	require.NoError(t, err)
	require.Equal(t, "top-level-tenant", env.TenantID)
}

func TestUnmarshalEnvelope_NoTimestampNoDeliveredAt(t *testing.T) {
	t.Parallel()
	// When neither timestamp nor delivered_at exist, Timestamp stays empty.
	payload, _ := json.Marshal(map[string]any{
		"version":  "2",
		"feedback": map[string]any{},
	})
	env, err := unmarshalEnvelope(payload)
	require.NoError(t, err)
	require.Empty(t, env.Timestamp)
}

func TestUnmarshalEnvelope_NoTenantAnywhere(t *testing.T) {
	t.Parallel()
	// When tenant_id is absent from both top-level and feedback, TenantID stays empty.
	payload, _ := json.Marshal(map[string]any{
		"version":   "2",
		"timestamp": "2026-01-01T00:00:00Z",
		"feedback":  map[string]any{"content": "hello"},
	})
	env, err := unmarshalEnvelope(payload)
	require.NoError(t, err)
	require.Empty(t, env.TenantID)
}

func TestUnmarshalEnvelope_TenantFromFeedbackNonString(t *testing.T) {
	t.Parallel()
	// When feedback.tenant_id is not a string, TenantID stays empty.
	payload, _ := json.Marshal(map[string]any{
		"version":   "2",
		"timestamp": "2026-01-01T00:00:00Z",
		"feedback": map[string]any{
			"tenant_id": 12345,
		},
	})
	env, err := unmarshalEnvelope(payload)
	require.NoError(t, err)
	require.Empty(t, env.TenantID)
}

func TestUnmarshalEnvelope_NilFeedback(t *testing.T) {
	t.Parallel()
	// When feedback is null/missing, still succeeds.
	payload, _ := json.Marshal(map[string]any{
		"version":   "2",
		"timestamp": "2026-01-01T00:00:00Z",
	})
	env, err := unmarshalEnvelope(payload)
	require.NoError(t, err)
	require.Empty(t, env.TenantID)
	require.Nil(t, env.Feedback)
}

// --- toOutboundTarget tests ---

func TestToOutboundTarget_AllFields(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	nt := ptrext.Of(notifytarget.NotifyTarget{
		ID:               id,
		TenantID:         "t-abc",
		DestinationType:  "raw-webhook",
		URL:              "https://example.com/hook",
		Secret:           "s3cret",
		SignatureVersion: "v2-bytes",
	})
	got := toOutboundTarget(nt)
	require.Equal(t, id.String(), got.ID)
	require.Equal(t, "t-abc", got.TenantID)
	require.Equal(t, "https://example.com/hook", got.URL)
	require.Equal(t, "s3cret", got.Secret)
	require.Equal(t, "v2-bytes", got.SignatureVersion)
	require.Equal(t, "raw-webhook", got.DestinationType)
}

func TestToOutboundTarget_DefaultSignatureVersion(t *testing.T) {
	t.Parallel()
	nt := ptrext.Of(notifytarget.NotifyTarget{
		ID:               uuid.New(),
		TenantID:         "t1",
		DestinationType:  "raw-webhook",
		URL:              "https://example.com",
		SignatureVersion: "",
	})
	got := toOutboundTarget(nt)
	require.Equal(t, outbound.SignatureVersionContentHash, got.SignatureVersion,
		"empty SignatureVersion should default to content-hash")
}

func TestToOutboundTarget_PreservesContentHash(t *testing.T) {
	t.Parallel()
	nt := ptrext.Of(notifytarget.NotifyTarget{
		ID:               uuid.New(),
		TenantID:         "t1",
		DestinationType:  "raw-webhook",
		URL:              "https://example.com",
		SignatureVersion: outbound.SignatureVersionContentHash,
	})
	got := toOutboundTarget(nt)
	require.Equal(t, outbound.SignatureVersionContentHash, got.SignatureVersion)
}

// --- sendByDestType tests ---

// testStubChannel is a minimal EventChannel for unit tests. Each instance
// uses a unique ID to avoid parallel test collision in the global registry.
type testStubChannel struct {
	id        string
	renderErr error
}

func (s *testStubChannel) ID() string { return s.id }

func (s *testStubChannel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	if s.renderErr != nil {
		return outbound.Rendered{}, s.renderErr
	}
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, strings.NewReader(`{"test":true}`))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			return req, nil
		},
		Check: outbound.CheckWebhook("test"),
	}, nil
}

// registerTestChannel registers a stub adapter and cleans it up on test end.
func registerTestChannel(t *testing.T, ch *testStubChannel) {
	t.Helper()
	outbound.Register(ch)
	t.Cleanup(func() { outbound.UnregisterForTest(ch.ID()) })
}

// validPayload returns a valid outbox envelope JSON for test use.
func validPayload() []byte {
	p, _ := json.Marshal(map[string]any{
		"version":   "2",
		"timestamp": "2026-01-01T00:00:00Z",
		"tenant_id": "t1",
		"feedback":  map[string]any{"content": "test"},
	})
	return p
}

func TestSendByDestType_HappyPath(t *testing.T) {
	t.Parallel()
	chID := "test-send-happy-" + uuid.NewString()[:8]
	registerTestChannel(t, &testStubChannel{id: chID})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	transport := notify.NewTransport(srv.Client(), notify.NoRetry())
	target := ptrext.Of(notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: chID,
		URL:             srv.URL,
		Secret:          "test-secret",
	})

	w := NewOutboxWorker(nil, nil, transport)
	row := outboxrepo.OutboxRow{
		ID:              1,
		TenantID:        "t1",
		DestinationType: chID,
		Payload:         validPayload(),
	}

	err := w.sendByDestType(context.Background(), row, target)
	require.NoError(t, err)
}

func TestSendByDestType_UnknownDestType(t *testing.T) {
	t.Parallel()
	transport := notify.NewTransport(http.DefaultClient, notify.NoRetry())
	w := NewOutboxWorker(nil, nil, transport)

	row := outboxrepo.OutboxRow{
		ID:              1,
		DestinationType: "carrier-pigeon-" + uuid.NewString()[:8],
	}
	target := ptrext.Of(notifytarget.NotifyTarget{})

	err := w.sendByDestType(context.Background(), row, target)
	require.Error(t, err)
	require.ErrorIs(t, err, notify.ErrTerminal)
}

func TestSendByDestType_BadPayload(t *testing.T) {
	t.Parallel()
	chID := "test-send-badpay-" + uuid.NewString()[:8]
	registerTestChannel(t, &testStubChannel{id: chID})

	transport := notify.NewTransport(http.DefaultClient, notify.NoRetry())
	w := NewOutboxWorker(nil, nil, transport)

	row := outboxrepo.OutboxRow{
		ID:              2,
		DestinationType: chID,
		Payload:         []byte(`{not valid`),
	}
	target := ptrext.Of(notifytarget.NotifyTarget{})

	err := w.sendByDestType(context.Background(), row, target)
	require.Error(t, err)
	require.ErrorIs(t, err, notify.ErrTerminal, "bad payload should be terminal")
}

func TestSendByDestType_RenderError(t *testing.T) {
	t.Parallel()
	chID := "test-send-renderr-" + uuid.NewString()[:8]
	renderErr := errors.New("render failed: missing required field")
	registerTestChannel(t, &testStubChannel{id: chID, renderErr: renderErr})

	transport := notify.NewTransport(http.DefaultClient, notify.NoRetry())
	w := NewOutboxWorker(nil, nil, transport)

	row := outboxrepo.OutboxRow{
		ID:              3,
		DestinationType: chID,
		Payload:         validPayload(),
	}
	target := ptrext.Of(notifytarget.NotifyTarget{})

	err := w.sendByDestType(context.Background(), row, target)
	require.Error(t, err)
	require.ErrorIs(t, err, renderErr)
}

func TestSendByDestType_ServerError(t *testing.T) {
	t.Parallel()
	chID := "test-send-5xx-" + uuid.NewString()[:8]
	registerTestChannel(t, &testStubChannel{id: chID})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	t.Cleanup(srv.Close)

	transport := notify.NewTransport(srv.Client(), notify.NoRetry())
	target := ptrext.Of(notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: chID,
		URL:             srv.URL,
	})

	w := NewOutboxWorker(nil, nil, transport)
	row := outboxrepo.OutboxRow{
		ID:              4,
		TenantID:        "t1",
		DestinationType: chID,
		Payload:         validPayload(),
	}

	err := w.sendByDestType(context.Background(), row, target)
	require.Error(t, err, "5xx should produce an error")
}

func TestSendByDestType_TerminalHTTPError(t *testing.T) {
	t.Parallel()
	chID := "test-send-403-" + uuid.NewString()[:8]
	registerTestChannel(t, &testStubChannel{id: chID})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	t.Cleanup(srv.Close)

	transport := notify.NewTransport(srv.Client(), notify.NoRetry())
	target := ptrext.Of(notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: chID,
		URL:             srv.URL,
	})

	w := NewOutboxWorker(nil, nil, transport)
	row := outboxrepo.OutboxRow{
		ID:              5,
		TenantID:        "t1",
		DestinationType: chID,
		Payload:         validPayload(),
	}

	err := w.sendByDestType(context.Background(), row, target)
	require.Error(t, err)
	require.ErrorIs(t, err, notify.ErrTerminal, "403 should be terminal via wrapCheck translation")
}

func TestSendByDestType_SetsDeliveryID(t *testing.T) {
	t.Parallel()
	chID := "test-send-dlvid-" + uuid.NewString()[:8]

	var capturedEnv *outbound.Envelope
	ch := &capturingChannel{id: chID, capture: func(env *outbound.Envelope) { capturedEnv = env }}
	outbound.Register(ch)
	t.Cleanup(func() { outbound.UnregisterForTest(chID) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	transport := notify.NewTransport(srv.Client(), notify.NoRetry())
	target := ptrext.Of(notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: chID,
		URL:             srv.URL,
	})

	w := NewOutboxWorker(nil, nil, transport)
	row := outboxrepo.OutboxRow{
		ID:              777,
		TenantID:        "t1",
		DestinationType: chID,
		Payload:         validPayload(),
	}

	err := w.sendByDestType(context.Background(), row, target)
	require.NoError(t, err)
	require.NotNil(t, capturedEnv)
	require.Equal(t, "777", capturedEnv.DeliveryID,
		"DeliveryID should be the row ID as a string")
}

// capturingChannel captures the envelope for inspection, then delegates to a
// simple POST builder (like testStubChannel).
type capturingChannel struct {
	id      string
	capture func(env *outbound.Envelope)
}

func (c *capturingChannel) ID() string { return c.id }

func (c *capturingChannel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	if c.capture != nil {
		c.capture(env)
	}
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, strings.NewReader(`{"test":true}`))
			if err != nil {
				return nil, err
			}
			return req, nil
		},
		Check: func(_ context.Context, status int, _ []byte) error {
			if status >= 200 && status < 300 {
				return nil
			}
			return fmt.Errorf("status=%d", status)
		},
	}, nil
}
