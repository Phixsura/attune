// SPDX-License-Identifier: Apache-2.0

package jsonrpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestHandler_Success(t *testing.T) {
	d := jsonrpc.NewDispatcher()
	d.Register("ping", func(_ context.Context, _ json.RawMessage) (any, error) {
		return map[string]string{"pong": "ok"}, nil
	})

	h := jsonrpc.NewHandler(d)

	body := `{"jsonrpc":"2.0","method":"ping","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp jsonrpc.Response
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "1", resp.ID)
	assert.Nil(t, resp.Error)
}

func TestHandler_Notification(t *testing.T) {
	d := jsonrpc.NewDispatcher()
	called := false
	d.Register("notify", func(_ context.Context, _ json.RawMessage) (any, error) {
		called = true
		return nil, nil
	})

	h := jsonrpc.NewHandler(d)

	body := `{"jsonrpc":"2.0","method":"notify"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.True(t, called)
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := jsonrpc.NewHandler(jsonrpc.NewDispatcher())

	req := httptest.NewRequest(http.MethodGet, "/mcp/v1", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHandler_InvalidJSON(t *testing.T) {
	h := jsonrpc.NewHandler(jsonrpc.NewDispatcher())

	body := `{invalid json}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp jsonrpc.Response
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeParseError, resp.Error.Code)
}

func TestHandler_MethodNotFound(t *testing.T) {
	h := jsonrpc.NewHandler(jsonrpc.NewDispatcher())

	body := `{"jsonrpc":"2.0","method":"unknown","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp jsonrpc.Response
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeMethodNotFound, resp.Error.Code)
}

func TestHandler_DispatchContext(t *testing.T) {
	d := jsonrpc.NewDispatcher()
	d.Register("test", func(_ context.Context, _ json.RawMessage) (any, error) {
		return "result", nil
	})

	h := jsonrpc.NewHandler(d)

	req := ptrext.Of(jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "test",
		ID:      "1",
	})

	resp := h.DispatchContext(context.Background(), req)
	assert.Nil(t, resp.Error)
	assert.Equal(t, "result", resp.Result)
}

func TestHandler_InvalidContentType(t *testing.T) {
	h := jsonrpc.NewHandler(jsonrpc.NewDispatcher())

	body := `{"jsonrpc":"2.0","method":"test","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)

	var resp jsonrpc.Response
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeParseError, resp.Error.Code)
}

func TestHandler_ValidContentTypeWithCharset(t *testing.T) {
	d := jsonrpc.NewDispatcher()
	d.Register("ping", func(_ context.Context, _ json.RawMessage) (any, error) {
		return "pong", nil
	})

	h := jsonrpc.NewHandler(d)

	body := `{"jsonrpc":"2.0","method":"ping","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
