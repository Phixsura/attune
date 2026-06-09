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

// Update handles PUT /fb/v1/console/enrich-config.
func (h *Handler) Update(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateEnrichConfigRequest) (dispatcher.Result[*attunev1.UpdateEnrichConfigResponse], error) {
	const where = "console.EnrichConfigHandler.Update"
	auth := ctx.Auth
	dims, err := dimsFromProto(req.GetDimensions())
	if err != nil {
		return dispatcher.Fail[*attunev1.UpdateEnrichConfigResponse](http.StatusBadRequest, enrich.ErrToCode(err), enrich.ErrToMessage(err))
	}
	in := enrich.View{Dimensions: dims}
	if req.PromptTemplate != nil {
		t := strings.TrimSpace(ptrext.Indirect(req.PromptTemplate))
		if t == "" {
			in.PromptTemplate = nil
		} else {
			in.PromptTemplate = ptrext.Of(t)
		}
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,has_template:%t,dims_n:%d",
		where, auth.TenantID, in.PromptTemplate != nil, len(in.Dimensions))
	if err := h.svc.Update(ctx, auth.TenantID, in); err != nil {
		if code := enrich.ErrToCode(err); code != attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED {
			msg := enrich.ErrToMessage(err)
			if msg == "" {
				msg = err.Error()
			}
			status := http.StatusBadRequest
			if errors.Is(err, tenant.ErrTenantNotFound) {
				status = http.StatusNotFound
			}
			return dispatcher.Fail[*attunev1.UpdateEnrichConfigResponse](status, code, msg)
		}
		logext.Errorf(ctx, "[%s] update failed,err:%+v,tenant_id:%s", where, err, auth.TenantID)
		return dispatcher.Fail[*attunev1.UpdateEnrichConfigResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to save enrich config")
	}
	v, err := h.svc.Get(ctx, auth.TenantID)
	if err != nil {
		return dispatcher.Fail[*attunev1.UpdateEnrichConfigResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read enrich config")
	}
	return dispatcher.OK(ptrext.Of(attunev1.UpdateEnrichConfigResponse{Config: toProtoConfig(v)}))
}
