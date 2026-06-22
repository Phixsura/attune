// SPDX-License-Identifier: Apache-2.0

package jsonrpc_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRequest_IsNotification(t *testing.T) {
	tests := []struct {
		name           string
		id             any
		isNotification bool
	}{
		{"with string id", "123", false},
		{"with int id", 123, false},
		{"with nil id", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ptrext.Of(jsonrpc.Request{ID: tt.id})
			assert.Equal(t, tt.isNotification, req.IsNotification())
		})
	}
}

func TestNewResponse(t *testing.T) {
	resp := jsonrpc.NewResponse("123", map[string]string{"key": "value"})

	assert.Equal(t, jsonrpc.Version, resp.JSONRPC)
	assert.Equal(t, "123", resp.ID)
	assert.NotNil(t, resp.Result)
	assert.Nil(t, resp.Error)

	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"jsonrpc":"2.0"`)
	assert.Contains(t, string(data), `"id":"123"`)
}

func TestNewErrorResponse(t *testing.T) {
	resp := jsonrpc.NewErrorResponse("123", jsonrpc.CodeInvalidParams, "bad params", nil)

	assert.Equal(t, jsonrpc.Version, resp.JSONRPC)
	assert.Equal(t, "123", resp.ID)
	assert.Nil(t, resp.Result)
	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeInvalidParams, resp.Error.Code)
	assert.Equal(t, "bad params", resp.Error.Message)
}

func TestParseError(t *testing.T) {
	resp := jsonrpc.ParseError("invalid json")

	assert.Nil(t, resp.ID)
	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeParseError, resp.Error.Code)
}

func TestMethodNotFound(t *testing.T) {
	resp := jsonrpc.MethodNotFound("123", "unknown_tool")

	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeMethodNotFound, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "unknown_tool")
}

func TestError_Error(t *testing.T) {
	err := ptrext.Of(jsonrpc.Error{Code: -32600, Message: "test error"})
	assert.Contains(t, err.Error(), "-32600")
	assert.Contains(t, err.Error(), "test error")
}
