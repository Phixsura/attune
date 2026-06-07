package notifytarget

// PATCH /notify-targets/{id} for sparse edits (change URL / toggle disabled /
// rotate secret without delete+recreate). destination_type is intentionally
// not patchable — changing type = different row identity.

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// Patch handles PATCH /fb/v1/console/notify-targets/{id}.
//
// Implementation: Get → merge → UpdateByID.
func (h *NotifyTargetsHandler) Patch(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.Patch"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad uuid,tenant_id:%s", where, auth.TenantID)
		respond.Error(ctx, w, http.StatusBadRequest, "bad_id", "id is not a UUID")
		return
	}
	var patch attunev1.UpdateNotifyTargetRequest
	if err := respond.Decode(r.Body, &patch); err != nil {
		logext.Warnf(ctx, "[%s] reject: bad json,tenant_id:%s,id:%s,err:%s",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "request body is not valid JSON")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)

	cur, err := h.repo.GetByID(ctx, auth.TenantID, id)
	if err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			respond.Error(ctx, w, http.StatusNotFound, "not_found",
				"notify target not found or not owned by tenant")
			return
		}
		slog.ErrorContext(ctx, "notify-targets patch get", "err", err)
		logext.Errorf(ctx, "[%s] repo.GetByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to read notify target")
		return
	}

	// Apply present fields (proto optional → nil means "leave unchanged").
	// Empty-string secret is intentionally allowed ("clear the secret").
	if patch.Audience != nil {
		cur.Audience = patch.GetAudience()
	}
	if patch.Url != nil {
		cur.URL = patch.GetUrl()
	}
	if patch.Secret != nil {
		cur.Secret = patch.GetSecret()
	}
	if patch.TimeoutSeconds != nil {
		cur.TimeoutSeconds = int(patch.GetTimeoutSeconds())
	}
	if patch.Disabled != nil {
		cur.Disabled = patch.GetDisabled()
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
		respond.Error(ctx, w, http.StatusBadRequest, "validation", err.Error())
		return
	}

	if err := h.repo.UpdateByID(ctx, auth.TenantID, id, *cur); err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetConflict) {
			logext.Warnf(ctx, "[%s] reject: conflict,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			respond.Error(ctx, w, http.StatusConflict, "conflict",
				"audience conflicts with another target of the same destination_type — revert or delete the other one first")
			return
		}
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found pre-update,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "notify target was deleted before the update could apply")
			return
		}
		slog.ErrorContext(ctx, "notify-targets patch update", "err", err)
		logext.Errorf(ctx, "[%s] repo.UpdateByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to update notify target")
		return
	}

	respond.Proto(w, http.StatusOK, toNotifyProto(*cur))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
}
