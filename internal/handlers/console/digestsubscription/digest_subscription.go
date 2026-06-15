// SPDX-License-Identifier: Apache-2.0

// Package digestsubscription serves /fb/v1/console/digest-subscription — the
// per-tenant daily digest config (#27). v1 is a singleton resource (one
// subscription per tenant), so the handler is get / upsert / delete.
package digestsubscription

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
	"github.com/Phixsura/attune/internal/repo/tenant"
	digestsvc "github.com/Phixsura/attune/internal/service/digest"
)

const defaultLLMMinFeedback = 6

// subscriptionRepo is the slice of the digest_subscriptions repo the handler
// uses (consumer-side interface so tests can pass a fake).
type subscriptionRepo interface {
	GetByTenant(ctx context.Context, tenantID string) (*digestsubrepo.Subscription, error)
	Upsert(ctx context.Context, s digestsubrepo.Subscription) (*digestsubrepo.Subscription, error)
	DeleteByTenant(ctx context.Context, tenantID string) error
}

// tenantReader resolves a tenant's default timezone for scheduling.
type tenantReader interface {
	GetByID(ctx context.Context, id string) (*tenant.Tenant, error)
	GetClusteringEnabled(ctx context.Context, id string) (bool, error)
	SetClusteringEnabled(ctx context.Context, id string, enabled bool) error
}

// Handler serves the digest-subscription endpoints.
type Handler struct {
	repo    subscriptionRepo
	tenants tenantReader
}

// NewHandler wires the handler.
func NewHandler(repo subscriptionRepo, tenants tenantReader) *Handler {
	return ptrext.Of(Handler{repo: repo, tenants: tenants})
}

// Get handles GET /fb/v1/console/digest-subscription.
func (h *Handler) Get(
	ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.GetDigestSubscriptionRequest,
) (dispatcher.Result[*attunev1.DigestSubscription], error) {
	const where = "console.DigestSubscriptionHandler.Get"
	auth := ctx.Auth
	sub, err := h.repo.GetByTenant(ctx, auth.TenantID)
	if errors.Is(err, digestsubrepo.ErrNotFound) {
		return dispatcher.Fail[*attunev1.DigestSubscription](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "no digest subscription configured",
		)
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] get failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.DigestSubscription](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load digest subscription",
		)
	}
	clusteringEnabled, err := h.tenants.GetClusteringEnabled(ctx, auth.TenantID)
	if err != nil {
		logext.Warnf(ctx, "[%s] get clustering status failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		// Non-fatal: default to false
	}
	proto := toDigestProto(ptrext.Indirect(sub))
	proto.ClusteringEnabled = clusteringEnabled
	return dispatcher.OK(proto)
}

// Upsert handles PUT /fb/v1/console/digest-subscription (create or replace). It
// validates the schedule, resolves the timezone (override else tenant default),
// and materializes next_run_at so the worker fires at the right local time.
func (h *Handler) Upsert(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpsertDigestSubscriptionRequest,
) (dispatcher.Result[*attunev1.DigestSubscription], error) {
	const where = "console.DigestSubscriptionHandler.Upsert"
	auth := ctx.Auth
	sub, err := normalizeUpsert(req)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,err:%s", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.DigestSubscription](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error(),
		)
	}
	tz, err := h.resolveTimezone(ctx, auth.TenantID, sub.Timezone)
	if err != nil {
		logext.Errorf(ctx, "[%s] resolve tz failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.DigestSubscription](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to resolve timezone",
		)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad timezone,tenant_id:%s,tz:%s", where, auth.TenantID, tz)
		return dispatcher.Fail[*attunev1.DigestSubscription](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "timezone must be a valid IANA name",
		)
	}
	sub.TenantID = auth.TenantID
	sub.NextRunAt = ptrext.Of(digestsvc.NextRun(time.Now(), sub.Frequency, sub.SendHour, sub.Byweekday, loc))
	out, err := h.repo.Upsert(ctx, sub)
	if err != nil {
		logext.Errorf(ctx, "[%s] upsert failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.DigestSubscription](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to save digest subscription",
		)
	}
	if err := h.tenants.SetClusteringEnabled(ctx, auth.TenantID, req.ClusteringEnabled); err != nil {
		logext.Warnf(ctx, "[%s] set clustering failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		// Non-fatal: clustering is optional
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,frequency:%s,send_hour:%d,clustering:%t",
		where, auth.TenantID, sub.Frequency, sub.SendHour, req.ClusteringEnabled)
	proto := toDigestProto(ptrext.Indirect(out))
	proto.ClusteringEnabled = req.ClusteringEnabled
	return dispatcher.OK(proto)
}

