// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"net/http"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
)

const (
	mcpToolResultOK          = "ok"
	mcpToolResultClientError = "client_error"
	mcpToolResultDenied      = "denied"
	mcpToolResultRateLimited = "rate_limited"
	mcpToolResultInternalErr = "internal_error"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *responseRecorder) WriteHeader(status int) {
	if !r.wrote {
		r.status = status
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(p)
}

func (r *responseRecorder) Status() int {
	if r.wrote {
		return r.status
	}
	return http.StatusOK
}

func recordMCPToolCall(tenantID, toolName, result string, started time.Time, observeLatency bool) {
	tenantID = normalizeMetricLabel(tenantID)
	toolName = normalizeMetricLabel(toolName)
	result = normalizeMetricLabel(result)

	metrics.MCPToolCallsTotal.WithLabelValues(tenantID, toolName, result).Inc()
	if observeLatency {
		metrics.MCPToolLatency.WithLabelValues(tenantID, toolName).Observe(time.Since(started).Seconds())
	}
}

func normalizeMetricLabel(v string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func classifyMCPResult(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return mcpToolResultRateLimited
	case status == http.StatusForbidden:
		return mcpToolResultDenied
	case status >= 200 && status < 300:
		return mcpToolResultOK
	case status >= 400 && status < 500:
		return mcpToolResultClientError
	default:
		return mcpToolResultInternalErr
	}
}
