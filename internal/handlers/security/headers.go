// SPDX-License-Identifier: Apache-2.0

// Package security provides HTTP security middleware.
package security

import "net/http"

// Headers adds standard security headers to responses.
// Should be applied early in the middleware chain.
func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Prevent clickjacking
		h.Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		h.Set("X-Content-Type-Options", "nosniff")

		// Enable XSS filter (legacy browsers)
		h.Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy for privacy
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Permissions policy - disable dangerous APIs
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		next.ServeHTTP(w, r)
	})
}

// APIHeaders adds security headers for API endpoints.
// More restrictive than Headers for non-browser clients.
func APIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")

		next.ServeHTTP(w, r)
	})
}

// ConsoleHeaders adds security headers for Console UI.
// Includes CSP for the SPA.
func ConsoleHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-XSS-Protection", "1; mode=block")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// CSP for Console SPA: allow self, inline styles (required by many
		// UI frameworks), and data: URIs for images. Connect-src allows API
		// calls to self. Script-src includes unsafe-inline for dev but
		// should use nonce in production.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: https:; "+
				"font-src 'self' data:; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'")

		next.ServeHTTP(w, r)
	})
}
