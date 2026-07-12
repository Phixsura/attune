// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	inboundcore "github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/slack"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

func (h *Handler) createSlack(ctx context.Context, auth *session.AuthCtx, req *attunev1.CreateInboundSourceRequest, name, slug string) (dispatcher.Result[*attunev1.CreateInboundSourceResponse], error) {
	const where = "console.inbound.createSlack"
	cfg := req.GetSlackConfig()
	if cfg == nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "slack_config is required for channel=slack")
	}
	inputs, err := slack.ValidateConnConfig(cfg, true)
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	validateChannel := h.slackValidateChannel
	if validateChannel == nil {
		validateChannel = slack.ValidateChannel
	}
	authInfo, channel, err := validateChannel(ctx, inputs.BotToken, inputs.ChannelID)
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}

	id := uuid.NewString()
	if err := secretlock.WithTx(ctx, h.pool, true, func(ctx context.Context, tx secretlock.Tx) error {
		if err := secretlock.EnsureWritableKey(ctx, tx, inboundcore.PrimaryKeyID(h.secrets)); err != nil {
			return err
		}
		envelope, err := h.encryptSlackConfig(inputs.BotToken, authInfo, channel)
		if err != nil {
			return err
		}
		return h.insertRowTx(ctx, tx, id, auth.TenantID, channelSlack, name, slug, envelope)
	}); err != nil {
		return dispatcher.Result[*attunev1.CreateInboundSourceResponse]{}, h.insertErr(ctx, where, auth.TenantID, err)
	}

	stored, err := h.sources.Get(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] post-insert reload failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "row created but reload failed")
	}
	resp := ptrext.Of(attunev1.CreateInboundSourceResponse{Source: rowToProto(stored)})
	if err := h.recordAudit(ctx, auth.UserType, auth.UserID, auth.TenantID, "inbound_source.create", stored.ID, "Created slack inbound source", nil, nil, map[string]any{
		"id":                 stored.ID,
		"channel":            stored.Channel,
		"name":               stored.Name,
		"slug":               stored.Slug,
		"enabled":            stored.Enabled,
		"slack_team_id":      authInfo.TeamID,
		"slack_team_name":    authInfo.TeamName,
		"slack_channel_id":   channel.ID,
		"slack_channel_name": channel.Name,
	}); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, stored.ID, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,slug:%s,slack_team_id:%s,slack_channel_id:%s",
		where, auth.TenantID, id, slug, authInfo.TeamID, channel.ID)
	return dispatcher.Created(resp)
}

func (h *Handler) encryptSlackConfig(token string, authInfo slack.AuthInfo, channel slack.Channel) ([]byte, error) {
	encToken, err := h.secrets.Encrypt([]byte(token))
	if err != nil {
		return nil, fmt.Errorf("encrypt slack token: %w", err)
	}
	inner := slack.Config{
		Version:        slack.ConfigVersion,
		TokenEncrypted: encToken,
		TeamID:         authInfo.TeamID,
		TeamName:       authInfo.TeamName,
		WorkspaceURL:   authInfo.WorkspaceURL,
		ChannelID:      channel.ID,
		ChannelName:    channel.Name,
	}
	raw, err := json.Marshal(inner)
	if err != nil {
		return nil, fmt.Errorf("marshal slack config: %w", err)
	}
	return h.secrets.Encrypt(raw)
}
