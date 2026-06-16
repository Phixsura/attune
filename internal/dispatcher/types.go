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

// ErrBodyTooLarge is returned when a dispatcher binder reads past its body cap.
var ErrBodyTooLarge = errors.New("dispatcher: request body exceeds size limit")

var decode = protojson.UnmarshalOptions{DiscardUnknown: true}

// RequestContext carries the underlying request context plus the auth
// payload for the current endpoint family.
type RequestContext[Auth any] struct {
	context.Context
	Auth     Auth
	response http.ResponseWriter
	request  *http.Request
}

// SetCookie allows handlers to attach cookie side-effects while keeping
// response body/status writing owned by dispatcher.
func (c *RequestContext[Auth]) SetCookie(cookie *http.Cookie) {
	http.SetCookie(c.response, cookie)
}

// SetHeader allows handlers to attach header side-effects while keeping
// response body/status writing owned by dispatcher.
func (c *RequestContext[Auth]) SetHeader(key, value string) {
	c.response.Header().Set(key, value)
}

// Request returns the underlying HTTP request when the handler was invoked via
// dispatcher.Bind. Unit tests that construct RequestContext directly may leave
// it nil.
func (c *RequestContext[Auth]) Request() *http.Request {
	return c.request
}

// Result carries the successful response payload and HTTP status.
type Result[Resp proto.Message] struct {
	Status int
	Body   Resp
}

// Success constructs a successful response with an explicit HTTP status.
func Success[Resp proto.Message](status int, body Resp) (Result[Resp], error) {
	return Result[Resp]{Status: status, Body: body}, nil
}

// OK constructs a 200 response.
func OK[Resp proto.Message](body Resp) (Result[Resp], error) {
	return Success(http.StatusOK, body)
}

// Created constructs a 201 response.
func Created[Resp proto.Message](body Resp) (Result[Resp], error) {
	return Success(http.StatusCreated, body)
}

// NoContent constructs a 204 response.
func NoContent[Resp proto.Message]() (Result[Resp], error) {
	return Result[Resp]{Status: http.StatusNoContent}, nil
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

// Fail constructs a typed handler error and the zero success result.
func Fail[Resp proto.Message](status int, code attunev1.ErrorCode, msg string) (Result[Resp], error) {
	return Result[Resp]{}, NewError(status, code, msg)
}

// decodeJSON reads a protoJSON request body with the shared 1 MiB limit.
func decodeJSON[Req proto.Message](r io.Reader, m Req) error {
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
