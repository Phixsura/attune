package enrichconfig

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Handler serves /fb/v1/console/enrich-config (#10 → E3
// metadata-driven Dimensions).
type Handler struct {
	svc *enrich.ConfigService
}

func NewHandler(svc *enrich.ConfigService) *Handler {
	return &Handler{svc: svc}
}

func toProtoConfig(v enrich.View) *attunev1.EnrichConfig {
	return &attunev1.EnrichConfig{
		PromptTemplate:        v.PromptTemplate,
		DefaultPromptTemplate: enrich.DefaultPromptTemplate(),
		Dimensions:            dimsToProto(v.Dimensions),
	}
}

func dimsToProto(dims domain.DimensionSet) []*attunev1.Dimension {
	if len(dims) == 0 {
		return nil
	}
	out := make([]*attunev1.Dimension, 0, len(dims))
	for _, d := range dims {
		out = append(out, &attunev1.Dimension{
			Name:        d.Name,
			DisplayName: i18nToProto(d.DisplayName),
			Kind:        string(d.Kind),
			Taxonomy:    taxonomyToProto(d.Taxonomy),
			UrgentSet:   d.UrgentSet,
			Required:    d.Required,
		})
	}
	return out
}

func taxonomyToProto(tax []domain.Taxonomy) []*attunev1.Taxonomy {
	if len(tax) == 0 {
		return nil
	}
	out := make([]*attunev1.Taxonomy, 0, len(tax))
	for _, t := range tax {
		out = append(out, &attunev1.Taxonomy{
			Value:       t.Value,
			DisplayName: i18nToProto(t.DisplayName),
		})
	}
	return out
}

func i18nToProto(s domain.I18nString) *attunev1.I18NString {
	if len(s) == 0 {
		return &attunev1.I18NString{Entries: map[string]string{}}
	}
	entries := make(map[string]string, len(s))
	for k, v := range s {
		entries[k] = v
	}
	return &attunev1.I18NString{Entries: entries}
}

func dimsFromProto(in []*attunev1.Dimension) domain.DimensionSet {
	if len(in) == 0 {
		return nil
	}
	out := make(domain.DimensionSet, 0, len(in))
	for _, d := range in {
		out = append(out, domain.Dimension{
			Name:        d.GetName(),
			DisplayName: i18nFromProto(d.GetDisplayName()),
			Kind:        domain.DimensionKind(d.GetKind()),
			Taxonomy:    taxonomyFromProto(d.GetTaxonomy()),
			UrgentSet:   d.GetUrgentSet(),
			Required:    d.GetRequired(),
		})
	}
	return out
}

func taxonomyFromProto(in []*attunev1.Taxonomy) []domain.Taxonomy {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.Taxonomy, 0, len(in))
	for _, t := range in {
		out = append(out, domain.Taxonomy{
			Value:       t.GetValue(),
			DisplayName: i18nFromProto(t.GetDisplayName()),
		})
	}
	return out
}

func i18nFromProto(in *attunev1.I18NString) domain.I18nString {
	if in == nil || len(in.GetEntries()) == 0 {
		return nil
	}
	out := make(domain.I18nString, len(in.GetEntries()))
	for k, v := range in.GetEntries() {
		out[k] = v
	}
	return out
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
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "tenant not found")
			return
		}
		logext.Errorf(ctx, "[%s] get failed,err:%+v,tenant_id:%s", where, err, auth.TenantID)
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to read enrich config")
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
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "request body is not valid JSON")
		return
	}
	in := enrich.View{Dimensions: dimsFromProto(req.GetDimensions())}
	if req.PromptTemplate != nil {
		t := strings.TrimSpace(*req.PromptTemplate)
		if t == "" {
			in.PromptTemplate = nil
		} else {
			in.PromptTemplate = &t
		}
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,has_template:%t,dims_n:%d",
		where, auth.TenantID, in.PromptTemplate != nil, len(in.Dimensions))
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
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to save enrich config")
		return
	}
	v, err := h.svc.Get(ctx, auth.TenantID)
	if err != nil {
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to read enrich config")
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
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "request body is not valid JSON")
		return
	}
	sample := strings.TrimSpace(req.GetSampleContent())
	if sample == "" {
		respond.Error(ctx, w, http.StatusBadRequest, "missing_sample", "sample_content must not be empty")
		return
	}
	rendered, err := h.svc.Preview(ctx, auth.TenantID, sample)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "tenant not found")
			return
		}
		logext.Errorf(ctx, "[%s] preview failed,err:%+v,tenant_id:%s", where, err, auth.TenantID)
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "preview failed")
		return
	}
	respond.Proto(w, http.StatusOK, &attunev1.PreviewEnrichPromptResponse{RenderedPrompt: rendered})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,sample_len:%d,rendered_len:%d",
		where, auth.TenantID, len(sample), len(rendered))
}
