package notifytarget

// PATCH /notify-targets/{id} for sparse edits (change URL / toggle disabled /
// rotate secret without delete+recreate). destination_type is intentionally
// not patchable — changing type = different row identity.

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

// Patch handles PATCH /fb/v1/console/notify-targets/{id}.
//
// Implementation: Get → merge → UpdateByID.
func (h *NotifyTargetsHandler) Patch(ctx *dispatcher.RequestContext[*session.AuthCtx], patch *attunev1.UpdateNotifyTargetRequest) (dispatcher.Result[*attunev1.NotifyTarget], error) {
	const where = "console.NotifyTargetsHandler.Patch"
	auth := ctx.Auth
	id, err := uuid.Parse(patch.GetId())
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad uuid,tenant_id:%s", where, auth.TenantID)
		return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id is not a UUID")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)

	cur, err := h.repo.GetByID(ctx, auth.TenantID, id)
	if err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND,
				"notify target not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] repo.GetByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read notify target")
	}
	before := ptrext.Indirect(cur)

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
	if err := validateNotifyCreate(ptrext.Of(createNotifyRequest{
		DestinationType: cur.DestinationType,
		Audience:        cur.Audience,
		URL:             cur.URL,
		Secret:          cur.Secret,
		TimeoutSeconds:  cur.TimeoutSeconds,
		Disabled:        cur.Disabled,
	})); err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,id:%s,err:%s",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}

	if err := h.repo.UpdateByID(ctx, auth.TenantID, id, ptrext.Indirect(cur)); err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetConflict) {
			logext.Warnf(ctx, "[%s] reject: conflict,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusConflict, attunev1.ErrorCode_CONFLICT,
				"audience conflicts with another target of the same destination_type — revert or delete the other one first")
		}
		if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found pre-update,tenant_id:%s,id:%s",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "notify target was deleted before the update could apply")
		}
		logext.Errorf(ctx, "[%s] repo.UpdateByID failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update notify target")
	}
	if err := h.recordAudit(
		ctx,
		auth.UserType,
		auth.UserID,
		auth.TenantID,
		"notify_target.update",
		ctx.Request(),
		before,
		notifyTargetSummary("Updated notify target", before),
		auditNotifyTargetSnapshot(before),
		auditNotifyTargetSnapshot(ptrext.Indirect(cur)),
	); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
	return dispatcher.OK(toNotifyProto(ptrext.Indirect(cur)))
}
