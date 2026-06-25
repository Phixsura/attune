// SPDX-License-Identifier: Apache-2.0

// Package timeout provides request timeout middleware.
package timeout

import (
	"context"
	"net/http"
	"time"
)

// Middleware returns a middleware that enforces a request timeout.
// If the handler doesn't complete within the timeout, the context is cancelled.
// Note: This cancels the context but doesn't stop the handler — handlers must
// check ctx.Done() to exit early.
func Middleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MiddlewareWithResponse returns a middleware that enforces a request timeout
// and writes a 503 Service Unavailable if the timeout is exceeded.
func MiddlewareWithResponse(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			done := make(chan struct{})
			tw := &timeoutWriter{ResponseWriter: w} // ptrext:allow wrapper struct

			go func() {
				next.ServeHTTP(tw, r.WithContext(ctx))
				close(done)
			}()

			select {
			case <-done:
				// Handler completed normally
			case <-ctx.Done():
				// Timeout exceeded
				tw.mu.Lock()
				if !tw.written {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":"request timeout"}`))
				}
				tw.mu.Unlock()
			}
		})
	}
}

type timeoutWriter struct {
	http.ResponseWriter
	mu      syncMutex
	written bool
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.written {
		tw.written = true
		tw.ResponseWriter.WriteHeader(code)
	}
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.written {
		tw.written = true
	}
	return tw.ResponseWriter.Write(b)
}

// syncMutex is a simple mutex without importing sync to avoid circular deps.
type syncMutex struct {
	ch chan struct{}
}

func (m *syncMutex) Lock() {
	if m.ch == nil {
		m.ch = make(chan struct{}, 1)
	}
	m.ch <- struct{}{}
}

func (m *syncMutex) Unlock() {
	<-m.ch
}
