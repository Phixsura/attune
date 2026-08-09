package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// AuthVerifyHandler handles GET /v1/auth/verify.
type AuthVerifyHandler struct {
	repo    apiKeyDetailStore
	tenants tenantNameStore
}

type apiKeyDetailStore interface {
	GetByID(ctx context.Context, tenantID string, id uuid.UUID) (*apikeyrepo.APIKeyListRow, error)
}

// tenantNameStore resolves the workspace display name for the connection
// label automation consumers show (e.g. Zapier — #234).
type tenantNameStore interface {
	GetByID(ctx context.Context, id string) (*tenant.Tenant, error)
}

// SetTenantStore wires the tenant display-name lookup. Unset → the
// tenant_display_name field stays empty (additive, older callers unaffected).
func (h *AuthVerifyHandler) SetTenantStore(tenants tenantNameStore) {
	h.tenants = tenants
}

// NewAuthVerifyHandler creates a new AuthVerifyHandler.
func NewAuthVerifyHandler(repo *apikeyrepo.APIKeyRepo) *AuthVerifyHandler {
	var store apiKeyDetailStore
	if repo != nil {
		store = repo
	}
	return ptrext.Of(AuthVerifyHandler{repo: store})
}

// Verify returns details about the current API key.
// The key is already validated by Middleware, so we just read the details.
func (h *AuthVerifyHandler) Verify(ctx *dispatcher.RequestContext[*apikey.AuthCtx], _ *attunev1.VerifyApiKeyRequest) (dispatcher.Result[*attunev1.VerifyApiKeyResponse], error) {
	auth := ctx.Auth
	row, err := h.repo.GetByID(ctx, auth.TenantID, auth.KeyID)
	if err != nil {
		return dispatcher.Fail[*attunev1.VerifyApiKeyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get key details")
	}

	scopeStrs := make([]string, len(auth.Scopes))
	for i, s := range auth.Scopes {
		scopeStrs[i] = string(s)
	}

	resp := ptrext.Of(attunev1.VerifyApiKeyResponse{
		Valid:     true,
		KeyPrefix: row.KeyPrefix,
		Label:     row.Label,
		Scopes:    scopeStrs,
	})
	if h.tenants != nil {
		// Best-effort: the connection label is a nicety, not a gate.
		if t, err := h.tenants.GetByID(ctx, auth.TenantID); err == nil && t != nil {
			resp.TenantDisplayName = t.Name
		}
	}
	if row.ExpiresAt != nil {
		resp.ExpiresAt = ptrext.Of(row.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if row.RateLimitRPM != nil {
		resp.RateLimitRpm = ptrext.Of(int32(ptrext.Indirect(row.RateLimitRPM)))
	}
	return dispatcher.OK(resp)
}
