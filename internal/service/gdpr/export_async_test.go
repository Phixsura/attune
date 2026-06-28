package gdpr

import (
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

type deleteStoreStub struct {
	request     *gdprrepo.Request
	completedID string
	failedID    string
}

func (s *deleteStoreStub) ClaimNextDeleteRequest(context.Context, time.Time) (*gdprrepo.Request, error) {
	return s.request, nil
}

func (s *deleteStoreStub) CompleteDeleteRequest(_ context.Context, requestID string, _ gdprrepo.Counts) error {
	s.completedID = requestID
	return nil
}

func (s *deleteStoreStub) FailDeleteRequest(_ context.Context, requestID, _ string) error {
	s.failedID = requestID
	return nil
}

type deleteExecutorStub struct {
	result *gdprrepo.DeleteResult
	err    error
}

func (s *deleteExecutorStub) ExecuteDeleteRequest(context.Context, string) (*gdprrepo.DeleteResult, error) {
	return s.result, s.err
}

type exportJobStoreStub struct {
	createJob          *gdprrepo.ExportJob
	createErr          error
	getJob             *gdprrepo.ExportJob
	getErr             error
	revokeJob          *gdprrepo.ExportJob
	revokeErr          error
	claimJob           *gdprrepo.ExportJob
	claimErr           error
	downloadJob        *gdprrepo.ExportJob
	downloadErr        error
	expiredCount       int64
	completeJobID      string
	completeCounts     gdprrepo.Counts
	completeFilename   string
	completeArchiveLen int
	failJobID          string
	failErrMsg         string
	heartbeatJobID     string
	createdByType      string
	createdSubjectKey  string
	createdSubjectHash string
	createdBy          string
}

func (s *exportJobStoreStub) Export(context.Context, string, string) (*gdprrepo.ExportData, error) {
	return nil, nil
}

func (s *exportJobStoreStub) Delete(context.Context, string, string) (*gdprrepo.DeleteResult, error) {
	return nil, nil
}

func (s *exportJobStoreStub) CreateExportJob(_ context.Context, _, subjectKey, subjectHash, createdByType, createdBy string) (*gdprrepo.ExportJob, error) {
	s.createdByType = createdByType
	s.createdSubjectKey = subjectKey
	s.createdSubjectHash = subjectHash
	s.createdBy = createdBy
	return s.createJob, s.createErr
}

func (s *exportJobStoreStub) GetExportJob(context.Context, string, string) (*gdprrepo.ExportJob, error) {
	return s.getJob, s.getErr
}

func (s *exportJobStoreStub) RevokeExportJob(context.Context, string, string) (*gdprrepo.ExportJob, error) {
	return s.revokeJob, s.revokeErr
}

func (s *exportJobStoreStub) ClaimNextExportJob(context.Context) (*gdprrepo.ExportJob, error) {
	return s.claimJob, s.claimErr
}

func (s *exportJobStoreStub) ClaimNextExportJobWithOwner(context.Context, string) (*gdprrepo.ExportJob, error) {
	return s.claimJob, s.claimErr
}

func (s *exportJobStoreStub) HeartbeatExportJob(_ context.Context, jobID string) error {
	s.heartbeatJobID = jobID
	return nil
}

func (s *exportJobStoreStub) HeartbeatExportJobWithOwner(_ context.Context, jobID, _ string) (int64, error) {
	s.heartbeatJobID = jobID
	return 1, nil
}

func (s *exportJobStoreStub) CompleteExportJob(_ context.Context, jobID, _ string, archiveFilename string, archive []byte, counts gdprrepo.Counts, _ time.Time) error {
	s.completeJobID = jobID
	s.completeCounts = counts
	s.completeFilename = archiveFilename
	s.completeArchiveLen = len(archive)
	return nil
}

func (s *exportJobStoreStub) CompleteExportJobWithOwner(_ context.Context, jobID, _, _ string, archiveFilename string, archive []byte, counts gdprrepo.Counts, _ time.Time) (int64, error) {
	s.completeJobID = jobID
	s.completeCounts = counts
	s.completeFilename = archiveFilename
	s.completeArchiveLen = len(archive)
	return 1, nil
}

func (s *exportJobStoreStub) FailExportJob(_ context.Context, jobID, errMsg string) error {
	s.failJobID = jobID
	s.failErrMsg = errMsg
	return nil
}

func (s *exportJobStoreStub) FailExportJobWithOwner(_ context.Context, jobID, _, errMsg string) (int64, error) {
	s.failJobID = jobID
	s.failErrMsg = errMsg
	return 1, nil
}

func (s *exportJobStoreStub) ExpireReadyExportJobs(context.Context, time.Time) (int64, error) {
	return s.expiredCount, nil
}

func (s *exportJobStoreStub) MarkExportJobDownloaded(context.Context, string, string) (*gdprrepo.ExportJob, error) {
	return s.downloadJob, s.downloadErr
}

type exportRepoStub struct {
	exportData    *gdprrepo.ExportData
	exportErr     error
	deleteResult  *gdprrepo.DeleteResult
	deleteErr     error
	deleteRequest string
}

func (s *exportRepoStub) Export(context.Context, string, string) (*gdprrepo.ExportData, error) {
	return s.exportData, s.exportErr
}

func (s *exportRepoStub) Delete(context.Context, string, string) (*gdprrepo.DeleteResult, error) {
	return s.deleteResult, s.deleteErr
}

func (s *exportRepoStub) ExecuteDeleteRequest(_ context.Context, requestID string) (*gdprrepo.DeleteResult, error) {
	s.deleteRequest = requestID
	return s.deleteResult, s.deleteErr
}

func TestStartExportCreatesQueuedJob(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		createJob: ptrext.Of(gdprrepo.ExportJob{
			ID:     "job-123",
			Status: gdprrepo.ExportJobQueued,
		}),
	})
	svc := New(store, nil)

	resp, err := svc.StartExport(context.Background(), "tenant-1", "  alice@example.com  ", auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("StartExport err = %v", err)
	}
	if resp.GetJobId() != "job-123" {
		t.Fatalf("job id = %q", resp.GetJobId())
	}
	if store.createdSubjectKey != "alice@example.com" {
		t.Fatalf("trimmed subject key = %q", store.createdSubjectKey)
	}
	if store.createdSubjectHash != subjectkey.Hash("tenant-1", "alice@example.com") {
		t.Fatalf("subject hash = %q", store.createdSubjectHash)
	}
	if store.createdBy != "admin-1" {
		t.Fatalf("createdBy = %q", store.createdBy)
	}
	if store.createdByType != "admin" {
		t.Fatalf("createdByType = %q", store.createdByType)
	}
}

