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
		"manifest.json":                         false,
		"feedback.jsonl":                        false,
		"feedback_tags.jsonl":                   false,
		"feedback_audit_log.jsonl":              false,
		"llm_audit.jsonl":                       false,
		"reply_drafts.jsonl":                    false,
		"reply_draft_revisions.jsonl":           false,
		"reply_draft_events.jsonl":              false,
		"reply_delivery_attempts.jsonl":         false,
		"customer_request_customer_links.jsonl": false,
		"customer_request_votes.jsonl":          false,
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

func TestDeleteDirectlyExecutesWhenRequestStoreUnavailable(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{
		deleteResult: ptrext.Of(gdprrepo.DeleteResult{
			SubjectKey: "alice@example.com",
			Counts: gdprrepo.Counts{
				FeedbackCount: 1,
			},
		}),
	})
	audit := ptrext.Of(stubAudit{})
	svc := New(repo, audit)

	result, err := svc.Delete(context.Background(), "tenant-1", "alice@example.com", auditlogsvc.Actor{Type: "admin", ID: "u-1"})
	if err != nil {
		t.Fatalf("Delete err = %v", err)
	}
	if result.SubjectKey != "alice@example.com" {
		t.Fatalf("result = %#v", result)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "gdpr.delete" {
		t.Fatalf("expected gdpr.delete audit event, got %#v", audit.events)
	}
}

type schedulingRepo struct {
	deleteResult       *gdprrepo.DeleteResult
	cancelledRequest   *gdprrepo.Request
	revokedJob         *gdprrepo.ExportJob
	listResult         gdprrepo.ListRequestResult
	operationsSummary  *gdprrepo.OperationsSummary
	createByType       string
	createBy           string
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
	return s.listResult, nil
}

func (s *schedulingRepo) GetOperationsSummary(context.Context, string) (*gdprrepo.OperationsSummary, error) {
	if s.operationsSummary == nil {
		return ptrext.Of(gdprrepo.OperationsSummary{}), nil
	}
	return s.operationsSummary, nil
}

func (s *schedulingRepo) CreateDeleteRequest(_ context.Context, _, _, subjectHash, createdByType, createdBy string, executeAfter time.Time) (*gdprrepo.DeleteResult, error) {
	s.createByType = createdByType
	s.createBy = createdBy
	s.createExecuteAfter = executeAfter
	s.createSubjectHash = subjectHash
	return s.deleteResult, nil
}

func (s *schedulingRepo) CancelDeleteRequest(_ context.Context, _, requestID string) (*gdprrepo.Request, error) {
	s.cancelRequestID = requestID
	return s.cancelledRequest, nil
}

