// SPDX-License-Identifier: Apache-2.0

// Package ratelimitheaders provides middleware for rate limit response headers.
package ratelimitheaders

import (
	"net/http"
	"strconv"
	"time"
)

// Headers contains rate limit information for response headers.
type Headers struct {
	Limit      int       // Maximum requests allowed
	Remaining  int       // Requests remaining in window
	Reset      time.Time // When the window resets
	RetryAfter int       // Seconds to wait (only set when limited)
}

// Write adds standard rate limit headers to the response.
// Follows draft-ietf-httpapi-ratelimit-headers specification.
func (h Headers) Write(w http.ResponseWriter) {
	header := w.Header()
	header.Set("X-RateLimit-Limit", strconv.Itoa(h.Limit))
	header.Set("X-RateLimit-Remaining", strconv.Itoa(h.Remaining))
	header.Set("X-RateLimit-Reset", strconv.FormatInt(h.Reset.Unix(), 10))

	// RateLimit header (newer standard)
	header.Set("RateLimit-Limit", strconv.Itoa(h.Limit))
	header.Set("RateLimit-Remaining", strconv.Itoa(h.Remaining))
	header.Set("RateLimit-Reset", strconv.FormatInt(h.Reset.Unix(), 10))

	if h.RetryAfter > 0 {
		header.Set("Retry-After", strconv.Itoa(h.RetryAfter))
	}
}

// ContextKey is the context key for rate limit headers.
type ContextKey struct{}

// FromContext extracts rate limit headers from context.
func FromContext(ctx interface{ Value(any) any }) *Headers {
	if v := ctx.Value(ContextKey{}); v != nil {
		if h, ok := v.(*Headers); ok {
			return h
		}
	}
	return nil
}
