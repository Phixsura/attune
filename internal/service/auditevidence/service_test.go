// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test stub fixtures use address-of for interface satisfaction

package auditevidence

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	aerepo "github.com/Phixsura/attune/internal/repo/auditevidence"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type stubJobStore struct {
	createJobFn    func(ctx context.Context, tenantID, createdBy string, filterJSON json.RawMessage) (*aerepo.ExportJob, error)
	getJobFn       func(ctx context.Context, tenantID, jobID string) (*aerepo.ExportJob, error)
	markDownloadFn func(ctx context.Context, tenantID, jobID string) (*aerepo.ExportJob, error)
	claimNextJobFn func(ctx context.Context, owner string) (*aerepo.ExportJob, error)
	heartbeatJobFn func(ctx context.Context, jobID, owner string) (int64, error)
	completeJobFn  func(ctx context.Context, jobID, owner, archiveFilename string, archive []byte, totalEvents int, expiresAt time.Time) (int64, error)
	failJobFn      func(ctx context.Context, jobID, owner, errMsg string) (int64, error)
	expireJobsFn   func(ctx context.Context, now time.Time) (int64, error)
	requeueStaleFn func(ctx context.Context, staleThreshold time.Duration) (int64, error)
}

func (s *stubJobStore) CreateJob(ctx context.Context, tenantID, createdBy string, filterJSON json.RawMessage) (*aerepo.ExportJob, error) {
	return s.createJobFn(ctx, tenantID, createdBy, filterJSON)
}

func (s *stubJobStore) GetJob(ctx context.Context, tenantID, jobID string) (*aerepo.ExportJob, error) {
	return s.getJobFn(ctx, tenantID, jobID)
}

func (s *stubJobStore) MarkDownloaded(ctx context.Context, tenantID, jobID string) (*aerepo.ExportJob, error) {
	return s.markDownloadFn(ctx, tenantID, jobID)
}

func (s *stubJobStore) ClaimNextJob(ctx context.Context, owner string) (*aerepo.ExportJob, error) {
	if s.claimNextJobFn != nil {
		return s.claimNextJobFn(ctx, owner)
	}
	return nil, nil
}

func (s *stubJobStore) HeartbeatJob(ctx context.Context, jobID, owner string) (int64, error) {
	if s.heartbeatJobFn != nil {
		return s.heartbeatJobFn(ctx, jobID, owner)
	}
	return 1, nil
}

func (s *stubJobStore) CompleteJob(ctx context.Context, jobID, owner, archiveFilename string, archive []byte, totalEvents int, expiresAt time.Time) (int64, error) {
	if s.completeJobFn != nil {
		return s.completeJobFn(ctx, jobID, owner, archiveFilename, archive, totalEvents, expiresAt)
	}
	return 1, nil
}

func (s *stubJobStore) FailJob(ctx context.Context, jobID, owner, errMsg string) (int64, error) {
	if s.failJobFn != nil {
		return s.failJobFn(ctx, jobID, owner, errMsg)
	}
	return 1, nil
}

func (s *stubJobStore) ExpireJobs(ctx context.Context, now time.Time) (int64, error) {
	if s.expireJobsFn != nil {
		return s.expireJobsFn(ctx, now)
	}
	return 0, nil
}

func (s *stubJobStore) RequeueStaleJobs(ctx context.Context, d time.Duration) (int64, error) {
	if s.requeueStaleFn != nil {
		return s.requeueStaleFn(ctx, d)
	}
	return 0, nil
}

type stubAuditLister struct {
	listFn func(ctx context.Context, filter auditlogsvc.ListFilter) (auditlogrepo.ListResult, error)
}

func (s *stubAuditLister) List(ctx context.Context, filter auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
	return s.listFn(ctx, filter)
}

type stubAuditRecorder struct {
	events []auditlogsvc.Event
}

func (s *stubAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestStartExport_CreatesJobAndRecordsAudit(t *testing.T) {
	store := &stubJobStore{
		createJobFn: func(_ context.Context, tenantID, createdBy string, _ json.RawMessage) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:       "job-1",
				TenantID: tenantID,
				Status:   aerepo.JobQueued,
			}), nil
		},
	}
	recorder := &stubAuditRecorder{}
	svc := New(store, nil, recorder)

	result, err := svc.StartExport(context.Background(), "t1", "admin-1", ExportFilter{
		Actions: []string{"api_key.create"},
	})
	if err != nil {
		t.Fatalf("StartExport: %v", err)
	}
	if result.JobID != "job-1" {
		t.Errorf("job ID = %s, want job-1", result.JobID)
	}
	if result.Status != "queued" {
		t.Errorf("status = %s, want queued", result.Status)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(recorder.events))
	}
	if recorder.events[0].Action != "audit_evidence.create" {
		t.Errorf("audit action = %s", recorder.events[0].Action)
	}
}

