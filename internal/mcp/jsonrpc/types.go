// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"encoding/json"
	"fmt"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const Version = "2.0"

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id,omitempty"`
}

// IsNotification returns true if this is a notification (no ID).
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
	ID      any    `json:"id"`
}

// Error is a JSON-RPC 2.0 error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// NewResponse creates a success response.
func NewResponse(id any, result any) *Response {
	return ptrext.Of(Response{
		JSONRPC: Version,
		Result:  result,
		ID:      id,
	})
}

// NewErrorResponse creates an error response.
func NewErrorResponse(id any, code int, message string, data any) *Response {
	return ptrext.Of(Response{
		JSONRPC: Version,
		Error: ptrext.Of(Error{
			Code:    code,
			Message: message,
			Data:    data,
		}),
		ID: id,
	})
}

// ParseError creates a parse error response (no ID since we couldn't parse).
func ParseError(message string) *Response {
	return NewErrorResponse(nil, CodeParseError, message, nil)
}

// InvalidRequest creates an invalid request error response.
func InvalidRequest(id any, message string) *Response {
	return NewErrorResponse(id, CodeInvalidRequest, message, nil)
}

// MethodNotFound creates a method not found error response.
func MethodNotFound(id any, method string) *Response {
	return NewErrorResponse(id, CodeMethodNotFound, fmt.Sprintf("method not found: %s", method), nil)
}

// InvalidParams creates an invalid params error response.
func InvalidParams(id any, message string) *Response {
	return NewErrorResponse(id, CodeInvalidParams, message, nil)
}

// InternalError creates an internal error response.
func InternalError(id any, message string) *Response {
	return NewErrorResponse(id, CodeInternalError, message, nil)
}
