// SPDX-License-Identifier: Apache-2.0

package auditevidence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	aerepo "github.com/Phixsura/attune/internal/repo/auditevidence"
	auditevidencesvc "github.com/Phixsura/attune/internal/service/auditevidence"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type stubService struct {
	startResult *auditevidencesvc.StartExportResult
	startErr    error
	getJob      *aerepo.ExportJob
	getErr      error
	dlBundle    *auditevidencesvc.DownloadBundle
	dlErr       error
	startActor  auditlogsvc.Actor
	dlActor     auditlogsvc.Actor
}

func (s *stubService) StartExport(_ context.Context, _ string, actor auditlogsvc.Actor, _ auditevidencesvc.ExportFilter) (*auditevidencesvc.StartExportResult, error) {
	s.startActor = actor
	return s.startResult, s.startErr
}

func (s *stubService) GetJob(_ context.Context, _, _ string) (*aerepo.ExportJob, error) {
	return s.getJob, s.getErr
}

func (s *stubService) Download(_ context.Context, _, _ string, actor auditlogsvc.Actor) (*auditevidencesvc.DownloadBundle, error) {
	s.dlActor = actor
	return s.dlBundle, s.dlErr
}

func testCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "t-1", UserID: "admin-1"}),
	})
}

func TestCreate_Success(t *testing.T) {
	svc := ptrext.Of(stubService{
		startResult: ptrext.Of(auditevidencesvc.StartExportResult{JobID: "job-1", Status: "queued"}),
	})
	h := NewHandler(svc)
	req := ptrext.Of(attunev1.CreateAuditEvidenceExportRequest{
		From: "2026-01-01T00:00:00Z",
		To:   "2026-01-31T23:59:59Z",
	})
	result, err := h.Create(testCtx(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Body.GetJobId() != "job-1" {
		t.Errorf("job_id: want job-1, got %s", result.Body.GetJobId())
	}
	if result.Body.GetStatus() != "queued" {
		t.Errorf("status: want queued, got %s", result.Body.GetStatus())
	}
	if svc.startActor.Type != "admin" || svc.startActor.ID != "admin-1" {
		t.Errorf("start actor = %+v", svc.startActor)
	}
}

func TestCreate_ServiceError(t *testing.T) {
	svc := ptrext.Of(stubService{startErr: errors.New("boom")})
	h := NewHandler(svc)
	req := ptrext.Of(attunev1.CreateAuditEvidenceExportRequest{})
	_, err := h.Create(testCtx(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected dispatcher.Error, got %T", err)
	}
	if de.Status != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", de.Status)
	}
}

func TestGet_NotFound(t *testing.T) {
	svc := ptrext.Of(stubService{getErr: auditevidencesvc.ErrJobNotFound})
	h := NewHandler(svc)
	req := ptrext.Of(attunev1.GetAuditEvidenceExportRequest{JobId: "missing"})
	_, err := h.Get(testCtx(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected dispatcher.Error, got %T", err)
	}
	if de.Status != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", de.Status)
	}
}

func TestGet_Completed(t *testing.T) {
	now := time.Now().UTC()
	expires := now.Add(72 * time.Hour)
	svc := ptrext.Of(stubService{
		getJob: ptrext.Of(aerepo.ExportJob{
			ID:              "job-1",
			TenantID:        "t-1",
			Status:          aerepo.JobCompleted,
			TotalEvents:     42,
			ArchiveFilename: "audit-evidence-t-1-20260115.zip",
			CreatedAt:       now,
			CompletedAt:     ptrext.Of(now),
			ExpiresAt:       ptrext.Of(expires),
		}),
	})
	h := NewHandler(svc)
	req := ptrext.Of(attunev1.GetAuditEvidenceExportRequest{JobId: "job-1"})
	result, err := h.Get(testCtx(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Body.GetTotalEvents() != 42 {
		t.Errorf("total_events: want 42, got %d", result.Body.GetTotalEvents())
	}
	if result.Body.GetDownloadPath() == "" {
		t.Error("expected download_path for completed job with future expiry")
	}
}

func TestGet_RewritesDownloadPathForAPIKeys(t *testing.T) {
	now := time.Now().UTC()
	expires := now.Add(72 * time.Hour)
	svc := ptrext.Of(stubService{
		getJob: ptrext.Of(aerepo.ExportJob{
			ID:        "job-1",
			TenantID:  "t-1",
			Status:    aerepo.JobCompleted,
			CreatedAt: now,
			ExpiresAt: ptrext.Of(expires),
		}),
	})
	h := NewHandler(svc)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "t-1", UserID: "apikey:1", UserType: "api_key"}),
	})

	result, err := h.Get(ctx, ptrext.Of(attunev1.GetAuditEvidenceExportRequest{JobId: "job-1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := result.Body.GetDownloadPath(); got != "/v1/audit-log/evidence/job-1/download" {
		t.Fatalf("download_path = %q", got)
	}
}

func TestDownload_NotDownloadable(t *testing.T) {
	svc := ptrext.Of(stubService{dlErr: auditevidencesvc.ErrJobNotDownloadable})
	h := NewHandler(svc)
	req := ptrext.Of(attunev1.DownloadAuditEvidenceExportRequest{JobId: "job-1"})
	_, err := h.Download(testCtx(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected dispatcher.Error, got %T", err)
	}
	if de.Status != http.StatusConflict {
		t.Errorf("status: want 409, got %d", de.Status)
	}
}

func TestDownload_FullHTTP(t *testing.T) {
	svc := ptrext.Of(stubService{
		dlBundle: ptrext.Of(auditevidencesvc.DownloadBundle{
			Data:     []byte("zipdata"),
			Filename: "test.zip",
		}),
	})
	h := NewHandler(svc)
	route := dispatcher.Bind(
		"console.auditevidence.Download",
		dispatcher.Empty(func() *attunev1.DownloadAuditEvidenceExportRequest {
			return ptrext.Of(attunev1.DownloadAuditEvidenceExportRequest{})
		}),
		h.Download,
		dispatcher.WithBinders(
			dispatcher.Param("job_id", func(req *attunev1.DownloadAuditEvidenceExportRequest, id string) {
				req.JobId = id
			}),
		),
		dispatcher.WithAuth(func(_ *http.Request, _ *attunev1.DownloadAuditEvidenceExportRequest) (*session.AuthCtx, error) {
			return ptrext.Of(session.AuthCtx{TenantID: "t-1", UserID: "admin-1"}), nil
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/audit-log/evidence/job-1/download", http.NoBody)
	req.SetPathValue("job_id", "job-1")
	rec := httptest.NewRecorder()
	route(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("content-type: want application/zip, got %s", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Error("missing Content-Disposition header")
	}
	if svc.dlActor.Type != "admin" || svc.dlActor.ID != "admin-1" {
		t.Errorf("download actor = %+v", svc.dlActor)
	}
}

func TestBuildFilter(t *testing.T) {
	tests := []struct {
		name string
		req  *attunev1.CreateAuditEvidenceExportRequest
		ok   bool
	}{
		{"empty_ok", ptrext.Of(attunev1.CreateAuditEvidenceExportRequest{}), true},
		{"valid_from_to", ptrext.Of(attunev1.CreateAuditEvidenceExportRequest{
			From: "2026-01-01T00:00:00Z", To: "2026-01-31T00:00:00Z",
		}), true},
		{"bad_from", ptrext.Of(attunev1.CreateAuditEvidenceExportRequest{From: "nope"}), false},
		{"bad_to", ptrext.Of(attunev1.CreateAuditEvidenceExportRequest{To: "nope"}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildFilter(tt.req)
			if tt.ok && err != nil {
				t.Fatalf("expected ok, got: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestMapError(t *testing.T) {
	tests := []struct {
		in         error
		wantStatus int
	}{
		{auditevidencesvc.ErrJobNotFound, http.StatusNotFound},
		{auditevidencesvc.ErrJobNotDownloadable, http.StatusConflict},
		{errors.New("other"), 0},
	}
	for _, tt := range tests {
		got := mapError(tt.in)
		var de *dispatcher.Error
		if tt.wantStatus == 0 {
			if errors.As(got, &de) {
				t.Errorf("mapError(%v) should not be dispatcher.Error", tt.in)
			}
			continue
		}
		if !errors.As(got, &de) {
			t.Fatalf("mapError(%v): expected dispatcher.Error", tt.in)
		}
		if de.Status != tt.wantStatus {
			t.Errorf("mapError(%v): want status %d, got %d", tt.in, tt.wantStatus, de.Status)
		}
	}
}
