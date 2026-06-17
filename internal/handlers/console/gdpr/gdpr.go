// SPDX-License-Identifier: Apache-2.0

package gdpr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	httpbody "google.golang.org/genproto/googleapis/api/httpbody"

	"github.com/Phixsura/attune/internal/dispatcher"
	consoleauth "github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	gdprsvc "github.com/Phixsura/attune/internal/service/gdpr"
)

type service interface {
	StartExport(ctx context.Context, tenantID, subjectKey string, actor auditlogsvc.Actor) (*attunev1.ExportGdprSubjectResponse, error)
	GetExportJob(ctx context.Context, tenantID, jobID string) (*attunev1.GdprExportStatusResponse, error)
	DownloadExport(ctx context.Context, tenantID, jobID string) (*gdprsvc.ExportBundle, error)
	RevokeExport(ctx context.Context, tenantID, jobID string, actor auditlogsvc.Actor) (*attunev1.RevokeGdprExportResponse, error)
	Delete(ctx context.Context, tenantID, subjectKey string, actor auditlogsvc.Actor) (*gdprrepo.DeleteResult, error)
	CancelDeleteRequest(ctx context.Context, tenantID, requestID string, actor auditlogsvc.Actor) error
	ListRequests(ctx context.Context, tenantID, cursor string, limit int, requestType string) (*attunev1.ListGdprRequestsResponse, error)
	GetOperations(ctx context.Context, tenantID string, stepUp gdprsvc.StepUpStatus) (*attunev1.GdprOperationsResponse, error)
}

type Handler struct {
	svc       service
	signer    *session.Signer
	admins    adminReader
	stepUpTTL time.Duration
}

type adminReader interface {
	GetByID(ctx context.Context, id string) (admin.Admin, error)
}

func NewHandler(svc service, signer *session.Signer, admins adminReader, stepUpTTL time.Duration) *Handler {
	if stepUpTTL <= 0 {
		stepUpTTL = 15 * time.Minute
	}
	return ptrext.Of(Handler{
		svc:       svc,
		signer:    signer,
		admins:    admins,
		stepUpTTL: stepUpTTL,
	})
}

func (h *Handler) Export(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ExportGdprSubjectRequest,
) (dispatcher.Result[*attunev1.ExportGdprSubjectResponse], error) {
	if err := h.requireStepUp(ctx.Auth); err != nil {
		return dispatcher.Result[*attunev1.ExportGdprSubjectResponse]{}, err
	}
	resp, err := h.svc.StartExport(ctx, ctx.Auth.TenantID, req.GetSubjectKey(), auditActor(ctx.Auth, ctx.Request()))
	if err != nil {
		return dispatcher.Result[*attunev1.ExportGdprSubjectResponse]{}, mapError(err)
	}
	return dispatcher.OK(resp)
}

func (h *Handler) GetExport(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetGdprExportRequest,
) (dispatcher.Result[*attunev1.GdprExportStatusResponse], error) {
	resp, err := h.svc.GetExportJob(ctx, ctx.Auth.TenantID, req.GetJobId())
	if err != nil {
		return dispatcher.Result[*attunev1.GdprExportStatusResponse]{}, mapError(err)
	}
	return dispatcher.OK(resp)
}

func (h *Handler) DownloadExport(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DownloadGdprExportRequest,
) (dispatcher.Result[*httpbody.HttpBody], error) {
	bundle, err := h.svc.DownloadExport(ctx, ctx.Auth.TenantID, req.GetJobId())
	if err != nil {
		return dispatcher.Result[*httpbody.HttpBody]{}, mapError(err)
	}
	ctx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", bundle.Filename))
	ctx.SetHeader("Cache-Control", "private, no-store")
	ctx.SetHeader("Pragma", "no-cache")
	ctx.SetHeader("X-Content-Type-Options", "nosniff")
	return dispatcher.OK(ptrext.Of(httpbody.HttpBody{
		ContentType: "application/zip",
		Data:        bundle.Data,
	}))
}

func (h *Handler) RevokeExport(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RevokeGdprExportRequest,
) (dispatcher.Result[*attunev1.RevokeGdprExportResponse], error) {
	if err := h.requireStepUp(ctx.Auth); err != nil {
		return dispatcher.Result[*attunev1.RevokeGdprExportResponse]{}, err
	}
	resp, err := h.svc.RevokeExport(ctx, ctx.Auth.TenantID, req.GetJobId(), auditActor(ctx.Auth, ctx.Request()))
	if err != nil {
		return dispatcher.Result[*attunev1.RevokeGdprExportResponse]{}, mapError(err)
	}
	return dispatcher.OK(resp)
}

