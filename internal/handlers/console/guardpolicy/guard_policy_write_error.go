package guardpolicy

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	serviceguard "github.com/Phixsura/attune/internal/service/guardpolicy"
)

func guardPolicyWriteError(ctx *dispatcher.RequestContext[*session.AuthCtx], where, tenantID string, err error) (dispatcher.Result[*attunev1.GuardPolicy], error) {
	if code := serviceguard.ErrToCode(err); code != attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED {
		return dispatcher.Fail[*attunev1.GuardPolicy](http.StatusBadRequest, code, serviceguard.ErrToMessage(err))
	}
	if errors.Is(err, llmguard.ErrPolicyNotFound) {
		return dispatcher.Fail[*attunev1.GuardPolicy](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "guard policy not found or not owned by tenant")
	}
	logext.Errorf(ctx, "[%s] write failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
	return dispatcher.Fail[*attunev1.GuardPolicy](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write guard policy")
}