func (s *schedulingRepo) CreateExportJob(context.Context, string, string, string, string, string) (*gdprrepo.ExportJob, error) {
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

func (s *schedulingRepo) ClaimNextExportJobWithOwner(context.Context, string) (*gdprrepo.ExportJob, error) {
	return nil, nil
}

func (s *schedulingRepo) HeartbeatExportJob(context.Context, string) error {
	return nil
}

func (s *schedulingRepo) HeartbeatExportJobWithOwner(context.Context, string, string) (int64, error) {
	return 1, nil
}

func (s *schedulingRepo) CompleteExportJob(context.Context, string, string, string, []byte, gdprrepo.Counts, time.Time) error {
	return nil
}

func (s *schedulingRepo) CompleteExportJobWithOwner(context.Context, string, string, string, string, []byte, gdprrepo.Counts, time.Time) (int64, error) {
	return 1, nil
}

func (s *schedulingRepo) FailExportJob(context.Context, string, string) error {
	return nil
}

func (s *schedulingRepo) FailExportJobWithOwner(context.Context, string, string, string) (int64, error) {
	return 1, nil
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
	if repo.createByType != "admin" || repo.createBy != "u-1" {
		t.Fatalf("actor persistence = type:%q id:%q", repo.createByType, repo.createBy)
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

func TestTranslateRepoErrorAndRecordNilAudit(t *testing.T) {
	t.Parallel()

	if got := translateRepoError(ErrInvalidSubjectKey); !errors.Is(got, ErrInvalidSubjectKey) {
		t.Fatalf("translateRepoError invalid subject = %v", got)
	}
	if got := translateRepoError(errors.New("boom")); got == nil || got.Error() != "boom" {
		t.Fatalf("translateRepoError passthrough = %v", got)
	}

	svc := New(ptrext.Of(stubRepo{}), nil)
	if err := svc.record(context.Background(), "tenant-1", "alice@example.com", auditlogsvc.Actor{}, "gdpr.export", "summary", nil, nil); err != nil {
		t.Fatalf("record err = %v", err)
	}
}

func TestRecordDeleteCompletionAudits(t *testing.T) {
	t.Parallel()

	audit := ptrext.Of(stubAudit{})
	svc := New(ptrext.Of(stubRepo{}), audit)

	err := svc.RecordDeleteCompletion(
		context.Background(),
		"tenant-1",
		"alice@example.com",
		auditlogsvc.Actor{Type: "admin", ID: "u-1"},
		ptrext.Of(gdprrepo.DeleteResult{
			Counts: gdprrepo.Counts{
				FeedbackCount:      2,
				TagAssignmentCount: 3,
				FeedbackAuditCount: 4,
				LLMAuditCount:      5,
			},
		}),
	)
	if err != nil {
		t.Fatalf("RecordDeleteCompletion err = %v", err)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "gdpr.delete" {
		t.Fatalf("expected gdpr.delete audit event, got %#v", audit.events)
	}
}

func TestListRequestsMapsProtoFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	startedAt := now.Add(time.Minute)
	completedAt := now.Add(2 * time.Minute)
	expiresAt := now.Add(time.Hour)
	executeAfter := now.Add(30 * time.Minute)
	downloadedAt := now.Add(3 * time.Minute)
	cancelledAt := now.Add(4 * time.Minute)
	revokedAt := now.Add(5 * time.Minute)

	repo := ptrext.Of(schedulingRepo{
		listResult: gdprrepo.ListRequestResult{
			Items: []gdprrepo.Request{
				{
					ID:              "req-1",
					RequestType:     gdprrepo.RequestTypeExport,
					Status:          gdprrepo.RequestStatusDownloaded,
					SubjectKey:      "alice@example.com",
					SubjectDisplay:  "Alice",
					SubjectHash:     subjectkey.Hash("tenant-1", "alice@example.com"),
					ArchiveFilename: "alice.zip",
					Error:           "warning",
					CreatedBy:       "admin-1",
					Counts: gdprrepo.Counts{
						FeedbackCount:      2,
						TagAssignmentCount: 3,
						FeedbackAuditCount: 4,
						LLMAuditCount:      5,
					},
					CreatedAt:    now,
					StartedAt:    ptrext.Of(startedAt),
					CompletedAt:  ptrext.Of(completedAt),
					ExpiresAt:    ptrext.Of(expiresAt),
					ExecuteAfter: ptrext.Of(executeAfter),
					DownloadedAt: ptrext.Of(downloadedAt),
					CancelledAt:  ptrext.Of(cancelledAt),
					RevokedAt:    ptrext.Of(revokedAt),
				},
			},
			NextCursor: "cursor-2",
		},
	})
	svc := New(
		repo,
		ptrext.Of(stubAudit{}),
		WithAuditRetention(90, 12*time.Hour),
		WithExportTTL(4*time.Hour),
		WithStepUpTTL(20*time.Minute),
		WithDeleteGraceWindow(45*time.Minute),
	)

	listResp, err := svc.ListRequests(context.Background(), "tenant-1", "", 25, "export")
	if err != nil {
		t.Fatalf("ListRequests err = %v", err)
	}
	if len(listResp.Items) != 1 {
		t.Fatalf("items len = %d", len(listResp.Items))
	}
	item := listResp.Items[0]
	if item.GetRequestId() != "req-1" || item.GetRequestType() != attunev1.GdprRequestType_GDPR_REQUEST_TYPE_EXPORT {
		t.Fatalf("list item = %#v", item)
	}
	if item.GetStatus() != attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_DOWNLOADED {
		t.Fatalf("status = %v", item.GetStatus())
	}
	if item.GetArchiveFilename() != "alice.zip" || item.GetError() != "warning" {
		t.Fatalf("archive fields = %#v", item)
	}
	if listResp.GetNextCursor() != "cursor-2" {
		t.Fatalf("next cursor = %q", listResp.GetNextCursor())
	}
}

func TestGetOperationsMapsProtoFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	nextExpiry := now.Add(6 * time.Hour)
	repo := ptrext.Of(schedulingRepo{
		operationsSummary: ptrext.Of(gdprrepo.OperationsSummary{
			QueuedRequestCount:   1,
			ActiveRequestCount:   2,
			ReadyExportCount:     3,
			ScheduledDeleteCount: 4,
			NextExportExpiryAt:   ptrext.Of(nextExpiry),
		}),
	})
	svc := New(
		repo,
		ptrext.Of(stubAudit{}),
		WithAuditRetention(90, 12*time.Hour),
		WithExportTTL(4*time.Hour),
		WithStepUpTTL(20*time.Minute),
		WithDeleteGraceWindow(45*time.Minute),
	)

	stepUpVerifiedAt := now.Add(-2 * time.Minute)
	stepUpExpiresAt := now.Add(18 * time.Minute)
	opsResp, err := svc.GetOperations(context.Background(), "tenant-1", StepUpStatus{
		Satisfied:       true,
		PasswordAllowed: true,
		Method:          "password",
		VerifiedAt:      ptrext.Of(stepUpVerifiedAt),
		ExpiresAt:       ptrext.Of(stepUpExpiresAt),
	})
	if err != nil {
		t.Fatalf("GetOperations err = %v", err)
	}
	if opsResp.GetExportTtlSeconds() != int32((4 * time.Hour).Seconds()) {
		t.Fatalf("export ttl = %d", opsResp.GetExportTtlSeconds())
	}
	if opsResp.GetAuditRetentionDays() != 90 || opsResp.GetDeleteGraceWindowSeconds() != int32((45*time.Minute).Seconds()) {
		t.Fatalf("operations = %#v", opsResp)
	}
	if opsResp.GetQueuedRequestCount() != 1 || opsResp.GetScheduledDeleteCount() != 4 {
		t.Fatalf("summary counts = %#v", opsResp)
	}
	if opsResp.GetNextExportExpiryAt() == "" || opsResp.GetStepUp().GetMethod() != "password" {
		t.Fatalf("step up = %#v", opsResp.GetStepUp())
	}
}

func TestListRequestsRejectsInvalidRequestType(t *testing.T) {
	t.Parallel()

	svc := New(ptrext.Of(schedulingRepo{}), ptrext.Of(stubAudit{}))
	_, err := svc.ListRequests(context.Background(), "tenant-1", "", 25, "typo")
	if !errors.Is(err, ErrInvalidRequestType) {
		t.Fatalf("ListRequests err = %v, want ErrInvalidRequestType", err)
	}
}

func TestRequestProtoHelpersCoverFallbacks(t *testing.T) {
	t.Parallel()

	requestTypes := map[gdprrepo.RequestType]attunev1.GdprRequestType{
		gdprrepo.RequestTypeExport: attunev1.GdprRequestType_GDPR_REQUEST_TYPE_EXPORT,
		gdprrepo.RequestTypeDelete: attunev1.GdprRequestType_GDPR_REQUEST_TYPE_DELETE,
	}
	for input, want := range requestTypes {
		if got := requestTypeProto(input); got != want {
			t.Fatalf("requestTypeProto(%q) = %v, want %v", input, got, want)
		}
	}
	if requestTypeProto(gdprrepo.RequestType("mystery")) != attunev1.GdprRequestType_GDPR_REQUEST_TYPE_UNSPECIFIED {
		t.Fatal("expected unknown request type fallback")
	}

	statuses := map[gdprrepo.RequestStatus]attunev1.GdprRequestStatus{
		gdprrepo.RequestStatusQueued:     attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_QUEUED,
		gdprrepo.RequestStatusRunning:    attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_RUNNING,
		gdprrepo.RequestStatusReady:      attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_READY,
		gdprrepo.RequestStatusDownloaded: attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_DOWNLOADED,
		gdprrepo.RequestStatusCompleted:  attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_COMPLETED,
		gdprrepo.RequestStatusFailed:     attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_FAILED,
		gdprrepo.RequestStatusExpired:    attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_EXPIRED,
		gdprrepo.RequestStatusScheduled:  attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_SCHEDULED,
		gdprrepo.RequestStatusCancelled:  attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_CANCELLED,
		gdprrepo.RequestStatusRevoked:    attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_REVOKED,
	}
	for input, want := range statuses {
		if got := requestStatusProto(input); got != want {
			t.Fatalf("requestStatusProto(%q) = %v, want %v", input, got, want)
		}
	}
	if requestStatusProto(gdprrepo.RequestStatus("mystery")) != attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_UNSPECIFIED {
		t.Fatal("expected unknown request status fallback")
	}
}
