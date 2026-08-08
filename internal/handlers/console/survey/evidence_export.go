// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/survey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/survey"
)

// ExportNPSCampaignRunEvidence creates and returns one persisted, aggregate-
// only artifact. Repeated downloads use DownloadNPSCampaignRunEvidence.
func (h *Handler) ExportNPSCampaignRunEvidence(w http.ResponseWriter, r *http.Request) {
	const where = "console.survey.ExportNPSCampaignRunEvidence"
	auth := session.FromContext(r.Context())
	if auth == nil {
		dispatcher.Reject(r.Context(), w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "authentication required")
		return
	}
	campaignID, runID, ok := parseNPSRunPath(r, w)
	if !ok {
		return
	}
	item, err := h.service.CreateNPSCampaignRunEvidenceExport(
		r.Context(), auth.TenantID, campaignID, runID, auth.UserType, auth.UserID)
	if err != nil {
		rejectNPSEvidenceError(r, w, err)
		return
	}
	if h.audit != nil {
		if err := h.audit.Record(r.Context(), auditlogsvc.Event{
			TenantID:   auth.TenantID,
			Actor:      auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, r),
			Action:     "survey.nps_run_evidence_export",
			TargetType: "survey_nps_campaign_run",
			TargetID:   runID.String(),
			Summary:    "Created aggregate NPS campaign run evidence artifact",
			After: map[string]any{
				"campaign_id":     campaignID.String(),
				"export_id":       item.ID.String(),
				"report_version":  item.ReportVersion,
				"generated_at":    item.GeneratedAt.UTC().Format(time.RFC3339),
				"artifact_sha256": item.ArtifactSHA256,
			},
		}); err != nil {
			logext.Errorf(r.Context(), "[%s] audit failed,tenant_id:%s,run_id:%s,export_id:%s,err:%+v", where, auth.TenantID, runID, item.ID, err)
			dispatcher.Reject(r.Context(), w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record NPS evidence export")
			return
		}
	}
	writeNPSCampaignRunEvidence(w, item, runID, uuid.Nil)
}

