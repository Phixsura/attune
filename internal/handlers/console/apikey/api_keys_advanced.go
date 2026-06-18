package apikey

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	apikeysvc "github.com/Phixsura/attune/internal/service/apikey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// Rotate creates a new key and puts the old one in grace period.
func (h *APIKeysHandler) Rotate(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RotateApiKeyRequest) (dispatcher.Result[*attunev1.RotateApiKeyResponse], error) {
	const where = "handlers.console.apikey.Rotate"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s,id:%s", where, auth.TenantID, req.GetId())
		return dispatcher.Fail[*attunev1.RotateApiKeyResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	gracePeriod := 24 * time.Hour
	if gp := ptrext.Indirect(req.GracePeriod); gp != "" {
		parsed, err := parseGracePeriod(gp)
		if err != nil {
			return dispatcher.Fail[*attunev1.RotateApiKeyResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid grace_period: "+err.Error())
		}
		gracePeriod = parsed
	}

	newRaw, newKeyID, err := h.svc.Rotate(ctx, apikeysvc.RotateParams{
		TenantID:    auth.TenantID,
		OldKeyID:    keyID,
		GracePeriod: gracePeriod,
	})
	if err != nil {
		if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
			return dispatcher.Fail[*attunev1.RotateApiKeyResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "key not found")
		}
		logext.Errorf(ctx, "[%s] rotate failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.RotateApiKeyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "rotation failed")
	}

	oldRow, _ := h.svc.GetByID(ctx, auth.TenantID, keyID)
	newRow, _ := h.svc.GetByID(ctx, auth.TenantID, newKeyID)
	scopes, _ := h.svc.GetScopes(ctx, newKeyID)

	gracePeriodEndsAt := time.Now().Add(gracePeriod)

	if h.audit != nil {
		_ = h.audit.Record(ctx, auditlogsvc.Event{
			TenantID:   auth.TenantID,
			Actor:      auditActor(auth, ctx.Request()),
			Action:     "api_key.rotate",
			TargetType: "api_key",
			TargetID:   keyID.String(),
			Summary:    "Rotated API key",
			Before: map[string]any{
				"key_id":    keyID.String(),
				"is_active": true,
			},
			After: map[string]any{
				"new_key_id":           newKeyID.String(),
				"grace_period_ends_at": gracePeriodEndsAt.Format(time.RFC3339),
			},
		})
	}

	return dispatcher.OK(ptrext.Of(attunev1.RotateApiKeyResponse{
		OldKey:            toProtoAPIKeyFromRow(oldRow, nil),
		NewKey:            toProtoAPIKeyFromRow(newRow, scopes),
		Secret:            newRaw,
		GracePeriodEndsAt: gracePeriodEndsAt.Format(time.RFC3339),
	}))
}

// ListLogs returns request logs for a key.
func (h *APIKeysHandler) ListLogs(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListApiKeyLogsRequest) (dispatcher.Result[*attunev1.ListApiKeyLogsResponse], error) {
	const where = "handlers.console.apikey.ListLogs"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListApiKeyLogsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	limit := 100
	if req.Limit != nil {
		limit = int(ptrext.Indirect(req.Limit))
		if limit < 1 {
			limit = 1
		}
		if limit > 1000 {
			limit = 1000
		}
	}

	logs, err := h.svc.ListRequestLogs(ctx, auth.TenantID, keyID, limit)
	if err != nil {
		logext.Errorf(ctx, "[%s] list logs failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.ListApiKeyLogsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list logs")
	}

	items := make([]*attunev1.ApiKeyLogEntry, 0, len(logs))
	for _, log := range logs {
		items = append(items, ptrext.Of(attunev1.ApiKeyLogEntry{
			Timestamp:  log.Timestamp.UTC().Format(time.RFC3339),
			Method:     log.Method,
			Path:       log.Path,
			StatusCode: int32(log.StatusCode),
			ClientIp:   log.ClientIP,
			UserAgent:  log.UserAgent,
			LatencyMs:  int32(log.LatencyMs),
			RequestId:  log.RequestID,
		}))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListApiKeyLogsResponse{Logs: items}))
}

// UpdateEnvironment updates the environment tag for a key.
func (h *APIKeysHandler) UpdateEnvironment(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateApiKeyEnvironmentRequest) (dispatcher.Result[*attunev1.UpdateApiKeyEnvironmentResponse], error) {
	const where = "handlers.console.apikey.UpdateEnvironment"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.UpdateApiKeyEnvironmentResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	env := strings.TrimSpace(req.GetEnvironment())
	if !domain.IsValidEnvironment(env) {
		return dispatcher.Fail[*attunev1.UpdateApiKeyEnvironmentResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid environment: must be production, staging, development, or test")
	}

	if err := h.svc.UpdateEnvironment(ctx, auth.TenantID, keyID, env); err != nil {
		if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
			return dispatcher.Fail[*attunev1.UpdateApiKeyEnvironmentResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "key not found")
		}
		logext.Errorf(ctx, "[%s] update environment failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.UpdateApiKeyEnvironmentResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "update failed")
	}

	row, _ := h.svc.GetByID(ctx, auth.TenantID, keyID)
	scopes, _ := h.svc.GetScopes(ctx, keyID)

	return dispatcher.OK(ptrext.Of(attunev1.UpdateApiKeyEnvironmentResponse{
		Key: toProtoAPIKeyFromRow(row, scopes),
	}))
}

// ListExpiring returns keys expiring within a time window.
func (h *APIKeysHandler) ListExpiring(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListExpiringApiKeysRequest) (dispatcher.Result[*attunev1.ListExpiringApiKeysResponse], error) {
	const where = "handlers.console.apikey.ListExpiring"

	within := 7 * 24 * time.Hour
	if w := ptrext.Indirect(req.Within); w != "" {
		parsed, err := parseGracePeriod(w)
		if err != nil {
			return dispatcher.Fail[*attunev1.ListExpiringApiKeysResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid within: "+err.Error())
		}
		within = parsed
	}

	keys, err := h.svc.GetExpiringKeys(ctx, within)
	if err != nil {
		logext.Errorf(ctx, "[%s] get expiring keys failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.ListExpiringApiKeysResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list expiring keys")
	}

	items := make([]*attunev1.ApiKey, 0, len(keys))
	for _, row := range keys {
		items = append(items, toProtoAPIKeyFromRow(ptrext.Of(row), nil))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListExpiringApiKeysResponse{Items: items}))
}

// ListServiceAccounts returns all service accounts.
func (h *APIKeysHandler) ListServiceAccounts(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListServiceAccountsRequest) (dispatcher.Result[*attunev1.ListServiceAccountsResponse], error) {
	const where = "handlers.console.apikey.ListServiceAccounts"
	auth := ctx.Auth

	accounts, err := h.svc.ListServiceAccounts(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list service accounts failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListServiceAccountsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list service accounts")
	}

	items := make([]*attunev1.ServiceAccount, 0, len(accounts))
	for _, sa := range accounts {
		items = append(items, ptrext.Of(attunev1.ServiceAccount{
			Id:          sa.ID.String(),
			Name:        sa.Name,
			Description: sa.Description,
			IsActive:    sa.IsActive,
			CreatedAt:   sa.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   sa.UpdatedAt.UTC().Format(time.RFC3339),
		}))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListServiceAccountsResponse{Items: items}))
}

// CreateServiceAccount creates a new service account.
func (h *APIKeysHandler) CreateServiceAccount(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateServiceAccountRequest) (dispatcher.Result[*attunev1.CreateServiceAccountResponse], error) {
	const where = "handlers.console.apikey.CreateServiceAccount"
	auth := ctx.Auth

	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return dispatcher.Fail[*attunev1.CreateServiceAccountResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_LABEL, "name is required")
	}

	description := strings.TrimSpace(ptrext.Indirect(req.Description))

	id, err := h.svc.CreateServiceAccount(ctx, auth.TenantID, name, description)
	if err != nil {
		logext.Errorf(ctx, "[%s] create service account failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateServiceAccountResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create service account")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	return dispatcher.Created(ptrext.Of(attunev1.CreateServiceAccountResponse{
		ServiceAccount: ptrext.Of(attunev1.ServiceAccount{
			Id:          id.String(),
			Name:        name,
			Description: description,
			IsActive:    true,
			CreatedAt:   now,
			UpdatedAt:   now,
		}),
	}))
}

// ListEventSubscriptions returns all event webhook subscriptions.
func (h *APIKeysHandler) ListEventSubscriptions(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListEventSubscriptionsRequest) (dispatcher.Result[*attunev1.ListEventSubscriptionsResponse], error) {
	const where = "handlers.console.apikey.ListEventSubscriptions"
	auth := ctx.Auth

	subs, err := h.svc.ListEventSubscriptions(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list event subscriptions failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListEventSubscriptionsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list subscriptions")
	}

	items := make([]*attunev1.EventSubscription, 0, len(subs))
	for _, sub := range subs {
		items = append(items, ptrext.Of(attunev1.EventSubscription{
			Id:         sub.ID.String(),
			EventTypes: sub.EventTypes,
			WebhookUrl: sub.WebhookURL,
			IsActive:   sub.IsActive,
			CreatedAt:  sub.CreatedAt.UTC().Format(time.RFC3339),
		}))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListEventSubscriptionsResponse{Items: items}))
}

