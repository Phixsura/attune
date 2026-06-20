// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ToolFunc is a function that handles an MCP tool invocation.
type ToolFunc func(ctx context.Context, params json.RawMessage) (any, error)

// ToolError is returned when a tool encounters an error with a specific code.
type ToolError struct {
	Code    int
	Message string
	Data    any
}

func (e *ToolError) Error() string {
	return e.Message
}

// NewToolError creates a tool error with the given code and message.
func NewToolError(code int, message string) *ToolError {
	return ptrext.Of(ToolError{Code: code, Message: message})
}

// Dispatcher routes JSON-RPC requests to registered tools.
type Dispatcher struct {
	tools map[string]ToolFunc
}

// NewDispatcher creates a new dispatcher.
func NewDispatcher() *Dispatcher {
	return ptrext.Of(Dispatcher{tools: make(map[string]ToolFunc)})
}

// Register adds a tool handler.
func (d *Dispatcher) Register(name string, fn ToolFunc) {
	d.tools[name] = fn
}

// Dispatch routes a request to the appropriate tool.
func (d *Dispatcher) Dispatch(ctx context.Context, req *Request) *Response {
	if req.JSONRPC != Version {
		return InvalidRequest(req.ID, "invalid jsonrpc version")
	}

	if req.Method == "" {
		return InvalidRequest(req.ID, "method is required")
	}

	fn, ok := d.tools[req.Method]
	if !ok {
		return MethodNotFound(req.ID, req.Method)
	}

	result, err := fn(ctx, req.Params)
	if err != nil {
		var toolErr *ToolError
		if errors.As(err, &toolErr) {
			return NewErrorResponse(req.ID, toolErr.Code, toolErr.Message, toolErr.Data)
		}
		return InternalError(req.ID, err.Error())
	}

	return NewResponse(req.ID, result)
}

// HasMethod returns true if the method is registered.
func (d *Dispatcher) HasMethod(method string) bool {
	_, ok := d.tools[method]
	return ok
}

// Methods returns all registered method names.
func (d *Dispatcher) Methods() []string {
	names := make([]string, 0, len(d.tools))
	for name := range d.tools {
		names = append(names, name)
	}
	return names
}
