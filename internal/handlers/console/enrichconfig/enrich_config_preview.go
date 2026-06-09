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
)

// Preview handles POST /fb/v1/console/enrich-config/preview.
func (h *Handler) Preview(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.PreviewEnrichPromptRequest) (dispatcher.Result[*attunev1.PreviewEnrichPromptResponse], error) {
	const where = "console.EnrichConfigHandler.Preview"
	auth := ctx.Auth
	sample := strings.TrimSpace(req.GetSampleContent())
	if sample == "" {
		return dispatcher.Fail[*attunev1.PreviewEnrichPromptResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_SAMPLE, "sample_content must not be empty")
	}
	rendered, err := h.svc.Preview(ctx, auth.TenantID, sample)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			return dispatcher.Fail[*attunev1.PreviewEnrichPromptResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "tenant not found")
		}
		logext.Errorf(ctx, "[%s] preview failed,err:%+v,tenant_id:%s", where, err, auth.TenantID)
		return dispatcher.Fail[*attunev1.PreviewEnrichPromptResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "preview failed")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,sample_len:%d,rendered_len:%d",
		where, auth.TenantID, len(sample), len(rendered))
	return dispatcher.OK(ptrext.Of(attunev1.PreviewEnrichPromptResponse{RenderedPrompt: rendered}))
}
