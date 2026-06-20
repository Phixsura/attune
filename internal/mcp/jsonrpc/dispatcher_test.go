// SPDX-License-Identifier: Apache-2.0

package jsonrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestDispatcher_Dispatch_Success(t *testing.T) {
	d := jsonrpc.NewDispatcher()

	d.Register("echo", func(_ context.Context, params json.RawMessage) (any, error) {
		var input struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, err
		}
		return map[string]string{"echo": input.Message}, nil
	})

	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "echo",
		Params:  json.RawMessage(`{"message":"hello"}`),
		ID:      "123",
	})

	resp := d.Dispatch(context.Background(), req)

	assert.Nil(t, resp.Error)
	assert.Equal(t, "123", resp.ID)

	result, ok := resp.Result.(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "hello", result["echo"])
}

func TestDispatcher_Dispatch_MethodNotFound(t *testing.T) {
	d := jsonrpc.NewDispatcher()

	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "unknown",
		ID:      "123",
	})

	resp := d.Dispatch(context.Background(), req)

	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeMethodNotFound, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "unknown")
}

func TestDispatcher_Dispatch_InvalidVersion(t *testing.T) {
	d := jsonrpc.NewDispatcher()

	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: "1.0",
		Method:  "test",
		ID:      "123",
	})

	resp := d.Dispatch(context.Background(), req)

	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeInvalidRequest, resp.Error.Code)
}

func TestDispatcher_Dispatch_EmptyMethod(t *testing.T) {
	d := jsonrpc.NewDispatcher()

	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "",
		ID:      "123",
	})

	resp := d.Dispatch(context.Background(), req)

	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeInvalidRequest, resp.Error.Code)
}

func TestDispatcher_Dispatch_ToolError(t *testing.T) {
	d := jsonrpc.NewDispatcher()

	d.Register("failing", func(_ context.Context, _ json.RawMessage) (any, error) {
		return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "bad input")
	})

	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "failing",
		ID:      "123",
	})

	resp := d.Dispatch(context.Background(), req)

	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeInvalidParams, resp.Error.Code)
	assert.Equal(t, "bad input", resp.Error.Message)
}

func TestDispatcher_Dispatch_InternalError(t *testing.T) {
	d := jsonrpc.NewDispatcher()

	d.Register("crashing", func(_ context.Context, _ json.RawMessage) (any, error) {
		return nil, errors.New("unexpected failure")
	})

	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "crashing",
		ID:      "123",
	})

	resp := d.Dispatch(context.Background(), req)

	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeInternalError, resp.Error.Code)
}

func TestDispatcher_HasMethod(t *testing.T) {
	d := jsonrpc.NewDispatcher()

	d.Register("exists", func(_ context.Context, _ json.RawMessage) (any, error) {
		return nil, nil
	})

	assert.True(t, d.HasMethod("exists"))
	assert.False(t, d.HasMethod("missing"))
}

func TestDispatcher_Methods(t *testing.T) {
	d := jsonrpc.NewDispatcher()

	d.Register("a", func(_ context.Context, _ json.RawMessage) (any, error) { return nil, nil })
	d.Register("b", func(_ context.Context, _ json.RawMessage) (any, error) { return nil, nil })

	methods := d.Methods()
	assert.Len(t, methods, 2)
	assert.Contains(t, methods, "a")
	assert.Contains(t, methods, "b")
}
