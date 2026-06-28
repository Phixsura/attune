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
		AllowedOrigins: []string{},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept",
			"Authorization",
			"Content-Type",
			"X-Request-ID",
			"X-API-Key",
			"Idempotency-Key",
			"X-Attune-Api-Version",
		},
		ExposedHeaders:   []string{"X-Request-ID", "X-Trace-Id", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "X-Attune-Api-Version", "Deprecation", "Sunset"},
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
	exposed := strings.Join(cfg.ExposedHeaders, ", ")
	maxAge := strconv.Itoa(cfg.MaxAge)
	varyByOrigin := !allowAll || cfg.AllowCredentials

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			if varyByOrigin {
				addVary(h, "Origin")
			}

			// Check if origin is allowed
			allowed := allowAll || allowedOrigins[origin]
			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			// Set CORS headers
			if allowAll && !cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
			}

			if cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}

			if exposed != "" {
				h.Set("Access-Control-Expose-Headers", exposed)
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				addVary(h, "Access-Control-Request-Method", "Access-Control-Request-Headers")
				h.Set("Access-Control-Allow-Methods", methods)
				h.Set("Access-Control-Allow-Headers", mergeAllowedHeaders(cfg.AllowedHeaders, r.Header.Get("Access-Control-Request-Headers")))
				h.Set("Access-Control-Max-Age", maxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func mergeAllowedHeaders(allowed []string, requested string) string {
	if strings.TrimSpace(requested) == "" {
		return strings.Join(allowed, ", ")
	}
	out := make([]string, 0, len(allowed)+4)
	seen := make(map[string]struct{}, len(allowed)+4)
	for _, header := range allowed {
		trimmed := strings.TrimSpace(header)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	for _, raw := range strings.Split(requested, ",") {
		trimmed := strings.TrimSpace(raw)
		if !isHeaderToken(trimmed) {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return strings.Join(out, ", ")
}

func isHeaderToken(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b <= 32 || b >= 127 {
			return false
		}
		switch b {
		case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}':
			return false
		}
	}
	return true
}

func addVary(h http.Header, names ...string) {
	for _, name := range names {
		if name == "" {
			continue
		}
		for _, existing := range h.Values("Vary") {
			for _, part := range strings.Split(existing, ",") {
				if strings.EqualFold(strings.TrimSpace(part), name) {
					goto next
				}
			}
		}
		h.Add("Vary", name)
	next:
	}
}
