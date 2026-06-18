package apikey

import (
	"net/http"
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

// Create handles POST /fb/v1/console/api-keys.
// Returns 201 with the raw key once. Subsequent List calls only show
// the prefix — the secret is unrecoverable.
func (h *APIKeysHandler) Create(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateApiKeyRequest) (dispatcher.Result[*attunev1.CreateApiKeyResponse], error) {
	const where = "console.APIKeysHandler.Create"
	auth := ctx.Auth

	params, scopes, err := h.validateCreateRequest(ctx, req)
	if err != nil {
		return dispatcher.Result[*attunev1.CreateApiKeyResponse]{}, err
	}

	logext.Infof(ctx, "[%s] start,tenant_id:%s,label:%s,scopes:%d,expires:%v,cidrs:%d",
		where, auth.TenantID, params.Label, len(scopes), params.ExpiresAt != nil, len(params.AllowedCIDRs))

	raw, id, err := h.svc.IssueFullWithScopes(ctx, params, scopes)
	if err != nil {
		logext.Errorf(ctx, "[%s] svc.IssueFullWithScopes failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateApiKeyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to issue API key")
	}

	newRow := h.findCreatedKey(ctx, auth.TenantID, id, where)
	resp := ptrext.Of(attunev1.CreateApiKeyResponse{
		Key:    toProtoAPIKey(newRow, scopes),
		Secret: raw,
	})

	if err := h.recordCreateAudit(ctx, auth, id, newRow, params); err != nil {
		return dispatcher.Fail[*attunev1.CreateApiKeyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
	return dispatcher.Created(resp)
}

func (h *APIKeysHandler) validateCreateRequest(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateApiKeyRequest) (apikeysvc.IssueParams, []domain.Scope, error) {
	const where = "console.APIKeysHandler.Create"
	auth := ctx.Auth

	label := strings.TrimSpace(req.GetLabel())
	if label == "" {
		logext.Warnf(ctx, "[%s] reject: missing label,tenant_id:%s", where, auth.TenantID)
		return apikeysvc.IssueParams{}, nil, ptrext.Of(dispatcher.Error{Status: http.StatusBadRequest, Code: attunev1.ErrorCode_MISSING_LABEL, Message: "label must not be empty"})
	}
	if len(label) > 200 {
		logext.Warnf(ctx, "[%s] reject: label too long,tenant_id:%s,len:%d", where, auth.TenantID, len(label))
		return apikeysvc.IssueParams{}, nil, ptrext.Of(dispatcher.Error{Status: http.StatusBadRequest, Code: attunev1.ErrorCode_LABEL_TOO_LONG, Message: "label must not exceed 200 characters"})
	}

	scopes, err := h.parseCreateScopes(ctx, req)
	if err != nil {
		return apikeysvc.IssueParams{}, nil, err
	}

	allowedCIDRs, err := h.parseCreateCIDRs(ctx, req)
	if err != nil {
		return apikeysvc.IssueParams{}, nil, err
	}

	rateLimitRPM, err := h.parseCreateRateLimit(ctx, req)
	if err != nil {
		return apikeysvc.IssueParams{}, nil, err
	}

	var expiresAt *time.Time
	if expiresIn := ptrext.Indirect(req.ExpiresIn); expiresIn != "" && expiresIn != "never" {
		expiresAt = parseExpiry(expiresIn)
	}

	return apikeysvc.IssueParams{
		TenantID:     auth.TenantID,
		Label:        label,
		Description:  strings.TrimSpace(ptrext.Indirect(req.Description)),
		ExpiresAt:    expiresAt,
		AllowedCIDRs: allowedCIDRs,
		RateLimitRPM: rateLimitRPM,
	}, scopes, nil
}

func (h *APIKeysHandler) parseCreateScopes(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateApiKeyRequest) ([]domain.Scope, error) {
	const where = "console.APIKeysHandler.Create"
	auth := ctx.Auth

	if len(req.GetScopes()) == 0 {
		return GetFullAccessScopes(), nil
	}

	scopes, err := domain.ParseScopes(req.GetScopes())
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: invalid scope,tenant_id:%s", where, auth.TenantID)
		return nil, ptrext.Of(dispatcher.Error{Status: http.StatusBadRequest, Code: attunev1.ErrorCode_BAD_REQUEST, Message: "invalid scope"})
	}

	seen := make(map[domain.Scope]struct{}, len(scopes))
	deduped := make([]domain.Scope, 0, len(scopes))
	for _, s := range scopes {
		if s == domain.ScopeAPIKeyAdmin {
			logext.Warnf(ctx, "[%s] reject: apikey:admin not allowed,tenant_id:%s", where, auth.TenantID)
			return nil, ptrext.Of(dispatcher.Error{Status: http.StatusForbidden, Code: attunev1.ErrorCode_FORBIDDEN, Message: "apikey:admin scope cannot be granted via Console"})
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			deduped = append(deduped, s)
		}
	}
	return deduped, nil
}

func (h *APIKeysHandler) parseCreateCIDRs(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateApiKeyRequest) ([]string, error) {
	const where = "console.APIKeysHandler.Create"
	auth := ctx.Auth

	if len(req.GetAllowedCidrs()) == 0 {
		return nil, nil
	}
	if _, err := domain.ParseCIDRs(req.GetAllowedCidrs()); err != nil {
		logext.Warnf(ctx, "[%s] reject: invalid CIDR,tenant_id:%s,err:%s", where, auth.TenantID, err.Error())
		return nil, ptrext.Of(dispatcher.Error{Status: http.StatusBadRequest, Code: attunev1.ErrorCode_BAD_REQUEST, Message: "invalid CIDR: " + err.Error()})
	}
	return req.GetAllowedCidrs(), nil
}

func (h *APIKeysHandler) parseCreateRateLimit(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateApiKeyRequest) (*int, error) {
	const where = "console.APIKeysHandler.Create"
	auth := ctx.Auth

	if req.RateLimitRpm == nil {
		return nil, nil
	}
	v := int(ptrext.Indirect(req.RateLimitRpm))
	if v < 1 || v > 100000 {
		logext.Warnf(ctx, "[%s] reject: invalid rate limit,tenant_id:%s,val:%d", where, auth.TenantID, v)
		return nil, ptrext.Of(dispatcher.Error{Status: http.StatusBadRequest, Code: attunev1.ErrorCode_BAD_REQUEST, Message: "rate_limit_rpm must be between 1 and 100000"})
	}
	return ptrext.Of(v), nil
}

func (h *APIKeysHandler) findCreatedKey(ctx *dispatcher.RequestContext[*session.AuthCtx], tenantID string, keyID uuid.UUID, where string) apikeyrepo.APIKeyListRow {
	rows, err := h.svc.List(ctx, tenantID)
	if err != nil {
		logext.Warnf(ctx, "[%s] post-issue list failed,tenant_id:%s,err:%s", where, tenantID, err.Error())
		return apikeyrepo.APIKeyListRow{}
	}
	for _, row := range rows {
		if row.ID == keyID {
			return row
		}
	}
	return apikeyrepo.APIKeyListRow{}
}

func (h *APIKeysHandler) recordCreateAudit(ctx *dispatcher.RequestContext[*session.AuthCtx], auth *session.AuthCtx, keyID uuid.UUID, row apikeyrepo.APIKeyListRow, params apikeysvc.IssueParams) error {
	const where = "console.APIKeysHandler.Create"
	if h.audit == nil {
		return nil
	}

	auditAfter := map[string]any{
		"id":         keyID.String(),
		"label":      row.Label,
		"key_prefix": row.KeyPrefix,
		"is_active":  row.IsActive,
	}
	if params.ExpiresAt != nil {
		auditAfter["expires_at"] = params.ExpiresAt.Format(time.RFC3339)
	}
	if len(params.AllowedCIDRs) > 0 {
		auditAfter["allowed_cidrs"] = params.AllowedCIDRs
	}
	if params.RateLimitRPM != nil {
		auditAfter["rate_limit_rpm"] = ptrext.Indirect(params.RateLimitRPM)
	}

	if err := h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   auth.TenantID,
		Actor:      auditActor(auth, ctx.Request()),
		Action:     "api_key.create",
		TargetType: "api_key",
		TargetID:   keyID.String(),
		Summary:    "Created API key",
		After:      auditAfter,
	}); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return err
	}
	return nil
}

// parseExpiry parses expiry strings like "7d", "30d", "90d", "1y" or RFC3339.
func parseExpiry(s string) *time.Time {
	now := time.Now()
	switch s {
	case "7d":
		t := now.Add(7 * 24 * time.Hour)
		return ptrext.Of(t)
	case "30d":
		t := now.Add(30 * 24 * time.Hour)
		return ptrext.Of(t)
	case "90d":
		t := now.Add(90 * 24 * time.Hour)
		return ptrext.Of(t)
	case "1y":
		t := now.Add(365 * 24 * time.Hour)
		return ptrext.Of(t)
	default:
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return ptrext.Of(t)
		}
		return nil
	}
}
