// SPDX-License-Identifier: Apache-2.0

// Package etag provides ETag generation and conditional request handling.
package etag

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// Generate creates an ETag from content bytes.
func Generate(content []byte) string {
	h := sha256.Sum256(content)
	return `"` + hex.EncodeToString(h[:8]) + `"`
}

// GenerateWeak creates a weak ETag from content bytes.
func GenerateWeak(content []byte) string {
	h := sha256.Sum256(content)
	return `W/"` + hex.EncodeToString(h[:8]) + `"`
}

// Match checks if the request's If-None-Match header matches the ETag.
// Returns true if the client's cached version is still valid.
func Match(r *http.Request, etag string) bool {
	ifNoneMatch := r.Header.Get("If-None-Match")
	if ifNoneMatch == "" {
		return false
	}

	// Handle * wildcard
	if ifNoneMatch == "*" {
		return true
	}

	// Parse comma-separated ETags
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		// Strip W/ prefix for weak comparison
		candidate = strings.TrimPrefix(candidate, "W/")
		compareEtag := strings.TrimPrefix(etag, "W/")
		if candidate == compareEtag {
			return true
		}
	}

	return false
}

// MatchStrong checks if the request's If-Match header matches the ETag.
// Used for conditional updates (PUT/PATCH).
func MatchStrong(r *http.Request, etag string) bool {
	ifMatch := r.Header.Get("If-Match")
	if ifMatch == "" {
		return true // No condition specified
	}

	// Handle * wildcard
	if ifMatch == "*" {
		return true
	}

	// Weak ETags cannot be used with If-Match (strong comparison)
	if strings.HasPrefix(etag, "W/") {
		return false
	}

	for _, candidate := range strings.Split(ifMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate, "W/") {
			continue // Skip weak ETags
		}
		if candidate == etag {
			return true
		}
	}

	return false
}

// SetHeader sets the ETag header on the response.
func SetHeader(w http.ResponseWriter, etag string) {
	w.Header().Set("ETag", etag)
}

// NotModified sends a 304 Not Modified response.
func NotModified(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotModified)
}

// PreconditionFailed sends a 412 Precondition Failed response.
func PreconditionFailed(w http.ResponseWriter) {
	w.WriteHeader(http.StatusPreconditionFailed)
}
