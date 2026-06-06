// Package respond holds the response/decode helpers shared by every
// console handler subpackage. Lives under `internal/` so it's only
// importable from within handlers/console/.
//
// Lives BELOW the handler subpackages in the import graph: each handler
// imports respond (and session), but neither respond nor session import
// any handler. The root handlers/console package wires them together via
// router.go without creating a cycle.
package respond

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Proto writes a proto message as protoJSON — the wire contract for the
// proto-migrated console endpoints (#19). Field names are lowerCamelCase
// and int64s serialize as JSON strings, per the protoJSON spec.
func Proto(w http.ResponseWriter, status int, m proto.Message) {
	b, err := protojson.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// Error writes the unified ErrorResponse {code, message, requestId}
// (request_id from the chi RequestID middleware). The single error
// shape every console endpoint emits.
func Error(ctx context.Context, w http.ResponseWriter, status int, code, message string) {
	Proto(w, status, &attunev1.ErrorResponse{
		Code:      code,
		Message:   message,
		RequestId: middleware.GetReqID(ctx),
	})
}

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
