// Package respond is the console-handler-internal view of the shared
// response helpers. Proto / Error are re-exported from internal/respond
// (the single canonical implementation, also used by infra middlewares
// like apikey) so existing console handler imports stay the same and no
// duplicate envelope writer can drift out of shape.
//
// Decode + ErrBodyTooLarge are console-specific (protoJSON request
// bodies, 1 MiB cap on customer inputs) and live here.
package respond

import (
	"errors"
	"io"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/respond"
)

// Proto / Error re-export the canonical implementations from
// internal/respond. Console handlers reading these via
// "console/internal/respond" find them at the same path with the same
// signature, but the implementation is shared with infra-layer callers
// (e.g. apikey middleware) — so every customer-facing error in attune
// emits the {code, message, requestId} envelope.
var (
	Proto = respond.Proto
	Error = respond.Error
)

var unmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}

// ErrBodyTooLarge is returned when the request body exceeds the 1 MiB cap.
// Callers map it to HTTP 413; kept as a sentinel rather than a typed
// *http.MaxBytesError because Decode takes io.Reader for testability and
// doesn't have a *http.Request to wrap with MaxBytesReader.
var ErrBodyTooLarge = errors.New("console: request body exceeds 1 MiB")

// Decode reads a JSON request body into a proto message (lenient: unknown
// fields are ignored, matching the previous encoding/json behaviour).
// Returns ErrBodyTooLarge when the body exceeds 1 MiB so callers can
// distinguish "oversized" (→ 413) from "bad JSON" (→ 400); reading
// limit+1 bytes makes the exact-1-MiB case round up to "too large" —
// acceptable off-by-one.
func Decode(r io.Reader, m proto.Message) error {
	const limit = 1 << 20
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if len(b) > limit {
		return ErrBodyTooLarge
	}
	return unmarshal.Unmarshal(b, m)
}
