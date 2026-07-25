// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound/adapter/email"
	"github.com/Phixsura/attune/internal/inbound/adapter/intercom"
	"github.com/Phixsura/attune/internal/inbound/adapter/slack"
	"github.com/Phixsura/attune/internal/inbound/adapter/zendesk"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

const testConnTimeout = 8 * time.Second

// testConnFn is the seam used by TestConnection for the IMAP probe.
// Production uses imapDialAndProbe; tests inject a fake that returns
// nil/err without opening a TCP socket.
type testConnFn func(ctx context.Context, cfg testConnInputs) error

// TestConnection handles POST /fb/v1/console/inbound/sources/test-connection.
// Email probes dial + login + select(folder) + logout. Slack probes auth.test
// and, when a channel is provided, verifies that the selected channel is
// actually readable from the token.
// Response: TestInboundConnectionResponse{ok=true} or {ok=false,error=...}
// for a decoded request. Malformed JSON is rejected by dispatcher before this
// handler runs.
func (h *Handler) TestConnection(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.TestInboundConnectionRequest) (dispatcher.Result[*attunev1.TestInboundConnectionResponse], error) {
	const where = "console.inbound.TestConnection"
	auth := ctx.Auth
	channel := strings.TrimSpace(strings.ToLower(req.GetChannel()))
	if channel != channelEmail && channel != channelSlack && channel != channelZendesk && channel != channelIntercom {
		return dispatcher.OK(ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of("test-connection only supports the email, slack, zendesk, or intercom channel"),
		}))
	}
	probeCtx, cancel := context.WithTimeout(ctx, testConnTimeout)
	defer cancel()
	start := time.Now()
	targetID, auditTitle, auditFields, err := h.resolveTestConnection(probeCtx, req, channel)
	if auditFields == nil {
		return dispatcher.OK(ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of(err.Error()),
		}))
	}

	latency := time.Since(start).Milliseconds()
	auditFields["ok"] = err == nil
	auditFields["latency_ms"] = latency
	if err != nil {
		auditFields["error"] = err.Error()
	}
	if auditErr := h.recordAudit(
		ctx,
		auth.UserType,
		auth.UserID,
		auth.TenantID,
		"inbound_source.test_connection",
		targetID,
		auditTitle,
		ctx.Request(),
		nil,
		auditFields,
	); auditErr != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,target_id:%s,err:%+v",
			where, auth.TenantID, targetID, auditErr.Error())
		return dispatcher.Fail[*attunev1.TestInboundConnectionResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log",
		)
	}
	if err != nil {
		logext.Warnf(ctx, "[%s] probe failed,tenant_id:%s,target_id:%s,err:%s",
			where, auth.TenantID, targetID, err.Error())
		return dispatcher.OK(ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of(err.Error()),
		}))
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,target_id:%s,latency_ms:%d",
		where, auth.TenantID, targetID, latency)
	return dispatcher.OK(ptrext.Of(attunev1.TestInboundConnectionResponse{
		Ok:        true,
		LatencyMs: ptrext.Of(latency),
	}))
}

func (h *Handler) resolveTestConnection(ctx context.Context, req *attunev1.TestInboundConnectionRequest, channel string) (string, string, map[string]any, error) {
	switch channel {
	case channelEmail:
		return h.testEmailConnection(ctx, req.GetEmailConfig())
	case channelSlack:
		return h.testSlackConnection(ctx, req.GetSlackConfig())
	case channelZendesk:
		return h.testZendeskConnection(ctx, req.GetZendeskConfig())
	case channelIntercom:
		return h.testIntercomConnection(ctx, req.GetIntercomConfig())
	default:
		return "", "", nil, fmt.Errorf("unsupported channel %q", channel)
	}
}

