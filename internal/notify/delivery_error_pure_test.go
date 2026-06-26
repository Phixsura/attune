// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package notify

import (
	"errors"
	"testing"
)

func TestDeliveryError_Error_NilReceiver(t *testing.T) {
	t.Parallel()

	var de *DeliveryError
	got := de.Error()
	if got != "delivery error" {
		t.Fatalf("nil receiver Error() = %q, want %q", got, "delivery error")
	}
}

func TestDeliveryError_Error_NilErr(t *testing.T) {
	t.Parallel()

	de := &DeliveryError{Kind: KindOther, HTTPStatus: 0, Err: nil}
	got := de.Error()
	if got != "delivery error" {
		t.Fatalf("nil Err Error() = %q, want %q", got, "delivery error")
	}
}

func TestDeliveryError_Error_WithErr(t *testing.T) {
	t.Parallel()

	inner := errors.New("something broke")
	de := &DeliveryError{Kind: KindHTTP5xx, HTTPStatus: 500, Err: inner}
	got := de.Error()
	if got != "something broke" {
		t.Fatalf("Error() = %q, want %q", got, "something broke")
	}
}

func TestDeliveryError_Unwrap_NilReceiver(t *testing.T) {
	t.Parallel()

	var de *DeliveryError
	if got := de.Unwrap(); got != nil {
		t.Fatalf("nil receiver Unwrap() = %v, want nil", got)
	}
}

func TestDeliveryError_Unwrap_ReturnsInner(t *testing.T) {
	t.Parallel()

	inner := errors.New("inner error")
	de := &DeliveryError{Kind: KindConn, Err: inner}
	if got := de.Unwrap(); !errors.Is(got, inner) {
		t.Fatalf("Unwrap() = %v, want %v", got, inner)
	}
}

func TestDeliveryError_Unwrap_NilErr(t *testing.T) {
	t.Parallel()

	de := &DeliveryError{Kind: KindOther}
	if got := de.Unwrap(); got != nil {
		t.Fatalf("Unwrap() with nil Err = %v, want nil", got)
	}
}

func TestDeliveryError_ErrorsIs_Chain(t *testing.T) {
	t.Parallel()

	inner := ErrTerminal
	de := &DeliveryError{Kind: KindTerminal, Err: inner}
	if !errors.Is(de, ErrTerminal) {
		t.Fatal("errors.Is(de, ErrTerminal) = false, want true")
	}
}

func TestDeliveryError_ErrorsAs(t *testing.T) {
	t.Parallel()

	inner := &DeliveryError{Kind: KindDNS, HTTPStatus: 0, Err: errors.New("dns fail")}
	wrapped := errors.Join(errors.New("context"), inner)

	var got *DeliveryError
	if !errors.As(wrapped, &got) {
		t.Fatal("errors.As should find DeliveryError")
	}
	if got.Kind != KindDNS {
		t.Fatalf("Kind = %q, want %q", got.Kind, KindDNS)
	}
}

func TestFailureKind_StringValues(t *testing.T) {
	t.Parallel()

	// These string values are persisted in the database and MUST stay stable.
	cases := []struct {
		kind FailureKind
		want string
	}{
		{KindHTTP4xx, "http_4xx"},
		{KindHTTP5xx, "http_5xx"},
		{KindTimeout, "timeout"},
		{KindDNS, "dns"},
		{KindConn, "connection"},
		{KindTLS, "tls"},
		{KindTerminal, "terminal"},
		{KindOther, "other"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if string(tc.kind) != tc.want {
				t.Fatalf("FailureKind = %q, want %q", tc.kind, tc.want)
			}
		})
	}
}
