// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound/adapter/slack"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// DiscoverSlackChannels handles POST /fb/v1/console/inbound/sources/slack/discover.
// It validates the token and returns the readable channel list without
// persisting anything.
func (h *Handler) DiscoverSlackChannels(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DiscoverSlackChannelsRequest) (dispatcher.Result[*attunev1.DiscoverSlackChannelsResponse], error) {
	const where = "console.inbound.DiscoverSlackChannels"
	cfg := req.GetSlackConfig()
	if cfg == nil {
		return dispatcher.Fail[*attunev1.DiscoverSlackChannelsResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "slack_config is required")
	}
	inputs, err := slack.ValidateConnConfig(cfg, false)
	if err != nil {
		return dispatcher.Fail[*attunev1.DiscoverSlackChannelsResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	discover := h.slackDiscover
	if discover == nil {
		discover = slack.Discover
	}
	authInfo, channels, err := discover(ctx, inputs.BotToken)
	if err != nil {
		status := http.StatusBadRequest
		code := attunev1.ErrorCode_VALIDATION
		if slack.IsPermanentError(err) {
			status = http.StatusUnauthorized
			code = attunev1.ErrorCode_UNAUTHORIZED
		}
		logext.Warnf(ctx, "[%s] discover failed,tenant_id:%s,err:%s", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.DiscoverSlackChannelsResponse](status, code, err.Error())
	}
	resp := make([]*attunev1.SlackChannel, 0, len(channels))
	for _, ch := range channels {
		resp = append(resp, ptrext.Of(attunev1.SlackChannel{
			Id:         ch.ID,
			Name:       ch.Name,
			IsPrivate:  ch.IsPrivate,
			IsArchived: ch.IsArchived,
			IsShared:   ch.IsShared,
		}))
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,slack_team_id:%s,channels:%d", where, ctx.Auth.TenantID, authInfo.TeamID, len(resp))
	return dispatcher.OK(ptrext.Of(attunev1.DiscoverSlackChannelsResponse{Channels: resp}))
}
