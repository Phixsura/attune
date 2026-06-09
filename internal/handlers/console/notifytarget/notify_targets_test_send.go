package notifytarget

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// Test handles POST /fb/v1/console/notify-targets/{id}/test.
// 200 = destination accepted the ping; 502 = upstream rejected.
func (h *NotifyTargetsHandler) Test(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.TestNotifyTargetRequest) (dispatcher.Result[*attunev1.TestNotifyTargetResponse], error) {
	const where = "console.NotifyTargetsHandler.Test"
	auth := ctx.Auth
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad uuid,tenant_id:%s", where, auth.TenantID)
		return dispatcher.Result[*attunev1.TestNotifyTargetResponse]{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id is not a UUID")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	target, err := h.repo.GetByID(ctx, auth.TenantID, id)
	if err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			return dispatcher.Result[*attunev1.TestNotifyTargetResponse]{}, dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "notify target not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] repo.GetByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Result[*attunev1.TestNotifyTargetResponse]{}, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load target")
	}

	result := notify.TestSend(ctx, ptrext.Indirect(target))
	if !result.OK {
		logext.Warnf(ctx, "[%s] reject: delivery failed,tenant_id:%s,id:%s,status:%d,latency_ms:%d,err:%s",
			where, auth.TenantID, id, result.StatusCode, result.LatencyMs, errMessage(result.Err))
		return dispatcher.Result[*attunev1.TestNotifyTargetResponse]{}, dispatcher.NewError(http.StatusBadGateway, attunev1.ErrorCode_DELIVERY_FAILED, errMessage(result.Err))
	}
	sc := int32(result.StatusCode)
	lat := result.LatencyMs
	resp := ptrext.Of(attunev1.TestNotifyTargetResponse{
		Ok:         true,
		StatusCode: ptrext.Of(sc),
		LatencyMs:  ptrext.Of(lat),
	})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,status:%d,latency_ms:%d",
		where, auth.TenantID, id, result.StatusCode, result.LatencyMs)
	return dispatcher.OK(http.StatusOK, resp), nil
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
