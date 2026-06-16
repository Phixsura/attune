package notifytarget

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
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
		return dispatcher.Fail[*attunev1.DeleteNotifyTargetResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id is not a UUID")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	target, err := h.repo.GetByID(ctx, auth.TenantID, id)
	if err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.DeleteNotifyTargetResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "notify target not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] repo.GetByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.DeleteNotifyTargetResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load notify target")
	}
	if err := h.repo.Delete(ctx, auth.TenantID, id); err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.DeleteNotifyTargetResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "notify target not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] repo.Delete failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.DeleteNotifyTargetResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "delete failed")
	}
	if target != nil {
		if err := h.recordAudit(
			ctx,
			auth.UserType,
			auth.UserID,
			auth.TenantID,
			"notify_target.delete",
			ctx.Request(),
			ptrext.Indirect(target),
			notifyTargetSummary("Deleted notify target", ptrext.Indirect(target)),
			auditNotifyTargetSnapshot(ptrext.Indirect(target)),
			nil,
		); err != nil {
			logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,id:%s,err:%+v",
				where, auth.TenantID, id, err.Error())
			return dispatcher.Fail[*attunev1.DeleteNotifyTargetResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
		}
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
	return dispatcher.NoContent[*attunev1.DeleteNotifyTargetResponse]()
}
