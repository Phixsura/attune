// SPDX-License-Identifier: Apache-2.0

// Package email delivers notifications as plain-text emails via SMTP.
//
// Unlike HTTP-based adapters (Slack, Discord, etc.), email uses SMTP for
// delivery. The adapter implements outbound.DirectEventSender and
// outbound.DirectDigestSender so the outbox/digest workers bypass the
// HTTP transport and call SendEvent/SendDigest directly.
//
// Target.URL carries the SMTP endpoint as smtp://host:port or
// smtps://host:port (implicit TLS). Target.Secret carries the
// password; Target.Config holds from/to/username fields.
package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strings"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/render"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "email"

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

// RenderEvent builds a stub Rendered — the real send goes through SendEvent.
// This satisfies EventChannel so the outbox worker's LookupEvent succeeds;
// the worker then checks DirectEventSender before using the HTTP path.
func (c *channel) RenderEvent(_ *outbound.Envelope, _ outbound.Target) (outbound.Rendered, error) {
	return outbound.Rendered{
		Build: func(_ context.Context) (*http.Request, error) { return nil, nil },
		Check: func(_ context.Context, _ int, _ []byte) error { return nil },
	}, nil
}

// RenderDigest — stub; real delivery is via SendDigest.
func (c *channel) RenderDigest(_ any, _ outbound.Target) (outbound.Rendered, error) {
	return outbound.Rendered{
		Build: func(_ context.Context) (*http.Request, error) { return nil, nil },
		Check: func(_ context.Context, _ int, _ []byte) error { return nil },
	}, nil
}

// SendEvent delivers a per-event notification email via SMTP.
func (c *channel) SendEvent(ctx context.Context, env *outbound.Envelope, dst outbound.Target) error {
	subject, body := renderEventEmail(env)
	return c.send(ctx, dst, subject, body)
}

// SendDigest delivers a digest summary email via SMTP.
func (c *channel) SendDigest(ctx context.Context, view any, dst outbound.Target) error {
	subject, body := renderDigestEmail(view)
	return c.send(ctx, dst, subject, body)
}

func (c *channel) send(ctx context.Context, dst outbound.Target, subject, body string) error {
	cfg, err := parseConfig(dst)
	if err != nil {
		return fmt.Errorf("%w: %w", outbound.ErrTerminal, err)
	}

	guardURL := "smtp://" + net.JoinHostPort(cfg.host, cfg.port)
	smtpPolicy := nethardening.Policy{AllowLoopback: true, AllowPrivate: true}
	if err := smtpPolicy.ValidateURL(guardURL); err != nil {
		return fmt.Errorf("%w: smtp host blocked: %w", outbound.ErrTerminal, err)
	}

	msg := buildRFC822(cfg.from, cfg.to, subject, body)
	addr := net.JoinHostPort(cfg.host, cfg.port)

	logext.Infof(ctx, "[outbound.email] sending,to:%s,host:%s,subject:%s",
		cfg.to, cfg.host, render.Truncate(subject, 60))

	if cfg.implicitTLS {
		return sendImplicitTLS(ctx, addr, cfg, msg)
	}
	return sendSTARTTLS(ctx, addr, cfg, msg)
}

func sendSTARTTLS(_ context.Context, addr string, cfg smtpConfig, msg []byte) error {
	cl, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer cl.Close()

	if err := cl.StartTLS(ptrext.Of(tls.Config{ServerName: cfg.host})); err != nil { //nolint:gosec // ServerName is set
		return fmt.Errorf("smtp starttls: %w", err)
	}

	return smtpSendSequence(cl, cfg, msg)
}

func sendImplicitTLS(ctx context.Context, addr string, cfg smtpConfig, msg []byte) error {
	dialer := ptrext.Of(tls.Dialer{Config: ptrext.Of(tls.Config{ServerName: cfg.host})}) //nolint:gosec // ServerName is set
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp tls dial %s: %w", addr, err)
	}

	cl, err := smtp.NewClient(conn, cfg.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer cl.Close()

	return smtpSendSequence(cl, cfg, msg)
}

func smtpSendSequence(cl *smtp.Client, cfg smtpConfig, msg []byte) error {
	if cfg.username != "" {
		auth := smtp.PlainAuth("", cfg.username, cfg.password, cfg.host)
		if err := cl.Auth(auth); err != nil {
			return fmt.Errorf("%w: smtp auth: %w", outbound.ErrTerminal, err)
		}
	}

	if err := cl.Mail(cfg.from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := cl.Rcpt(cfg.to); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}

	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data close: %w", err)
	}

	return cl.Quit()
}

type smtpConfig struct {
	host        string
	port        string
	implicitTLS bool
	username    string
	password    string
	from        string
	to          string
}

func parseConfig(dst outbound.Target) (smtpConfig, error) {
	from := render.MapStr(dst.Config, "from")
	if from == "" {
		return smtpConfig{}, fmt.Errorf("email config: from address is required")
	}
	to := render.MapStr(dst.Config, "to")
	if to == "" {
		to = dst.URL
	}
	if to == "" {
		return smtpConfig{}, fmt.Errorf("email config: to address is required")
	}

	host := render.MapStr(dst.Config, "smtp_host")
	port := render.MapStr(dst.Config, "smtp_port")
	if host == "" {
		return smtpConfig{}, fmt.Errorf("email config: smtp_host is required")
	}
	if port == "" {
		port = "587"
	}

	implicitTLS := false
	if v, ok := dst.Config["smtp_implicit_tls"].(bool); ok {
		implicitTLS = v
	}

	username := render.MapStr(dst.Config, "smtp_username")
	password := dst.Secret

	return smtpConfig{
		host:        host,
		port:        port,
		implicitTLS: implicitTLS,
		username:    username,
		password:    password,
		from:        from,
		to:          to,
	}, nil
}

func buildRFC822(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("X-Mailer: attune/1.0\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}
