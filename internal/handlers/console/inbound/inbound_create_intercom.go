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
	"github.com/Phixsura/attune/internal/inbound/adapter/intercom"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

func (h *Handler) createIntercom(ctx context.Context, auth *session.AuthCtx, req *attunev1.CreateInboundSourceRequest, name, slug string) (dispatcher.Result[*attunev1.CreateInboundSourceResponse], error) {
	const where = "console.inbound.createIntercom"
	cfg := req.GetIntercomConfig()
	if cfg == nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "intercom_config is required for channel=intercom")
	}
	inputs, err := intercom.ValidateConnConfig(
		cfg.GetRegion(),
		cfg.GetAccessToken(),
		cfg.GetStartFrom(),
		cfg.GetFilterStates(),
		cfg.GetFilterTags(),
		cfg.GetFilterExcludeTags(),
		int(cfg.GetMaxDetailFetches()),
	)
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}

	// Validate credentials against Intercom's /me endpoint; the returned
	// workspace id_code is persisted for permalinks + idempotency keys.
	authTest := h.intercomAuthTest
	if authTest == nil {
		authTest = intercom.AuthTest
	}
	acct, err := authTest(ctx, inputs.Region, inputs.AccessToken)
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, friendlyIntercomError(err))
	}

	id := uuid.NewString()
	withTx := h.intercomWithTx
	if withTx == nil {
		withTx = secretlock.WithTx
	}
	if err := withTx(ctx, h.pool, true, func(ctx context.Context, tx secretlock.Tx) error {
		if err := secretlock.EnsureWritableKey(ctx, tx, inboundcore.PrimaryKeyID(h.secrets)); err != nil {
			return err
		}
		envelope, err := h.encryptIntercomConfig(inputs, acct.WorkspaceID)
		if err != nil {
			return err
		}
		return h.insertRowTx(ctx, tx, id, auth.TenantID, channelIntercom, name, slug, envelope)
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
	if err := h.recordAudit(ctx, auth.UserType, auth.UserID, auth.TenantID, "inbound_source.create", stored.ID, "Created intercom inbound source", nil, nil, map[string]any{
		"id":                      stored.ID,
		"channel":                 stored.Channel,
		"name":                    stored.Name,
		"slug":                    stored.Slug,
		"enabled":                 stored.Enabled,
		"intercom_region":         inputs.Region,
		"intercom_workspace_id":   acct.WorkspaceID,
		"intercom_workspace_name": acct.WorkspaceName,
	}); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, stored.ID, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,slug:%s,region:%s,workspace_id:%s",
		where, auth.TenantID, id, slug, inputs.Region, acct.WorkspaceID)
	return dispatcher.Created(resp)
}

func (h *Handler) encryptIntercomConfig(inputs intercom.ConnInputs, workspaceID string) ([]byte, error) {
	tokenEnc, err := h.secrets.Encrypt([]byte(inputs.AccessToken))
	if err != nil {
		return nil, fmt.Errorf("encrypt intercom access_token: %w", err)
	}
	inner := intercom.Config{
		Version:              intercom.ConfigVersion,
		Region:               inputs.Region,
		AccessTokenEncrypted: tokenEnc,
		WorkspaceID:          workspaceID,
		StartFrom:            inputs.StartFrom,
		FilterStates:         inputs.FilterStates,
		FilterTags:           inputs.FilterTags,
		FilterExcludeTags:    inputs.FilterExcludeTags,
		MaxDetailFetches:     inputs.MaxDetailFetches,
	}
	raw, err := jsonMarshal(inner)
	if err != nil {
		return nil, fmt.Errorf("marshal intercom config: %w", err)
	}
	return h.secrets.Encrypt(raw)
}

// friendlyIntercomError maps Intercom API failures to operator-facing
// messages. Never echoes the token.
func friendlyIntercomError(err error) string {
	// Status-code mapping first: a 401 with an empty error body carries
	// no "unauthorized" substring, and a 403 code like "forbidden" has
	// no dedicated case.
	if status, code, ok := intercom.APIErrorStatus(err); ok {
		switch status {
		case http.StatusUnauthorized:
			return "Intercom rejected the access token. Check the token in your Developer Hub app (Configure > Authentication)."
		case http.StatusForbidden:
			if code == "api_plan_restricted" {
				return "This Intercom workspace's plan does not allow API access."
			}
			return "Intercom denied access for this token. Check the app's permissions in your Developer Hub."
		}
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unauthorized") || strings.Contains(msg, "token_revoked") || strings.Contains(msg, "token_expired"):
		return "Intercom rejected the access token. Check the token in your Developer Hub app (Configure > Authentication)."
	case strings.Contains(msg, "api_plan_restricted"):
		return "This Intercom workspace's plan does not allow API access."
	case strings.Contains(msg, "rate limited"):
		return "Intercom rate limit reached. Wait a minute and try again."
	case strings.Contains(msg, "no such host") || strings.Contains(msg, "dial tcp") || strings.Contains(msg, "context deadline"):
		return "Could not reach the Intercom API. Check the region and your network egress rules."
	default:
		// Never echo raw upstream body content (the API error code path
		// truncates arbitrary response bodies into the message).
		return "Intercom connection failed. Check the region, token, and network egress rules."
	}
}
