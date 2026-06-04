package service

// Phase 3.2 webhook failure visibility — split from outbox_worker.go to
// honor the listen ≤300-line file rule (CLAUDE.md 律 2). Holds the
// self-report dispatch logic that fires when a row marks dead.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Phixsura/listen/internal/logext"
	"github.com/Phixsura/listen/internal/notify"
	"github.com/Phixsura/listen/internal/repo"
)

// selfReportDead pushes a one-shot text card to the tenant's lark-bot
// describing the dead delivery. Skipped silently if:
//   - the dead target was itself a lark-bot (sending to a sibling lark-
//     bot would be confusing; alert-of-alert is mostly noise);
//   - the tenant has no active lark-bot configured;
//   - the lark-bot send itself errors (we log + carry on, no recursion).
func (w *OutboxWorker) selfReportDead(ctx context.Context, row repo.OutboxRow, reason string) {
	const where = "service.OutboxWorker.selfReportDead"
	if row.DestinationType == repo.DestLarkBot {
		return // alerting a sibling lark-bot is noise
	}
	bots, err := w.targets.ListLarkBots(ctx, row.TenantID)
	if err != nil {
		slog.WarnContext(ctx, "outbox: list lark bots for alert", "tenant", row.TenantID, "err", err)
		logext.Errorf(ctx, "[%s] list lark bots failed,tenant:%s,err:%+v",
			where, row.TenantID, err.Error())
		return
	}
	if len(bots) == 0 {
		logext.Infof(ctx, "[%s] no lark-bot,skip alert,tenant:%s", where, row.TenantID)
		return // no chat to alert
	}
	text := fmt.Sprintf(
		"⚠️ 通知投递失败\n类型：%s\nURL：%s\n原因：%s\n听见已停止重试此目标，请到控制台修改配置后再发布。",
		row.DestinationType, row.DestinationTarget, truncate(reason, 200),
	)
	// Use the first bot only — repeated alerts to every bot would spam
	// when a tenant has multiple chats wired up.
	res := notify.SendAlert(ctx, bots[0], text)
	if !res.OK {
		slog.WarnContext(ctx, "outbox: self-report alert failed",
			"tenant", row.TenantID, "bot_id", bots[0].ID,
			"latency_ms", res.LatencyMs, "err", res.Err)
		return
	}
	slog.InfoContext(ctx, "outbox: self-report alert sent",
		"tenant", row.TenantID, "dead_outbox_id", row.ID,
		"latency_ms", res.LatencyMs)
}

// outboxBackoff is the retry schedule from design doc §3.6:
// 30s / 2m / 10m / 1h. Beyond the 5th attempt callers should mark dead.
