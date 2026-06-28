// SPDX-License-Identifier: Apache-2.0

package auditevidence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/genproto/googleapis/api/httpbody"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	aerepo "github.com/Phixsura/attune/internal/repo/auditevidence"
	auditevidencesvc "github.com/Phixsura/attune/internal/service/auditevidence"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type evidenceService interface {
	StartExport(ctx context.Context, tenantID string, actor auditlogsvc.Actor, filter auditevidencesvc.ExportFilter) (*auditevidencesvc.StartExportResult, error)
	GetJob(ctx context.Context, tenantID, jobID string) (*aerepo.ExportJob, error)
	Download(ctx context.Context, tenantID, jobID string, actor auditlogsvc.Actor) (*auditevidencesvc.DownloadBundle, error)
}

type Handler struct {
	svc evidenceService
}

func NewHandler(svc evidenceService) *Handler {
	return ptrext.Of(Handler{svc: svc})
}

func (h *Handler) Create(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateAuditEvidenceExportRequest,
) (dispatcher.Result[*attunev1.CreateAuditEvidenceExportResponse], error) {
	const where = "console.auditevidence.Create"
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, ctx.Auth.TenantID)

	filter, err := buildFilter(req)
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateAuditEvidenceExportResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
	}
	result, err := h.svc.StartExport(ctx, ctx.Auth.TenantID, auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()), filter)
	if err != nil {
		logext.Errorf(ctx, "[%s] start failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateAuditEvidenceExportResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create evidence export")
	}
	return dispatcher.OK(ptrext.Of(attunev1.CreateAuditEvidenceExportResponse{
		JobId:             result.JobID,
		Status:            result.Status,
		RetryAfterSeconds: 2,
	}))
}

func (h *Handler) Get(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetAuditEvidenceExportRequest,
) (dispatcher.Result[*attunev1.GetAuditEvidenceExportResponse], error) {
	const where = "console.auditevidence.Get"
	job, err := h.svc.GetJob(ctx, ctx.Auth.TenantID, req.GetJobId())
	if err != nil {
		return dispatcher.Result[*attunev1.GetAuditEvidenceExportResponse]{}, mapError(err)
	}
	logext.Infof(ctx, "[%s] ok,tenant_id:%s,job_id:%s,status:%s", where, ctx.Auth.TenantID, job.ID, job.Status)
	resp := jobToProto(job)
	if ctx.Auth.UserType == "api_key" && resp.DownloadPath != nil {
		resp.DownloadPath = ptrext.Of("/v1/audit-log/evidence/" + job.ID + "/download")
	}
	return dispatcher.OK(resp)
}

func (h *Handler) Download(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DownloadAuditEvidenceExportRequest,
) (dispatcher.Result[*httpbody.HttpBody], error) {
	const where = "console.auditevidence.Download"
	actor := auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request())
	bundle, err := h.svc.Download(ctx, ctx.Auth.TenantID, req.GetJobId(), actor)
	if err != nil {
		return dispatcher.Result[*httpbody.HttpBody]{}, mapError(err)
	}
	logext.Infof(ctx, "[%s] ok,tenant_id:%s,job_id:%s,filename:%s", where, ctx.Auth.TenantID, req.GetJobId(), bundle.Filename)
	ctx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", bundle.Filename))
	ctx.SetHeader("Cache-Control", "private, no-store")
	ctx.SetHeader("Pragma", "no-cache")
	ctx.SetHeader("X-Content-Type-Options", "nosniff")
	return dispatcher.OK(ptrext.Of(httpbody.HttpBody{
		ContentType: "application/zip",
		Data:        bundle.Data,
	}))
}

func buildFilter(req *attunev1.CreateAuditEvidenceExportRequest) (auditevidencesvc.ExportFilter, error) {
	var f auditevidencesvc.ExportFilter
	f.Actions = req.GetActions()
	f.ActorType = req.GetActorType()
	f.ActorID = req.GetActorId()
	f.TargetType = req.GetTargetType()
	f.TargetID = req.GetTargetId()
	if req.GetFrom() != "" {
		from, err := time.Parse(time.RFC3339, req.GetFrom())
		if err != nil {
			return f, fmt.Errorf("from must be RFC3339")
		}
		f.From = ptrext.Of(from)
	}
	if req.GetTo() != "" {
		to, err := time.Parse(time.RFC3339, req.GetTo())
		if err != nil {
			return f, fmt.Errorf("to must be RFC3339")
		}
		f.To = ptrext.Of(to)
	}
	return f, nil
}

func jobToProto(job *aerepo.ExportJob) *attunev1.GetAuditEvidenceExportResponse {
	resp := ptrext.Of(attunev1.GetAuditEvidenceExportResponse{
		JobId:             job.ID,
		Status:            string(job.Status),
		TotalEvents:       int32(job.TotalEvents),
		CreatedAt:         job.CreatedAt.UTC().Format(time.RFC3339),
		RetryAfterSeconds: 2,
	})
	if job.StartedAt != nil {
		resp.StartedAt = ptrext.Of(job.StartedAt.UTC().Format(time.RFC3339))
	}
	if job.CompletedAt != nil {
		resp.CompletedAt = ptrext.Of(job.CompletedAt.UTC().Format(time.RFC3339))
	}
	if job.ExpiresAt != nil {
		resp.ExpiresAt = ptrext.Of(job.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if job.DownloadedAt != nil {
		resp.DownloadedAt = ptrext.Of(job.DownloadedAt.UTC().Format(time.RFC3339))
	}
	if job.ArchiveFilename != "" {
		resp.ArchiveFilename = ptrext.Of(job.ArchiveFilename)
	}
	if job.Error != "" {
		resp.Error = ptrext.Of("export failed — check server logs for details")
	}
	if job.Status == aerepo.JobCompleted && job.ExpiresAt != nil && job.ExpiresAt.After(time.Now().UTC()) {
		resp.DownloadPath = ptrext.Of("/fb/v1/console/audit-log/evidence/" + job.ID + "/download")
	}
	return resp
}

func mapError(err error) error {
	switch {
	case errors.Is(err, auditevidencesvc.ErrJobNotFound):
		return dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "audit evidence export not found")
	case errors.Is(err, auditevidencesvc.ErrJobNotDownloadable):
		return dispatcher.NewError(http.StatusConflict, attunev1.ErrorCode_CONFLICT, "audit evidence export is not ready for download")
	default:
		return err
	}
}
