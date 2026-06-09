package enrichconfig

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// Get handles GET /fb/v1/console/enrich-config.
func (h *Handler) Get(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.GetEnrichConfigRequest) (dispatcher.Result[*attunev1.GetEnrichConfigResponse], error) {
	const where = "console.EnrichConfigHandler.Get"
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	v, err := h.svc.Get(ctx, auth.TenantID)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			return dispatcher.Fail[*attunev1.GetEnrichConfigResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "tenant not found")
		}
		logext.Errorf(ctx, "[%s] get failed,err:%+v,tenant_id:%s", where, err, auth.TenantID)
		return dispatcher.Fail[*attunev1.GetEnrichConfigResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read enrich config")
	}
	return dispatcher.OK(ptrext.Of(attunev1.GetEnrichConfigResponse{Config: toProtoConfig(v)}))
}
