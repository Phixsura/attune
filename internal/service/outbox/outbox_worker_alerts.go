package outbox

// Phase 3.2 webhook failure visibility — split from outbox_worker.go to
// honor the no-grab-bag-files guidance (per-file scope kept narrow). Holds the
// self-report dispatch logic that fires when a row marks dead.

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

// selfReportDead pushes a one-shot text card to the tenant's lark-bot
// describing the dead delivery. Skipped silently if:
// - the dead target was itself a lark-bot (sending to a sibling lark-
// bot would be confusing; alert-of-alert is mostly noise);
// - the tenant has no active lark-bot configured;
// - the lark-bot send itself errors (we log + carry on, no recursion).
func (w *OutboxWorker) selfReportDead(ctx context.Context, row outboxrepo.OutboxRow, reason string) {
	const where = "service.OutboxWorker.selfReportDead"
	if row.DestinationType == notifytarget.DestLarkBot {
		return // alerting a sibling lark-bot is noise
	}
	bots, err := w.targets.ListLarkBots(ctx, row.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list lark bots failed,tenant:%s,err:%+v",
			where, row.TenantID, err.Error())
		return
	}
	if len(bots) == 0 {
		logext.Infof(ctx, "[%s] no lark-bot,skip alert,tenant:%s", where, row.TenantID)
		return // no chat to alert
	}
	text := fmt.Sprintf(
		"⚠️ Notify delivery failed\nType: %s\nURL: %s\nReason: %s\nAttune has stopped retrying this target. Update the configuration in the console and re-enable.",
		row.DestinationType, row.DestinationTarget, truncateStr(reason, 200),
	)
	// Use the first bot only — repeated alerts to every bot would spam
	// when a tenant has multiple chats wired up.
	res := notify.SendAlert(ctx, bots[0], text)
	if !res.OK {
		logext.Warnf(ctx, "[%s] self-report alert failed,tenant:%s,bot_id:%s,latency_ms:%d,err:%+v",
			where, row.TenantID, bots[0].ID, res.LatencyMs, res.Err)
		return
	}
	logext.Infof(ctx, "[%s] self-report alert sent,tenant:%s,dead_outbox_id:%d,latency_ms:%d",
		where, row.TenantID, row.ID, res.LatencyMs)
}

// outboxBackoff is the retry schedule from design doc §3.6:
// 30s / 2m / 10m / 1h. Beyond the 5th attempt callers should mark dead.
