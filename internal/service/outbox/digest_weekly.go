// Package service · weekly digest sender (weekly digest).
//
// Composes a 7-day summary per tenant and pushes it to the tenant's
// active lark-bot via the same notify.SendAlert envelope used for
// failure alerts. Tracks last_digest_sent_at on tenants so a restart
// or pod move doesn't re-send.
//
// Design choices (YAGNI):
// - Scheduler tick = 30 min. Within each tick: scan tenants whose
// last_digest_sent_at < now-6d, send to their first active lark-bot.
// "At least once a week" with up to ~30 min jitter — acceptable.
// - No per-tenant opt-out flag. Delete or disable the lark-bot to
// mute. A follow-up can add a column when a customer asks.
// - UTC week boundary, ignores tenants.timezone. Acceptable until
// billing-grade accuracy.
// - Empty week → skip send (silent week is less noise than a "0 rows" digest).
package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/domain"
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
// - tenant has no active lark-bot (nothing to send TO)
// - last 7 days had zero feedback (empty digest = noise)
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

	cfg, err := s.tenants.GetEnrichConfig(ctx, tenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] read dim cfg failed,tenant:%s,err:%+v",
			where, slug, err.Error())
		return fmt.Errorf("read dim cfg: %w", err)
	}
	// Total = sum of daily ingest buckets — independent of any dim.
	buckets, err := s.feedback.UsageByDay(ctx, tenantID, from, now)
	if err != nil {
		logext.Errorf(ctx, "[%s] usage failed,tenant:%s,err:%+v",
			where, slug, err.Error())
		return fmt.Errorf("usage: %w", err)
	}
	var total int64
	for _, b := range buckets {
		total += b.Value
	}
	if total == 0 {
		slog.DebugContext(ctx, "digest: zero feedback this week, skipping", "tenant", slug)
		return nil
	}
	urgent, _ := s.feedback.UrgentCount(ctx, tenantID, from, now)
	// Top values per dim (5 each) — fed to composeDigest verbatim.
	dimTops := make(map[string][]feedback.ValueCount, len(cfg.Dimensions))
	for _, d := range cfg.Dimensions {
		tops, err := s.feedback.TopValuesByDim(ctx, tenantID, d.Name,
			d.Kind == domain.DimMulti, from, now, 5)
		if err != nil {
			slog.WarnContext(ctx, "digest: top values per dim",
				"dim", d.Name, "err", err)
			continue
		}
		dimTops[d.Name] = tops
	}

	text := composeDigest(displayName, from, now, total, urgent, cfg.Dimensions, dimTops)
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

// composeDigest is pure — no IO. The function takes the tenant's
// DimensionSet so it can render the per-dim "top values" sections
// using each dim's DisplayName (resolved to the digest body's
// canonical locale).
func composeDigest(
	displayName string,
	from, to time.Time,
	total, urgent int64,
	dims domain.DimensionSet,
	dimTops map[string][]feedback.ValueCount,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Attune weekly digest · %s\n", displayName)
	fmt.Fprintf(&b, "Window: %s ~ %s\n", from.Format("01-02"), to.Format("01-02"))
	fmt.Fprintf(&b, "Total feedback received: %d\n", total)
	if urgent > 0 {
		fmt.Fprintf(&b, "Urgent rows: %d\n", urgent)
	}
	for _, d := range dims {
		tops := dimTops[d.Name]
		if len(tops) == 0 {
			continue
		}
		label := d.DisplayName.Resolve([]string{"en"})
		if label == "" {
			label = d.Name
		}
		fmt.Fprintf(&b, "\nTop %s:\n", label)
		// dimTops is already DESC by count from the repo.
		for _, vc := range tops {
			fmt.Fprintf(&b, " - %s: %d\n", displayForValue(d, vc.Value), vc.Count)
		}
	}
	b.WriteString("\n→ Open the Attune console for details.")
	return b.String()
}

// displayForValue resolves a Taxonomy.Value to its preferred display
// (English fallback). Returns the raw Value when the taxonomy is
// freeform or the value isn't found in the taxonomy.
func displayForValue(d domain.Dimension, value string) string {
	for _, t := range d.Taxonomy {
		if t.Value == value {
			disp := t.DisplayName.Resolve([]string{"en"})
			if disp == "" {
				return value
			}
			return disp
		}
	}
	return value
}
