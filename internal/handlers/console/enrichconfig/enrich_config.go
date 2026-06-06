package enrichconfig

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Handler serves /fb/v1/console/enrich-config (#10).
type Handler struct {
	svc *enrich.ConfigService
}

func NewHandler(svc *enrich.ConfigService) *Handler {
	return &Handler{svc: svc}
}

func toProtoConfig(v enrich.View) *attunev1.EnrichConfig {
	mode := attunev1.ModuleMode_MODULE_MODE_UNSPECIFIED
	switch v.ModuleMode {
	case "constrained":
		mode = attunev1.ModuleMode_MODULE_MODE_CONSTRAINED
	case "freeform":
		mode = attunev1.ModuleMode_MODULE_MODE_FREEFORM
	}
	return &attunev1.EnrichConfig{
		PromptTemplate:        v.PromptTemplate,
		DefaultPromptTemplate: enrich.DefaultPromptTemplate(),
		Modules:               v.Modules,
		ModuleMode:            mode,
	}
}

// Get handles GET /fb/v1/console/enrich-config.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	const where = "console.EnrichConfigHandler.Get"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	v, err := h.svc.Get(ctx, auth.TenantID)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "租户不存在")
			return
		}
		logext.Errorf(ctx, "[%s] get failed,err:%+v,tenant_id:%s", where, err, auth.TenantID)
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "读取 enrich 配置失败")
		return
	}
	respond.Proto(w, http.StatusOK, &attunev1.GetEnrichConfigResponse{Config: toProtoConfig(v)})
}

// Update handles PUT /fb/v1/console/enrich-config.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	const where = "console.EnrichConfigHandler.Update"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	var req attunev1.UpdateEnrichConfigRequest
	if err := respond.Decode(r.Body, &req); err != nil {
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	in := enrich.View{Modules: req.GetModules()}
	if req.PromptTemplate != nil {
		t := strings.TrimSpace(*req.PromptTemplate)
		if t == "" {
			in.PromptTemplate = nil
		} else {
			in.PromptTemplate = &t
		}
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,has_template:%t,modules_n:%d",
		where, auth.TenantID, in.PromptTemplate != nil, len(in.Modules))
	if err := h.svc.Update(ctx, auth.TenantID, in); err != nil {
		if code := enrich.ErrToCode(err); code != "" {
			msg := enrich.ErrToMessage(err)
			if msg == "" {
				msg = err.Error()
			}
			status := http.StatusBadRequest
			if errors.Is(err, tenant.ErrTenantNotFound) {
				status = http.StatusNotFound
			}
			respond.Error(ctx, w, status, code, msg)
			return
		}
		logext.Errorf(ctx, "[%s] update failed,err:%+v,tenant_id:%s", where, err, auth.TenantID)
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "保存 enrich 配置失败")
		return
	}
	v, err := h.svc.Get(ctx, auth.TenantID)
	if err != nil {
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "读取 enrich 配置失败")
		return
	}
	respond.Proto(w, http.StatusOK, &attunev1.UpdateEnrichConfigResponse{Config: toProtoConfig(v)})
}

// Preview handles POST /fb/v1/console/enrich-config/preview.
func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	const where = "console.EnrichConfigHandler.Preview"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	var req attunev1.PreviewEnrichPromptRequest
	if err := respond.Decode(r.Body, &req); err != nil {
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	sample := strings.TrimSpace(req.GetSampleContent())
	if sample == "" {
		respond.Error(ctx, w, http.StatusBadRequest, "missing_sample", "sample_content 不能为空")
		return
	}
	rendered, err := h.svc.Preview(ctx, auth.TenantID, sample)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "租户不存在")
			return
		}
		logext.Errorf(ctx, "[%s] preview failed,err:%+v,tenant_id:%s", where, err, auth.TenantID)
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "预览失败")
		return
	}
	respond.Proto(w, http.StatusOK, &attunev1.PreviewEnrichPromptResponse{RenderedPrompt: rendered})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,sample_len:%d,rendered_len:%d",
		where, auth.TenantID, len(sample), len(rendered))
}
