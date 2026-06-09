package notifytarget

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// Create handles POST /fb/v1/console/notify-targets.
func (h *NotifyTargetsHandler) Create(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateNotifyTargetRequest) (dispatcher.Result[*attunev1.NotifyTarget], error) {
	const where = "console.NotifyTargetsHandler.Create"
	auth := ctx.Auth
	nreq := ptrext.Of(createNotifyRequest{
		DestinationType: req.GetDestinationType(),
		Audience:        req.GetAudience(),
		URL:             req.GetUrl(),
		Secret:          req.GetSecret(),
		TimeoutSeconds:  int(req.GetTimeoutSeconds()),
		Disabled:        req.GetDisabled(),
	})
	if err := validateNotifyCreate(nreq); err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	// Today only ships lark-bot + raw-webhook adapters.
	if nreq.DestinationType == notifytarget.DestSlackBot || nreq.DestinationType == notifytarget.DestEmail {
		logext.Warnf(ctx, "[%s] reject: not implemented,tenant_id:%s,dest:%s",
			where, auth.TenantID, nreq.DestinationType)
		return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusNotImplemented, attunev1.ErrorCode_NOT_IMPLEMENTED,
			"destination_type "+nreq.DestinationType+" is not implemented yet")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,dest:%s,audience:%s",
		where, auth.TenantID, nreq.DestinationType, nreq.Audience)

	target := notifytarget.NotifyTarget{
		TenantID:        auth.TenantID,
		DestinationType: nreq.DestinationType,
		Audience:        nreq.Audience,
		URL:             nreq.URL,
		Secret:          nreq.Secret,
		TimeoutSeconds:  nreq.TimeoutSeconds,
		Disabled:        nreq.Disabled,
	}
	id, createdAt, err := h.repo.Insert(ctx, target)
	if err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetConflict) {
			logext.Warnf(ctx, "[%s] reject: conflict,tenant_id:%s,dest:%s,audience:%s",
				where, auth.TenantID, nreq.DestinationType, nreq.Audience)
			return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusConflict, attunev1.ErrorCode_CONFLICT,
				"a target already exists for this (destination_type, audience) combination — delete the old one first")
		}
		logext.Errorf(ctx, "[%s] repo.Insert failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create notify target")
	}
	target.ID = id
	target.CreatedAt = createdAt
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,dest:%s",
		where, auth.TenantID, id, nreq.DestinationType)
	return dispatcher.Created(toNotifyProto(target))
}
