package console

// notify_targets_write.go — the by-id mutating + probe handlers
// (Delete, Test) split out of notify_targets.go to keep that file under
// the attune ≤300-line rule (CLAUDE.md 律 2). Pure move; List/Create and
// the shared DTO + validation stay in notify_targets.go.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/repo"
)

// Delete handles DELETE /fb/v1/console/notify-targets/{id}.
func (h *NotifyTargetsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.Delete"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad uuid,tenant_id:%s", where, auth.TenantID)
		writeError(w, http.StatusBadRequest, "bad_id", "id 不是 UUID")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	if err := h.repo.Delete(r.Context(), auth.TenantID, id); err != nil {
		if errors.Is(err, repo.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			writeError(w, http.StatusNotFound, "not_found", "通知目标不存在或不属于当前 tenant")
			return
		}
		slog.ErrorContext(ctx, "notify-targets delete", "err", err)
		logext.Errorf(ctx, "[%s] repo.Delete failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "删除失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
}

// Test handles POST /fb/v1/console/notify-targets/{id}/test.
// 200 = destination accepted the ping; 502 = upstream rejected;
// 404 = wrong tenant / unknown id.
func (h *NotifyTargetsHandler) Test(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.Test"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad uuid,tenant_id:%s", where, auth.TenantID)
		writeError(w, http.StatusBadRequest, "bad_id", "id 不是 UUID")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	target, err := h.repo.GetByID(r.Context(), auth.TenantID, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			writeError(w, http.StatusNotFound, "not_found", "通知目标不存在或不属于当前 tenant")
			return
		}
		slog.ErrorContext(ctx, "notify-targets test load", "err", err)
		logext.Errorf(ctx, "[%s] repo.GetByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "加载目标失败")
		return
	}

	result := notify.TestSend(r.Context(), *target)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !result.OK {
		logext.Warnf(ctx, "[%s] reject: delivery failed,tenant_id:%s,id:%s,status:%d,latency_ms:%d,err:%s",
			where, auth.TenantID, id, result.StatusCode, result.LatencyMs, errMessage(result.Err))
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":        "delivery_failed",
			"message":     errMessage(result.Err),
			"status_code": result.StatusCode,
			"latency_ms":  result.LatencyMs,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"status_code": result.StatusCode,
		"latency_ms":  result.LatencyMs,
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