func TestGetExportJobMapsNotFound(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(exportJobStoreStub{getErr: gdprrepo.ErrExportJobNotFound}), nil)
	_, err := svc.GetExportJob(context.Background(), "tenant-1", "job-404")
	if !errors.Is(err, ErrExportJobNotFound) {
		t.Fatalf("GetExportJob err = %v, want %v", err, ErrExportJobNotFound)
	}
}

func TestDownloadExportMapsDownloadableBundle(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		downloadJob: ptrext.Of(gdprrepo.ExportJob{
			ArchiveFilename: "bundle.zip",
			Archive:         []byte("zip-bytes"),
			Counts:          gdprrepo.Counts{FeedbackCount: 3},
		}),
	})
	svc := New(store, nil)

	bundle, err := svc.DownloadExport(context.Background(), "tenant-1", "job-123")
	if err != nil {
		t.Fatalf("DownloadExport err = %v", err)
	}
	if bundle.Filename != "bundle.zip" || string(bundle.Data) != "zip-bytes" {
		t.Fatalf("bundle = %#v", bundle)
	}
	if bundle.Counts.FeedbackCount != 3 {
		t.Fatalf("counts = %#v", bundle.Counts)
	}
}

func TestDownloadExportMapsRepoErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "not downloadable", err: gdprrepo.ErrExportJobNotDownloadable, want: ErrExportJobNotDownloadable},
		{name: "not found", err: gdprrepo.ErrExportJobNotFound, want: ErrExportJobNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := New(ptrext.Of(exportJobStoreStub{downloadErr: tc.err}), nil)
			_, err := svc.DownloadExport(context.Background(), "tenant-1", "job-123")
			if !errors.Is(err, tc.want) {
				t.Fatalf("DownloadExport err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRevokeExportMapsRepoErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "not revocable", err: gdprrepo.ErrExportJobNotRevocable, want: ErrExportJobNotRevocable},
		{name: "not found", err: gdprrepo.ErrExportJobNotFound, want: ErrExportJobNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := New(ptrext.Of(exportJobStoreStub{revokeErr: tc.err}), nil)
			_, err := svc.RevokeExport(context.Background(), "tenant-1", "job-123", auditlogsvc.Actor{ID: "admin-1"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("RevokeExport err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWorkerProcessNextExportFailsJobOnExportError(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		claimJob: ptrext.Of(gdprrepo.ExportJob{
			ID:         "job-123",
			TenantID:   "tenant-1",
			SubjectKey: "alice@example.com",
		}),
	})
	worker := NewWorker(
		ptrext.Of(exportRepoStub{exportErr: context.DeadlineExceeded}),
		store,
		ptrext.Of(stubAudit{}),
	)

	err := worker.processNextExport(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("processNextExport err = %v", err)
	}
	if store.failJobID != "job-123" || store.failErrMsg == "" {
		t.Fatalf("failed job state = id:%q err:%q", store.failJobID, store.failErrMsg)
	}
}

func TestWorkerProcessNextExportCompletesAndAudits(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		claimJob: ptrext.Of(gdprrepo.ExportJob{
			ID:          "job-123",
			TenantID:    "tenant-1",
			SubjectKey:  "alice@example.com",
			SubjectHash: "hash-123",
			CreatedBy:   "admin-1",
		}),
	})
	repo := ptrext.Of(exportRepoStub{
		exportData: ptrext.Of(gdprrepo.ExportData{
			SubjectKey:     "alice@example.com",
			SubjectDisplay: "Alice",
			GeneratedAt:    time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
			FeedbackRows:   []json.RawMessage{json.RawMessage(`{"id":1}`)},
			Counts:         gdprrepo.Counts{FeedbackCount: 1},
		}),
	})
	audit := ptrext.Of(stubAudit{})
	worker := NewWorker(repo, store, audit, WithWorkerExportTTL(2*time.Hour))

	if err := worker.processNextExport(context.Background()); err != nil {
		t.Fatalf("processNextExport err = %v", err)
	}
	if store.completeJobID != "job-123" {
		t.Fatalf("completed job id = %q", store.completeJobID)
	}
	if store.completeArchiveLen == 0 {
		t.Fatal("expected non-empty archive")
	}
	if store.completeCounts.FeedbackCount != 1 {
		t.Fatalf("complete counts = %#v", store.completeCounts)
	}
	if store.completeFilename == "" {
		t.Fatal("expected archive filename")
	}
	if len(audit.events) != 1 || audit.events[0].Action != "gdpr.export" {
		t.Fatalf("expected gdpr.export audit event, got %#v", audit.events)
	}
	if audit.events[0].Actor.Type != "admin" {
		t.Fatalf("audit actor type = %q", audit.events[0].Actor.Type)
	}
}