func TestStartExport_PropagatesStoreError(t *testing.T) {
	store := &stubJobStore{
		createJobFn: func(context.Context, string, string, json.RawMessage) (*aerepo.ExportJob, error) {
			return nil, errors.New("db down")
		},
	}
	svc := New(store, nil, nil)

	_, err := svc.StartExport(context.Background(), "t1", "a", ExportFilter{})
	if err == nil || err.Error() != "db down" {
		t.Fatalf("expected db error, got %v", err)
	}
}

func TestGetJob_MapsNotFoundError(t *testing.T) {
	store := &stubJobStore{
		getJobFn: func(context.Context, string, string) (*aerepo.ExportJob, error) {
			return nil, aerepo.ErrJobNotFound
		},
	}
	svc := New(store, nil, nil)

	_, err := svc.GetJob(context.Background(), "t1", "nonexistent")
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}

func TestGetJob_ReturnsJob(t *testing.T) {
	store := &stubJobStore{
		getJobFn: func(_ context.Context, tenantID, jobID string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:       jobID,
				TenantID: tenantID,
				Status:   aerepo.JobCompleted,
			}), nil
		},
	}
	svc := New(store, nil, nil)

	job, err := svc.GetJob(context.Background(), "t1", "job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.ID != "job-1" || job.Status != aerepo.JobCompleted {
		t.Errorf("unexpected job: %+v", job)
	}
}

func TestDownload_MarksDownloadedAndRecordsAudit(t *testing.T) {
	store := &stubJobStore{
		markDownloadFn: func(_ context.Context, _, jobID string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:              jobID,
				Archive:         []byte("zip-data"),
				ArchiveFilename: "evidence.zip",
			}), nil
		},
	}
	recorder := &stubAuditRecorder{}
	svc := New(store, nil, recorder)

	bundle, err := svc.Download(context.Background(), "t1", "job-1", auditlogsvc.Actor{
		Type: "admin", ID: "admin-1",
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if bundle.Filename != "evidence.zip" {
		t.Errorf("filename = %s", bundle.Filename)
	}
	if string(bundle.Data) != "zip-data" {
		t.Errorf("data = %s", bundle.Data)
	}
	if len(recorder.events) != 1 || recorder.events[0].Action != "audit_evidence.download" {
		t.Errorf("audit events: %+v", recorder.events)
	}
}

func TestDownload_NotDownloadable(t *testing.T) {
	store := &stubJobStore{
		markDownloadFn: func(context.Context, string, string) (*aerepo.ExportJob, error) {
			return nil, aerepo.ErrJobNotDownloadable
		},
	}
	svc := New(store, nil, nil)

	_, err := svc.Download(context.Background(), "t1", "job-1", auditlogsvc.Actor{})
	if !errors.Is(err, ErrJobNotDownloadable) {
		t.Fatalf("expected ErrJobNotDownloadable, got %v", err)
	}
}

func TestDownload_NotFound(t *testing.T) {
	store := &stubJobStore{
		markDownloadFn: func(context.Context, string, string) (*aerepo.ExportJob, error) {
			return nil, aerepo.ErrJobNotFound
		},
	}
	svc := New(store, nil, nil)

	_, err := svc.Download(context.Background(), "t1", "job-1", auditlogsvc.Actor{})
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}

func TestWithExportTTL(t *testing.T) {
	svc := New(nil, nil, nil, WithExportTTL(24*time.Hour))
	if svc.exportTTL != 24*time.Hour {
		t.Errorf("exportTTL = %v, want 24h", svc.exportTTL)
	}
}

func TestWithExportTTL_IgnoresZero(t *testing.T) {
	svc := New(nil, nil, nil, WithExportTTL(0))
	if svc.exportTTL != 72*time.Hour {
		t.Errorf("exportTTL = %v, want default 72h", svc.exportTTL)
	}
}
