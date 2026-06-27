// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package notify

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// ---------------------------------------------------------------------------
// Transport.attempt — build error path (transport.go:148-149)
// ---------------------------------------------------------------------------

func TestTransport_Attempt_BuildError(t *testing.T) {
	t.Parallel()

	tr := NewTransport(nil, NoRetry())
	buildErr := errors.New("cannot build request")

	err := tr.Send(context.Background(), "build-fail", func(_ context.Context) (*http.Request, error) {
		return nil, buildErr
	}, func(_ context.Context, _ int, _ []byte) error {
		t.Fatal("check should not be called when build fails")
		return nil
	})

	if err == nil {
		t.Fatal("expected error when build fails, got nil")
	}
	var de *DeliveryError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DeliveryError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "build request") {
		t.Fatalf("error should mention build request: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Transport.attempt — body read error path (transport.go:158-159)
// ---------------------------------------------------------------------------

// errReader is an io.ReadCloser that always returns an error on Read.
type errReader struct{ err error }

func (e *errReader) Read([]byte) (int, error) { return 0, e.err }
func (e *errReader) Close() error             { return nil }

func TestTransport_Attempt_BodyReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("broken pipe mid-body")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// We need the server to return a valid response header so that
		// httpClient.Do succeeds; the body read error is injected via a
		// custom transport that replaces the response body.
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// Wrap the default transport to inject a broken response body.
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			resp, err := baseTransport.RoundTrip(req)
			if err != nil {
				return nil, err
			}
			resp.Body.Close()
			resp.Body = &errReader{err: readErr}
			return resp, nil
		}),
	}

	tr := NewTransport(httpClient, NoRetry())

	err := tr.Send(context.Background(), "body-read-fail", func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader(`{}`))
	}, func(_ context.Context, _ int, _ []byte) error {
		t.Fatal("check should not be called when body read fails")
		return nil
	})

	if err == nil {
		t.Fatal("expected error on body read failure, got nil")
	}
	var de *DeliveryError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DeliveryError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "read body") {
		t.Fatalf("error should mention read body: %q", err.Error())
	}
}

// roundTripFunc adapts a function into an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// ---------------------------------------------------------------------------
// Transport.Send — exhausted retries wraps last error
// ---------------------------------------------------------------------------

func TestTransport_Send_ExhaustedRetriesMessage(t *testing.T) {
	t.Parallel()

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	tr := NewTransport(nil, fastRetry(3))
	err := tr.Send(context.Background(), "exhaust", func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader(`{}`))
	}, func(_ context.Context, status int, _ []byte) error {
		return fmt.Errorf("status %d", status)
	})

	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if !strings.Contains(err.Error(), "3 attempts failed") {
		t.Fatalf("error should mention attempt count: %q", err.Error())
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

// ---------------------------------------------------------------------------
// TestSend — RenderEvent error path (test_send.go:70-73)
// ---------------------------------------------------------------------------

func TestTestSend_RenderError(t *testing.T) {
	t.Parallel()

	adapterID := "test-render-err"
	outbound.Register(&renderErrChannel{id: adapterID})
	t.Cleanup(func() { outbound.UnregisterForTest(adapterID) })

	result := TestSend(t.Context(), notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: adapterID,
		URL:             "https://example.com/hook",
		TimeoutSeconds:  5,
	})
	if result.OK {
		t.Fatal("expected failure when RenderEvent errors")
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "render") {
		t.Fatalf("unexpected error: %v", result.Err)
	}
}

// renderErrChannel is a stub adapter whose RenderEvent always fails.
type renderErrChannel struct{ id string }

func (c *renderErrChannel) ID() string { return c.id }

func (c *renderErrChannel) RenderEvent(*outbound.Envelope, outbound.Target) (outbound.Rendered, error) {
	return outbound.Rendered{}, errors.New("render: unsupported payload shape")
}

// ---------------------------------------------------------------------------
// TestSend — Build error path (test_send.go:77-80)
// ---------------------------------------------------------------------------

func TestTestSend_BuildError(t *testing.T) {
	t.Parallel()

	adapterID := "test-build-err"
	outbound.Register(&buildErrChannel{id: adapterID})
	t.Cleanup(func() { outbound.UnregisterForTest(adapterID) })

	result := TestSend(t.Context(), notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: adapterID,
		URL:             "https://example.com/hook",
		TimeoutSeconds:  5,
	})
	if result.OK {
		t.Fatal("expected failure when Build errors")
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "build request") {
		t.Fatalf("unexpected error: %v", result.Err)
	}
}

// buildErrChannel is a stub adapter whose Rendered.Build always fails.
type buildErrChannel struct{ id string }

func (c *buildErrChannel) ID() string { return c.id }

func (c *buildErrChannel) RenderEvent(_ *outbound.Envelope, _ outbound.Target) (outbound.Rendered, error) {
	return outbound.Rendered{
		Build: func(context.Context) (*http.Request, error) {
			return nil, errors.New("invalid URL")
		},
		Check: func(context.Context, int, []byte) error {
			return nil
		},
	}, nil
}

// ---------------------------------------------------------------------------
// classifyTransport — TLS error subtypes not covered by existing tests
// ---------------------------------------------------------------------------

