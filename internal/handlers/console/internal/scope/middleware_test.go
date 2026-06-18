package scope

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
)

func TestRequireScope_APIKeyWithScope(t *testing.T) {
	handler := RequireScope(domain.ScopeFeedbackRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/feedback", nil)
	ctx := apikey.WithAuthForTest(req.Context(), "tenant-1", "00000000-0000-0000-0000-000000000001", []domain.Scope{domain.ScopeFeedbackRead})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRequireScope_APIKeyWithoutScope(t *testing.T) {
	handler := RequireScope(domain.ScopeFeedbackRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/feedback", nil)
	ctx := apikey.WithAuthForTest(req.Context(), "tenant-1", "00000000-0000-0000-0000-000000000001", []domain.Scope{domain.ScopeIngestWrite})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestRequireScope_SessionPassThrough(t *testing.T) {
	handler := RequireScope(domain.ScopeFeedbackRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/feedback", nil)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRequireScope_WriteImpliesRead(t *testing.T) {
	handler := RequireScope(domain.ScopeFeedbackRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/feedback", nil)
	ctx := apikey.WithAuthForTest(req.Context(), "tenant-1", "00000000-0000-0000-0000-000000000001", []domain.Scope{domain.ScopeFeedbackWrite})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "write should imply read")
}
