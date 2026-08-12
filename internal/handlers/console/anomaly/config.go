// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// auditRecorder is the enrichconfig-style audit hook.
type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

// SetAuditLogger attaches the audit recorder (optional; nil = no audit).
func (h *Handler) SetAuditLogger(audit auditRecorder) { h.audit = audit }

// digestSubscriptionChecker reports whether a tenant has a digest
// subscription; wired from cmd (optional — nil skips the advisory).
type digestSubscriptionChecker interface {
	GetByTenant(ctx context.Context, tenantID string) (bool, error)
}

// SetDigestChecker attaches the digest-subscription advisory source.
func (h *Handler) SetDigestChecker(c digestSubscriptionChecker) { h.digest = c }

// maxConfiguredSeries caps how many distinct slice keys a tenant's enabled
// configuration may produce (spec §9): beyond it detection quality and
// notification noise degrade, so the update is rejected outright.
const maxConfiguredSeries = 500

// GetAnomalyConfig returns the tenant config with custom slices.
func (h *Handler) GetAnomalyConfig(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetAnomalyConfigRequest,
) (dispatcher.Result[*attunev1.GetAnomalyConfigResponse], error) {
	const where = "console.AnomalyHandler.GetAnomalyConfig"
	cfg, slices, err := h.loadConfig(ctx, ctx.Auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] load failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetAnomalyConfigResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read anomaly config")
	}
	return dispatcher.OK(ptrext.Of(attunev1.GetAnomalyConfigResponse{
		Config: configToProto(cfg, slices),
	}))
}

// UpdateAnomalyConfig validates and persists the tenant config plus the
// full custom slice set, recording an audit event.
func (h *Handler) UpdateAnomalyConfig(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateAnomalyConfigRequest,
) (dispatcher.Result[*attunev1.UpdateAnomalyConfigResponse], error) {
	const where = "console.AnomalyHandler.UpdateAnomalyConfig"
	in := req.GetConfig()
	if in == nil {
		return dispatcher.Fail[*attunev1.UpdateAnomalyConfigResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "config is required")
	}
	cfg, slices, err := configFromProto(ctx.Auth.TenantID, in)
	if err != nil {
		return dispatcher.Fail[*attunev1.UpdateAnomalyConfigResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	// Series-cap guard. Deliberately fail-open on a read error: the cap is
	// a soft quality guard (the worker's per-tick slice cap is the hard
	// backstop), and a transient DB error must not brick the config page.
	// The count measures ALREADY-MATERIALIZED series (the outgoing
	// config's footprint), so a shrinking change — fewer slice types or
	// custom slices than currently stored — must pass: it is the only way
	// an over-cap tenant can dig itself back out.
	if count, err := h.store.CountRecentSliceKeys(ctx, ctx.Auth.TenantID, 30); err == nil && count > maxConfiguredSeries {
		if !isShrinkingConfig(ctx, h, cfg, slices) {
			return dispatcher.Fail[*attunev1.UpdateAnomalyConfigResponse](
				http.StatusBadRequest, attunev1.ErrorCode_VALIDATION,
				fmt.Sprintf("configuration produces %d monitored series over the last 30 days (max %d) — disable slice types or remove custom slices", count, maxConfiguredSeries))
		}
	}
	if err := h.store.UpsertConfig(ctx, cfg, ctx.Auth.UserID); err != nil {
		logext.Errorf(ctx, "[%s] upsert failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.UpdateAnomalyConfigResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to save anomaly config")
	}
	if err := h.store.ReplaceCustomSlices(ctx, ctx.Auth.TenantID, slices); err != nil {
		logext.Errorf(ctx, "[%s] slices failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.UpdateAnomalyConfigResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to save custom slices")
	}
	h.recordAudit(ctx, in)

	saved, savedSlices, err := h.loadConfig(ctx, ctx.Auth.TenantID)
	if err != nil {
		return dispatcher.Fail[*attunev1.UpdateAnomalyConfigResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to reload anomaly config")
	}
	return dispatcher.OK(ptrext.Of(attunev1.UpdateAnomalyConfigResponse{
		Config: configToProto(saved, savedSlices),
		Warning: joinWarnings(
			h.digestModeWarning(ctx, ctx.Auth.TenantID, cfg.NotifyMode),
			backfillWarning(saved),
		),
	}))
}

func (h *Handler) loadConfig(
	ctx context.Context, tenantID string,
) (anomalyrepo.Config, []anomalyrepo.StoredCustomSlice, error) {
	cfg, err := h.store.GetConfig(ctx, tenantID)
	if err != nil {
		return anomalyrepo.Config{}, nil, err
	}
	slices, err := h.store.ListCustomSlices(ctx, tenantID)
	if err != nil {
		return anomalyrepo.Config{}, nil, err
	}
	return cfg, slices, nil
}

func (h *Handler) recordAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx], in *attunev1.AnomalyConfig,
) {
	if h.audit == nil {
		return
	}
	actorType := ctx.Auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	after := map[string]any{
		"sensitivity": in.GetSensitivity(),
		"min_count":   in.GetMinCount(),
		"notify_mode": in.GetNotifyMode(),
		"slice_count": len(in.GetCustomSlices()),
		"enabled":     in.GetDetectionEnabled(),
	}
	_ = h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   ctx.Auth.TenantID,
		Actor:      auditlogsvc.ActorFromRequest(actorType, ctx.Auth.UserID, ctx.Request()),
		Action:     "anomaly_config.update",
		TargetType: "anomaly_config",
		TargetID:   ctx.Auth.TenantID,
		Summary:    "anomaly detection configuration updated",
		After:      after,
	})
}