func TestClassify_TLS_RecordHeaderError(t *testing.T) {
	t.Parallel()

	recErr := tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}
	err := fmt.Errorf("wrapped: %w", recErr)
	de := Classify(err, 0)
	if de.Kind != KindTLS {
		t.Fatalf("Kind = %q, want %q", de.Kind, KindTLS)
	}
}

func TestClassify_TLS_HostnameError(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("dial: %w", x509.HostnameError{
		Host: "example.com",
		Certificate: &x509.Certificate{
			Subject: pkix.Name{CommonName: "wrong.example.com"},
		},
	})
	de := Classify(err, 0)
	if de.Kind != KindTLS {
		t.Fatalf("Kind = %q, want %q", de.Kind, KindTLS)
	}
}

func TestClassify_TLS_CertificateInvalidError(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("verify: %w", x509.CertificateInvalidError{
		Cert:   &x509.Certificate{},
		Reason: x509.Expired,
	})
	de := Classify(err, 0)
	if de.Kind != KindTLS {
		t.Fatalf("Kind = %q, want %q", de.Kind, KindTLS)
	}
}

// ---------------------------------------------------------------------------
// Classify edge: status in 200-399 range with an error falls to transport
// ---------------------------------------------------------------------------

func TestClassify_SuccessStatusWithError(t *testing.T) {
	t.Parallel()

	// Status 200 but an error is still passed (should classify via transport).
	err := errors.New("something unexpected")
	de := Classify(err, 200)
	if de.Kind != KindOther {
		t.Fatalf("Kind = %q, want %q for status=200 + generic error", de.Kind, KindOther)
	}
	if de.HTTPStatus != 200 {
		t.Fatalf("HTTPStatus = %d, want 200", de.HTTPStatus)
	}
}

func TestClassify_Status399WithTerminalError(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: redirect loop", ErrTerminal)
	de := Classify(err, 399)
	if de.Kind != KindTerminal {
		t.Fatalf("Kind = %q, want %q for status=399 + ErrTerminal", de.Kind, KindTerminal)
	}
}

// ---------------------------------------------------------------------------
// AsDeliveryError — unwrap preserves Kind when wrapped in fmt.Errorf
// ---------------------------------------------------------------------------

func TestAsDeliveryError_WrappedPreservesKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind FailureKind
	}{
		{"timeout", KindTimeout},
		{"dns", KindDNS},
		{"tls", KindTLS},
		{"conn", KindConn},
		{"terminal", KindTerminal},
		{"other", KindOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inner := &DeliveryError{Kind: tc.kind, HTTPStatus: 0, Err: errors.New("inner")}
			wrapped := fmt.Errorf("outer: %w", error(inner))
			got := AsDeliveryError(wrapped)
			if got == nil {
				t.Fatal("AsDeliveryError returned nil")
			}
			if got.Kind != tc.kind {
				t.Fatalf("Kind = %q, want %q", got.Kind, tc.kind)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Transport.attempt — httpClient.Do error (non-build, non-body-read)
// ---------------------------------------------------------------------------

func TestTransport_Attempt_DoError(t *testing.T) {
	t.Parallel()

	doErr := errors.New("dial tcp: connection refused")
	httpClient := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, doErr
		}),
	}
	tr := NewTransport(httpClient, NoRetry())

	err := tr.Send(context.Background(), "do-fail", func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, "http://unreachable.invalid", strings.NewReader(`{}`))
	}, func(_ context.Context, _ int, _ []byte) error {
		t.Fatal("check should not be called when Do fails")
		return nil
	})

	if err == nil {
		t.Fatal("expected error when Do fails, got nil")
	}
	if !strings.Contains(err.Error(), "http do") {
		t.Fatalf("error should mention http do: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// buildTestEnvelope — field validation
// ---------------------------------------------------------------------------

func TestBuildTestEnvelope_Fields(t *testing.T) {
	t.Parallel()

	env := buildTestEnvelope()
	if env.Version != "2" {
		t.Errorf("Version = %q, want %q", env.Version, "2")
	}
	if env.EventType != "test" {
		t.Errorf("EventType = %q, want %q", env.EventType, "test")
	}
	if env.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
	if env.Feedback == nil {
		t.Fatal("Feedback should not be nil")
	}
	if _, ok := env.Feedback["title"]; !ok {
		t.Error("Feedback should contain 'title' key")
	}
	if _, ok := env.Feedback["content"]; !ok {
		t.Error("Feedback should contain 'content' key")
	}
	if _, ok := env.Feedback["source"]; !ok {
		t.Error("Feedback should contain 'source' key")
	}
}

// ---------------------------------------------------------------------------
// TestSend — negative TimeoutSeconds defaults to 10s
// ---------------------------------------------------------------------------

func TestTestSend_NegativeTimeout(t *testing.T) {
	t.Parallel()

	adapterID := "test-neg-timeout"
	outbound.Register(&stubChannel{id: adapterID})
	t.Cleanup(func() { outbound.UnregisterForTest(adapterID) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	result := TestSend(t.Context(), notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "t1",
		DestinationType: adapterID,
		URL:             srv.URL,
		TimeoutSeconds:  -5,
	})
	if !result.OK {
		t.Fatalf("negative timeout should default to 10s and succeed, err=%v", result.Err)
	}
}

// Verify io import is used (errReader).
var _ io.Reader = (*errReader)(nil)
