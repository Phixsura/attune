package console

import (
	"context"
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
