// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"
)

// ---------------------------------------------------------------------------
// consoleSPAHandler
// ---------------------------------------------------------------------------

func TestConsoleSPAHandler(t *testing.T) {
	dir := t.TempDir()
	// Create index.html and a static asset.
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>SPA"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	handler := consoleSPAHandler(dir)

	readBody := func(t *testing.T, rec *httptest.ResponseRecorder) string {
		t.Helper()
		b, err := io.ReadAll(rec.Result().Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return string(b)
	}

	t.Run("serves existing static file", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/console/app.js", nil)
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body := readBody(t, rec)
		if body != "console.log('ok')" {
			t.Fatalf("body = %q, want app.js content", body)
		}
	})

	t.Run("SPA fallback for unknown path", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/console/settings", nil)
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body := readBody(t, rec)
		if body != "<!doctype html>SPA" {
			t.Fatalf("body = %q, want index.html content", body)
		}
	})

	t.Run("path traversal blocked via absolute clean", func(t *testing.T) {
		// filepath.Clean("/foo/../bar") resolves to "/bar" which is
		// absolute. consoleSPAHandler detects filepath.IsAbs and falls
		// back to index.html. Simulate by injecting a URL.Path whose
		// TrimPrefix("console/") result cleans to an absolute path.
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/console/foo", nil)
		req.URL.Path = "/console//etc/passwd"
		handler.ServeHTTP(rec, req)

		body := readBody(t, rec)
		// The handler must NOT serve /etc/passwd content; it should fall
		// back to index.html because the cleaned path is suspicious.
		if body != "<!doctype html>SPA" {
			t.Fatalf("traversal not blocked: body = %q, want index.html", body)
		}
	})

	t.Run("dotdot prefix in relative path serves index.html", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/console/foo", nil)
		req.URL.Path = "/console/..hidden"
		handler.ServeHTTP(rec, req)

		body := readBody(t, rec)
		if body != "<!doctype html>SPA" {
			t.Fatalf("dotdot prefix not blocked: body = %q, want index.html", body)
		}
	})

	t.Run("backslash in path serves index.html", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/console/foo\\bar", nil)
		handler.ServeHTTP(rec, req)

		body := readBody(t, rec)
		if body != "<!doctype html>SPA" {
			t.Fatalf("backslash not blocked: body = %q, want index.html", body)
		}
	})

	t.Run("clean dot path serves index.html", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/console/.", nil)
		handler.ServeHTTP(rec, req)

		body := readBody(t, rec)
		if body != "<!doctype html>SPA" {
			t.Fatalf("dot path not handled: body = %q, want index.html", body)
		}
	})
}

func TestConsoleStaticDirAndMount(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	dist := filepath.Join(root, "console", "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatalf("mkdir console dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<!doctype html>mounted"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "app.js"), []byte("console.log('mounted')"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	dir, ok := consoleStaticDir()
	if !ok {
		t.Fatal("consoleStaticDir did not find console/dist")
	}
	if dir == "console/dist" {
		r := chi.NewRouter()
		mountConsoleStatic(context.Background(), r)

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/console", nil))
		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("/console status = %d, want %d", rec.Code, http.StatusMovedPermanently)
		}
		if got := rec.Header().Get("Location"); got != "/console/" {
			t.Fatalf("Location = %q, want /console/", got)
		}

		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/console/app.js", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("/console/app.js status = %d, want %d", rec.Code, http.StatusOK)
		}
		body, err := io.ReadAll(rec.Result().Body)
		if err != nil {
			t.Fatalf("read app.js body: %v", err)
		}
		if got := string(body); got != "console.log('mounted')" {
			t.Fatalf("app.js body = %q, want mounted asset", got)
		}
	}
}

// ---------------------------------------------------------------------------
// startupzHandler
// ---------------------------------------------------------------------------

func TestStartupzHandler(t *testing.T) {
	t.Run("returns 200 when ping succeeds", func(t *testing.T) {
		h := startupzHandler(readyFunc(func(context.Context) error { return nil }))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/startupz", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body, err := io.ReadAll(rec.Result().Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != "ok" {
			t.Fatalf("body = %q, want %q", got, "ok")
		}
	})

	t.Run("returns 503 when ping fails", func(t *testing.T) {
		h := startupzHandler(readyFunc(func(context.Context) error {
			return errors.New("not ready yet")
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/startupz", nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("returns 503 when ready is nil", func(t *testing.T) {
		h := startupzHandler(nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/startupz", nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}

// ---------------------------------------------------------------------------
// traceIDResponseHeader
// ---------------------------------------------------------------------------

func TestTraceIDResponseHeader_Coverage(t *testing.T) {
	echoHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("no header without OTel span", func(t *testing.T) {
		wrapped := traceIDResponseHeader(echoHandler)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		wrapped.ServeHTTP(rec, req)

		if got := rec.Header().Get("X-Trace-Id"); got != "" {
			t.Fatalf("X-Trace-Id = %q, want empty", got)
		}
	})

	t.Run("header present with valid OTel span", func(t *testing.T) {
		traceID, err := trace.TraceIDFromHex("00000000000000000000000000000001")
		if err != nil {
			t.Fatalf("TraceIDFromHex: %v", err)
		}
		spanID, err := trace.SpanIDFromHex("0000000000000001")
		if err != nil {
			t.Fatalf("SpanIDFromHex: %v", err)
		}
		sc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: trace.FlagsSampled,
		})
		ctx := trace.ContextWithSpanContext(context.Background(), sc)

		wrapped := traceIDResponseHeader(echoHandler)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		wrapped.ServeHTTP(rec, req)

		got := rec.Header().Get("X-Trace-Id")
		if got != "00000000000000000000000000000001" {
			t.Fatalf("X-Trace-Id = %q, want %q", got, "00000000000000000000000000000001")
		}
	})
}

// ---------------------------------------------------------------------------
// drainAwareReadiness edge cases
// ---------------------------------------------------------------------------

func TestDrainAwareReadiness_EdgeCases(t *testing.T) {
	t.Run("nil receiver returns errServerDraining", func(t *testing.T) {
		var d *drainAwareReadiness
		err := d.Ping(context.Background())
		if !errors.Is(err, errServerDraining) {
			t.Fatalf("err = %v, want errServerDraining", err)
		}
	})

	t.Run("nil base returns errServerDraining", func(t *testing.T) {
		d := newDrainAwareReadiness(nil)
		err := d.Ping(context.Background())
		if !errors.Is(err, errServerDraining) {
			t.Fatalf("err = %v, want errServerDraining", err)
		}
	})
}