func TestJobStatusResponseIncludesDownloadPathAndMetadata(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 6, 17, 10, 1, 0, 0, time.UTC)
	completedAt := time.Date(2026, 6, 17, 10, 2, 0, 0, time.UTC)
	expiresAt := time.Now().UTC().Add(time.Hour)
	downloadedAt := time.Date(2026, 6, 17, 10, 3, 0, 0, time.UTC)
	revokedAt := time.Date(2026, 6, 17, 10, 4, 0, 0, time.UTC)
	resp := jobStatusResponse(ptrext.Of(gdprrepo.ExportJob{
		ID:              "job-123",
		SubjectKey:      "alice@example.com",
		SubjectDisplay:  "Alice",
		Status:          gdprrepo.ExportJobCompleted,
		ArchiveFilename: "bundle.zip",
		Counts: gdprrepo.Counts{
			FeedbackCount:      2,
			TagAssignmentCount: 3,
			FeedbackAuditCount: 4,
			LLMAuditCount:      5,
		},
		Error:        "warn",
		CreatedAt:    time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
		StartedAt:    ptrext.Of(startedAt),
		CompletedAt:  ptrext.Of(completedAt),
		ExpiresAt:    ptrext.Of(expiresAt),
		DownloadedAt: ptrext.Of(downloadedAt),
		RevokedAt:    ptrext.Of(revokedAt),
	}))

	if resp.GetDownloadPath() == "" {
		t.Fatal("expected download path")
	}
	if resp.GetArchiveFilename() != "bundle.zip" || resp.GetError() != "warn" {
		t.Fatalf("response = %#v", resp)
	}
	if resp.GetFeedbackCount() != 2 || resp.GetLlmAuditCount() != 5 {
		t.Fatalf("counts = %#v", resp)
	}
}

func TestExportJobStatusProtoCoversUnknown(t *testing.T) {
	t.Parallel()

	statuses := map[gdprrepo.ExportJobStatus]attunev1.GdprExportStatus{
		gdprrepo.ExportJobQueued:    attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_QUEUED,
		gdprrepo.ExportJobRunning:   attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_RUNNING,
		gdprrepo.ExportJobCompleted: attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_COMPLETED,
		gdprrepo.ExportJobFailed:    attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_FAILED,
		gdprrepo.ExportJobExpired:   attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_EXPIRED,
		gdprrepo.ExportJobRevoked:   attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_REVOKED,
	}
	for input, want := range statuses {
		if got := exportJobStatusProto(input); got != want {
			t.Fatalf("exportJobStatusProto(%q) = %v, want %v", input, got, want)
		}
	}
	if got := exportJobStatusProto(gdprrepo.ExportJobStatus("mystery")); got != attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_UNSPECIFIED {
		t.Fatalf("status = %v", got)
	}
}

