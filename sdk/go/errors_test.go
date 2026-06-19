package attune

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

func TestAttuneErrorErrorString(t *testing.T) {
	e := &AttuneError{Code: CodeValidation, Message: "content is required", Status: 400, RequestID: "abc123"}
	s := e.Error()
	for _, want := range []string{"VALIDATION", "content is required", "400"} {
		if !strings.Contains(s, want) {
			t.Errorf("Error() = %q, want it to contain %q", s, want)
		}
	}
}

func TestAttuneErrorAsTarget(t *testing.T) {
	var err error = &AttuneError{Code: CodeUnauthorized, Status: 401}
	var ae *AttuneError
	if !errors.As(err, &ae) {
		t.Fatal("errors.As failed to extract *AttuneError")
	}
	if ae.Code != CodeUnauthorized {
		t.Errorf("Code = %q, want %q", ae.Code, CodeUnauthorized)
	}
}

func TestErrorFromResponseUsesBody(t *testing.T) {
	body := &attunev1.ErrorResponse{
		Code:      "IDEMPOTENCY_CONFLICT",
		Message:   "key reused",
		RequestId: "r1",
	}
	e := errorFromResponse(http.StatusConflict, body, http.Header{})
	if e.Code != "IDEMPOTENCY_CONFLICT" || e.Message != "key reused" || e.RequestID != "r1" || e.Status != 409 {
		t.Errorf("errorFromResponse mismatch: %+v", e)
	}
}

func TestErrorFromResponseFallsBackToStatusCode(t *testing.T) {
	hdr := http.Header{"X-Request-Id": []string{"hdr-req"}}
	e := errorFromResponse(http.StatusUnauthorized, nil, hdr)
	if e.Code != CodeUnauthorized {
		t.Errorf("fallback Code = %q, want %q", e.Code, CodeUnauthorized)
	}
	if e.RequestID != "hdr-req" {
		t.Errorf("RequestID = %q, want it taken from X-Request-Id header", e.RequestID)
	}
	if e.Message == "" {
		t.Error("fallback Message should not be empty")
	}
}

// TestCodeConstantsMatchProtoEnum pins each public Code* constant to the
// generated ErrorCode enum name, so a proto enum rename can't silently drift the
// hand-maintained string constants out of sync with the wire.
func TestCodeConstantsMatchProtoEnum(t *testing.T) {
	cases := map[string]attunev1.ErrorCode{
		CodeBadRequest:          attunev1.ErrorCode_BAD_REQUEST,
		CodeUnauthorized:        attunev1.ErrorCode_UNAUTHORIZED,
		CodeForbidden:           attunev1.ErrorCode_FORBIDDEN,
		CodeNotFound:            attunev1.ErrorCode_NOT_FOUND,
		CodeConflict:            attunev1.ErrorCode_CONFLICT,
		CodeIdempotencyConflict: attunev1.ErrorCode_IDEMPOTENCY_CONFLICT,
		CodeBodyTooLarge:        attunev1.ErrorCode_BODY_TOO_LARGE,
		CodeValidation:          attunev1.ErrorCode_VALIDATION,
		CodeRateLimited:         attunev1.ErrorCode_RATE_LIMITED,
		CodeInternal:            attunev1.ErrorCode_INTERNAL,
	}
	for constVal, enum := range cases {
		if constVal != enum.String() {
			t.Errorf("Code constant %q != proto enum %q", constVal, enum.String())
		}
	}
}

func TestErrorFromResponseEmptyCodeFallsBack(t *testing.T) {
	// A body with a message but no code must fall back to the status-derived code.
	body := &attunev1.ErrorResponse{Message: "oops"}
	e := errorFromResponse(http.StatusForbidden, body, http.Header{})
	if e.Code != CodeForbidden {
		t.Errorf("Code = %q, want %q (status fallback when body code empty)", e.Code, CodeForbidden)
	}
	if e.Message != "oops" {
		t.Errorf("Message = %q, want oops", e.Message)
	}
}

func TestCodeFromStatusMapping(t *testing.T) {
	cases := map[int]string{
		400: CodeBadRequest,
		401: CodeUnauthorized,
		403: CodeForbidden,
		404: CodeNotFound,
		409: CodeConflict,
		413: CodeBodyTooLarge,
		422: CodeValidation,
		429: CodeRateLimited,
		500: CodeInternal,
		503: CodeInternal,
	}
	for status, want := range cases {
		if got := codeFromStatus(status); got != want {
			t.Errorf("codeFromStatus(%d) = %q, want %q", status, got, want)
		}
	}
}