// CreateEventSubscription creates a new event webhook subscription.
func (h *APIKeysHandler) CreateEventSubscription(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateEventSubscriptionRequest) (dispatcher.Result[*attunev1.CreateEventSubscriptionResponse], error) {
	const where = "handlers.console.apikey.CreateEventSubscription"
	auth := ctx.Auth

	eventTypes := req.GetEventTypes()
	if len(eventTypes) == 0 {
		return dispatcher.Fail[*attunev1.CreateEventSubscriptionResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "at least one event type required")
	}

	webhookURL := strings.TrimSpace(req.GetWebhookUrl())
	if webhookURL == "" {
		return dispatcher.Fail[*attunev1.CreateEventSubscriptionResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "webhook_url is required")
	}

	secret := strings.TrimSpace(ptrext.Indirect(req.Secret))

	id, err := h.svc.CreateEventSubscription(ctx, auth.TenantID, eventTypes, webhookURL, secret)
	if err != nil {
		logext.Errorf(ctx, "[%s] create event subscription failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateEventSubscriptionResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create subscription")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateEventSubscriptionResponse{
		Subscription: ptrext.Of(attunev1.EventSubscription{
			Id:         id.String(),
			EventTypes: eventTypes,
			WebhookUrl: webhookURL,
			IsActive:   true,
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		}),
	}))
}

