package attune

import (
	"fmt"
	"net/http"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Error codes. Server codes mirror the ErrorResponse.code enum on the wire;
// transport codes are produced by the client for failures that never reached a
// response. Switch on [AttuneError.Code] — never on the human-facing Message.
const (
	// Server (HTTP) error codes.
	CodeBadRequest          = "BAD_REQUEST"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeNotFound            = "NOT_FOUND"
	CodeConflict            = "CONFLICT"
	CodeIdempotencyConflict = "IDEMPOTENCY_CONFLICT"
	CodeBodyTooLarge        = "BODY_TOO_LARGE"
	CodeValidation          = "VALIDATION"
	CodeRateLimited         = "RATE_LIMITED"
	CodeInternal            = "INTERNAL"

	// Transport error codes (no HTTP response was received).
	CodeNetwork = "NETWORK"
	CodeTimeout = "TIMEOUT"
	CodeAborted = "ABORTED"
)

// AttuneError is the error type returned by [Client.Ingest] for any non-success
// outcome. Code is stable and safe to switch on; Status is the HTTP status (0
// for transport errors); RequestID is the server's request id when available.
type AttuneError struct {
	Code      string
	Message   string
	Status    int
	RequestID string
	// Headers is the response header set when an HTTP response was received
	// (nil for transport errors). Mirrors the Node SDK's AttuneError.headers.
	Headers http.Header
	cause   error
}

func (e *AttuneError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("attune: %s (status %d): %s", e.Code, e.Status, e.Message)
	}
	return fmt.Sprintf("attune: %s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying transport error (e.g. a *net.OpError) for
// errors.Is / errors.As inspection.
func (e *AttuneError) Unwrap() error { return e.cause }

// errorFromResponse builds an AttuneError from an HTTP status and a (possibly
// nil) parsed error body — the proto-generated ErrorResponse — falling back to a
// status-derived code and the X-Request-Id header when the body is missing or
// carries no code.
func errorFromResponse(status int, body *attunev1.ErrorResponse, hdr http.Header) *AttuneError {
	e := &AttuneError{Status: status, Headers: hdr}
	if body != nil {
		e.Code = body.GetCode() // string on the wire (ErrorCode enum name)
		e.Message = body.GetMessage()
		e.RequestID = body.GetRequestId()
	}
	if e.Code == "" {
		e.Code = codeFromStatus(status)
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("attune request failed with status %d", status)
	}
	if e.RequestID == "" {
		e.RequestID = hdr.Get("X-Request-Id")
	}
	return e
}

// codeFromStatus maps an HTTP status to a fallback error code for responses
// without a parseable error envelope.
func codeFromStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return CodeBadRequest
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeConflict
	case http.StatusRequestEntityTooLarge:
		return CodeBodyTooLarge
	case http.StatusUnprocessableEntity:
		return CodeValidation
	case http.StatusTooManyRequests:
		return CodeRateLimited
	default:
		if status >= 500 {
			return CodeInternal
		}
		return CodeBadRequest
	}
}
