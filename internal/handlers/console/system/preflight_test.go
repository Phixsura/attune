// ptrext:file-allow test fixtures use handler/config pointers and json decoding.
package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/preflight"
	_ "github.com/Phixsura/attune/internal/preflight/checks"
)

func TestPreflightHandler_ContentType(t *testing.T) {
	h := NewPreflightHandler(&preflight.Environment{
		Cfg: &config.Config{
			ConsoleBaseURL:    "https://example.com",
			ConsoleSessionKey: "a]sufficiently-long-session-key-32b",
			EnricherWorkers:   3,
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system/preflight", nil)
	h.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/health+json" {
		t.Errorf("Content-Type = %q; want application/health+json", ct)
	}
}

func TestPreflightHandler_ReturnsValidJSON(t *testing.T) {
	h := NewPreflightHandler(&preflight.Environment{
		Cfg: &config.Config{EnricherWorkers: 3},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system/preflight", nil)
	h.ServeHTTP(rec, req)

	var report preflight.Report
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(report.Checks) == 0 {
		t.Error("expected at least one check result")
	}
	if report.Status == "" {
		t.Error("report.status is empty")
	}
}

func TestPreflightHandler_StatusCodeMatchesReport(t *testing.T) {
	h := NewPreflightHandler(&preflight.Environment{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system/preflight", nil)
	h.ServeHTTP(rec, req)

	var report preflight.Report
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantCode := http.StatusOK
	if report.Status == preflight.StatusFail {
		wantCode = http.StatusServiceUnavailable
	}
	if rec.Code != wantCode {
		t.Errorf("status code = %d; want %d for report.status=%q", rec.Code, wantCode, report.Status)
	}
}

func TestPreflightHandler_503OnFail(t *testing.T) {
	h := NewPreflightHandler(&preflight.Environment{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system/preflight", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d; want 503", rec.Code)
	}
	var report preflight.Report
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if report.Status != preflight.StatusFail {
		t.Errorf("report.status = %q; want fail", report.Status)
	}
}