// ListLeakDetections returns leak detections for the tenant.
func (h *APIKeysHandler) ListLeakDetections(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListLeakDetectionsRequest) (dispatcher.Result[*attunev1.ListLeakDetectionsResponse], error) {
	const where = "handlers.console.apikey.ListLeakDetections"
	auth := ctx.Auth

	leaks, err := h.svc.GetUnresolvedLeaks(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get leaks failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListLeakDetectionsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list leak detections")
	}

	items := make([]*attunev1.LeakDetection, 0, len(leaks))
	for _, leak := range leaks {
		item := ptrext.Of(attunev1.LeakDetection{
			Id:         leak.ID.String(),
			KeyId:      leak.KeyID.String(),
			Source:     leak.Source,
			SourceUrl:  leak.SourceURL,
			DetectedAt: leak.DetectedAt.UTC().Format(time.RFC3339),
			Resolution: leak.Resolution,
		})
		if leak.ResolvedAt != nil {
			item.ResolvedAt = ptrext.Of(leak.ResolvedAt.UTC().Format(time.RFC3339))
		}
		items = append(items, item)
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListLeakDetectionsResponse{Items: items}))
}

func parseGracePeriod(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasSuffix(s, "h") {
		hours, err := strconv.Atoi(strings.TrimSuffix(s, "h"))
		if err != nil || hours < 1 {
			return 0, err
		}
		return time.Duration(hours) * time.Hour, nil
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || days < 1 {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func toProtoAPIKeyFromRow(row *apikeyrepo.APIKeyListRow, scopes []domain.Scope) *attunev1.ApiKey {
	if row == nil {
		return nil
	}
	k := ptrext.Of(attunev1.ApiKey{
		Id:          row.ID.String(),
		KeyPrefix:   row.KeyPrefix,
		Label:       row.Label,
		IsActive:    row.IsActive,
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
		Description: ptrext.Of(row.Description),
		UsageCount:  row.UsageCount,
		Environment: "production",
	})
	if row.LastUsedAt != nil {
		k.LastUsedAt = ptrext.Of(row.LastUsedAt.UTC().Format(time.RFC3339))
	}
	if row.RevokedAt != nil {
		k.RevokedAt = ptrext.Of(row.RevokedAt.UTC().Format(time.RFC3339))
	}
	if row.ExpiresAt != nil {
		k.ExpiresAt = ptrext.Of(row.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if row.RateLimitRPM != nil {
		k.RateLimitRpm = ptrext.Of(int32(ptrext.Indirect(row.RateLimitRPM)))
	}
	k.AllowedCidrs = row.AllowedCIDRs
	for _, s := range scopes {
		k.Scopes = append(k.Scopes, string(s))
	}
	return k
}
