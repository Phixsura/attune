// Package service · weekly digest sender (Phase 5 M6).
//
// Composes a 7-day summary per tenant and pushes it to the tenant's
// active lark-bot via the same notify.SendAlert envelope used for
// failure alerts. Tracks last_digest_sent_at on tenants so a restart
// or pod move doesn't re-send.
//
// Design choices (YAGNI):
//   - Scheduler tick = 30 min. Within each tick: scan tenants whose
//     last_digest_sent_at < now-6d, send to their first active lark-bot.
//     "At least once a week" with up to ~30 min jitter — acceptable.
//   - No per-tenant opt-out flag. Delete or disable the lark-bot to
//     mute. Wave 3 can add a column when a customer asks.
//   - UTC week boundary, ignores tenants.timezone. Acceptable until
//     billing-grade accuracy.
//   - Empty week → skip send (silent week is less noise than "0 条").
package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

const (
	digestCadence = 6 * 24 * time.Hour // resend cutoff (≈ 1 week)
	digestTick    = 30 * time.Minute
)

// DigestService bundles every dep needed to compose + send a digest.
// Scheduler goroutine owns one instance; CLI subcommand reuses Send.
type DigestService struct {
	tenants  *tenant.TenantRepo
	feedback *feedback.FeedbackRepo
	targets  *notifytarget.NotifyTargetRepo
}

func NewDigestService(
	t *tenant.TenantRepo, f *feedback.FeedbackRepo, n *notifytarget.NotifyTargetRepo,
) *DigestService {
	return &DigestService{tenants: t, feedback: f, targets: n}
}

// Run blocks until ctx is cancelled, dispatching digests on each tick.
// Safe to call once at startup; not safe to run two instances.
func (s *DigestService) Run(ctx context.Context) {
	slog.InfoContext(ctx, "digest scheduler started", "cadence", digestCadence, "tick", digestTick)
	t := time.NewTicker(digestTick)
	defer t.Stop()
	// Fire once immediately on startup so a long-stopped service catches
	// up quickly instead of waiting digestTick.
	s.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.runOnce(ctx)
		}
	}
}

func (s *DigestService) runOnce(ctx context.Context) {
	cutoff := time.Now().Add(-digestCadence)
	candidates, err := s.tenants.TenantsNeedingDigest(ctx, cutoff)
	if err != nil {
		slog.WarnContext(ctx, "digest: scan tenants", "err", err)
		return
	}
	for _, c := range candidates {
		if err := s.SendForTenant(ctx, c.ID, c.Slug, c.Name); err != nil {
			slog.WarnContext(ctx, "digest: tenant send failed", "tenant", c.Slug, "err", err)
		}
	}
}

// SendForTenant composes and sends one digest. Public so the CLI can
// call it for smoke-testing or manual catch-up. Skips silently when:
//   - tenant has no active lark-bot (nothing to send TO)
//   - last 7 days had zero feedback (empty digest = noise)
//
// Updates last_digest_sent_at on success only. Failure leaves the
// timestamp untouched so the next scheduler tick will retry.
func (s *DigestService) SendForTenant(
	ctx context.Context, tenantID, slug, displayName string,
) error {
	const where = "service.DigestService.SendForTenant"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,slug:%s", where, tenantID, slug)
	bots, err := s.targets.ListLarkBots(ctx, tenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list lark-bots failed,tenant:%s,err:%+v",
			where, slug, err.Error())
		return fmt.Errorf("list lark-bots: %w", err)
	}
	if len(bots) == 0 {
		slog.DebugContext(ctx, "digest: no lark-bot, skipping", "tenant", slug)
		return nil
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -7)
	counts, err := s.feedback.KindCounts(ctx, tenantID, from, now)
	if err != nil {
		logext.Errorf(ctx, "[%s] kind counts failed,tenant:%s,err:%+v",
			where, slug, err.Error())
		return fmt.Errorf("kind counts: %w", err)
	}
	var total int64
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		slog.DebugContext(ctx, "digest: zero feedback this week, skipping", "tenant", slug)
		return nil
	}
	mods, err := s.feedback.TopModulesByTenant(ctx, tenantID, from, now, 3)
	if err != nil {
		slog.WarnContext(ctx, "digest: top modules", "err", err) // non-fatal
	}

	text := composeDigest(displayName, from, now, total, counts, mods)
	res := notify.SendAlert(ctx, bots[0], text)
	if !res.OK {
		logext.Errorf(ctx, "[%s] SendAlert failed,tenant:%s,latency_ms:%d,err:%+v",
			where, slug, res.LatencyMs, res.Err.Error())
		return fmt.Errorf("send alert: %w", res.Err)
	}
	logext.Infof(ctx, "[%s] OK,tenant:%s,total:%d,latency_ms:%d",
		where, slug, total, res.LatencyMs)
	if err := s.tenants.TouchDigestSent(ctx, tenantID); err != nil {
		slog.WarnContext(ctx, "digest: touch sent failed", "tenant", slug, "err", err)
	}
	slog.InfoContext(ctx, "digest: sent",
		"tenant", slug, "total", total, "latency_ms", res.LatencyMs)
	return nil
}

// composeDigest is pure — no IO. Easy to unit-test the formatting alone.
func composeDigest(
	displayName string,
	from, to time.Time,
	total int64,
	byKind map[string]int64,
	topModules []string,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📊 Attune周报 · %s\n", displayName)
	fmt.Fprintf(&b, "时间窗口：%s ~ %s\n",
		from.Format("01-02"), to.Format("01-02"))
	fmt.Fprintf(&b, "本周共收到 %d 条反馈\n", total)

	if len(byKind) > 0 {
		entries := make([][2]any, 0, len(byKind))
		for k, n := range byKind {
			entries = append(entries, [2]any{k, n})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i][1].(int64) > entries[j][1].(int64)
		})
		b.WriteString("\n分布：\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "  · %s: %d\n", kindZH(e[0].(string)), e[1].(int64))
		}
	}

	if len(topModules) > 0 {
		b.WriteString("\n高发模块：")
		b.WriteString(strings.Join(topModules, " / "))
		b.WriteString("\n")
	}

	b.WriteString("\n→ 打开Attune控制台查看详情。")
	return b.String()
}

func kindZH(k string) string {
	switch k {
	case "bug":
		return "缺陷"
	case "feature":
		return "功能"
	case "ops":
		return "运维"
	case "question":
		return "咨询"
	case "other":
		return "其他"
	default:
		return k
	}
}
