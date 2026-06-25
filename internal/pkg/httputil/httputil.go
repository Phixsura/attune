// SPDX-License-Identifier: Apache-2.0

// Package httputil provides HTTP utilities.
package httputil

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]string{
		"error":   code,
		"message": message,
	})
}

// WriteNoContent writes a 204 No Content response.
func WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// IsSuccess returns true if the status code indicates success (2xx).
func IsSuccess(status int) bool {
	return status >= 200 && status < 300
}

// IsClientError returns true if the status code indicates a client error (4xx).
func IsClientError(status int) bool {
	return status >= 400 && status < 500
}

// IsServerError returns true if the status code indicates a server error (5xx).
func IsServerError(status int) bool {
	return status >= 500 && status < 600
}

// IsRetryable returns true if the status code indicates a retryable error.
func IsRetryable(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		http.StatusBadGateway:
		return true
	default:
		return false
	}
}

// StatusText returns the text for the HTTP status code.
// Wrapper around http.StatusText for convenience.
func StatusText(code int) string {
	return http.StatusText(code)
}
