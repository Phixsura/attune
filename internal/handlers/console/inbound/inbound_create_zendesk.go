// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	inboundcore "github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/zendesk"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

func (h *Handler) createZendesk(ctx context.Context, auth *session.AuthCtx, req *attunev1.CreateInboundSourceRequest, name, slug string) (dispatcher.Result[*attunev1.CreateInboundSourceResponse], error) {
	const where = "console.inbound.createZendesk"
	cfg := req.GetZendeskConfig()
	if cfg == nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "zendesk_config is required for channel=zendesk")
	}
	inputs, err := zendesk.ValidateConnConfig(
		cfg.GetSubdomain(),
		cfg.GetAuthMode(),
		cfg.GetEmail(),
		cfg.GetApiToken(),
		cfg.GetOauthAccessToken(),
		cfg.GetOauthRefreshToken(),
		cfg.GetOauthClientIdV2(),
		cfg.GetOauthClientSecretV2(),
	)
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}

	// Validate credentials by calling Zendesk's auth endpoint.
	authTest := h.zendeskAuthTest
	if authTest == nil {
		switch inputs.AuthMode {
		case zendesk.AuthModeAPIToken:
			authTest = func(ctx2 context.Context, _ zendesk.ConnInputs) (zendesk.AccountInfo, error) {
				return zendesk.AuthTestAPIToken(ctx2, inputs.Subdomain, inputs.Email, inputs.APIToken)
			}
		case zendesk.AuthModeOAuth:
			authTest = func(ctx2 context.Context, _ zendesk.ConnInputs) (zendesk.AccountInfo, error) {
				return zendesk.AuthTestOAuth(ctx2, inputs.Subdomain, inputs.OAuthAccessToken)
			}
		}
	}
	acct, err := authTest(ctx, inputs)
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, friendlyZendeskError(err, inputs.Subdomain))
	}

	startFrom := strings.TrimSpace(cfg.GetStartFrom())
	if startFrom != "" && startFrom != "now" && startFrom != "full" {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "start_from must be 'now' or 'full'")
	}

	id := uuid.NewString()
	withTx := h.zendeskWithTx
	if withTx == nil {
		withTx = secretlock.WithTx
	}
	if err := withTx(ctx, h.pool, true, func(ctx context.Context, tx secretlock.Tx) error {
		if err := secretlock.EnsureWritableKey(ctx, tx, inboundcore.PrimaryKeyID(h.secrets)); err != nil {
			return err
		}
		envelope, err := h.encryptZendeskConfig(inputs, cfg)
		if err != nil {
			return err
		}
		return h.insertRowTx(ctx, tx, id, auth.TenantID, channelZendesk, name, slug, envelope)
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
	if err := h.recordAudit(ctx, auth.UserType, auth.UserID, auth.TenantID, "inbound_source.create", stored.ID, "Created zendesk inbound source", nil, nil, map[string]any{
		"id":                 stored.ID,
		"channel":            stored.Channel,
		"name":               stored.Name,
		"slug":               stored.Slug,
		"enabled":            stored.Enabled,
		"zendesk_subdomain":  inputs.Subdomain,
		"zendesk_auth_mode":  inputs.AuthMode,
		"zendesk_account_id": acct.AccountID,
	}); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, stored.ID, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,slug:%s,subdomain:%s,auth_mode:%s",
		where, auth.TenantID, id, slug, inputs.Subdomain, inputs.AuthMode)
	return dispatcher.Created(resp)
}

func (h *Handler) encryptZendeskConfig(inputs zendesk.ConnInputs, cfg *attunev1.ZendeskConnConfig) ([]byte, error) {
	startFrom := strings.TrimSpace(cfg.GetStartFrom())
	if startFrom == "" {
		startFrom = "now"
	}
	inner := zendesk.Config{
		Version:           zendesk.ConfigVersion,
		AuthMode:          inputs.AuthMode,
		Subdomain:         inputs.Subdomain,
		StartFrom:         startFrom,
		MaxCommentFetches: int(cfg.GetMaxCommentFetches()),
		Filter: zendesk.TicketFilter{
			Tags:        cfg.GetFilterTags(),
			ExcludeTags: cfg.GetFilterExcludeTags(),
			Statuses:    cfg.GetFilterStatuses(),
		},
	}
	switch inputs.AuthMode {
	case zendesk.AuthModeAPIToken:
		inner.Email = inputs.Email
		enc, err := h.secrets.Encrypt([]byte(inputs.APIToken))
		if err != nil {
			return nil, fmt.Errorf("encrypt zendesk api_token: %w", err)
		}
		inner.APITokenEncrypted = enc
	case zendesk.AuthModeOAuth:
		tok := struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token,omitempty"`
			ClientID     string `json:"client_id,omitempty"`
			ClientSecret string `json:"client_secret,omitempty"`
		}{
			AccessToken:  inputs.OAuthAccessToken,
			RefreshToken: inputs.OAuthRefreshToken,
			ClientID:     inputs.OAuthClientID,
			ClientSecret: inputs.OAuthClientSecret,
		}
		raw, err := jsonMarshal(tok)
		if err != nil {
			return nil, fmt.Errorf("marshal zendesk oauth_token: %w", err)
		}
		enc, err := h.secrets.Encrypt(raw)
		if err != nil {
			return nil, fmt.Errorf("encrypt zendesk oauth_token: %w", err)
		}
		inner.OAuthTokenEncrypted = enc
	}
	raw, err := jsonMarshal(inner)
	if err != nil {
		return nil, fmt.Errorf("marshal zendesk config: %w", err)
	}
	return h.secrets.Encrypt(raw)
}