func (h *Handler) Delete(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DeleteGdprSubjectRequest,
) (dispatcher.Result[*attunev1.DeleteGdprSubjectResponse], error) {
	if err := h.requireStepUp(ctx.Auth); err != nil {
		return dispatcher.Result[*attunev1.DeleteGdprSubjectResponse]{}, err
	}
	result, err := h.svc.Delete(ctx, ctx.Auth.TenantID, req.GetSubjectKey(), auditActor(ctx.Auth, ctx.Request()))
	if err != nil {
		return dispatcher.Result[*attunev1.DeleteGdprSubjectResponse]{}, mapError(err)
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteGdprSubjectResponse{
		RequestId:          result.RequestID,
		Status:             requestStatusProto(result.Status),
		ExecuteAfter:       ptrext.Map(result.ExecuteAfter, func(v time.Time) string { return v.UTC().Format(time.RFC3339) }),
		SubjectKey:         result.SubjectKey,
		FeedbackCount:      int32(result.Counts.FeedbackCount),
		TagAssignmentCount: int32(result.Counts.TagAssignmentCount),
		FeedbackAuditCount: int32(result.Counts.FeedbackAuditCount),
		LlmAuditCount:      int32(result.Counts.LLMAuditCount),
	}))
}

func (h *Handler) CancelRequest(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CancelGdprRequestRequest,
) (dispatcher.Result[*attunev1.CancelGdprRequestResponse], error) {
	if err := h.requireStepUp(ctx.Auth); err != nil {
		return dispatcher.Result[*attunev1.CancelGdprRequestResponse]{}, err
	}
	if err := h.svc.CancelDeleteRequest(ctx, ctx.Auth.TenantID, req.GetRequestId(), auditActor(ctx.Auth, ctx.Request())); err != nil {
		return dispatcher.Result[*attunev1.CancelGdprRequestResponse]{}, mapError(err)
	}
	return dispatcher.OK(ptrext.Of(attunev1.CancelGdprRequestResponse{
		RequestId: req.GetRequestId(),
		Status:    attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_CANCELLED,
	}))
}

func BindListRequests(r *http.Request, req *attunev1.ListGdprRequestsRequest) error {
	q := r.URL.Query()
	if cursor := strings.TrimSpace(q.Get("cursor")); cursor != "" {
		req.Cursor = ptrext.Of(cursor)
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "limit must be an integer")
		}
		req.Limit = int32(limit)
	}
	req.RequestType = firstQueryValue(q, "request_type", "requestType", "type")
	return nil
}

func (h *Handler) ListRequests(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListGdprRequestsRequest,
) (dispatcher.Result[*attunev1.ListGdprRequestsResponse], error) {
	resp, err := h.svc.ListRequests(ctx, ctx.Auth.TenantID, req.GetCursor(), int(req.GetLimit()), req.GetRequestType())
	if err != nil {
		return dispatcher.Result[*attunev1.ListGdprRequestsResponse]{}, mapError(err)
	}
	return dispatcher.OK(resp)
}

func (h *Handler) GetOperations(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetGdprOperationsRequest,
) (dispatcher.Result[*attunev1.GdprOperationsResponse], error) {
	resp, err := h.svc.GetOperations(ctx, ctx.Auth.TenantID, h.stepUpStatus(ctx.Auth))
	if err != nil {
		return dispatcher.Result[*attunev1.GdprOperationsResponse]{}, mapError(err)
	}
	return dispatcher.OK(resp)
}

func (h *Handler) VerifyStepUp(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.VerifyGdprStepUpRequest,
) (dispatcher.Result[*attunev1.VerifyGdprStepUpResponse], error) {
	status, err := h.verifyStepUp(ctx, req)
	if err != nil {
		return dispatcher.Result[*attunev1.VerifyGdprStepUpResponse]{}, err
	}
	return dispatcher.OK(ptrext.Of(attunev1.VerifyGdprStepUpResponse{StepUp: status}))
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gdprsvc.ErrInvalidSubjectKey):
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
	case errors.Is(err, gdprsvc.ErrSubjectNotFound):
		return dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "subject data not found")
	case errors.Is(err, gdprsvc.ErrExportJobNotFound):
		return dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "gdpr export job not found")
	case errors.Is(err, gdprsvc.ErrExportJobNotDownloadable):
		return dispatcher.NewError(http.StatusConflict, attunev1.ErrorCode_CONFLICT, "gdpr export archive is not ready for download")
	case errors.Is(err, gdprsvc.ErrExportJobNotRevocable):
		return dispatcher.NewError(http.StatusConflict, attunev1.ErrorCode_CONFLICT, "gdpr export archive is not revocable")
	case errors.Is(err, gdprrepo.ErrDeleteRequestNotCancellable):
		return dispatcher.NewError(http.StatusConflict, attunev1.ErrorCode_CONFLICT, "gdpr delete request is not cancellable")
	default:
		return dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to process GDPR request")
	}
}