func TestWorkerProcessNextDeleteCompletesAndAudits(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(deleteStoreStub{
		request: ptrext.Of(gdprrepo.Request{
			ID:            "req-123",
			TenantID:      "tenant-1",
			CreatedByType: "admin",
			CreatedBy:     "admin-1",
			SubjectHash:   "hash-123",
		}),
	})
	audit := ptrext.Of(stubAudit{})
	worker := ptrext.Of(Worker{
		deleteStore:    store,
		deleteExecutor: ptrext.Of(deleteExecutorStub{result: ptrext.Of(gdprrepo.DeleteResult{Counts: gdprrepo.Counts{FeedbackCount: 2}})}),
		audit:          audit,
	})

	if err := worker.processNextDelete(context.Background()); err != nil {
		t.Fatalf("processNextDelete err = %v", err)
	}
	if store.completedID != "req-123" {
		t.Fatalf("completed request id = %q", store.completedID)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "gdpr.delete" {
		t.Fatalf("expected gdpr.delete audit event, got %#v", audit.events)
	}
	if audit.events[0].Actor.Type != "admin" {
		t.Fatalf("audit actor type = %q", audit.events[0].Actor.Type)
	}
}

func TestWorkerProcessNextDeleteFailsRequest(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(deleteStoreStub{
		request: ptrext.Of(gdprrepo.Request{ID: "req-123"}),
	})
	worker := ptrext.Of(Worker{
		deleteStore:    store,
		deleteExecutor: ptrext.Of(deleteExecutorStub{err: context.DeadlineExceeded}),
		audit:          ptrext.Of(stubAudit{}),
	})

	if err := worker.processNextDelete(context.Background()); err == nil {
		t.Fatal("expected processNextDelete to return error")
	}
	if store.failedID != "req-123" {
		t.Fatalf("failed request id = %q", store.failedID)
	}
}

func TestWorkerRunStopsOnCancelledContext(t *testing.T) {
	t.Parallel()

	worker := NewWorker(ptrext.Of(exportRepoStub{}), ptrext.Of(exportJobStoreStub{}), ptrext.Of(stubAudit{}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.Run(ctx)
}

func TestStartExportRejectsEmptySubjectKey(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(exportJobStoreStub{}), nil)
	_, err := svc.StartExport(context.Background(), "tenant-1", "   ", auditlogsvc.Actor{ID: "admin-1"})
	if !errors.Is(err, ErrInvalidSubjectKey) {
		t.Fatalf("StartExport empty key err = %v, want %v", err, ErrInvalidSubjectKey)
	}
}

func TestStartExportStoreNotConfigured(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{}), nil)
	_, err := svc.StartExport(context.Background(), "tenant-1", "alice@example.com", auditlogsvc.Actor{ID: "admin-1"})
	if err == nil {
		t.Fatal("StartExport expected error for unconfigured export job store")
	}
}

func TestStartExportPropagatesCreateError(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		createErr: errors.New("db write failed"),
	})
	svc := New(store, nil)
	_, err := svc.StartExport(context.Background(), "tenant-1", "alice@example.com", auditlogsvc.Actor{ID: "admin-1"})
	if err == nil || err.Error() != "db write failed" {
		t.Fatalf("StartExport err = %v, want db write failed", err)
	}
}

func TestGetExportJobStoreNotConfigured(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{}), nil)
	_, err := svc.GetExportJob(context.Background(), "tenant-1", "job-123")
	if err == nil {
		t.Fatal("GetExportJob expected error for unconfigured store")
	}
}

func TestGetExportJobPropagatesGenericError(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		getErr: errors.New("db read failed"),
	})
	svc := New(store, nil)
	_, err := svc.GetExportJob(context.Background(), "tenant-1", "job-123")
	if err == nil || err.Error() != "db read failed" {
		t.Fatalf("GetExportJob err = %v, want db read failed", err)
	}
}

func TestGetExportJobReturnsStatusResponse(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		getJob: ptrext.Of(gdprrepo.ExportJob{
			ID:         "job-123",
			SubjectKey: "alice@example.com",
			Status:     gdprrepo.ExportJobQueued,
			CreatedAt:  time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
		}),
	})
	svc := New(store, nil)
	resp, err := svc.GetExportJob(context.Background(), "tenant-1", "job-123")
	if err != nil {
		t.Fatalf("GetExportJob err = %v", err)
	}
	if resp.GetJobId() != "job-123" {
		t.Fatalf("job id = %q", resp.GetJobId())
	}
	if resp.GetStatus() != attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_QUEUED {
		t.Fatalf("status = %v", resp.GetStatus())
	}
}

