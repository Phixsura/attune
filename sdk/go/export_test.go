package attune

import (
	"net/http"
	"testing"
	"time"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// TestExportedSurface is a compile-time + smoke check that the public API mirrors
// the Node SDK's exports: the proto-generated wire types, the ErrorCode enum, the
// grouped transport codes, and the retry-policy helpers are all reachable from
// the root package.
// rootTypesAccept proves the root re-exports are aliases of the generated types:
// it only compiles if a generated value satisfies the root-package type. Proto
// messages are passed by pointer (they embed a mutex; copying is a vet error).
func rootTypesAccept(*IngestRequest, *IngestResponse, *ErrorResponse, ErrorCode) {}

func TestExportedSurface(t *testing.T) {
	// Generated wire types re-exported (aliases to the public attune/v1 package).
	rootTypesAccept(
		&attunev1.IngestRequest{},
		&attunev1.IngestResponse{},
		&attunev1.ErrorResponse{},
		attunev1.ErrorCode_VALIDATION,
	)

	if TransportErrorCode.Network != CodeNetwork ||
		TransportErrorCode.Timeout != CodeTimeout ||
		TransportErrorCode.Aborted != CodeAborted {
		t.Errorf("TransportErrorCode group mismatch: %+v", TransportErrorCode)
	}

	// Exported retry-policy helpers behave like the unexported core.
	if !IsRetryable(500) || IsRetryable(400) {
		t.Error("IsRetryable: wrong classification")
	}
	if d := BackoffDelay(0); d < 150*time.Millisecond || d > 250*time.Millisecond {
		t.Errorf("BackoffDelay(0) = %v, want ~200ms ±25%%", d)
	}
	if _, ok := ParseRetryAfter(http.Header{"Retry-After": []string{"5"}}, time.Unix(0, 0)); !ok {
		t.Error("ParseRetryAfter: should parse delta-seconds")
	}
}
