package apikey

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// List handles GET /fb/v1/console/api-keys.
func (h *APIKeysHandler) List(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListApiKeysRequest) (dispatcher.Result[*attunev1.ListApiKeysResponse], error) {
	const where = "console.APIKeysHandler.List"
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	rows, err := h.svc.List(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] svc.List failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListApiKeysResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list API keys")
	}

	activeIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.IsActive && row.RevokedAt == nil {
			activeIDs = append(activeIDs, row.ID)
		}
	}

	var scopeMap map[uuid.UUID][]domain.Scope
	if len(activeIDs) > 0 {
		scopeMap, err = h.svc.GetScopesBatch(ctx, activeIDs)
		if err != nil {
			logext.Warnf(ctx, "[%s] GetScopesBatch failed,tenant_id:%s,err:%s",
				where, auth.TenantID, err.Error())
			scopeMap = make(map[uuid.UUID][]domain.Scope)
		}
	} else {
		scopeMap = make(map[uuid.UUID][]domain.Scope)
	}

	items := make([]*attunev1.ApiKey, 0, len(rows))
	for _, row := range rows {
		var scopes []domain.Scope
		if row.IsActive && row.RevokedAt == nil {
			scopes = scopeMap[row.ID]
		}
		items = append(items, toProtoAPIKey(row, scopes))
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
	return dispatcher.OK(ptrext.Of(attunev1.ListApiKeysResponse{Items: items}))
}