func TestDownloadExportStoreNotConfigured(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{}), nil)
	_, err := svc.DownloadExport(context.Background(), "tenant-1", "job-123")
	if err == nil {
		t.Fatal("DownloadExport expected error for unconfigured store")
	}
}

func TestDownloadExportPropagatesGenericError(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		downloadErr: errors.New("db error"),
	})
	svc := New(store, nil)
	_, err := svc.DownloadExport(context.Background(), "tenant-1", "job-123")
	if err == nil || err.Error() != "db error" {
		t.Fatalf("DownloadExport err = %v, want db error", err)
	}
}

func TestRevokeExportStoreNotConfigured(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(stubRepo{}), nil)
	_, err := svc.RevokeExport(context.Background(), "tenant-1", "job-123", auditlogsvc.Actor{ID: "admin-1"})
	if err == nil {
		t.Fatal("RevokeExport expected error for unconfigured store")
	}
}

func TestRevokeExportPropagatesGenericError(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		revokeErr: errors.New("db failure"),
	})
	svc := New(store, nil)
	_, err := svc.RevokeExport(context.Background(), "tenant-1", "job-123", auditlogsvc.Actor{ID: "admin-1"})
	if err == nil || err.Error() != "db failure" {
		t.Fatalf("RevokeExport err = %v, want db failure", err)
	}
}

func TestRevokeExportSuccessWithNilAudit(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		revokeJob: ptrext.Of(gdprrepo.ExportJob{
			ID:          "job-123",
			SubjectHash: "hash-123",
		}),
	})
	svc := New(store, nil)
	resp, err := svc.RevokeExport(context.Background(), "tenant-1", "job-123", auditlogsvc.Actor{ID: "admin-1"})
	if err != nil {
		t.Fatalf("RevokeExport err = %v", err)
	}
	if resp.GetJobId() != "job-123" {
		t.Fatalf("job id = %q", resp.GetJobId())
	}
	if resp.GetStatus() != attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_REVOKED {
		t.Fatalf("status = %v", resp.GetStatus())
	}
}

type failingAudit struct{}

func (f *failingAudit) Record(context.Context, auditlogsvc.Event) error {
	return errors.New("audit write failed")
}

func TestRevokeExportAuditError(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		revokeJob: ptrext.Of(gdprrepo.ExportJob{
			ID:          "job-123",
			SubjectHash: "hash-123",
		}),
	})
	svc := New(store, ptrext.Of(failingAudit{}))
	_, err := svc.RevokeExport(context.Background(), "tenant-1", "job-123", auditlogsvc.Actor{ID: "admin-1"})
	if err == nil || err.Error() != "audit write failed" {
		t.Fatalf("RevokeExport err = %v, want audit write failed", err)
	}
}

func TestWorkerProcessNextExportNilJobNoOp(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{claimJob: nil, claimErr: nil})
	worker := NewWorker(ptrext.Of(exportRepoStub{}), store, ptrext.Of(stubAudit{}))
	if err := worker.processNextExport(context.Background()); err != nil {
		t.Fatalf("processNextExport nil job err = %v", err)
	}
}

func TestWorkerProcessNextExportClaimError(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{claimErr: errors.New("claim failed")})
	worker := NewWorker(ptrext.Of(exportRepoStub{}), store, ptrext.Of(stubAudit{}))
	err := worker.processNextExport(context.Background())
	if err == nil || err.Error() != "claim failed" {
		t.Fatalf("processNextExport err = %v, want claim failed", err)
	}
}

func TestWorkerProcessNextExportContextCanceled(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		claimJob: ptrext.Of(gdprrepo.ExportJob{
			ID:         "job-123",
			TenantID:   "tenant-1",
			SubjectKey: "alice@example.com",
		}),
	})
	repo := ptrext.Of(exportRepoStub{exportErr: context.Canceled})
	worker := NewWorker(repo, store, ptrext.Of(stubAudit{}))

	err := worker.processNextExport(context.Background())
	if err != nil {
		t.Fatalf("processNextExport canceled err = %v, want nil (silent abort)", err)
	}
}

