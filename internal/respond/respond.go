// Package respond is the single canonical home for the HTTP response
// shapes attune emits everywhere: protoJSON for typed bodies, and the
// unified ErrorResponse {code, message, requestId} for any 4xx/5xx.
//
// Lives at internal/ (not under handlers/) so that both handler
// subpackages AND infra-layer middlewares (apikey, future rate-limit)
// can call it — every customer-facing error then shares one shape,
// closing the gap E2E discovered with apikey middleware (#19 follow-up).
//
// handlers/console/internal/respond is a thin re-export of this package
// so existing console handlers don't need to change their imports.
package respond

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Proto writes a proto message as protoJSON — the wire contract for
// every proto-defined endpoint (#19). Field names are lowerCamelCase
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

// Error writes the unified ErrorResponse {code, message, requestId}.
// requestId is pulled from the chi RequestID middleware so support
// engineers can grep one id across logs / customer reports / traces.
//
// Every customer-facing HTTP error in attune flows through this single
// function — the previous {"error":"..."} shape from apikey middleware
// and similar ad-hoc handlers is the bug class this exists to prevent.
func Error(ctx context.Context, w http.ResponseWriter, status int, code, message string) {
	Proto(w, status, &attunev1.ErrorResponse{
		Code:      code,
		Message:   message,
		RequestId: middleware.GetReqID(ctx),
	})
}
