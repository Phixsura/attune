// SPDX-License-Identifier: Apache-2.0

// Package cors provides Cross-Origin Resource Sharing middleware.
package cors

import (
	"net/http"
	"strconv"
	"strings"
)

// Config configures CORS behavior.
type Config struct {
	// AllowedOrigins is a list of origins allowed to access the resource.
	// Use "*" to allow all origins (not recommended for production).
	AllowedOrigins []string

	// AllowedMethods is a list of methods allowed when accessing the resource.
	AllowedMethods []string

	// AllowedHeaders is a list of headers allowed in actual requests.
	AllowedHeaders []string

	// ExposedHeaders is a list of headers safe to expose to the API.
	ExposedHeaders []string

	// AllowCredentials indicates whether credentials can be included.
	AllowCredentials bool

	// MaxAge indicates how long preflight results can be cached (seconds).
	MaxAge int
}

// DefaultConfig returns a sensible default CORS configuration.
func DefaultConfig() Config {
	return Config{
		AllowedOrigins:   []string{},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID", "X-Trace-Id", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		AllowCredentials: false,
		MaxAge:           86400, // 24 hours
	}
}

// Middleware returns a CORS middleware handler.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	allowedOrigins := make(map[string]bool)
	allowAll := false
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
		} else {
			allowedOrigins[o] = true
		}
	}

	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")
	exposed := strings.Join(cfg.ExposedHeaders, ", ")
	maxAge := strconv.Itoa(cfg.MaxAge)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Check if origin is allowed
			allowed := allowAll || allowedOrigins[origin]
			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()

			// Set CORS headers
			if allowAll {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
			}

			if cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}

			if exposed != "" {
				h.Set("Access-Control-Expose-Headers", exposed)
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", methods)
				h.Set("Access-Control-Allow-Headers", headers)
				h.Set("Access-Control-Max-Age", maxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