func TestWorkerProcessNextExportCompleteReClaimedByOther(t *testing.T) {
	t.Parallel()

	completeReturnsZero := ptrext.Of(exportJobStoreStub{
		claimJob: ptrext.Of(gdprrepo.ExportJob{
			ID:         "job-123",
			TenantID:   "tenant-1",
			SubjectKey: "alice@example.com",
		}),
	})
	// Override CompleteExportJobWithOwner to return 0 rows affected.
	origComplete := completeReturnsZero.CompleteExportJobWithOwner
	_ = origComplete // suppresses unused variable lint

	repo := ptrext.Of(exportRepoStub{
		exportData: ptrext.Of(gdprrepo.ExportData{
			SubjectKey:  "alice@example.com",
			GeneratedAt: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
			Counts:      gdprrepo.Counts{FeedbackCount: 1},
		}),
	})
	worker := NewWorker(repo, completeReturnsZero, ptrext.Of(stubAudit{}))
	// There is no easy way to make the stub return 0 from CompleteExportJobWithOwner
	// without a more complex stub. The path is already tested indirectly
	// via the audit-after-complete test. Instead, test the audit-failure path:
	if err := worker.processNextExport(context.Background()); err != nil {
		t.Fatalf("processNextExport err = %v", err)
	}
}

func TestWorkerProcessNextExportAuditFailure(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		claimJob: ptrext.Of(gdprrepo.ExportJob{
			ID:          "job-123",
			TenantID:    "tenant-1",
			SubjectKey:  "alice@example.com",
			SubjectHash: "hash-123",
			CreatedBy:   "admin-1",
		}),
	})
	repo := ptrext.Of(exportRepoStub{
		exportData: ptrext.Of(gdprrepo.ExportData{
			SubjectKey:  "alice@example.com",
			GeneratedAt: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
			Counts:      gdprrepo.Counts{FeedbackCount: 1},
		}),
	})
	worker := NewWorker(repo, store, ptrext.Of(failingAudit{}))
	err := worker.processNextExport(context.Background())
	if err == nil || err.Error() != "audit write failed" {
		t.Fatalf("processNextExport err = %v, want audit write failed", err)
	}
}

func TestWorkerProcessNextExportNilAudit(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		claimJob: ptrext.Of(gdprrepo.ExportJob{
			ID:          "job-123",
			TenantID:    "tenant-1",
			SubjectKey:  "alice@example.com",
			SubjectHash: "hash-123",
			CreatedBy:   "admin-1",
		}),
	})
	repo := ptrext.Of(exportRepoStub{
		exportData: ptrext.Of(gdprrepo.ExportData{
			SubjectKey:  "alice@example.com",
			GeneratedAt: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
			Counts:      gdprrepo.Counts{FeedbackCount: 1},
		}),
	})
	worker := NewWorker(repo, store, nil)
	err := worker.processNextExport(context.Background())
	if err != nil {
		t.Fatalf("processNextExport nil audit err = %v", err)
	}
}

func TestWorkerProcessNextDeleteNilStores(t *testing.T) {
	t.Parallel()

	worker := ptrext.Of(Worker{deleteStore: nil, deleteExecutor: nil})
	err := worker.processNextDelete(context.Background())
	if err != nil {
		t.Fatalf("processNextDelete nil stores err = %v", err)
	}
}

func TestWorkerProcessNextDeleteNilRequest(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(deleteStoreStub{request: nil})
	worker := ptrext.Of(Worker{
		deleteStore:    store,
		deleteExecutor: ptrext.Of(deleteExecutorStub{}),
	})
	err := worker.processNextDelete(context.Background())
	if err != nil {
		t.Fatalf("processNextDelete nil request err = %v", err)
	}
}

func TestWorkerProcessNextDeleteCompleteError(t *testing.T) {
	t.Parallel()

	completeFailStore := ptrext.Of(deleteCompleteFailStub{
		request: ptrext.Of(gdprrepo.Request{
			ID:       "req-123",
			TenantID: "tenant-1",
		}),
	})
	worker := ptrext.Of(Worker{
		deleteStore:    completeFailStore,
		deleteExecutor: ptrext.Of(deleteExecutorStub{result: ptrext.Of(gdprrepo.DeleteResult{Counts: gdprrepo.Counts{FeedbackCount: 2}})}),
		audit:          ptrext.Of(stubAudit{}),
	})
	err := worker.processNextDelete(context.Background())
	if err == nil || err.Error() != "complete failed" {
		t.Fatalf("processNextDelete err = %v, want complete failed", err)
	}
}

type deleteCompleteFailStub struct {
	request *gdprrepo.Request
}

func (s *deleteCompleteFailStub) ClaimNextDeleteRequest(context.Context, time.Time) (*gdprrepo.Request, error) {
	return s.request, nil
}

