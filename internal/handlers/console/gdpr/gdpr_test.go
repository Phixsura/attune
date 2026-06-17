package gdpr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	gdprsvc "github.com/Phixsura/attune/internal/service/gdpr"
)

type fakeService struct {
	startExportResp  *attunev1.ExportGdprSubjectResponse
	getExportResp    *attunev1.GdprExportStatusResponse
	downloadBundle   *gdprsvc.ExportBundle
	revokeExportResp *attunev1.RevokeGdprExportResponse
	exportErr        error
	deleteResult     *gdprrepo.DeleteResult
	deleteErr        error
	cancelRequestID  string
	listResp         *attunev1.ListGdprRequestsResponse
	opsResp          *attunev1.GdprOperationsResponse
}

func (f *fakeService) StartExport(context.Context, string, string, auditlogsvc.Actor) (*attunev1.ExportGdprSubjectResponse, error) {
	return f.startExportResp, f.exportErr
}

func (f *fakeService) GetExportJob(context.Context, string, string) (*attunev1.GdprExportStatusResponse, error) {
	return f.getExportResp, f.exportErr
}

func (f *fakeService) DownloadExport(context.Context, string, string) (*gdprsvc.ExportBundle, error) {
	return f.downloadBundle, f.exportErr
}

func (f *fakeService) RevokeExport(context.Context, string, string, auditlogsvc.Actor) (*attunev1.RevokeGdprExportResponse, error) {
	return f.revokeExportResp, f.exportErr
}

func (f *fakeService) Delete(context.Context, string, string, auditlogsvc.Actor) (*gdprrepo.DeleteResult, error) {
	return f.deleteResult, f.deleteErr
}

func (f *fakeService) CancelDeleteRequest(context.Context, string, string, auditlogsvc.Actor) error {
	f.cancelRequestID = "called"
	return f.deleteErr
}

func (f *fakeService) ListRequests(context.Context, string, string, int, string) (*attunev1.ListGdprRequestsResponse, error) {
	return f.listResp, f.exportErr
}

func (f *fakeService) GetOperations(context.Context, string, gdprsvc.StepUpStatus) (*attunev1.GdprOperationsResponse, error) {
	return f.opsResp, f.exportErr
}

func TestExportReturnsQueuedJob(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		startExportResp: ptrext.Of(attunev1.ExportGdprSubjectResponse{
			JobId:             "job-123",
			Status:            attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_QUEUED,
			RetryAfterSeconds: 2,
		}),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
			StepUpAt: ptrext.Of(time.Now().Add(time.Minute)),
		}),
	})
	result, err := h.Export(ctx, ptrext.Of(attunev1.ExportGdprSubjectRequest{SubjectKey: "alice@example.com"}))
	if err != nil {
		t.Fatalf("Export err = %v", err)
	}
	if result.Body.GetJobId() != "job-123" || result.Body.GetRetryAfterSeconds() != 2 {
		t.Fatalf("Export response = %#v", result.Body)
	}
}

func TestGetExportMapsStatus(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		getExportResp: ptrext.Of(attunev1.GdprExportStatusResponse{
			JobId:          "job-123",
			SubjectKey:     "alice@example.com",
			SubjectDisplay: "Alice",
			Status:         attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_COMPLETED,
		}),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}),
	})
	result, err := h.GetExport(ctx, ptrext.Of(attunev1.GetGdprExportRequest{JobId: "job-123"}))
	if err != nil {
		t.Fatalf("GetExport err = %v", err)
	}
	if result.Body.GetJobId() != "job-123" || result.Body.GetSubjectKey() != "alice@example.com" {
		t.Fatalf("GetExport response = %#v", result.Body)
	}
}

