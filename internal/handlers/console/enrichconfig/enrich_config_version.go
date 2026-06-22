package enrichconfig

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// ActivatePromptVersion handles POST /fb/v1/console/enrich-config/versions/{id}:activate.
func (h *Handler) ActivatePromptVersion(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ActivateEnrichPromptVersionRequest) (dispatcher.Result[*attunev1.ActivateEnrichPromptVersionResponse], error) {
	const where = "console.EnrichConfigHandler.ActivatePromptVersion"
	auth := ctx.Auth
	versionID := strings.TrimSpace(req.GetVersionId())
	if versionID == "" {
		return dispatcher.Fail[*attunev1.ActivateEnrichPromptVersionResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "version_id must not be empty")
	}
	beforeView, beforeErr := h.svc.Get(ctx, auth.TenantID)
	var beforeSnapshot map[string]any
	if beforeErr == nil {
		beforeSnapshot = enrichConfigSnapshot(beforeView)
	} else if !errors.Is(beforeErr, tenant.ErrTenantNotFound) {
		logext.Warnf(ctx, "[%s] before snapshot load failed,tenant_id:%s,err:%+v", where, auth.TenantID, beforeErr.Error())
	}
	if err := h.svc.ActivatePromptVersion(ctx, auth.TenantID, versionID); err != nil {
		if code := enrich.ErrToCode(err); code != attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED {
			msg := enrich.ErrToMessage(err)
			if msg == "" {
				msg = err.Error()
			}
			status := http.StatusBadRequest
			if errors.Is(err, tenant.ErrTenantNotFound) {
				status = http.StatusNotFound
			}
			return dispatcher.Fail[*attunev1.ActivateEnrichPromptVersionResponse](status, code, msg)
		}
		logext.Errorf(ctx, "[%s] activate failed,tenant_id:%s,version_id:%s,err:%+v",
			where, auth.TenantID, versionID, err.Error())
		return dispatcher.Fail[*attunev1.ActivateEnrichPromptVersionResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to activate prompt version")
	}
	v, err := h.svc.Get(ctx, auth.TenantID)
	if err != nil {
		return dispatcher.Fail[*attunev1.ActivateEnrichPromptVersionResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read enrich config")
	}
	if err := h.recordAudit(
		ctx,
		"enrich_config.activate_version",
		"Activated enrich prompt version",
		beforeSnapshot,
		enrichConfigSnapshot(v),
	); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,version_id:%s,err:%+v",
			where, auth.TenantID, versionID, err.Error())
		return dispatcher.Fail[*attunev1.ActivateEnrichPromptVersionResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,version_id:%s", where, auth.TenantID, versionID)
	return dispatcher.OK(ptrext.Of(attunev1.ActivateEnrichPromptVersionResponse{Config: toProtoConfig(v)}))
}
