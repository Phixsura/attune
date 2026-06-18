package handlers

import (
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

// AuthVerifyHandler handles GET /v1/auth/verify.
type AuthVerifyHandler struct {
	repo *apikeyrepo.APIKeyRepo
}

// NewAuthVerifyHandler creates a new AuthVerifyHandler.
func NewAuthVerifyHandler(repo *apikeyrepo.APIKeyRepo) *AuthVerifyHandler {
	return ptrext.Of(AuthVerifyHandler{repo: repo})
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
	if row.ExpiresAt != nil {
		resp.ExpiresAt = ptrext.Of(row.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if row.RateLimitRPM != nil {
		resp.RateLimitRpm = ptrext.Of(int32(ptrext.Indirect(row.RateLimitRPM)))
	}
	return dispatcher.OK(resp)
}
