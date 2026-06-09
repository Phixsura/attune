package notifytarget

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// List handles GET /fb/v1/console/notify-targets.
func (h *NotifyTargetsHandler) List(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListNotifyTargetsRequest) (dispatcher.Result[*attunev1.ListNotifyTargetsResponse], error) {
	const where = "console.NotifyTargetsHandler.List"
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	rows, err := h.repo.ListByTenant(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] notifytarget.ListByTenant failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Result[*attunev1.ListNotifyTargetsResponse]{}, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list notify targets")
	}
	items := make([]*attunev1.NotifyTarget, 0, len(rows))
	for _, row := range rows {
		items = append(items, toNotifyProto(row))
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
	return dispatcher.OK(http.StatusOK, ptrext.Of(attunev1.ListNotifyTargetsResponse{Items: items})), nil
}