// isShrinkingConfig reports whether the submitted config STRICTLY reduces
// what the stored config monitors: no new slice types, no more enabled
// custom slices, and at least one of the two actually smaller. Strictly
// shrinking changes bypass the series cap so an over-cap tenant can dig
// itself back out through the Console; re-submitting the same over-cap
// config is still rejected.
func isShrinkingConfig(
	ctx context.Context, h *Handler,
	cfg anomalyrepo.Config, slices []anomalyrepo.StoredCustomSlice,
) bool {
	stored, err := h.store.GetConfig(ctx, cfg.TenantID)
	if err != nil {
		return false
	}
	storedEnabled := map[string]bool{}
	for _, t := range stored.EnabledSliceTypes {
		storedEnabled[t] = true
	}
	for _, t := range cfg.EnabledSliceTypes {
		if !storedEnabled[t] {
			return false // enables a new slice type: growing
		}
	}
	storedSlices, err := h.store.ListCustomSlices(ctx, cfg.TenantID)
	if err != nil {
		return false
	}
	enabledCount := func(in []anomalyrepo.StoredCustomSlice) int {
		n := 0
		for _, s := range in {
			if s.Enabled {
				n++
			}
		}
		return n
	}
	fewerTypes := len(cfg.EnabledSliceTypes) < len(stored.EnabledSliceTypes)
	fewerCustom := enabledCount(slices) < enabledCount(storedSlices)
	noMoreCustom := enabledCount(slices) <= enabledCount(storedSlices)
	return noMoreCustom && (fewerTypes || fewerCustom)
}

// backfillWarning tells the operator detection pauses until the worker
// re-backfills under the new settings (§7: BackfillVersion must catch up
// to ConfigVersion before any date is judged).
func backfillWarning(cfg anomalyrepo.Config) string {
	if cfg.BackfillVersion == cfg.ConfigVersion {
		return ""
	}
	return "detection is paused while historical volume is re-computed under the new settings — it resumes automatically within the next worker cycles"
}

// joinWarnings concatenates non-empty warnings with a separator.
func joinWarnings(ws ...string) string {
	var out []string
	for _, w := range ws {
		if w != "" {
			out = append(out, w)
		}
	}
	return strings.Join(out, " · ")
}

// digestModeWarning returns the spec §9 advisory: digest notify mode with
// no digest subscription silently delivers nothing, so warn (never fail).
func (h *Handler) digestModeWarning(ctx context.Context, tenantID, notifyMode string) string {
	if notifyMode != anomalyrepo.NotifyDigest || h.digest == nil {
		return ""
	}
	has, err := h.digest.GetByTenant(ctx, tenantID)
	if err != nil || has {
		return ""
	}
	return "notify_mode is digest but no digest subscription exists — anomaly notifications will not be delivered until one is configured"
}

// ── proto ⇄ repo mapping + validation ────────────────────────────────────

func configToProto(cfg anomalyrepo.Config, slices []anomalyrepo.StoredCustomSlice) *attunev1.AnomalyConfig {
	out := ptrext.Of(attunev1.AnomalyConfig{
		Sensitivity:           cfg.Sensitivity,
		MinCount:              int32(cfg.MinCount),
		SettleDelayHours:      int32(cfg.SettleDelayHours),
		EnabledSliceTypes:     cfg.EnabledSliceTypes,
		DropEnabledSliceTypes: cfg.DropEnabledSliceTypes,
		NotifyMode:            cfg.NotifyMode,
		DetectionEnabled:      cfg.DetectionEnabled,
	})
	for _, s := range slices {
		out.CustomSlices = append(out.CustomSlices, ptrext.Of(attunev1.AnomalyCustomSlice{
			Id:             s.ID.String(),
			Name:           s.Name,
			DefinitionJson: s.DefinitionJSON,
			Enabled:        s.Enabled,
			LastError:      s.LastError,
		}))
	}
	return out
}

