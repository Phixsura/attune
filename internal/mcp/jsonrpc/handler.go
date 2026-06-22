// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Handler is an HTTP handler for JSON-RPC over Streamable HTTP.
type Handler struct {
	dispatcher *Dispatcher
}

// NewHandler creates a new JSON-RPC HTTP handler.
func NewHandler(dispatcher *Dispatcher) *Handler {
	return ptrext.Of(Handler{dispatcher: dispatcher})
}

// ServeHTTP handles JSON-RPC requests via POST.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, ParseError("content-type must be application/json"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		logext.Warnf(ctx, "mcp read body error: %v", err)
		writeJSON(w, http.StatusBadRequest, ParseError("failed to read request body"))
		return
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		logext.Warnf(ctx, "mcp parse error: %v", err)
		writeJSON(w, http.StatusBadRequest, ParseError("invalid JSON"))
		return
	}

	resp := h.dispatcher.Dispatch(ctx, ptrext.Of(req))

	if req.IsNotification() {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	status := http.StatusOK
	if resp.Error != nil {
		status = errorToHTTPStatus(resp.Error.Code)
	}

	writeJSON(w, status, resp)
}

// DispatchContext dispatches a request with the given context.
func (h *Handler) DispatchContext(ctx context.Context, req *Request) *Response {
	return h.dispatcher.Dispatch(ctx, req)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func errorToHTTPStatus(code int) int {
	switch code {
	case CodeParseError, CodeInvalidRequest:
		return http.StatusBadRequest
	case CodeMethodNotFound:
		return http.StatusNotFound
	case CodeInvalidParams:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
