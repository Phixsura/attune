package notifytarget

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// Delete handles DELETE /fb/v1/console/notify-targets/{id}.
func (h *NotifyTargetsHandler) Delete(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DeleteNotifyTargetRequest) (dispatcher.Result[*attunev1.DeleteNotifyTargetResponse], error) {
	const where = "console.NotifyTargetsHandler.Delete"
	auth := ctx.Auth
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad uuid,tenant_id:%s", where, auth.TenantID)
		return dispatcher.Result[*attunev1.DeleteNotifyTargetResponse]{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id is not a UUID")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	if err := h.repo.Delete(ctx, auth.TenantID, id); err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			return dispatcher.Result[*attunev1.DeleteNotifyTargetResponse]{}, dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "notify target not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] repo.Delete failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Result[*attunev1.DeleteNotifyTargetResponse]{}, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "delete failed")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
	return dispatcher.NoContent[*attunev1.DeleteNotifyTargetResponse](), nil
}
