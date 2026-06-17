package gdpr

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type stubRepo struct {
	exportData   *gdprrepo.ExportData
	exportErr    error
	deleteResult *gdprrepo.DeleteResult
	deleteErr    error
}

func (s *stubRepo) Export(context.Context, string, string) (*gdprrepo.ExportData, error) {
	return s.exportData, s.exportErr
}

func (s *stubRepo) Delete(context.Context, string, string) (*gdprrepo.DeleteResult, error) {
	return s.deleteResult, s.deleteErr
}

type stubAudit struct {
	events []auditlogsvc.Event
}

func (s *stubAudit) Record(_ context.Context, event auditlogsvc.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestExportBuildsZipAndAudits(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{
		exportData: ptrext.Of(gdprrepo.ExportData{
			SubjectKey:     "alice@example.com",
			SubjectDisplay: "alice@example.com",
			FeedbackRows: []json.RawMessage{
				json.RawMessage(`{"id":1,"content":"hello"}`),
			},
			Counts: gdprrepo.Counts{FeedbackCount: 1},
		}),
	})
	audit := ptrext.Of(stubAudit{})
	svc := New(repo, audit)

	bundle, err := svc.Export(context.Background(), "tenant-1", "alice@example.com", auditlogsvc.Actor{Type: "admin", ID: "u-1"})
	if err != nil {
		t.Fatalf("Export err = %v", err)
	}
	if bundle.Filename == "" || len(bundle.Data) == 0 {
		t.Fatalf("Export bundle not populated: %#v", bundle)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "gdpr.export" {
		t.Fatalf("expected one gdpr.export audit event, got %#v", audit.events)
	}

	zr, err := zip.NewReader(bytes.NewReader(bundle.Data), int64(len(bundle.Data)))
	if err != nil {
		t.Fatalf("zip.NewReader err = %v", err)
	}
	wantFiles := map[string]bool{
		"manifest.json":            false,
		"feedback.jsonl":           false,
		"feedback_tags.jsonl":      false,
		"feedback_audit_log.jsonl": false,
		"llm_audit.jsonl":          false,
	}
	for _, f := range zr.File {
		if _, ok := wantFiles[f.Name]; ok {
			wantFiles[f.Name] = true
		}
	}
	for name, seen := range wantFiles {
		if !seen {
			t.Fatalf("zip missing file %s", name)
		}
	}
}

func TestDeleteTranslatesNotFound(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{deleteErr: gdprrepo.ErrSubjectNotFound}), nil)
	_, err := svc.Delete(context.Background(), "tenant-1", "missing@example.com", auditlogsvc.Actor{})
	if !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("Delete err = %v, want %v", err, ErrSubjectNotFound)
	}
}

type schedulingRepo struct {
	deleteResult       *gdprrepo.DeleteResult
	cancelledRequest   *gdprrepo.Request
	revokedJob         *gdprrepo.ExportJob
	createExecuteAfter time.Time
	createSubjectHash  string
	cancelRequestID    string
	revokeJobID        string
}

func (s *schedulingRepo) Export(context.Context, string, string) (*gdprrepo.ExportData, error) {
	return nil, nil
}

func (s *schedulingRepo) Delete(context.Context, string, string) (*gdprrepo.DeleteResult, error) {
	return nil, nil
}

func (s *schedulingRepo) ListRequests(context.Context, gdprrepo.ListRequestFilter) (gdprrepo.ListRequestResult, error) {
	return gdprrepo.ListRequestResult{}, nil
}

func (s *schedulingRepo) GetOperationsSummary(context.Context, string) (*gdprrepo.OperationsSummary, error) {
	return ptrext.Of(gdprrepo.OperationsSummary{}), nil
}

func (s *schedulingRepo) CreateDeleteRequest(_ context.Context, _, _, subjectHash, _ string, executeAfter time.Time) (*gdprrepo.DeleteResult, error) {
	s.createExecuteAfter = executeAfter
	s.createSubjectHash = subjectHash
	return s.deleteResult, nil
}

func (s *schedulingRepo) CancelDeleteRequest(_ context.Context, _, requestID string) (*gdprrepo.Request, error) {
	s.cancelRequestID = requestID
	return s.cancelledRequest, nil
}

func (s *schedulingRepo) CreateExportJob(context.Context, string, string, string, string) (*gdprrepo.ExportJob, error) {
	return nil, nil
}

func (s *schedulingRepo) GetExportJob(context.Context, string, string) (*gdprrepo.ExportJob, error) {
	return nil, nil
}