func (h *Handler) CreateNPSCampaignRunEvidenceExport(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateNpsCampaignRunEvidenceExportRequest,
) (dispatcher.Result[*attunev1.NpsCampaignRunEvidenceExport], error) {
	campaignID, err := parseUUID(req.GetCampaignId())
	if err != nil {
		return dispatcher.Fail[*attunev1.NpsCampaignRunEvidenceExport](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
	}
	runID, err := parseUUID(req.GetRunId())
	if err != nil {
		return dispatcher.Fail[*attunev1.NpsCampaignRunEvidenceExport](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid NPS campaign run id")
	}
	requestKey, err := parseUUID(req.GetClientRequestKey())
	if err != nil {
		return dispatcher.Fail[*attunev1.NpsCampaignRunEvidenceExport](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid client request key")
	}
	item, replayed, err := h.service.CreateNPSCampaignRunEvidenceExportWithRequestKey(
		ctx, ctx.Auth.TenantID, campaignID, runID, requestKey, ctx.Auth.UserType, ctx.Auth.UserID,
	)
	if err != nil {
		return consoleError[*attunev1.NpsCampaignRunEvidenceExport](err, "NPS evidence export creation failed")
	}
	if replayed {
		return dispatcher.OK(npsEvidenceExportToProto(item))
	}
	if err := h.recordAfter(ctx, "survey.nps_run_evidence_export", "survey_nps_campaign_run", runID.String(), "Created aggregate NPS campaign run evidence artifact", map[string]any{
		"campaign_id":     campaignID.String(),
		"export_id":       item.ID.String(),
		"report_version":  item.ReportVersion,
		"generated_at":    item.GeneratedAt.UTC().Format(time.RFC3339),
		"artifact_sha256": item.ArtifactSHA256,
	}); err != nil {
		return dispatcher.Fail[*attunev1.NpsCampaignRunEvidenceExport](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record NPS evidence export")
	}
	return dispatcher.Created(npsEvidenceExportToProto(item))
}

func (h *Handler) DownloadNPSCampaignRunEvidence(w http.ResponseWriter, r *http.Request) {
	const where = "console.survey.DownloadNPSCampaignRunEvidence"
	auth := session.FromContext(r.Context())
	if auth == nil {
		dispatcher.Reject(r.Context(), w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "authentication required")
		return
	}
	campaignID, runID, ok := parseNPSRunPath(r, w)
	if !ok {
		return
	}
	exportID, err := uuid.Parse(chi.URLParam(r, "export_id"))
	if err != nil {
		dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid NPS evidence export id")
		return
	}
	item, err := h.service.DownloadNPSCampaignRunEvidenceExport(r.Context(), auth.TenantID, campaignID, runID, exportID)
	if err != nil {
		rejectNPSEvidenceError(r, w, err)
		return
	}
	if h.audit != nil {
		if err := h.audit.Record(r.Context(), auditlogsvc.Event{
			TenantID:   auth.TenantID,
			Actor:      auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, r),
			Action:     "survey.nps_run_evidence_export",
			TargetType: "survey_nps_campaign_run_evidence_export",
			TargetID:   item.ID.String(),
			Summary:    "Downloaded persisted NPS campaign run evidence artifact",
			After: map[string]any{
				"campaign_id":     campaignID.String(),
				"run_id":          runID.String(),
				"operation":       "download",
				"report_version":  item.ReportVersion,
				"artifact_sha256": item.ArtifactSHA256,
			},
		}); err != nil {
			logext.Errorf(r.Context(), "[%s] audit failed,tenant_id:%s,run_id:%s,export_id:%s,err:%+v", where, auth.TenantID, runID, item.ID, err)
			dispatcher.Reject(r.Context(), w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record NPS evidence download")
			return
		}
	}
	writeNPSCampaignRunEvidence(w, item, runID, exportID)
}

func (h *Handler) ListNPSCampaignRunEvidenceExports(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListNpsCampaignRunEvidenceExportsRequest,
) (dispatcher.Result[*attunev1.ListNpsCampaignRunEvidenceExportsResponse], error) {
	campaignID, err := parseUUID(req.GetCampaignId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListNpsCampaignRunEvidenceExportsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
	}
	runID, err := parseUUID(req.GetRunId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListNpsCampaignRunEvidenceExportsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid NPS campaign run id")
	}
	items, err := h.service.ListNPSCampaignRunEvidenceExports(ctx, ctx.Auth.TenantID, campaignID, runID, int(req.GetLimit()))
	if err != nil {
		return consoleError[*attunev1.ListNpsCampaignRunEvidenceExportsResponse](err, "NPS evidence export history failed")
	}
	out := ptrext.Of(attunev1.ListNpsCampaignRunEvidenceExportsResponse{
		Exports: make([]*attunev1.NpsCampaignRunEvidenceExport, 0, len(items)),
	})
	for _, item := range items {
		out.Exports = append(out.Exports, npsEvidenceExportSummaryToProto(item))
	}
	return dispatcher.OK(out)
}

func parseNPSRunPath(r *http.Request, w http.ResponseWriter) (uuid.UUID, uuid.UUID, bool) {
	campaignID, err := uuid.Parse(chi.URLParam(r, "campaign_id"))
	if err != nil {
		dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
		return uuid.Nil, uuid.Nil, false
	}
	runID, err := uuid.Parse(chi.URLParam(r, "run_id"))
	if err != nil {
		dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid NPS campaign run id")
		return uuid.Nil, uuid.Nil, false
	}
	return campaignID, runID, true
}

func writeNPSCampaignRunEvidence(w http.ResponseWriter, item repo.NPSCampaignRunEvidenceExport, runID, exportID uuid.UUID) {
	digest := sha256.Sum256(item.Artifact)
	artifactHash := fmt.Sprintf("sha256:%x", digest)
	filename := "nps-run-evidence-" + runID.String() + ".csv"
	if exportID != uuid.Nil {
		filename = "nps-run-evidence-" + runID.String() + "-" + exportID.String() + ".csv"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(item.Artifact)))
	if !item.GeneratedAt.IsZero() {
		w.Header().Set("Last-Modified", item.GeneratedAt.UTC().Format(http.TimeFormat))
	}
	w.Header().Set("Digest", "sha-256="+base64.StdEncoding.EncodeToString(digest[:]))
	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", artifactHash))
	_, _ = w.Write(item.Artifact)
}

func npsEvidenceCSV(evidence svc.NPSCampaignRunEvidence, generatedAt time.Time) ([]byte, error) {
	return svc.BuildNPSCampaignRunEvidenceCSV(evidence, generatedAt)
}

func rejectNPSEvidenceError(r *http.Request, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, svc.ErrValidation):
		dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid NPS run evidence request")
	case errors.Is(err, svc.ErrNotFound):
		dispatcher.Reject(r.Context(), w, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "NPS campaign run or evidence export not found")
	case errors.Is(err, svc.ErrExpired):
		dispatcher.Reject(r.Context(), w, http.StatusGone, attunev1.ErrorCode_NOT_FOUND, "NPS evidence export expired")
	case errors.Is(err, svc.ErrDisabled):
		dispatcher.Reject(r.Context(), w, http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "NPS run evidence is unavailable")
	default:
		dispatcher.Reject(r.Context(), w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to export NPS run evidence")
	}
}