func TestDownloadExportWritesAttachmentResponse(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		downloadBundle: ptrext.Of(gdprsvc.ExportBundle{
			Filename: "gdpr-export.zip",
			Data:     []byte("zip-bytes"),
		}),
	}), nil, nil, 0)
	route := dispatcher.Bind(
		"console.gdpr.DownloadExport",
		dispatcher.Empty(func() *attunev1.DownloadGdprExportRequest {
			return ptrext.Of(attunev1.DownloadGdprExportRequest{})
		}),
		h.DownloadExport,
		dispatcher.WithBinders(
			dispatcher.Param("job_id", func(req *attunev1.DownloadGdprExportRequest, id string) {
				req.JobId = id
			}),
		),
		dispatcher.WithAuth(func(_ *http.Request, _ *attunev1.DownloadGdprExportRequest) (*session.AuthCtx, error) {
			return ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}), nil
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/gdpr/exports/job-123/download", http.NoBody)
	req.SetPathValue("job_id", "job-123")
	rec := httptest.NewRecorder()
	route(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got == "" {
		t.Fatal("expected Content-Disposition header")
	}
	if rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "zip-bytes" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestDeleteMapsCounts(t *testing.T) {
	t.Parallel()

	executeAfter := time.Now().UTC().Add(15 * time.Minute)
	h := NewHandler(ptrext.Of(fakeService{
		deleteResult: ptrext.Of(gdprrepo.DeleteResult{
			RequestID:    "req-123",
			SubjectKey:   "alice@example.com",
			Status:       gdprrepo.RequestStatusScheduled,
			ExecuteAfter: ptrext.Of(executeAfter),
			Counts: gdprrepo.Counts{
				FeedbackCount:      3,
				TagAssignmentCount: 2,
				FeedbackAuditCount: 1,
				LLMAuditCount:      4,
			},
		}),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
			StepUpAt: ptrext.Of(time.Now().Add(time.Minute)),
		}),
	})
	result, err := h.Delete(ctx, ptrext.Of(attunev1.DeleteGdprSubjectRequest{SubjectKey: "alice@example.com"}))
	if err != nil {
		t.Fatalf("Delete err = %v", err)
	}
	if result.Body.GetFeedbackCount() != 3 || result.Body.GetLlmAuditCount() != 4 {
		t.Fatalf("Delete response = %#v", result.Body)
	}
	if result.Body.GetRequestId() != "req-123" {
		t.Fatalf("Delete request id = %q", result.Body.GetRequestId())
	}
	if result.Body.GetStatus() != attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_SCHEDULED {
		t.Fatalf("Delete status = %v", result.Body.GetStatus())
	}
	if result.Body.GetExecuteAfter() == "" {
		t.Fatal("expected execute_after in response")
	}
}

func TestRevokeExportReturnsRevokedStatus(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		revokeExportResp: ptrext.Of(attunev1.RevokeGdprExportResponse{
			JobId:         "job-123",
			Status:        attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_REVOKED,
			RequestStatus: attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_REVOKED,
		}),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
			StepUpAt: ptrext.Of(time.Now().Add(time.Minute)),
		}),
	})
	result, err := h.RevokeExport(ctx, ptrext.Of(attunev1.RevokeGdprExportRequest{JobId: "job-123"}))
	if err != nil {
		t.Fatalf("RevokeExport err = %v", err)
	}
	if result.Body.GetStatus() != attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_REVOKED {
		t.Fatalf("RevokeExport status = %v", result.Body.GetStatus())
	}
	if result.Body.GetRequestStatus() != attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_REVOKED {
		t.Fatalf("RevokeExport request status = %v", result.Body.GetRequestStatus())
	}
}

func TestExportRequiresRecentStepUp(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}),
	})
	_, err := h.Export(ctx, ptrext.Of(attunev1.ExportGdprSubjectRequest{SubjectKey: "alice@example.com"}))
	if err == nil {
		t.Fatal("expected step-up requirement error")
	}
}

func TestCancelRequestReturnsCancelledStatus(t *testing.T) {
	t.Parallel()

	fake := ptrext.Of(fakeService{})
	h := NewHandler(fake, nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
			StepUpAt: ptrext.Of(time.Now().Add(time.Minute)),
		}),
	})
	result, err := h.CancelRequest(ctx, ptrext.Of(attunev1.CancelGdprRequestRequest{RequestId: "req-123"}))
	if err != nil {
		t.Fatalf("CancelRequest err = %v", err)
	}
	if result.Body.GetRequestId() != "req-123" {
		t.Fatalf("CancelRequest id = %q", result.Body.GetRequestId())
	}
	if result.Body.GetStatus() != attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_CANCELLED {
		t.Fatalf("CancelRequest status = %v", result.Body.GetStatus())
	}
}

func TestBindListRequestsParsesAliasesAndLimit(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/gdpr/requests?cursor=abc&limit=15&requestType=delete", http.NoBody)
	protoReq := ptrext.Of(attunev1.ListGdprRequestsRequest{})
	if err := BindListRequests(req, protoReq); err != nil {
		t.Fatalf("BindListRequests err = %v", err)
	}
	if protoReq.GetCursor() != "abc" || protoReq.GetLimit() != 15 || protoReq.GetRequestType() != "delete" {
		t.Fatalf("parsed request = %#v", protoReq)
	}
}

func TestBindListRequestsRejectsNonIntegerLimit(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/gdpr/requests?limit=nope", http.NoBody)
	err := BindListRequests(req, ptrext.Of(attunev1.ListGdprRequestsRequest{}))
	if err == nil {
		t.Fatal("expected BindListRequests error")
	}
}

