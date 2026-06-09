package apikey

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

// Revoke handles DELETE /fb/v1/console/api-keys/{id}.
// 204 on success, 404 if id doesn't exist for this tenant.
func (h *APIKeysHandler) Revoke(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DeleteApiKeyRequest) (dispatcher.Result[*attunev1.DeleteApiKeyResponse], error) {
	const where = "console.APIKeysHandler.Revoke"
	auth := ctx.Auth
	idStr := req.GetId()
	id, err := uuid.Parse(idStr)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s,id_str:%s",
			where, auth.TenantID, idStr)
		return dispatcher.Fail[*attunev1.DeleteApiKeyResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id is not a UUID")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
	if err := h.svc.Revoke(ctx, auth.TenantID, id); err != nil {
		if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,key_id:%s",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.DeleteApiKeyResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "API key not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] svc.Revoke failed,tenant_id:%s,key_id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.DeleteApiKeyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "revoke failed")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
	return dispatcher.NoContent[*attunev1.DeleteApiKeyResponse]()
}
