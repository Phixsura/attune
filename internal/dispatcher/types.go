// Package dispatcher centralizes the HTTP handler boilerplate that is
// repeated across attune's product endpoints: request decoding, auth
// extraction, success/error envelope writing, and the common logext
// entry/exit pattern.
package dispatcher

import (
	"context"
	"errors"
	"io"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// ErrBodyTooLarge is returned when DecodeJSON reads more than the 1 MiB cap.
var ErrBodyTooLarge = errors.New("dispatcher: request body exceeds 1 MiB")

var decode = protojson.UnmarshalOptions{DiscardUnknown: true}

// RequestContext carries the underlying request context plus the auth
// payload for the current endpoint family.
type RequestContext[Auth any] struct {
	context.Context
	Auth     Auth
	response http.ResponseWriter
}

// Response exposes the response writer for narrow side-effects such as setting
// or clearing cookies. Response body/status writing stays owned by dispatcher.
func (c *RequestContext[Auth]) Response() http.ResponseWriter {
	return c.response
}

// Result carries the successful response payload and HTTP status.
type Result[Resp proto.Message] struct {
	Status int
	Body   Resp
}

// OK constructs a successful response.
func OK[Resp proto.Message](status int, body Resp) Result[Resp] {
	return Result[Resp]{Status: status, Body: body}
}

// NoContent constructs a 204 response.
func NoContent[Resp proto.Message]() Result[Resp] {
	return Result[Resp]{Status: http.StatusNoContent}
}

// Error carries the HTTP status plus the machine-readable envelope code.
type Error struct {
	Status  int
	Code    attunev1.ErrorCode
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

// NewError creates a typed handler error that the dispatcher can map into the
// shared ErrorResponse envelope.
func NewError(status int, code attunev1.ErrorCode, msg string) *Error {
	return ptrext.Of(Error{Status: status, Code: code, Message: msg})
}

// DecodeJSON reads a protoJSON request body with the shared 1 MiB limit.
func DecodeJSON[Req proto.Message](r io.Reader, m Req) error {
	const limit = 1 << 20
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if len(b) > limit {
		return ErrBodyTooLarge
	}
	return decode.Unmarshal(b, m)
}
