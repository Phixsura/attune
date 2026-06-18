// ptrext:file-allow test fixtures construct typed error/pointer values directly.
package notify

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestClassify_ByHTTPStatus(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   FailureKind
	}{
		{"client error", 404, KindHTTP4xx},
		{"unauthorized", 401, KindHTTP4xx},
		{"server error", 503, KindHTTP5xx},
		{"internal", 500, KindHTTP5xx},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			de := Classify(fmt.Errorf("upstream said %d", tc.status), tc.status)
			if de.Kind != tc.want {
				t.Fatalf("Kind = %q, want %q", de.Kind, tc.want)
			}
			if de.HTTPStatus != tc.status {
				t.Fatalf("HTTPStatus = %d, want %d", de.HTTPStatus, tc.status)
			}
		})
	}
}

func TestClassify_StatusBeatsTerminal(t *testing.T) {
	// githubissue raises ErrTerminal on 4xx; the status must still win so the
	// operator sees http_4xx, and errors.Is(.., ErrTerminal) must still hold.
	err := fmt.Errorf("%w: github 403", ErrTerminal)
	de := Classify(err, 403)
	if de.Kind != KindHTTP4xx {
		t.Fatalf("Kind = %q, want %q", de.Kind, KindHTTP4xx)
	}
	if !errors.Is(de, ErrTerminal) {
		t.Fatal("errors.Is(de, ErrTerminal) = false, want true (Unwrap chain broken)")
	}
}

func TestClassify_TransportErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want FailureKind
	}{
		{"context deadline", context.DeadlineExceeded, KindTimeout},
		{"os deadline", os.ErrDeadlineExceeded, KindTimeout},
		{"net timeout", &timeoutErr{}, KindTimeout},
		{"dns not found", &net.DNSError{Err: "no such host", Name: "x.example", IsNotFound: true}, KindDNS},
		{"conn refused", fmt.Errorf("dial tcp: %w", syscall.ECONNREFUSED), KindConn},
		{"conn reset", fmt.Errorf("read: %w", syscall.ECONNRESET), KindConn},
		{"tls cert verify", &tls.CertificateVerificationError{}, KindTLS},
		{"tls unknown authority", x509.UnknownAuthorityError{}, KindTLS},
		{"terminal no status", fmt.Errorf("%w: bad payload", ErrTerminal), KindTerminal},
		{"unknown", errors.New("something weird"), KindOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			de := Classify(tc.err, 0)
			if de.Kind != tc.want {
				t.Fatalf("Classify(%v).Kind = %q, want %q", tc.err, de.Kind, tc.want)
			}
			if de.HTTPStatus != 0 {
				t.Fatalf("HTTPStatus = %d, want 0", de.HTTPStatus)
			}
		})
	}
}

func TestClassify_NilIsNil(t *testing.T) {
	if Classify(nil, 0) != nil {
		t.Fatal("Classify(nil, 0) != nil")
	}
	if Classify(nil, 500) != nil {
		t.Fatal("Classify(nil, 500) != nil")
	}
}

func TestAsDeliveryError(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if AsDeliveryError(nil) != nil {
			t.Fatal("want nil")
		}
	})
	t.Run("preserves existing through wrap", func(t *testing.T) {
		inner := &DeliveryError{Kind: KindHTTP5xx, HTTPStatus: 502, Err: errors.New("bad gateway")}
		wrapped := fmt.Errorf("transport outbox-7: 5 attempts failed: %w", error(inner))
		got := AsDeliveryError(wrapped)
		if got == nil || got.Kind != KindHTTP5xx || got.HTTPStatus != 502 {
			t.Fatalf("got %+v, want kind=http_5xx status=502", got)
		}
	})
	t.Run("classifies fresh", func(t *testing.T) {
		got := AsDeliveryError(&net.DNSError{Err: "no such host", IsNotFound: true})
		if got == nil || got.Kind != KindDNS {
			t.Fatalf("got %+v, want kind=dns", got)
		}
	})
}

// timeoutErr is a net.Error whose Timeout() reports true.
type timeoutErr struct{}

func (*timeoutErr) Error() string   { return "i/o timeout" }
func (*timeoutErr) Timeout() bool   { return true }
func (*timeoutErr) Temporary() bool { return true }