func (h *Handler) requireStepUp(auth *session.AuthCtx) error {
	if h.stepUpSatisfied(auth) {
		return nil
	}
	return dispatcher.NewError(http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "recent step-up authentication required")
}

func (h *Handler) verifyStepUp(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.VerifyGdprStepUpRequest,
) (*attunev1.GdprStepUpStatus, error) {
	auth := ctx.Auth
	method := "session"
	if auth.UserType == "" || auth.UserType == "admin" {
		adminRow, err := h.admins.GetByID(ctx, auth.UserID)
		switch {
		case errors.Is(err, admin.ErrNotFound):
			return nil, dispatcher.NewError(http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "only console admins can verify password step-up")
		case err != nil:
			return nil, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load admin account")
		}
		password := strings.TrimSpace(req.GetPassword())
		if password == "" {
			return nil, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "password is required for step-up verification")
		}
		if !consoleauth.VerifyOrDummy(adminRow.PasswordHash, password) {
			return nil, dispatcher.NewError(http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "password verification failed")
		}
		method = "password"
	} else if h.stepUpSatisfied(auth) {
		return stepUpStatusProto(gdprsvc.StepUpStatus{
			Satisfied:       true,
			PasswordAllowed: false,
			Method:          "session",
			VerifiedAt:      auth.StepUpAt,
			ExpiresAt:       authStepUpExpiry(auth.StepUpAt, h.stepUpTTL),
		}, h.stepUpTTL), nil
	}

	if err := h.signer.RefreshStepUpCookie(ctx, auth); err != nil {
		return nil, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to refresh session")
	}
	now := time.Now().UTC()
	return stepUpStatusProto(gdprsvc.StepUpStatus{
		Satisfied:       true,
		PasswordAllowed: auth.UserType == "" || auth.UserType == "admin",
		Method:          method,
		VerifiedAt:      ptrext.Of(now),
		ExpiresAt:       ptrext.Of(now.Add(h.stepUpTTL)),
	}, h.stepUpTTL), nil
}

func (h *Handler) stepUpStatus(auth *session.AuthCtx) gdprsvc.StepUpStatus {
	return gdprsvc.StepUpStatus{
		Satisfied:       h.stepUpSatisfied(auth),
		PasswordAllowed: auth.UserType == "" || auth.UserType == "admin",
		Method:          stepUpMethod(auth),
		VerifiedAt:      auth.StepUpAt,
		ExpiresAt:       authStepUpExpiry(auth.StepUpAt, h.stepUpTTL),
	}
}

func (h *Handler) stepUpSatisfied(auth *session.AuthCtx) bool {
	if auth.StepUpAt == nil || auth.StepUpAt.IsZero() {
		return false
	}
	return auth.StepUpAt.Add(h.stepUpTTL).After(time.Now())
}

func authStepUpExpiry(verifiedAt *time.Time, ttl time.Duration) *time.Time {
	if verifiedAt == nil || verifiedAt.IsZero() {
		return nil
	}
	return ptrext.Of(verifiedAt.Add(ttl))
}

func stepUpMethod(auth *session.AuthCtx) string {
	if auth.StepUpAt == nil || auth.StepUpAt.IsZero() {
		return ""
	}
	return "session"
}

func firstQueryValue(q url.Values, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(q.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func stepUpStatusProto(status gdprsvc.StepUpStatus, ttl time.Duration) *attunev1.GdprStepUpStatus {
	resp := ptrext.Of(attunev1.GdprStepUpStatus{
		Satisfied:       status.Satisfied,
		PasswordAllowed: status.PasswordAllowed,
		Method:          status.Method,
		TtlSeconds:      int32(ttl.Seconds()),
	})
	if status.VerifiedAt != nil {
		resp.VerifiedAt = ptrext.Of(status.VerifiedAt.UTC().Format(time.RFC3339))
	}
	if status.ExpiresAt != nil {
		resp.ExpiresAt = ptrext.Of(status.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return resp
}

func requestStatusProto(status gdprrepo.RequestStatus) attunev1.GdprRequestStatus {
	switch status {
	case gdprrepo.RequestStatusQueued:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_QUEUED
	case gdprrepo.RequestStatusRunning:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_RUNNING
	case gdprrepo.RequestStatusReady:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_READY
	case gdprrepo.RequestStatusDownloaded:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_DOWNLOADED
	case gdprrepo.RequestStatusCompleted:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_COMPLETED
	case gdprrepo.RequestStatusFailed:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_FAILED
	case gdprrepo.RequestStatusExpired:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_EXPIRED
	case gdprrepo.RequestStatusScheduled:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_SCHEDULED
	case gdprrepo.RequestStatusCancelled:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_CANCELLED
	default:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_UNSPECIFIED
	}
}

func auditActor(auth *session.AuthCtx, req *http.Request) auditlogsvc.Actor {
	actorType := auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	return auditlogsvc.ActorFromRequest(actorType, auth.UserID, req)
}
