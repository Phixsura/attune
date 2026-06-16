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
// Email-only IMAP probe: dial + login + select(folder) + logout.
// Response: TestInboundConnectionResponse{ok=true} or {ok=false,error=...}
// for a decoded request. Malformed JSON is rejected by dispatcher before this
// handler runs.
func (h *Handler) TestConnection(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.TestInboundConnectionRequest) (dispatcher.Result[*attunev1.TestInboundConnectionResponse], error) {
	const where = "console.inbound.TestConnection"
	auth := ctx.Auth
	if strings.TrimSpace(strings.ToLower(req.GetChannel())) != channelEmail {
		return dispatcher.OK(ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of("test-connection only supports the email channel"),
		}))
	}
	cfg := req.GetEmailConfig()
	if cfg == nil {
		return dispatcher.OK(ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of("email_config is required"),
		}))
	}
	inputs, err := validateEmailConnConfig(cfg)
	if err != nil {
		return dispatcher.OK(ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of(err.Error()),
		}))
	}
	probeCtx, cancel := context.WithTimeout(ctx, testConnTimeout)
	defer cancel()
	start := time.Now()
	if err := h.testConn(probeCtx, inputs); err != nil {
		if auditErr := h.recordAudit(
			ctx,
			auth.UserType,
			auth.UserID,
			auth.TenantID,
			"inbound_source.test_connection",
			inputs.Host,
			"Tested inbound email connection",
			ctx.Request(),
			nil,
			map[string]any{
				"channel":    channelEmail,
				"host":       inputs.Host,
				"port":       inputs.Port,
				"tls":        inputs.TLS,
				"folder":     inputs.Folder,
				"ok":         false,
				"latency_ms": time.Since(start).Milliseconds(),
				"error":      err.Error(),
			},
		); auditErr != nil {
			logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,host:%s,err:%+v",
				where, auth.TenantID, inputs.Host, auditErr.Error())
			return dispatcher.Fail[*attunev1.TestInboundConnectionResponse](
				http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log",
			)
		}
		logext.Warnf(ctx, "[%s] probe failed,tenant_id:%s,host:%s,err:%s",
			where, auth.TenantID, inputs.Host, err.Error())
		return dispatcher.OK(ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of(err.Error()),
		}))
	}
	latency := time.Since(start).Milliseconds()
	resp := ptrext.Of(attunev1.TestInboundConnectionResponse{
		Ok:        true,
		LatencyMs: ptrext.Of(latency),
	})
	if err := h.recordAudit(
		ctx,
		auth.UserType,
		auth.UserID,
		auth.TenantID,
		"inbound_source.test_connection",
		inputs.Host,
		"Tested inbound email connection",
		ctx.Request(),
		nil,
		map[string]any{
			"channel":    channelEmail,
			"host":       inputs.Host,
			"port":       inputs.Port,
			"tls":        inputs.TLS,
			"folder":     inputs.Folder,
			"ok":         true,
			"latency_ms": latency,
		},
	); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,host:%s,err:%+v",
			where, auth.TenantID, inputs.Host, err.Error())
		return dispatcher.Fail[*attunev1.TestInboundConnectionResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log",
		)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,host:%s,latency_ms:%d",
		where, auth.TenantID, inputs.Host, latency)
	return dispatcher.OK(resp)
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