func (s *schedulingRepo) RevokeExportJob(_ context.Context, _, jobID string) (*gdprrepo.ExportJob, error) {
	s.revokeJobID = jobID
	return s.revokedJob, nil
}

func (s *schedulingRepo) ClaimNextExportJob(context.Context) (*gdprrepo.ExportJob, error) {
	return nil, nil
}

func (s *schedulingRepo) HeartbeatExportJob(context.Context, string) error {
	return nil
}

func (s *schedulingRepo) CompleteExportJob(context.Context, string, string, string, []byte, gdprrepo.Counts, time.Time) error {
	return nil
}

func (s *schedulingRepo) FailExportJob(context.Context, string, string) error {
	return nil
}

func (s *schedulingRepo) ExpireReadyExportJobs(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (s *schedulingRepo) MarkExportJobDownloaded(context.Context, string, string) (*gdprrepo.ExportJob, error) {
	return nil, nil
}

func TestDeleteSchedulesRequestAndAudits(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(schedulingRepo{
		deleteResult: ptrext.Of(gdprrepo.DeleteResult{
			RequestID:  "req-123",
			SubjectKey: "alice@example.com",
			Counts: gdprrepo.Counts{
				FeedbackCount: 2,
				LLMAuditCount: 1,
			},
		}),
	})
	audit := ptrext.Of(stubAudit{})
	svc := New(repo, audit, WithDeleteGraceWindow(5*time.Minute))

	result, err := svc.Delete(context.Background(), "tenant-1", "alice@example.com", auditlogsvc.Actor{Type: "admin", ID: "u-1"})
	if err != nil {
		t.Fatalf("Delete err = %v", err)
	}
	if result.Status != gdprrepo.RequestStatusScheduled {
		t.Fatalf("Delete status = %q", result.Status)
	}
	if result.ExecuteAfter == nil {
		t.Fatal("expected execute_after to be populated")
	}
	if repo.createExecuteAfter.IsZero() {
		t.Fatal("expected scheduled execute_after to be captured")
	}
	if repo.createSubjectHash != subjectkey.Hash("tenant-1", "alice@example.com") {
		t.Fatalf("unexpected subject hash %q", repo.createSubjectHash)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "gdpr.delete.requested" {
		t.Fatalf("expected gdpr.delete.requested audit event, got %#v", audit.events)
	}
}

func TestCancelDeleteRequestAudits(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(schedulingRepo{
		cancelledRequest: ptrext.Of(gdprrepo.Request{
			ID:          "req-123",
			SubjectKey:  "alice@example.com",
			SubjectHash: subjectkey.Hash("tenant-1", "alice@example.com"),
		}),
	})
	audit := ptrext.Of(stubAudit{})
	svc := New(repo, audit)

	if err := svc.CancelDeleteRequest(context.Background(), "tenant-1", "req-123", auditlogsvc.Actor{Type: "admin", ID: "u-1"}); err != nil {
		t.Fatalf("CancelDeleteRequest err = %v", err)
	}
	if repo.cancelRequestID != "req-123" {
		t.Fatalf("CancelDeleteRequest request id = %q", repo.cancelRequestID)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "gdpr.delete.cancelled" {
		t.Fatalf("expected gdpr.delete.cancelled audit event, got %#v", audit.events)
	}
}

func TestRevokeExportAudits(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(schedulingRepo{
		revokedJob: ptrext.Of(gdprrepo.ExportJob{
			ID:          "job-123",
			SubjectHash: subjectkey.Hash("tenant-1", "alice@example.com"),
			RevokedAt:   ptrext.Of(time.Now().UTC()),
		}),
	})
	audit := ptrext.Of(stubAudit{})
	svc := New(repo, audit)

	resp, err := svc.RevokeExport(context.Background(), "tenant-1", "job-123", auditlogsvc.Actor{Type: "admin", ID: "u-1"})
	if err != nil {
		t.Fatalf("RevokeExport err = %v", err)
	}
	if repo.revokeJobID != "job-123" {
		t.Fatalf("RevokeExport job id = %q", repo.revokeJobID)
	}
	if resp.GetStatus() != attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_REVOKED {
		t.Fatalf("unexpected export status %v", resp.GetStatus())
	}
	if len(audit.events) != 1 || audit.events[0].Action != "gdpr.export.revoked" {
		t.Fatalf("expected gdpr.export.revoked audit event, got %#v", audit.events)
	}
}
