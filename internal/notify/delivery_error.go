package notify

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"syscall"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// FailureKind is a structured classification of a delivery failure. It lets
// the dead-queue operator surface (#33) filter/triage by failure type without
// parsing the free-text error — e.g. "show me every 5xx" vs "every DNS
// failure". The string values are persisted in notify_outbox.failure_kind and
// MUST stay in lockstep with the CHECK constraint in
// migrations/051_notify_outbox_dead_queue.sql.
type FailureKind string

const (
	KindHTTP4xx  FailureKind = "http_4xx"   // upstream returned a 4xx (usually terminal)
	KindHTTP5xx  FailureKind = "http_5xx"   // upstream returned a 5xx
	KindTimeout  FailureKind = "timeout"    // request/response deadline exceeded
	KindDNS      FailureKind = "dns"        // name resolution failed
	KindConn     FailureKind = "connection" // connection refused / reset
	KindTLS      FailureKind = "tls"        // TLS handshake / certificate failure
	KindTerminal FailureKind = "terminal"   // ErrTerminal with no HTTP status (bad payload, disabled, no channel)
	KindOther    FailureKind = "other"      // anything unclassified
)

// DeliveryError carries a structured FailureKind and the upstream HTTP status
// (0 when the request never produced a response) alongside the underlying
// error. It Unwraps to Err so errors.Is(err, ErrTerminal) keeps working.
type DeliveryError struct {
	Kind       FailureKind
	HTTPStatus int
	Err        error
}

func (e *DeliveryError) Error() string {
	if e == nil || e.Err == nil {
		return "delivery error"
	}
	return e.Err.Error()
}

func (e *DeliveryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Classify turns a delivery error plus the upstream HTTP status into a
// DeliveryError. status takes precedence (a 4xx/5xx is classified by code even
// when the checker wrapped it in ErrTerminal); status == 0 means no response,
// so the underlying transport error is inspected.
func Classify(err error, status int) *DeliveryError {
	if err == nil {
		return nil
	}
	de := ptrext.Of(DeliveryError{HTTPStatus: status, Err: err})
	switch {
	case status >= 500:
		de.Kind = KindHTTP5xx
	case status >= 400:
		de.Kind = KindHTTP4xx
	default:
		de.Kind = classifyTransport(err)
	}
	return de
}

// classifyTransport inspects an error from a request that never produced an
// HTTP status (the httpClient.Do / dial / TLS layer, or a service-side
// ErrTerminal). Order matters: a DNS timeout is a timeout; a refused
// connection is checked before the generic terminal/other buckets.
func classifyTransport(err error) FailureKind {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, os.ErrDeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout()) {
		return KindTimeout
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return KindDNS
	}

	var certErr *tls.CertificateVerificationError
	var recErr tls.RecordHeaderError
	var authErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invErr x509.CertificateInvalidError
	if errors.As(err, &certErr) || errors.As(err, &recErr) ||
		errors.As(err, &authErr) || errors.As(err, &hostErr) || errors.As(err, &invErr) {
		return KindTLS
	}

	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return KindConn
	}

	if errors.Is(err, ErrTerminal) {
		return KindTerminal
	}
	return KindOther
}

// AsDeliveryError returns the *DeliveryError already wrapped in err if present,
// otherwise classifies err fresh with no HTTP status. Returns nil for nil.
func AsDeliveryError(err error) *DeliveryError {
	if err == nil {
		return nil
	}
	var de *DeliveryError
	if errors.As(err, &de) {
		return de
	}
	return Classify(err, 0)
}
