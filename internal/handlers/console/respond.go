package console

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

// respondProto writes a proto message as protoJSON — the wire contract for the
// proto-migrated console endpoints (#19). Field names are lowerCamelCase and
// int64s serialize as JSON strings, per the protoJSON spec.
func respondProto(w http.ResponseWriter, status int, m proto.Message) {
	b, err := protojson.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// respondError writes the unified ErrorResponse {code, message, requestId}
// (request_id from the chi RequestID middleware). Replaces the console's old
// writeError as endpoints migrate.
func respondError(ctx context.Context, w http.ResponseWriter, status int, code, message string) {
	respondProto(w, status, &attunev1.ErrorResponse{
		Code:      code,
		Message:   message,
		RequestId: middleware.GetReqID(ctx),
	})
}

var consoleUnmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}

// errBodyTooLarge is returned when the request body exceeds the 1 MiB cap.
// Callers map it to HTTP 413 (via decodeRequest); kept as a sentinel rather
// than a typed *http.MaxBytesError because decodeProto takes io.Reader for
// testability and doesn't have a *http.Request to wrap with MaxBytesReader.
var errBodyTooLarge = errors.New("console: request body exceeds 1 MiB")

// decodeProto reads a JSON request body into a proto message (lenient: unknown
// fields are ignored, matching the previous encoding/json behaviour). Returns
// errBodyTooLarge when the body exceeds 1 MiB so callers can distinguish
// "oversized" (→ 413) from "bad JSON" (→ 400); reading limit+1 bytes makes
// the exact-1-MiB case round up to "too large" — acceptable off-by-one.
func decodeProto(r io.Reader, m proto.Message) error {
	const limit = 1 << 20
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if len(b) > limit {
		return errBodyTooLarge
	}
	return consoleUnmarshal.Unmarshal(b, m)
}
