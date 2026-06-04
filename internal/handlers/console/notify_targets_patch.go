package console

// Phase 1.2 follow-up — PATCH /notify-targets/{id} for sparse edits
// (change URL / toggle disabled / rotate secret without delete+recreate).
// Split into its own file to keep notify_targets.go under the listen
// ≤300-line discipline.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/listen/internal/logext"
	"github.com/Phixsura/listen/internal/repo"
)

// patchNotifyRequest is the sparse PATCH body. Every field is *T so we
// can distinguish "key present and explicit" from "key omitted, leave
// unchanged". destination_type is intentionally absent — changing type
// = different row identity; clients should delete + recreate.
type patchNotifyRequest struct {
	Audience       *string `json:"audience"`
	URL            *string `json:"url"`
	Secret         *string `json:"secret"`
	TimeoutSeconds *int    `json:"timeout_seconds"`
	Disabled       *bool   `json:"disabled"`
}

// Patch handles PATCH /fb/v1/console/notify-targets/{id}.
//
// Implementation: Get → merge → UpdateByID. Two DB round-trips, but a
// console edit happens at human speed; the simpler code path beats a
// COALESCE-laden single UPDATE.
func (h *NotifyTargetsHandler) Patch(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.Patch"
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
	var patch patchNotifyRequest
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		logext.Warnf(ctx, "[%s] reject: bad json,tenant_id:%s,id:%s,err:%s",
			where, auth.TenantID, id, err.Error())
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)

	cur, err := h.repo.GetByID(r.Context(), auth.TenantID, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			writeError(w, http.StatusNotFound, "not_found",
				"通知目标不存在或不属于当前 tenant")
			return
		}
		slog.ErrorContext(ctx, "notify-targets patch get", "err", err)
		logext.Errorf(ctx, "[%s] repo.GetByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		writeError(w, http.StatusInternalServerError, "internal",
			"读取通知目标失败")
		return
	}

	// Apply patch fields. Empty-string secret is intentionally allowed
	// — matches the schema description ("clear the secret").
	if patch.Audience != nil {
		cur.Audience = *patch.Audience
	}
	if patch.URL != nil {
		cur.URL = *patch.URL
	}
	if patch.Secret != nil {
		cur.Secret = *patch.Secret
	}
	if patch.TimeoutSeconds != nil {
		cur.TimeoutSeconds = *patch.TimeoutSeconds
	}
	if patch.Disabled != nil {
		cur.Disabled = *patch.Disabled
	}

	// Reuse Create's invariant checks on the post-merge state.
	if err := validateNotifyCreate(&createNotifyRequest{
		DestinationType: cur.DestinationType,
		Audience:        cur.Audience,
		URL:             cur.URL,
		Secret:          cur.Secret,
		TimeoutSeconds:  cur.TimeoutSeconds,
		Disabled:        cur.Disabled,
	}); err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,id:%s,err:%s",
			where, auth.TenantID, id, err.Error())
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}

	if err := h.repo.UpdateByID(r.Context(), auth.TenantID, id, *cur); err != nil {
		if errors.Is(err, repo.ErrNotifyTargetConflict) {
			logext.Warnf(ctx, "[%s] reject: conflict,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			writeError(w, http.StatusConflict, "conflict",
				"audience 与同 destination_type 下另一目标冲突；改回去或先删那条")
			return
		}
		if errors.Is(err, repo.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found pre-update,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			writeError(w, http.StatusNotFound, "not_found",
				"通知目标在更新前被删除")
			return
		}
		slog.ErrorContext(ctx, "notify-targets patch update", "err", err)
		logext.Errorf(ctx, "[%s] repo.UpdateByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		writeError(w, http.StatusInternalServerError, "internal",
			"更新通知目标失败")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(toNotifyDTO(*cur))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
}
