// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package gdpr

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
)

func TestExportServiceError(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		exportErr: errors.New("db down"),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
			StepUpAt: ptrext.Of(time.Now().Add(time.Minute)),
		}),
	})

	_, err := h.Export(ctx, ptrext.Of(attunev1.ExportGdprSubjectRequest{SubjectKey: "alice@example.com"}))
	if err == nil {
		t.Fatal("expected error from Export when service fails")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 dispatcher error, got %v", err)
	}
}

func TestGetExportServiceError(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		exportErr: errors.New("db down"),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}),
	})

	_, err := h.GetExport(ctx, ptrext.Of(attunev1.GetGdprExportRequest{JobId: "job-1"}))
	if err == nil {
		t.Fatal("expected error from GetExport when service fails")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 dispatcher error, got %v", err)
	}
}

func TestDownloadExportServiceError(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		exportErr: errors.New("db down"),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}),
	})

	_, err := h.DownloadExport(ctx, ptrext.Of(attunev1.DownloadGdprExportRequest{JobId: "job-1"}))
	if err == nil {
		t.Fatal("expected error from DownloadExport when service fails")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 dispatcher error, got %v", err)
	}
}

func TestRevokeExportStepUpRequired(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}),
	})

	_, err := h.RevokeExport(ctx, ptrext.Of(attunev1.RevokeGdprExportRequest{JobId: "job-1"}))
	if err == nil {
		t.Fatal("expected step-up error from RevokeExport")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusForbidden {
		t.Fatalf("expected 403 dispatcher error, got %v", err)
	}
}

func TestRevokeExportServiceError(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		exportErr: errors.New("db down"),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
			StepUpAt: ptrext.Of(time.Now().Add(time.Minute)),
		}),
	})

	_, err := h.RevokeExport(ctx, ptrext.Of(attunev1.RevokeGdprExportRequest{JobId: "job-1"}))
	if err == nil {
		t.Fatal("expected error from RevokeExport when service fails")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 dispatcher error, got %v", err)
	}
}

func TestDeleteStepUpRequired(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}),
	})

	_, err := h.Delete(ctx, ptrext.Of(attunev1.DeleteGdprSubjectRequest{SubjectKey: "alice@example.com"}))
	if err == nil {
		t.Fatal("expected step-up error from Delete")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusForbidden {
		t.Fatalf("expected 403 dispatcher error, got %v", err)
	}
}

func TestDeleteServiceError(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		deleteErr: errors.New("db down"),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
			StepUpAt: ptrext.Of(time.Now().Add(time.Minute)),
		}),
	})

	_, err := h.Delete(ctx, ptrext.Of(attunev1.DeleteGdprSubjectRequest{SubjectKey: "alice@example.com"}))
	if err == nil {
		t.Fatal("expected error from Delete when service fails")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 dispatcher error, got %v", err)
	}
}

func TestCancelRequestStepUpRequired(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}),
	})

	_, err := h.CancelRequest(ctx, ptrext.Of(attunev1.CancelGdprRequestRequest{RequestId: "req-1"}))
	if err == nil {
		t.Fatal("expected step-up error from CancelRequest")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusForbidden {
		t.Fatalf("expected 403 dispatcher error, got %v", err)
	}
}

func TestCancelRequestServiceError(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		deleteErr: errors.New("db down"),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
			StepUpAt: ptrext.Of(time.Now().Add(time.Minute)),
		}),
	})

	_, err := h.CancelRequest(ctx, ptrext.Of(attunev1.CancelGdprRequestRequest{RequestId: "req-1"}))
	if err == nil {
		t.Fatal("expected error from CancelRequest when service fails")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 dispatcher error, got %v", err)
	}
}

func TestListRequestsServiceError(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		exportErr: errors.New("db down"),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}),
	})

	_, err := h.ListRequests(ctx, ptrext.Of(attunev1.ListGdprRequestsRequest{}))
	if err == nil {
		t.Fatal("expected error from ListRequests when service fails")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 dispatcher error, got %v", err)
	}
}

func TestGetOperationsServiceError(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeService{
		exportErr: errors.New("db down"),
	}), nil, nil, 0)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-1"}),
	})

	_, err := h.GetOperations(ctx, ptrext.Of(attunev1.GetGdprOperationsRequest{}))
	if err == nil {
		t.Fatal("expected error from GetOperations when service fails")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 dispatcher error, got %v", err)
	}
}

func TestMapErrorNilReturnsNil(t *testing.T) {
	t.Parallel()

	if got := mapError(nil); got != nil {
		t.Fatalf("mapError(nil) = %v, want nil", got)
	}
}

func TestVerifyStepUpAdminReaderInternalError(t *testing.T) {
	t.Parallel()

	h := NewHandler(
		ptrext.Of(fakeService{}),
		nil,
		ptrext.Of(fakeAdminReader{err: errors.New("db connection lost")}),
		15*time.Minute,
	)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
		}),
	})

	_, err := h.VerifyStepUp(ctx, ptrext.Of(attunev1.VerifyGdprStepUpRequest{Password: ptrext.Of("any")}))
	if err == nil {
		t.Fatal("expected error from VerifyStepUp when admin reader fails")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusInternalServerError {
		t.Fatalf("expected 500 dispatcher error, got %v", err)
	}
}

func TestFirstQueryValueAllEmpty(t *testing.T) {
	t.Parallel()

	q := url.Values{
		"request_type": []string{""},
		"requestType":  []string{"  "},
		"type":         []string{""},
	}
	got := firstQueryValue(q, "request_type", "requestType", "type")
	if got != "" {
		t.Fatalf("firstQueryValue with all-empty values = %q, want empty", got)
	}
}

func TestVerifyStepUpAdminNotFoundReturns403(t *testing.T) {
	t.Parallel()

	h := NewHandler(
		ptrext.Of(fakeService{}),
		nil,
		ptrext.Of(fakeAdminReader{err: admin.ErrNotFound}),
		15*time.Minute,
	)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
		}),
	})

	_, err := h.VerifyStepUp(ctx, ptrext.Of(attunev1.VerifyGdprStepUpRequest{Password: ptrext.Of("any")}))
	if err == nil {
		t.Fatal("expected error from VerifyStepUp for missing admin")
	}
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Status != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", err)
	}
}