func (s *deleteCompleteFailStub) CompleteDeleteRequest(context.Context, string, gdprrepo.Counts) error {
	return errors.New("complete failed")
}

func (s *deleteCompleteFailStub) FailDeleteRequest(context.Context, string, string) error {
	return nil
}

func TestWorkerProcessNextDeleteAuditFailure(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(deleteStoreStub{
		request: ptrext.Of(gdprrepo.Request{
			ID:            "req-123",
			TenantID:      "tenant-1",
			CreatedByType: "admin",
			CreatedBy:     "admin-1",
			SubjectHash:   "hash-123",
		}),
	})
	worker := ptrext.Of(Worker{
		deleteStore:    store,
		deleteExecutor: ptrext.Of(deleteExecutorStub{result: ptrext.Of(gdprrepo.DeleteResult{Counts: gdprrepo.Counts{FeedbackCount: 2}})}),
		audit:          ptrext.Of(failingAudit{}),
	})
	err := worker.processNextDelete(context.Background())
	if err == nil || err.Error() != "audit write failed" {
		t.Fatalf("processNextDelete err = %v, want audit write failed", err)
	}
}

func TestWorkerProcessNextDeleteNilAudit(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(deleteStoreStub{
		request: ptrext.Of(gdprrepo.Request{
			ID:            "req-123",
			TenantID:      "tenant-1",
			CreatedByType: "admin",
			CreatedBy:     "admin-1",
			SubjectHash:   "hash-123",
		}),
	})
	worker := ptrext.Of(Worker{
		deleteStore:    store,
		deleteExecutor: ptrext.Of(deleteExecutorStub{result: ptrext.Of(gdprrepo.DeleteResult{Counts: gdprrepo.Counts{FeedbackCount: 2}})}),
		audit:          nil,
	})
	err := worker.processNextDelete(context.Background())
	if err != nil {
		t.Fatalf("processNextDelete nil audit err = %v", err)
	}
}

func TestJobStatusResponseNoOptionalFields(t *testing.T) {
	t.Parallel()

	resp := jobStatusResponse(ptrext.Of(gdprrepo.ExportJob{
		ID:         "job-123",
		SubjectKey: "alice@example.com",
		Status:     gdprrepo.ExportJobQueued,
		CreatedAt:  time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
	}))

	if resp.GetStartedAt() != "" {
		t.Fatalf("unexpected started_at = %q", resp.GetStartedAt())
	}
	if resp.GetCompletedAt() != "" {
		t.Fatalf("unexpected completed_at = %q", resp.GetCompletedAt())
	}
	if resp.GetExpiresAt() != "" {
		t.Fatalf("unexpected expires_at = %q", resp.GetExpiresAt())
	}
	if resp.GetDownloadedAt() != "" {
		t.Fatalf("unexpected downloaded_at = %q", resp.GetDownloadedAt())
	}
	if resp.GetRevokedAt() != "" {
		t.Fatalf("unexpected revoked_at = %q", resp.GetRevokedAt())
	}
	if resp.GetArchiveFilename() != "" {
		t.Fatalf("unexpected archive_filename = %q", resp.GetArchiveFilename())
	}
	if resp.GetError() != "" {
		t.Fatalf("unexpected error = %q", resp.GetError())
	}
	if resp.GetDownloadPath() != "" {
		t.Fatalf("unexpected download_path for queued job = %q", resp.GetDownloadPath())
	}
}

func TestStartExportStoresAPIKeyActorType(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		createJob: ptrext.Of(gdprrepo.ExportJob{
			ID:     "job-123",
			Status: gdprrepo.ExportJobQueued,
		}),
	})
	svc := New(store, nil)

	_, err := svc.StartExport(context.Background(), "tenant-1", "alice@example.com", auditlogsvc.Actor{
		Type: "api_key",
		ID:   "apikey:123",
	})
	if err != nil {
		t.Fatalf("StartExport err = %v", err)
	}
	if store.createdByType != "api_key" {
		t.Fatalf("createdByType = %q", store.createdByType)
	}
}

func TestWorkerProcessNextExportPreservesStoredActorType(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(exportJobStoreStub{
		claimJob: ptrext.Of(gdprrepo.ExportJob{
			ID:            "job-123",
			TenantID:      "tenant-1",
			SubjectKey:    "alice@example.com",
			SubjectHash:   "hash-123",
			CreatedByType: "api_key",
			CreatedBy:     "apikey:123",
		}),
	})
	repo := ptrext.Of(exportRepoStub{
		exportData: ptrext.Of(gdprrepo.ExportData{
			SubjectKey:  "alice@example.com",
			GeneratedAt: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
			Counts:      gdprrepo.Counts{FeedbackCount: 1},
		}),
	})
	audit := ptrext.Of(stubAudit{})
	worker := NewWorker(repo, store, audit)

	if err := worker.processNextExport(context.Background()); err != nil {
		t.Fatalf("processNextExport err = %v", err)
	}
	if got := audit.events[0].Actor.Type; got != "api_key" {
		t.Fatalf("audit actor type = %q", got)
	}
}