func (h *Handler) testEmailConnection(ctx context.Context, cfg *attunev1.EmailConnConfig) (string, string, map[string]any, error) {
	if cfg == nil {
		return "", "", nil, errors.New("email_config is required")
	}
	inputs, validateErr := validateEmailConnConfig(cfg)
	if validateErr != nil {
		return "", "", nil, validateErr
	}
	auditFields := map[string]any{
		"channel": channelEmail,
		"host":    inputs.Host,
		"port":    inputs.Port,
		"tls":     inputs.TLS,
		"folder":  inputs.Folder,
	}
	return inputs.Host, "Tested inbound email connection", auditFields, h.testConn(ctx, inputs)
}

func (h *Handler) testSlackConnection(ctx context.Context, cfg *attunev1.SlackConnConfig) (string, string, map[string]any, error) {
	if cfg == nil {
		return "", "", nil, errors.New("slack_config is required")
	}
	inputs, validateErr := slack.ValidateConnConfig(cfg, false)
	if validateErr != nil {
		return "", "", nil, validateErr
	}
	auditFields := map[string]any{
		"channel":     channelSlack,
		"channel_id":  inputs.ChannelID,
		"has_channel": inputs.ChannelID != "",
	}
	if inputs.ChannelID == "" {
		targetID, err := h.testSlackAuth(ctx, inputs, auditFields)
		return targetID, "Tested inbound slack connection", auditFields, err
	}
	targetID, err := h.testSlackChannel(ctx, inputs, auditFields)
	return targetID, "Tested inbound slack connection", auditFields, err
}

func (h *Handler) testSlackAuth(ctx context.Context, inputs slack.ConnInputs, auditFields map[string]any) (string, error) {
	authTest := h.slackAuthTest
	if authTest == nil {
		authTest = slack.AuthTest
	}
	authInfo, err := authTest(ctx, inputs.BotToken)
	if err != nil {
		return "slack-auth", err
	}
	auditFields["slack_team_id"] = authInfo.TeamID
	auditFields["slack_team_name"] = authInfo.TeamName
	auditFields["workspace_url"] = authInfo.WorkspaceURL
	return "slack-auth", nil
}

func (h *Handler) testSlackChannel(ctx context.Context, inputs slack.ConnInputs, auditFields map[string]any) (string, error) {
	validateChannel := h.slackValidateChannel
	if validateChannel == nil {
		validateChannel = slack.ValidateChannel
	}
	authInfo, channelInfo, err := validateChannel(ctx, inputs.BotToken, inputs.ChannelID)
	if err != nil {
		return inputs.ChannelID, err
	}
	auditFields["slack_team_id"] = authInfo.TeamID
	auditFields["slack_team_name"] = authInfo.TeamName
	auditFields["workspace_url"] = authInfo.WorkspaceURL
	auditFields["slack_channel_id"] = channelInfo.ID
	auditFields["slack_channel_name"] = channelInfo.Name
	return inputs.ChannelID, nil
}

func (h *Handler) testZendeskConnection(ctx context.Context, cfg *attunev1.ZendeskConnConfig) (string, string, map[string]any, error) {
	if cfg == nil {
		return "", "", nil, errors.New("zendesk_config is required")
	}
	inputs, validateErr := zendesk.ValidateConnConfig(
		cfg.GetSubdomain(),
		cfg.GetAuthMode(),
		cfg.GetEmail(),
		cfg.GetApiToken(),
		cfg.GetOauthAccessToken(),
		cfg.GetOauthRefreshToken(),
		cfg.GetOauthClientIdV2(),
		cfg.GetOauthClientSecretV2(),
	)
	if validateErr != nil {
		return "", "", nil, validateErr
	}
	auditFields := map[string]any{
		"channel":   channelZendesk,
		"subdomain": inputs.Subdomain,
		"auth_mode": inputs.AuthMode,
	}
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
		return inputs.Subdomain, "Tested inbound zendesk connection", auditFields, errors.New(friendlyZendeskError(err, inputs.Subdomain))
	}
	auditFields["zendesk_account_id"] = acct.AccountID
	return inputs.Subdomain, "Tested inbound zendesk connection", auditFields, nil
}