func configFromProto(tenantID string, in *attunev1.AnomalyConfig) (anomalyrepo.Config, []anomalyrepo.StoredCustomSlice, error) {
	cfg := anomalyrepo.Config{
		TenantID:              tenantID,
		Sensitivity:           in.GetSensitivity(),
		MinCount:              int(in.GetMinCount()),
		SettleDelayHours:      int(in.GetSettleDelayHours()),
		EnabledSliceTypes:     in.GetEnabledSliceTypes(),
		DropEnabledSliceTypes: in.GetDropEnabledSliceTypes(),
		NotifyMode:            in.GetNotifyMode(),
		DetectionEnabled:      in.GetDetectionEnabled(),
	}
	if err := validateConfig(cfg); err != nil {
		return anomalyrepo.Config{}, nil, err
	}
	slices, err := customSlicesFromProto(in.GetCustomSlices())
	if err != nil {
		return anomalyrepo.Config{}, nil, err
	}
	return cfg, slices, nil
}

func validateConfig(cfg anomalyrepo.Config) error {
	switch cfg.Sensitivity {
	case "high", "medium", "low":
	default:
		return fmt.Errorf("sensitivity must be high, medium, or low")
	}
	if cfg.MinCount < 0 || cfg.MinCount > 10000 {
		return fmt.Errorf("min_count must be between 0 and 10000")
	}
	if cfg.SettleDelayHours < 0 || cfg.SettleDelayHours > 48 {
		return fmt.Errorf("settle_delay_hours must be between 0 and 48")
	}
	switch cfg.NotifyMode {
	case anomalyrepo.NotifyImmediate, anomalyrepo.NotifyDigest, anomalyrepo.NotifyOff:
	default:
		return fmt.Errorf("notify_mode must be immediate, digest, or off")
	}
	valid := map[string]bool{}
	for _, t := range anomalyrepo.AllSliceTypes() {
		valid[t] = true
	}
	enabled := map[string]bool{}
	for _, t := range cfg.EnabledSliceTypes {
		if !valid[t] {
			return fmt.Errorf("unknown slice type %q", t)
		}
		enabled[t] = true
	}
	for _, t := range cfg.DropEnabledSliceTypes {
		if !valid[t] {
			return fmt.Errorf("unknown drop slice type %q", t)
		}
		if !enabled[t] {
			return fmt.Errorf("drop slice type %q must also be enabled", t)
		}
	}
	return nil
}

func customSlicesFromProto(in []*attunev1.AnomalyCustomSlice) ([]anomalyrepo.StoredCustomSlice, error) {
	if len(in) > maxCustomSlices {
		return nil, fmt.Errorf("at most %d custom slices allowed", maxCustomSlices)
	}
	names := map[string]bool{}
	out := make([]anomalyrepo.StoredCustomSlice, 0, len(in))
	for _, s := range in {
		name := s.GetName()
		if name == "" || len(name) > 80 {
			return nil, fmt.Errorf("custom slice name must be 1-80 characters")
		}
		if names[name] {
			return nil, fmt.Errorf("duplicate custom slice name %q", name)
		}
		names[name] = true
		if err := validateDefinitionJSON(s.GetDefinitionJson()); err != nil {
			return nil, fmt.Errorf("custom slice %q: %w", name, err)
		}
		id := uuid.New()
		if raw := s.GetId(); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("custom slice %q: invalid id", name)
			}
			id = parsed
		}
		out = append(out, anomalyrepo.StoredCustomSlice{
			ID:             id,
			Name:           name,
			DefinitionJSON: s.GetDefinitionJson(),
			Enabled:        s.GetEnabled(),
			LastError:      "",
		})
	}
	return out, nil
}

// validateDefinitionJSON enforces the whitelisted conjunction shape.
func validateDefinitionJSON(raw string) error {
	var def struct {
		Conditions []anomalyrepo.CustomCondition `json:"conditions"`
	}
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		return fmt.Errorf("definition must be valid JSON")
	}
	if len(def.Conditions) < 1 || len(def.Conditions) > maxConditions {
		return fmt.Errorf("definition must have 1-%d conditions", maxConditions)
	}
	for _, c := range def.Conditions {
		switch c.Field {
		case "source", "dimension", "cohort":
		default:
			return fmt.Errorf("condition field must be source, dimension, or cohort")
		}
		if c.Field == "dimension" && c.Name == "" {
			return fmt.Errorf("dimension condition requires a name")
		}
		if len(c.Values) < 1 || len(c.Values) > maxConditionVals {
			return fmt.Errorf("condition must have 1-%d values", maxConditionVals)
		}
		if c.Field == "cohort" {
			for _, v := range c.Values {
				if _, err := uuid.Parse(v); err != nil {
					return fmt.Errorf("cohort values must be UUIDs")
				}
			}
		}
	}
	return nil
}
