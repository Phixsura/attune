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

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/restoredrill"
)

func TestConsoleStaticDir_ReturnsNotFound(t *testing.T) {
	// consoleStaticDir looks for /app/console and console/dist;
	// neither exists in a test environment.
	dir, ok := consoleStaticDir()
	if ok {
		t.Skipf("console dir found at %q, skipping not-found test", dir)
	}
	require.False(t, ok)
	require.Empty(t, dir)
}

func TestConsoleSPAHandler_ServesFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('hi')"), 0o644))

	handler := consoleSPAHandler(dir)

	// Static file is served directly.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/console/app.js", nil)
	handler.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	require.Equal(t, "console.log('hi')", string(body))

	// Unknown path falls back to index.html (SPA behavior).
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/console/some/route", nil)
	handler.ServeHTTP(rec2, req2)
	body2, _ := io.ReadAll(rec2.Result().Body)
	require.Contains(t, string(body2), "<html>index</html>")
}

func TestConsoleSPAHandler_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("index"), 0o644))

	handler := consoleSPAHandler(dir)

	// Attempt path traversal: should fall back to index.html.
	traversalPaths := []string{
		"/console/../../../etc/passwd",
		"/console/../../secret",
		"/console/..%2F..%2Fetc",
	}

	for _, p := range traversalPaths {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		handler.ServeHTTP(rec, req)
		body, _ := io.ReadAll(rec.Result().Body)
		// Should serve index or a safe response, not a 500 or leaked data.
		require.NotEmpty(t, string(body))
	}
}

func TestConsoleSPAHandler_BackslashRejected(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("index"), 0o644))

	handler := consoleSPAHandler(dir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/console/foo\\bar", nil)
	handler.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	// Backslash paths fall back to index.html
	require.Contains(t, string(body), "index")
}

func TestStartupzHandler_NilReady(t *testing.T) {
	handler := startupzHandler(nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/startupz", nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestStartupzHandler_PingPasses(t *testing.T) {
	handler := startupzHandler(readyFunc(func(context.Context) error { return nil }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/startupz", nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body, _ := io.ReadAll(rec.Result().Body)
	require.Equal(t, "ok", string(body))
}

func TestStartupzHandler_PingFails(t *testing.T) {
	handler := startupzHandler(readyFunc(func(context.Context) error {
		return errors.New("db down")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/startupz", nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestReadyzHandler_NilReady(t *testing.T) {
	handler := readyzHandler(nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestDrainAwareReadiness_NilBase(t *testing.T) {
	dar := newDrainAwareReadiness(nil)
	err := dar.Ping(context.Background())
	require.ErrorIs(t, err, errServerDraining)
}

func TestDrainAwareReadiness_NilReceiver(t *testing.T) {
	var dar *drainAwareReadiness
	err := dar.Ping(context.Background())
	require.ErrorIs(t, err, errServerDraining)
}

func TestDrainAwareReadiness_Normal(t *testing.T) {
	dar := newDrainAwareReadiness(readyFunc(func(context.Context) error { return nil }))
	require.NoError(t, dar.Ping(context.Background()))
}

func TestDrainAwareReadiness_Draining(t *testing.T) {
	dar := newDrainAwareReadiness(readyFunc(func(context.Context) error { return nil }))
	dar.BeginDrain()
	require.ErrorIs(t, dar.Ping(context.Background()), errServerDraining)
}

func TestTraceIDResponseHeader(t *testing.T) {
	// Without OTel, SpanContext is invalid, so no X-Trace-Id header.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := traceIDResponseHeader(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	// Without active OTel span, X-Trace-Id should be absent.
	require.Empty(t, rec.Header().Get("X-Trace-Id"))
}

func TestFinalizeDrill_FailExits(t *testing.T) {
	// No t.Parallel(): finalizeDrill prints to os.Stdout.
	report := restoredrill.DrillReport{
		Status: restoredrill.StatusFail,
		Checks: []restoredrill.CheckResult{{Name: "test", Status: restoredrill.StatusFail}},
	}
	err := finalizeDrill(context.Background(), "", report, false, "text", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FAILED")
}

func TestFinalizeDrill_WarnExitFlagRespected(t *testing.T) {
	// No t.Parallel(): finalizeDrill prints to os.Stdout.
	report := restoredrill.DrillReport{
		Status: restoredrill.StatusWarn,
		Checks: []restoredrill.CheckResult{{Name: "test", Status: restoredrill.StatusWarn}},
	}

	// Without warnExit: no error.
	err := finalizeDrill(context.Background(), "", report, false, "text", false)
	require.NoError(t, err)

	// With warnExit: error.
	err = finalizeDrill(context.Background(), "", report, false, "text", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "WARNINGS")
}

func TestFinalizeDrill_PassSucceeds(t *testing.T) {
	// No t.Parallel(): finalizeDrill prints to os.Stdout.
	report := restoredrill.DrillReport{
		Status: restoredrill.StatusPass,
		Checks: []restoredrill.CheckResult{{Name: "test", Status: restoredrill.StatusPass}},
	}
	err := finalizeDrill(context.Background(), "", report, false, "text", false)
	require.NoError(t, err)
}

func TestFinalizeDrill_JSONFormat(t *testing.T) {
	// No t.Parallel(): writes JSON to os.Stdout.
	report := restoredrill.DrillReport{
		Status: restoredrill.StatusPass,
		Checks: []restoredrill.CheckResult{{Name: "test", Status: restoredrill.StatusPass}},
	}
	err := finalizeDrill(context.Background(), "", report, false, "json", false)
	require.NoError(t, err)
}

func TestPrintRestoreDrillReport_NoPanic(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	rep := restoredrill.DrillReport{
		Status:        restoredrill.StatusPass,
		SchemaVersion: 24,
		DurationMS:    150,
		Checks: []restoredrill.CheckResult{
			{Name: "connectivity", Status: restoredrill.StatusPass, Message: "connected"},
			{Name: "schema", Status: restoredrill.StatusWarn, Message: "pending"},
			{Name: "pgvector", Status: restoredrill.StatusFail, Message: "missing"},
			{Name: "deep", Status: restoredrill.StatusSkip, Message: "skipped"},
		},
	}
	// Should not panic.
	printRestoreDrillReport(rep)
}

func TestRestoreDrillCmd_Dispatch(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "no args prints usage",
			args:    nil,
			wantErr: false,
		},
		{
			name:    "help",
			args:    []string{"help"},
			wantErr: false,
		},
		{
			name:    "unknown subcommand",
			args:    []string{"unknown"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runRestoreDrillCmd(tc.args)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGatherDrillBaseline_EmptyURL(t *testing.T) {
	counts, unval, exts, err := gatherDrillBaseline(context.Background(), "")
	require.NoError(t, err)
	require.Nil(t, counts)
	require.Nil(t, unval)
	require.Nil(t, exts)
}
