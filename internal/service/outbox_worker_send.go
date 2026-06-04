package service

// outbox_worker_send.go — per-destination sender switchboard. Split from
// outbox_worker.go to keep both files under the listen ≤300-line rule
// (CLAUDE.md 律 2). Each outbox-routed destination_type owns one branch
// of sendByDestType plus, if its protocol differs from raw-webhook, a
// dedicated send* helper here.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Phixsura/listen/internal/logext"
	"github.com/Phixsura/listen/internal/notify"
	"github.com/Phixsura/listen/internal/repo"
)

// sendByDestType is the per-destination switchboard. The outbox row's
// payload is the same generic listen-envelope for every dest; each
// branch shapes the actual HTTP request (URL, headers, body transform).
// Adding a new dest_type means adding one case here and one sender file
// in internal/notify/.
func (w *OutboxWorker) sendByDestType(
	ctx context.Context,
	row repo.OutboxRow,
	target *repo.NotifyTarget,
) error {
	switch row.DestinationType {
	case repo.DestRawWebhook:
		return w.sendRawWebhook(ctx, row, target)
	case repo.DestGitHubIssue:
		return notify.SendGitHubIssue(ctx, w.transport, target.URL, target.Secret, row.Payload)
	default:
		// Should be unreachable because processRow filters first, but
		// keep the explicit branch so a config drift gets logged loud.
		return fmt.Errorf("%w: outbox worker has no sender for dest_type %q",
			notify.ErrTerminal, row.DestinationType)
	}
}

// sendRawWebhook is the original raw-webhook delivery path — POST the
// payload verbatim to target.URL with an HMAC-SHA256 signature header.
// Behavior is unchanged from before sendByDestType existed.
func (w *OutboxWorker) sendRawWebhook(
	ctx context.Context,
	row repo.OutboxRow,
	target *repo.NotifyTarget,
) error {
	const where = "service.OutboxWorker.sendRawWebhook"
	label := fmt.Sprintf("outbox-%d", row.ID)
	build := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(
			ctx, http.MethodPost, target.URL, bytes.NewReader(row.Payload),
		)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Listen-Signature", signRawBody(row.Payload, target.Secret))
		req.Header.Set("X-Trace-ID", row.TraceID)
		req.Header.Set("User-Agent", "listen/1.0")
		// 上游 req body truncate 1024 字节; X-Listen-Signature 签名头 skip。
		logext.Infof(ctx, "[%s] upstream req,label:%s,url:%s,body:%s",
			where, label, target.URL, truncateStr(string(row.Payload), 1024))
		return req, nil
	}
	check := checkOutboxResponse(label, row)
	return w.transport.Send(ctx, label, build, check)
}

// signRawBody mirrors notify/raw_webhook.go's signing. Kept in the
// service package so outbox doesn't import notify for one helper
// (cycle prevention). The string format MUST match — customers verify
// against "sha256=<hex>" produced by HMAC-SHA256 over the request body.
func signRawBody(body []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// checkOutboxResponse mirrors notify/raw_webhook.go classification for
// HTTP statuses. Kept here so outbox worker doesn't depend on notify's
// internal closure.
func checkOutboxResponse(label string, row repo.OutboxRow) notify.ResponseChecker {
	const where = "service.checkOutboxResponse"
	return func(status int, body []byte) error {
		// 上游响应日志(每 attempt 都有,truncate 1024 字节)。
		logext.Infof(context.Background(),
			"[%s] upstream resp,label:%s,id:%d,status:%d,body:%s",
			where, label, row.ID, status, truncateStr(string(body), 1024))
		switch {
		case status >= 200 && status < 300:
			// OTel: inbound_trace_id (not trace_id) — see docs/observability-sop.md.
			// Ctx isn't threaded into ResponseChecker so use Background; the
			// row already carries the inbound trace correlation in fields.
			slog.InfoContext(context.Background(), "outbox row delivered",
				"id", row.ID, "tenant", row.TenantID,
				"inbound_trace_id", row.TraceID, "status", status)
			return nil
		case status == 408 || status == 429:
			return fmt.Errorf("outbox %s retryable status=%d body=%s",
				label, status, truncateStr(string(body), 200))
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: outbox %s status=%d body=%s",
				notify.ErrTerminal, label, status, truncateStr(string(body), 200))
		default:
			return fmt.Errorf("outbox %s status=%d body=%s",
				label, status, truncateStr(string(body), 200))
		}
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
