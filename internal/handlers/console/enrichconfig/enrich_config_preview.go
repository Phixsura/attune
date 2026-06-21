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

// Preview handles POST /fb/v1/console/enrich-config/preview.
func (h *Handler) Preview(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.PreviewEnrichPromptRequest) (dispatcher.Result[*attunev1.PreviewEnrichPromptResponse], error) {
	const where = "console.EnrichConfigHandler.Preview"
	auth := ctx.Auth
	sample := strings.TrimSpace(req.GetSampleContent())
	if sample == "" {
		return dispatcher.Fail[*attunev1.PreviewEnrichPromptResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_SAMPLE, "sample_content must not be empty")
	}
	dims, err := dimsFromProto(req.GetDimensions())
	if err != nil {
		return dispatcher.Fail[*attunev1.PreviewEnrichPromptResponse](http.StatusBadRequest, enrich.ErrToCode(err), enrich.ErrToMessage(err))
	}
	if len(dims) > 0 {
		if err := dims.Validate(); err != nil {
			return dispatcher.Fail[*attunev1.PreviewEnrichPromptResponse](http.StatusBadRequest, enrich.ErrToCode(err), enrich.ErrToMessage(err))
		}
	}
	in := enrich.PreviewInput{SampleContent: sample}
	if req.GetUseDraftConfig() {
		in.HasDraftConfig = true
		in.Dimensions = dims
		in.PolicyConfig = policyConfigFromProto(req.GetPolicyConfig())
		if req.PromptTemplate != nil {
			t := strings.TrimSpace(ptrext.Indirect(req.PromptTemplate))
			if t != "" {
				in.PromptTemplate = ptrext.Of(t)
			}
		}
	}
	preview, err := h.svc.Preview(ctx, auth.TenantID, in)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			return dispatcher.Fail[*attunev1.PreviewEnrichPromptResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "tenant not found")
		}
		logext.Errorf(ctx, "[%s] preview failed,err:%+v,tenant_id:%s", where, err, auth.TenantID)
		return dispatcher.Fail[*attunev1.PreviewEnrichPromptResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "preview failed")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,sample_len:%d,rendered_len:%d",
		where, auth.TenantID, len(sample), len(preview.RenderedPrompt))
	return dispatcher.OK(ptrext.Of(attunev1.PreviewEnrichPromptResponse{
		RenderedPrompt: preview.RenderedPrompt,
		PromptPolicy:   promptPolicyToProto(preview.PromptPolicy),
	}))
}
