// Package respond is the low-level encoder behind dispatcher-owned HTTP
// responses: protoJSON for typed bodies, and the unified ErrorResponse
// {code, message, requestId} for any 4xx/5xx.
//
// Production handlers and middleware should use internal/dispatcher instead of
// calling this package directly; respond stays separate so dispatcher can keep
// the concrete serialization code small and testable.
package respond

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// marshal is the canonical protojson encoder for every typed response
// attune emits. EmitUnpopulated=true is the load-bearing setting: by
// default protojson OMITS zero-valued fields, so a `ListFooResponse{
// Items: nil }` serialises as `{}` (no `items` key), and a SPA query
// fn that does `return resp.items` ends up with `undefined` and breaks
// react-query's "data cannot be undefined" invariant. Emitting the
// zero value (`"items": []`, `"buckets": []`, `"count": 0`) instead
// keeps the wire shape stable across empty vs populated tenants —
// which is what every list / stats page in the console assumes.
//
// proto3 explicit `optional` fields (`optional string foo`) are NOT
// affected: their underlying oneof is intentionally exempt from
// EmitUnpopulated, so genuinely nullable fields stay omitted.
var marshal = protojson.MarshalOptions{EmitUnpopulated: true}

// Proto writes a proto message as protoJSON — the wire contract for
// every proto-defined endpoint (#19). Field names are lowerCamelCase
// and int64s serialize as JSON strings, per the protoJSON spec.
// Empty repeated / message / scalar fields emit their zero value so
// the wire shape is stable for SPA consumers (see `marshal` above).
func Proto(w http.ResponseWriter, status int, m proto.Message) {
	b, err := marshal.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// Error writes the unified ErrorResponse {code, message, requestId}.
// requestId is pulled from the chi RequestID middleware so support
// engineers can grep one id across logs / customer reports / traces.
//
// `code` is the proto-defined ErrorCode enum (see proto/attune/v1/common.proto);
// its enum name is emitted as the stable wire string. Every customer-facing
// HTTP error in attune flows through this single function — the previous
// {"error":"..."} shape from apikey middleware and similar ad-hoc handlers is
// the bug class this exists to prevent.
func Error(ctx context.Context, w http.ResponseWriter, status int, code attunev1.ErrorCode, message string) {
	Proto(w, status, ptrext.Of(attunev1.ErrorResponse{
		Code:      code.String(),
		Message:   message,
		RequestId: middleware.GetReqID(ctx),
	}))
}
