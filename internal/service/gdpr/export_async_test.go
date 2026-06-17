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

func (s *exportJobStoreStub) CreateExportJob(_ context.Context, _, subjectKey, subjectHash, createdBy string) (*gdprrepo.ExportJob, error) {
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

func (s *exportJobStoreStub) HeartbeatExportJob(_ context.Context, jobID string) error {
	s.heartbeatJobID = jobID
	return nil
}

func (s *exportJobStoreStub) CompleteExportJob(_ context.Context, jobID, _ string, archiveFilename string, archive []byte, counts gdprrepo.Counts, _ time.Time) error {
	s.completeJobID = jobID
	s.completeCounts = counts
	s.completeFilename = archiveFilename
	s.completeArchiveLen = len(archive)
	return nil
}

func (s *exportJobStoreStub) FailExportJob(_ context.Context, jobID, errMsg string) error {
	s.failJobID = jobID
	s.failErrMsg = errMsg
	return nil
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

	resp, err := svc.StartExport(context.Background(), "tenant-1", "  alice@example.com  ", auditlogsvc.Actor{ID: "admin-1"})
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
			ID:          "req-123",
			TenantID:    "tenant-1",
			CreatedBy:   "admin-1",
			SubjectHash: "hash-123",
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
