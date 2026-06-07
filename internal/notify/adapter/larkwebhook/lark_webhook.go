// Package larkwebhook pushes enriched feedback to a Lark / Feishu
// custom group-bot webhook (the per-chat "Custom Bot" the chat admin
// configures). One of three notify adapter implementations alongside
// rawwebhook and githubissue.
//
// Per-tenant routing arrives in a follow-up; today two Lark destinations
// (pool + radar) are configured statically via env and the tenantID
// threads through every Push call for future per-tenant lookup.
package larkwebhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/notify/sig"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

// LarkWebhook delivers Snapshot payloads to one or two Lark group bot
// webhook URLs. A zero-value (empty URL) destination is silently
// skipped; callers do not need to gate Push* on Enabled() — they can
// call freely.
type LarkWebhook struct {
	poolURL     string
	poolSecret  string
	radarURL    string
	radarSecret string
	transport   *notify.Transport
}

// NewLarkWebhook returns a notifier wired to the given destinations.
// Pass "" for any URL to disable that destination. Secrets are
// optional — set them only if the Lark bot has signature verification
// enabled in the chat settings.
//
// Retry policy is NoRetry: Lark group bots are at-most-once by
// convention (duplicate cards would spam the chat). 's raw
// webhook flips on DefaultRetry.
func NewLarkWebhook(poolURL, poolSecret, radarURL, radarSecret string) *LarkWebhook {
	return &LarkWebhook{
		poolURL:     poolURL,
		poolSecret:  poolSecret,
		radarURL:    radarURL,
		radarSecret: radarSecret,
		transport:   notify.NewTransport(nil, notify.NoRetry()),
	}
}

// PoolEnabled / RadarEnabled report whether each destination is wired.
func (l *LarkWebhook) PoolEnabled() bool  { return l != nil && l.poolURL != "" }
func (l *LarkWebhook) RadarEnabled() bool { return l != nil && l.radarURL != "" }

// PushPool delivers s to the "feedback pool" group (every enriched row).
// Returns nil if the pool destination is unconfigured.
func (l *LarkWebhook) PushPool(ctx context.Context, s domain.Snapshot) error {
	if !l.PoolEnabled() {
		return nil
	}
	return l.send(ctx, "lark-pool", l.poolURL, l.poolSecret, s)
}

// PushRadar delivers s to the "engineering radar" group (urgent rows
// only — callers should filter, but we don't reject non-urgent
// snapshots here to keep the notifier shape simple). Returns nil if
// radar is unconfigured.
func (l *LarkWebhook) PushRadar(ctx context.Context, s domain.Snapshot) error {
	if !l.RadarEnabled() {
		return nil
	}
	return l.send(ctx, "lark-radar", l.radarURL, l.radarSecret, s)
}

// send routes through Transport. The RequestBuilder regenerates the
// timestamp + signature on each attempt (irrelevant for NoRetry but
// keeps the contract right when the destination later opts into
// retries). The ResponseChecker decodes Lark's in-band error body —
// Lark returns HTTP 200 even on logical failure.
func (l *LarkWebhook) send(ctx context.Context, dest, url, secret string, s domain.Snapshot) error {
	const where = "notify.LarkWebhook.send"
	logext.Infof(ctx, "[%s] start,dest:%s,feedback_id:%d,urgent:%t,tenant_id:%s",
		where, dest, s.ID, s.IsUrgent, s.TenantID)
	build := func(ctx context.Context) (*http.Request, error) {
		body, err := l.buildBody(secret, s)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		// Upstream request body — secret / Authorization headers are
		// intentionally not logged. Body is truncated at 1024 bytes so
		// a large card body never blows the log buffer.
		logext.Infof(ctx, "[%s] upstream req,dest:%s,url:%s,body:%s",
			where, dest, url, truncate(string(body), 1024))
		return req, nil
	}
	err := l.transport.Send(ctx, dest, build, checkLarkResponse(dest, s))
	if err != nil {
		reason := "transport"
		if errors.Is(err, notify.ErrTerminal) {
			reason = "terminal"
		}
		metrics.NotifyFailuresTotal.WithLabelValues(dest, reason).Inc()
		logext.Errorf(ctx, "[%s] send failed,dest:%s,feedback_id:%d,reason:%s,err:%+v",
			where, dest, s.ID, reason, err.Error())
		return err
	}
	logext.Infof(ctx, "[%s] OK,dest:%s,feedback_id:%d", where, dest, s.ID)
	return nil
}

// buildBody serializes a Snapshot into Lark's interactive-card envelope,
// optionally adding the timestamp + sign fields if a secret is set.
func (l *LarkWebhook) buildBody(secret string, s domain.Snapshot) ([]byte, error) {
	envelope := map[string]any{
		"msg_type": "interactive",
		"card":     buildCard(s),
	}
	if secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig, err := sig.SignLarkBot(ts, secret)
		if err != nil {
			return nil, fmt.Errorf("sign lark bot: %w", err)
		}
		envelope["timestamp"] = ts
		envelope["sign"] = sig
	}
	return json.Marshal(envelope)
}

// checkLarkResponse is the ResponseChecker for Lark group webhooks.
// Lark always responds 200 even on logical failure, embedding the real
// status in {"code":N,"msg":"..."}. Non-zero code is treated as
// terminal — retrying a malformed card or a revoked bot URL won't help.
func checkLarkResponse(dest string, s domain.Snapshot) notify.ResponseChecker {
	const where = "notify.checkLarkResponse"
	return func(ctx context.Context, status int, body []byte) error {
		var out struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		_ = json.Unmarshal(body, &out)
		// Upstream response log — body truncated at 1024 bytes so a
		// runaway response body never blows the log buffer.
		logext.Infof(ctx,
			"[%s] upstream resp,dest:%s,feedback_id:%d,status:%d,code:%d,msg:%s,body:%s",
			where, dest, s.ID, status, out.Code, out.Msg, truncate(string(body), 1024))
		if status != http.StatusOK || out.Code != 0 {
			return fmt.Errorf("%w: http=%d code=%d msg=%s body=%s",
				notify.ErrTerminal, status, out.Code, out.Msg, truncate(string(body), 200))
		}
		slog.InfoContext(ctx, "lark webhook pushed",
			"dest", dest, "feedback_id", s.ID, "urgent", s.IsUrgent)
		return nil
	}
}

// signLarkBot moved to `internal/notify/sig.SignLarkBot` — see that
// package for the algorithm reference (open.feishu.cn/document/.../bot-v2).

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
