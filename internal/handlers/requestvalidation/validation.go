// SPDX-License-Identifier: Apache-2.0

// Package requestvalidation provides request validation utilities.
package requestvalidation

import (
	"net/http"
	"strings"
)

// ContentTypeJSON checks that the request has a JSON content type.
func ContentTypeJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			ct := r.Header.Get("Content-Type")
			if ct != "" && !strings.HasPrefix(ct, "application/json") {
				http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequireContentLength rejects requests without Content-Length.
func RequireContentLength(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			if r.ContentLength < 0 {
				http.Error(w, "Content-Length required", http.StatusLengthRequired)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// MaxContentLength rejects requests with body larger than max bytes.
func MaxContentLength(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireHeaders checks that specified headers are present.
func RequireHeaders(headers ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, h := range headers {
				if r.Header.Get(h) == "" {
					http.Error(w, "Missing required header: "+h, http.StatusBadRequest)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AcceptJSON checks that the client accepts JSON responses.
func AcceptJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		if accept != "" && accept != "*/*" && !strings.Contains(accept, "application/json") {
			http.Error(w, "Accept must include application/json", http.StatusNotAcceptable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Methods restricts allowed HTTP methods.
func Methods(allowed ...string) func(http.Handler) http.Handler {
	allowedMap := make(map[string]bool)
	for _, m := range allowed {
		allowedMap[strings.ToUpper(m)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allowedMap[r.Method] {
				w.Header().Set("Allow", strings.Join(allowed, ", "))
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