// Delete handles DELETE /fb/v1/console/digest-subscription.
func (h *Handler) Delete(
	ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.DeleteDigestSubscriptionRequest,
) (dispatcher.Result[*attunev1.DeleteDigestSubscriptionResponse], error) {
	const where = "console.DigestSubscriptionHandler.Delete"
	auth := ctx.Auth
	if err := h.repo.DeleteByTenant(ctx, auth.TenantID); err != nil {
		if errors.Is(err, digestsubrepo.ErrNotFound) {
			return dispatcher.Fail[*attunev1.DeleteDigestSubscriptionResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "no digest subscription configured",
			)
		}
		logext.Errorf(ctx, "[%s] delete failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.DeleteDigestSubscriptionResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete digest subscription",
		)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s", where, auth.TenantID)
	return dispatcher.NoContent[*attunev1.DeleteDigestSubscriptionResponse]()
}

func (h *Handler) resolveTimezone(ctx context.Context, tenantID string, override *string) (string, error) {
	if override != nil && ptrext.Indirect(override) != "" {
		return ptrext.Indirect(override), nil
	}
	t, err := h.tenants.GetByID(ctx, tenantID)
	if err != nil {
		return "", err
	}
	return t.Timezone, nil
}

// normalizeUpsert validates the request and maps it to a repo Subscription
// (without TenantID / NextRunAt, which the handler fills). Timezone is validated
// as IANA here; weekly requires a byweekday in [0,6].
func normalizeUpsert(req *attunev1.UpsertDigestSubscriptionRequest) (digestsubrepo.Subscription, error) {
	freq := strings.TrimSpace(req.GetFrequency())
	if freq != "daily" && freq != "weekly" {
		return digestsubrepo.Subscription{}, errors.New("frequency must be 'daily' or 'weekly'")
	}
	sendHour := int(req.GetSendHour())
	if sendHour < 0 || sendHour > 23 {
		return digestsubrepo.Subscription{}, errors.New("send_hour must be between 0 and 23")
	}
	sub := digestsubrepo.Subscription{
		Enabled:        req.GetEnabled(),
		Frequency:      freq,
		SendHour:       sendHour,
		LLMMinFeedback: normalizeMin(int(req.GetLlmMinFeedback())),
		SendOnEmpty:    req.GetSendOnEmpty(),
	}
	bw, err := weekdayField(freq, req)
	if err != nil {
		return digestsubrepo.Subscription{}, err
	}
	sub.Byweekday = bw
	tz, err := timezoneField(req)
	if err != nil {
		return digestsubrepo.Subscription{}, err
	}
	sub.Timezone = tz
	if req.ThemePrompt != nil {
		if tp := strings.TrimSpace(req.GetThemePrompt()); tp != "" {
			sub.ThemePrompt = ptrext.Of(tp)
		}
	}
	return sub, nil
}

// weekdayField returns the byweekday pointer for the subscription (nil for
// daily), validating the [0,6] range and requiring it for weekly.
func weekdayField(frequency string, req *attunev1.UpsertDigestSubscriptionRequest) (*int, error) {
	if frequency != "weekly" {
		return nil, nil
	}
	if req.Byweekday == nil {
		return nil, errors.New("byweekday is required for weekly frequency")
	}
	wd := int(req.GetByweekday())
	if wd < 0 || wd > 6 {
		return nil, errors.New("byweekday must be between 0 (Sun) and 6 (Sat)")
	}
	return ptrext.Of(wd), nil
}

// timezoneField returns the IANA timezone override pointer (nil when unset),
// validated via time.LoadLocation.
func timezoneField(req *attunev1.UpsertDigestSubscriptionRequest) (*string, error) {
	if req.Timezone == nil {
		return nil, nil
	}
	tz := strings.TrimSpace(req.GetTimezone())
	if tz == "" {
		return nil, nil
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return nil, errors.New("timezone must be a valid IANA name")
	}
	return ptrext.Of(tz), nil
}

func normalizeMin(v int) int {
	if v <= 0 {
		return defaultLLMMinFeedback
	}
	return v
}

// toDigestProto maps a repo Subscription to the wire shape.
func toDigestProto(s digestsubrepo.Subscription) *attunev1.DigestSubscription {
	out := ptrext.Of(attunev1.DigestSubscription{
		Enabled:        s.Enabled,
		Frequency:      s.Frequency,
		SendHour:       int32(s.SendHour),
		LlmMinFeedback: int32(s.LLMMinFeedback),
		SendOnEmpty:    s.SendOnEmpty,
		CreatedAt:      s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      s.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if s.Byweekday != nil {
		out.Byweekday = ptrext.Of(int32(ptrext.Indirect(s.Byweekday)))
	}
	if s.Timezone != nil {
		out.Timezone = ptrext.Of(ptrext.Indirect(s.Timezone))
	}
	if s.ThemePrompt != nil {
		out.ThemePrompt = ptrext.Of(ptrext.Indirect(s.ThemePrompt))
	}
	if s.NextRunAt != nil {
		out.NextRunAt = ptrext.Of(ptrext.Indirect(s.NextRunAt).UTC().Format(time.RFC3339))
	}
	if s.LastRunAt != nil {
		out.LastRunAt = ptrext.Of(ptrext.Indirect(s.LastRunAt).UTC().Format(time.RFC3339))
	}
	return out
}