func (h *Handler) testIntercomConnection(ctx context.Context, cfg *attunev1.IntercomConnConfig) (string, string, map[string]any, error) {
	if cfg == nil {
		return "", "", nil, errors.New("intercom_config is required")
	}
	inputs, validateErr := intercom.ValidateConnConfig(
		cfg.GetRegion(),
		cfg.GetAccessToken(),
		cfg.GetStartFrom(),
		cfg.GetFilterStates(),
		int(cfg.GetMaxDetailFetches()),
	)
	if validateErr != nil {
		return "", "", nil, validateErr
	}
	auditFields := map[string]any{
		"channel": channelIntercom,
		"region":  inputs.Region,
	}
	authTest := h.intercomAuthTest
	if authTest == nil {
		authTest = intercom.AuthTest
	}
	acct, err := authTest(ctx, inputs.Region, inputs.AccessToken)
	if err != nil {
		return "intercom-auth", "Tested inbound intercom connection", auditFields, errors.New(friendlyIntercomError(err))
	}
	auditFields["intercom_workspace_id"] = acct.WorkspaceID
	auditFields["intercom_workspace_name"] = acct.WorkspaceName
	return acct.WorkspaceID, "Tested inbound intercom connection", auditFields, nil
}

// testConnInputs — narrower variant of EmailCreateConfig used only by
// the test-connection probe. Keeps password in plaintext since the IMAP
// client needs it for the LOGIN command and the value never lands in
// the DB.
type testConnInputs struct {
	Host     string
	Port     int
	TLS      bool
	Username string
	Password string
	Folder   string
}

// imapDialAndProbe — production IMAP probe. TLS-only (review H2, #66):
// the cfg.TLS=false escape hatch is gone here just as it is in the
// inbound/adapter/email runtime — loopback reverse proxies that
// terminate TLS can front a plain-IMAP server if the operator truly
// needs one. After dial, runs LOGIN / SELECT / LOGOUT. Returns any
// error verbatim — the handler wraps it into the proto response.
func imapDialAndProbe(_ context.Context, cfg testConnInputs) error {
	if err := email.ValidateOutboundHost(cfg.Host); err != nil {
		// SSRF guard (#66 review M-3) — surfaces the validate-shape
		// error verbatim. The handler wraps it into the proto response;
		// operators see the blocked-host detail.
		return err
	}
	addr := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	opt := ptrext.Of(imapclient.Options{})
	cli, err := imapclient.DialTLS(addr, opt)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = cli.Logout() }()
	if err := cli.Login(cfg.Username, cfg.Password).Wait(); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	folder := cfg.Folder
	if folder == "" {
		folder = "INBOX"
	}
	if _, err := cli.Select(folder, ptrext.Of(imap.SelectOptions{})).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", folder, err)
	}
	return nil
}

// validateEmailConnConfig — narrower variant for the test-connection
// probe. Returns a normalised testConnInputs on success. Like the
// create-source path it requires tls=true (review H2, #66).
func validateEmailConnConfig(cfg *attunev1.EmailConnConfig) (testConnInputs, error) {
	if !cfg.GetTls() {
		return testConnInputs{}, errors.New("tls must be true (plain IMAP is disallowed)")
	}
	out := testConnInputs{
		Host:     strings.TrimSpace(cfg.GetHost()),
		Port:     int(cfg.GetPort()),
		TLS:      true,
		Username: strings.TrimSpace(cfg.GetUsername()),
		Password: cfg.GetPassword(),
		Folder:   strings.TrimSpace(cfg.GetFolder()),
	}
	if out.Host == "" {
		return out, errors.New("email_config.host must not be empty")
	}
	if out.Port < 1 || out.Port > 65535 {
		return out, errors.New("email_config.port must be 1..65535")
	}
	if out.Username == "" {
		return out, errors.New("email_config.username must not be empty")
	}
	if out.Password == "" {
		return out, errors.New("email_config.password must not be empty")
	}
	return out, nil
}
