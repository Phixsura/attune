package console

// notify_targets_write.go — the by-id mutating + probe handlers (Delete, Test).

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/notify"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// Delete handles DELETE /fb/v1/console/notify-targets/{id}.
func (h *NotifyTargetsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.Delete"
	ctx := r.Context()
	auth := FromContext(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad uuid,tenant_id:%s", where, auth.TenantID)
		respondError(ctx, w, http.StatusBadRequest, "bad_id", "id 不是 UUID")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	if err := h.repo.Delete(ctx, auth.TenantID, id); err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			respondError(ctx, w, http.StatusNotFound, "not_found", "通知目标不存在或不属于当前 tenant")
			return
		}
		slog.ErrorContext(ctx, "notify-targets delete", "err", err)
		logext.Errorf(ctx, "[%s] repo.Delete failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respondError(ctx, w, http.StatusInternalServerError, "internal", "删除失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
}

// Test handles POST /fb/v1/console/notify-targets/{id}/test.
// 200 = destination accepted the ping; 502 = upstream rejected.
func (h *NotifyTargetsHandler) Test(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.Test"
	ctx := r.Context()
	auth := FromContext(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad uuid,tenant_id:%s", where, auth.TenantID)
		respondError(ctx, w, http.StatusBadRequest, "bad_id", "id 不是 UUID")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	target, err := h.repo.GetByID(ctx, auth.TenantID, id)
	if err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			respondError(ctx, w, http.StatusNotFound, "not_found", "通知目标不存在或不属于当前 tenant")
			return
		}
		slog.ErrorContext(ctx, "notify-targets test load", "err", err)
		logext.Errorf(ctx, "[%s] repo.GetByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respondError(ctx, w, http.StatusInternalServerError, "internal", "加载目标失败")
		return
	}

	result := notify.TestSend(ctx, *target)
	if !result.OK {
		logext.Warnf(ctx, "[%s] reject: delivery failed,tenant_id:%s,id:%s,status:%d,latency_ms:%d,err:%s",
			where, auth.TenantID, id, result.StatusCode, result.LatencyMs, errMessage(result.Err))
		respondError(ctx, w, http.StatusBadGateway, "delivery_failed", errMessage(result.Err))
		return
	}
	sc := int32(result.StatusCode)
	lat := result.LatencyMs
	respondProto(w, http.StatusOK, &attunev1.TestNotifyTargetResponse{
		Ok:         true,
		StatusCode: &sc,
		LatencyMs:  &lat,
	})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,status:%d,latency_ms:%d",
		where, auth.TenantID, id, result.StatusCode, result.LatencyMs)
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