func TestListRequestsAndOperationsProxyServiceResponses(t *testing.T) {
	t.Parallel()

	fake := ptrext.Of(fakeService{
		listResp: ptrext.Of(attunev1.ListGdprRequestsResponse{
			Items: []*attunev1.GdprRequestSummary{{RequestId: "req-1"}},
		}),
		opsResp: ptrext.Of(attunev1.GdprOperationsResponse{
			QueuedRequestCount: 3,
		}),
	})
	h := NewHandler(fake, nil, nil, 20*time.Minute)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
		}),
	})

	listResult, err := h.ListRequests(ctx, ptrext.Of(attunev1.ListGdprRequestsRequest{
		Cursor:      ptrext.Of("cursor-1"),
		Limit:       10,
		RequestType: "export",
	}))
	if err != nil {
		t.Fatalf("ListRequests err = %v", err)
	}
	if len(listResult.Body.GetItems()) != 1 || listResult.Body.GetItems()[0].GetRequestId() != "req-1" {
		t.Fatalf("list result = %#v", listResult.Body)
	}

	opsResult, err := h.GetOperations(ctx, ptrext.Of(attunev1.GetGdprOperationsRequest{}))
	if err != nil {
		t.Fatalf("GetOperations err = %v", err)
	}
	if opsResult.Body.GetQueuedRequestCount() != 3 {
		t.Fatalf("ops result = %#v", opsResult.Body)
	}
}

func TestMapErrorAndHelpers(t *testing.T) {
	t.Parallel()

	cases := []error{
		gdprsvc.ErrInvalidSubjectKey,
		gdprsvc.ErrSubjectNotFound,
		gdprsvc.ErrExportJobNotFound,
		gdprsvc.ErrExportJobNotDownloadable,
		gdprsvc.ErrExportJobNotRevocable,
		gdprrepo.ErrDeleteRequestNotCancellable,
		errors.New("boom"),
	}
	for _, input := range cases {
		if mapError(input) == nil {
			t.Fatalf("expected mapped error for %v", input)
		}
	}

	now := time.Now().UTC()
	expiry := authStepUpExpiry(ptrext.Of(now), 5*time.Minute)
	if expiry == nil || !expiry.After(now) {
		t.Fatalf("expiry = %#v", expiry)
	}
	if authStepUpExpiry(nil, 5*time.Minute) != nil {
		t.Fatal("expected nil expiry for nil timestamp")
	}
	if stepUpMethod(ptrext.Of(session.AuthCtx{})) != "" {
		t.Fatal("expected empty method without step-up time")
	}
	if stepUpMethod(ptrext.Of(session.AuthCtx{StepUpAt: ptrext.Of(now)})) != "session" {
		t.Fatal("expected session step-up method")
	}
	if firstQueryValue(url.Values{"type": []string{" export "}}, "request_type", "type") != "export" {
		t.Fatal("expected firstQueryValue to trim and return alias")
	}
	statusCases := map[gdprrepo.RequestStatus]attunev1.GdprRequestStatus{
		gdprrepo.RequestStatusQueued:     attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_QUEUED,
		gdprrepo.RequestStatusRunning:    attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_RUNNING,
		gdprrepo.RequestStatusReady:      attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_READY,
		gdprrepo.RequestStatusDownloaded: attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_DOWNLOADED,
		gdprrepo.RequestStatusCompleted:  attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_COMPLETED,
		gdprrepo.RequestStatusFailed:     attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_FAILED,
		gdprrepo.RequestStatusExpired:    attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_EXPIRED,
		gdprrepo.RequestStatusScheduled:  attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_SCHEDULED,
		gdprrepo.RequestStatusCancelled:  attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_CANCELLED,
		gdprrepo.RequestStatusRevoked:    attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_UNSPECIFIED,
	}
	for input, want := range statusCases {
		if got := requestStatusProto(input); got != want {
			t.Fatalf("requestStatusProto(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestVerifyStepUpReusesSatisfiedSessionForNonAdmin(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	h := NewHandler(ptrext.Of(fakeService{}), nil, nil, 15*time.Minute)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "member-1",
			UserType: "member",
			StepUpAt: ptrext.Of(now),
		}),
	})

	result, err := h.VerifyStepUp(ctx, ptrext.Of(attunev1.VerifyGdprStepUpRequest{}))
	if err != nil {
		t.Fatalf("VerifyStepUp err = %v", err)
	}
	if !result.Body.GetStepUp().GetSatisfied() || result.Body.GetStepUp().GetMethod() != "session" {
		t.Fatalf("step-up status = %#v", result.Body.GetStepUp())
	}
	if result.Body.GetStepUp().GetVerifiedAt() == "" || result.Body.GetStepUp().GetExpiresAt() == "" {
		t.Fatalf("expected timestamps in step-up status: %#v", result.Body.GetStepUp())
	}
}
