package apikey

import (
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

// Create handles POST /fb/v1/console/api-keys.
// Returns 201 with the raw key once. Subsequent List calls only show
// the prefix — the secret is unrecoverable.
func (h *APIKeysHandler) Create(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateApiKeyRequest) (dispatcher.Result[*attunev1.CreateApiKeyResponse], error) {
	const where = "console.APIKeysHandler.Create"
	auth := ctx.Auth
	label := strings.TrimSpace(req.GetLabel())
	if label == "" {
		logext.Warnf(ctx, "[%s] reject: missing label,tenant_id:%s", where, auth.TenantID)
		return dispatcher.Result[*attunev1.CreateApiKeyResponse]{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_MISSING_LABEL, "label must not be empty")
	}
	if len(label) > 200 {
		logext.Warnf(ctx, "[%s] reject: label too long,tenant_id:%s,len:%d",
			where, auth.TenantID, len(label))
		return dispatcher.Result[*attunev1.CreateApiKeyResponse]{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_LABEL_TOO_LONG, "label must not exceed 200 characters")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,label:%s", where, auth.TenantID, label)

	raw, id, err := h.svc.Issue(ctx, auth.TenantID, label)
	if err != nil {
		logext.Errorf(ctx, "[%s] svc.Issue failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Result[*attunev1.CreateApiKeyResponse]{}, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to issue API key")
	}

	// Re-read the row so we return canonical timestamps. Cheap: N is tiny.
	rows, err := h.svc.List(ctx, auth.TenantID)
	if err != nil {
		logext.Warnf(ctx, "[%s] post-issue list failed,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
	}
	var newRow apikeyrepo.APIKeyListRow
	for _, row := range rows {
		if row.ID == id {
			newRow = row
			break
		}
	}

	result := dispatcher.OK(http.StatusCreated, ptrext.Of(attunev1.CreateApiKeyResponse{
		Key:    toProtoAPIKey(newRow),
		Secret: raw,
	}))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
	return result, nil
}