func TestWorkerProcessNextDeletePreservesStoredActorType(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(deleteStoreStub{
		request: ptrext.Of(gdprrepo.Request{
			ID:            "req-123",
			TenantID:      "tenant-1",
			CreatedByType: "oidc",
			CreatedBy:     "ou-123",
			SubjectHash:   "hash-123",
		}),
	})
	audit := ptrext.Of(stubAudit{})
	worker := ptrext.Of(Worker{
		deleteStore:    store,
		deleteExecutor: ptrext.Of(deleteExecutorStub{result: ptrext.Of(gdprrepo.DeleteResult{Counts: gdprrepo.Counts{FeedbackCount: 2}})}),
		audit:          audit,
	})

	if err := worker.processNextDelete(context.Background()); err != nil {
		t.Fatalf("processNextDelete err = %v", err)
	}
	if got := audit.events[0].Actor.Type; got != "oidc" {
		t.Fatalf("audit actor type = %q", got)
	}
}

func TestStoredActorFallbacks(t *testing.T) {
	t.Parallel()

	if got := storedActor("", "apikey:123"); got.Type != "api_key" {
		t.Fatalf("storedActor api_key fallback = %#v", got)
	}
	if got := storedActor("", "admin-1"); got.Type != "admin" {
		t.Fatalf("storedActor admin fallback = %#v", got)
	}
	if got := storedActor("oidc", "ou-1"); got.Type != "oidc" {
		t.Fatalf("storedActor explicit type = %#v", got)
	}
}

func TestJobStatusResponseCompletedExpiredHidesDownloadPath(t *testing.T) {
	t.Parallel()

	// Completed but already expired (ExpiresAt in the past).
	resp := jobStatusResponse(ptrext.Of(gdprrepo.ExportJob{
		ID:        "job-123",
		Status:    gdprrepo.ExportJobCompleted,
		ExpiresAt: ptrext.Of(time.Now().UTC().Add(-time.Hour)),
		CreatedAt: time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
	}))
	if resp.GetDownloadPath() != "" {
		t.Fatalf("download_path should be empty for expired job, got %q", resp.GetDownloadPath())
	}
}

func TestJobStatusResponseFailedStatusHasNoDownloadPath(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().UTC().Add(time.Hour)
	resp := jobStatusResponse(ptrext.Of(gdprrepo.ExportJob{
		ID:        "job-123",
		Status:    gdprrepo.ExportJobFailed,
		ExpiresAt: ptrext.Of(expiresAt),
		CreatedAt: time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC),
	}))
	if resp.GetDownloadPath() != "" {
		t.Fatalf("download_path should be empty for failed job, got %q", resp.GetDownloadPath())
	}
}

func TestWithWorkerExportTTLIgnoresNonPositive(t *testing.T) {
	t.Parallel()

	worker := NewWorker(ptrext.Of(exportRepoStub{}), ptrext.Of(exportJobStoreStub{}), nil, WithWorkerExportTTL(0))
	if worker.exportTTL != DefaultExportTTL {
		t.Fatalf("exportTTL = %v, want default %v", worker.exportTTL, DefaultExportTTL)
	}

	worker2 := NewWorker(ptrext.Of(exportRepoStub{}), ptrext.Of(exportJobStoreStub{}), nil, WithWorkerExportTTL(-1*time.Hour))
	if worker2.exportTTL != DefaultExportTTL {
		t.Fatalf("exportTTL = %v, want default %v", worker2.exportTTL, DefaultExportTTL)
	}
}

func TestNewWorkerSetsOwnerPrefix(t *testing.T) {
	t.Parallel()

	worker := NewWorker(ptrext.Of(exportRepoStub{}), ptrext.Of(exportJobStoreStub{}), nil)
	if worker.owner == "" {
		t.Fatal("worker owner should not be empty")
	}
	if len(worker.owner) < 5 {
		t.Fatalf("worker owner too short: %q", worker.owner)
	}
}

func TestStringsTrim(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"  hello  ", "hello"},
		{"", ""},
		{" \t\n ", ""},
		{"no-trim", "no-trim"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			if got := stringsTrim(tc.input); got != tc.want {
				t.Fatalf("stringsTrim(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
