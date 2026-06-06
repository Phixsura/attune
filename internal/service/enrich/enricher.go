package enrich

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

const (
	enrichmentModelID    = "gpt-4o-mini"
	enrichmentSystemUser = "system-feedback-enricher"
)

const enrichPromptTmpl = `你是产品反馈分类助手。给你一段用户反馈原文，严格只输出一行 JSON 对象（不要 markdown 包裹，不要解释，不要前后空行），字段如下：

{
  "title": "10-30 字一句话摘要，不要标点结尾",
  "kind": "bug | feature | ops | question | other 之一",
  "modules": [识别到的产品模块名字符串数组，没识别到就空数组],
  "severity": "P0 | P1 | P2 | P3 之一",
  "rationale": "30 字以内为什么这么打"
}

严重度判断：P0=阻塞（不能用/丢钱/数据丢失）、P1=严重（关键功能失效但有 workaround）、P2=影响体验、P3=优化建议。

用户反馈原文：
"""
%s
"""`

// Enricher classifies user_feedback rows via the LLM gateway. It owns
// the claim-then-update loop but holds no SQL itself — repo does.
//
// Wave 1.2 wiring:
//   - Lark webhook → inline call via notifier (best-effort, no retry)
//   - raw-webhook → outbox row in same tx as MarkDone (at-least-once)
//
// SetNotifier and SetOutbox are independent; either / both / neither
// may be wired without code changes elsewhere.
type Enricher struct {
	repo *feedback.FeedbackRepo
	llm  llmclient.LLMClient
	// notifier is read from fanOut goroutines, written by SetNotifier
	// (typically once at startup, but Wave 2 plans dynamic per-tenant
	// re-wiring). atomic.Pointer keeps the read race-free without
	// per-call locking — fanOut takes a snapshot via .Load().
	notifier atomic.Pointer[notify.Notifier]
	outbox   *outboxrepo.OutboxRepo         // optional outbox writer
	targets  *notifytarget.NotifyTargetRepo // optional, paired with outbox
}

func NewEnricher(r *feedback.FeedbackRepo, llm llmclient.LLMClient) *Enricher {
	return &Enricher{repo: r, llm: llm}
}

// SetNotifier wires the inline webhook fan-out (Lark). nil = no
// notifications; rows still land in Postgres normally. Safe for concurrent
// reads (fanOut goroutines).
func (e *Enricher) SetNotifier(n notify.Notifier) {
	if n == nil {
		e.notifier.Store(nil)
		return
	}
	e.notifier.Store(&n)
}

// SetOutbox wires at-least-once delivery for raw-webhook destinations.
// When set, every enrich success inserts one outbox row per active
// matching destination inside the same tx as MarkDone. Both repos
// must be non-nil or this is a no-op.
func (e *Enricher) SetOutbox(outbox *outboxrepo.OutboxRepo, targets *notifytarget.NotifyTargetRepo) {
	e.outbox = outbox
	e.targets = targets
}

// EnrichOne runs the full pipeline for one row: claim, load, LLM,
// persist, fan-out. Returns nil quietly when the row was already done
// or held by another worker. Errors come from real failures (LLM down,
// parse failure, DB write failure).
func (e *Enricher) EnrichOne(ctx context.Context, id int64) error {
	const where = "service.Enricher.EnrichOne"
	if e.llm == nil {
		logext.Warnf(ctx, "[%s] reject: llm not configured,feedback_id:%d", where, id)
		return fmt.Errorf("llm client not configured")
	}
	claimed, err := e.repo.TryClaim(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] try claim failed,feedback_id:%d,err:%+v",
			where, id, err.Error())
		return err
	}
	if !claimed {
		metrics.ClaimContentionTotal.Inc()
		logext.Infof(ctx, "[%s] claim contention skip,feedback_id:%d", where, id)
		return nil
	}
	row, err := e.repo.LoadForEnrich(ctx, id)
	if err != nil {
		metrics.EnrichDuration.WithLabelValues("unknown", "db_err").Observe(0)
		logext.Errorf(ctx, "[%s] load failed,feedback_id:%d,err:%+v",
			where, id, err.Error())
		return err
	}
	logext.Infof(ctx, "[%s] start,feedback_id:%d,tenant_id:%s,content_len:%d",
		where, id, row.TenantID, len(row.Content))

	// Sprint 1.3 (Y1 工程): triage gate in front of the LLM call. Cheap
	// rule-based filter that diverts noise to the ignore path (no LLM
	// cost, no dispatch) and the future fast-path (Sprint 2.x).
	decision := Triage(row.Content)
	metrics.TriageDecisionsTotal.WithLabelValues(row.TenantID, string(decision.Mode)).Inc()

	switch decision.Mode {
	case TriageIgnore:
		return e.persistIgnored(ctx, id, row, decision.Reason)
	case TriageFast:
		// v0 scaffold — Sprint 2.x will set decision.FastEnriched from a
		// per-tenant rule match. Until then, fall through to full enrich.
		if decision.FastEnriched == nil {
			return e.runFullEnrich(ctx, id, row)
		}
		return e.persistFromTriage(ctx, id, row, *decision.FastEnriched)
	default:
		return e.runFullEnrich(ctx, id, row)
	}
}

// runFullEnrich is the pre-Sprint-1.3 path — call the LLM, build the
// snapshot, persist, fan out. Extracted so the triage switch in
// EnrichOne stays one expression per branch.
func (e *Enricher) runFullEnrich(ctx context.Context, id int64, row *feedback.EnrichInput) error {
	start := time.Now()
	enriched, err := e.classify(ctx, id, row.Content)
	if err != nil {
		metrics.EnrichDuration.WithLabelValues(row.TenantID, classifyErrResult(err)).
			Observe(time.Since(start).Seconds())
		return err
	}
	snapshot := buildSnapshot(id, row, enriched, time.Now())
	if err := e.persistEnriched(ctx, snapshot, enriched); err != nil {
		metrics.EnrichDuration.WithLabelValues(row.TenantID, "db_err").
			Observe(time.Since(start).Seconds())
		return err
	}
	metrics.EnrichDuration.WithLabelValues(row.TenantID, "ok").
		Observe(time.Since(start).Seconds())
	slog.InfoContext(ctx, "feedback enriched",
		"inbound_trace_id", trace.FromContext(ctx),
		"tenant_id", row.TenantID,
		"feedback_id", id,
		"kind", enriched.Kind,
		"severity", enriched.Severity,
		"title", enriched.Title)
	if n := e.notifier.Load(); n != nil {
		go e.fanOut(snapshot, *n)
	}
	return nil
}

// Classify is the public, side-effect-free LLM classification entry
// point. eval CLI re-runs historical content through this without
// touching the DB. The internal classify wraps it with MarkFailed.
func (e *Enricher) Classify(ctx context.Context, content string) (domain.Enriched, error) {
	const where = "service.Enricher.Classify"
	prompt := fmt.Sprintf(enrichPromptTmpl, content)
	out, err := e.llm.Chat(ctx, enrichmentSystemUser, enrichmentModelID, prompt, 0.0, 512)
	if err != nil {
		logext.Errorf(ctx, "[%s] llm.Chat failed,model:%s,err:%+v",
			where, enrichmentModelID, err.Error())
		return domain.Enriched{}, fmt.Errorf("llm: %w", err)
	}
	parsed, err := parseEnrichJSON(out)
	if err != nil {
		logext.Warnf(ctx, "[%s] parse failed,err:%s,raw:%s",
			where, err.Error(), truncate(out, 300))
		return domain.Enriched{}, fmt.Errorf("parse: %w; raw=%s", err, truncate(out, 300))
	}
	parsed.Priority = domain.SeverityWeight[parsed.Severity]
	return parsed, nil
}

// classify calls Classify and, on failure, marks the row 'failed' so the
// next sweep won't retry immediately. Used by the main enricher loop.
func (e *Enricher) classify(ctx context.Context, id int64, content string) (domain.Enriched, error) {
	parsed, err := e.Classify(ctx, content)
	if err != nil {
		e.repo.MarkFailed(ctx, id, err.Error())
		return domain.Enriched{}, err
	}
	return parsed, nil
}

// fanOut pushes a freshly enriched snapshot to the snapshot of `notifier`
// taken at fire time. Best-effort: per-destination errors are logged but
// never propagated — webhook outages must not block downstream rows.
// `n` is captured at goroutine launch so a concurrent SetNotifier(nil)
// or replacement can't trip a nil deref mid-call.
func (e *Enricher) fanOut(s domain.Snapshot, n notify.Notifier) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := n.PushPool(ctx, s); err != nil {
		slog.WarnContext(ctx, "notify pool failed", "id", s.ID, "err", err)
	}
	if s.IsHighSeverity() {
		if err := n.PushRadar(ctx, s); err != nil {
			slog.WarnContext(ctx, "notify radar failed", "id", s.ID, "err", err)
		}
	}
}

// EnrichPending sweeps up to n rows that need classification.
func (e *Enricher) EnrichPending(ctx context.Context, n int) {
	ids, err := e.repo.ListPending(ctx, n)
	if err != nil {
		slog.ErrorContext(ctx, "enrich list pending failed", "err", err)
		return
	}
	for _, id := range ids {
		if err := e.EnrichOne(ctx, id); err != nil {
			slog.WarnContext(ctx, "feedback enrich failed", "id", id, "err", err)
		}
	}
}

// RunBackground polls pending rows on `interval`. A nil-llm enricher
// logs once and returns so it doesn't busy-spin in misconfigured envs.
func (e *Enricher) RunBackground(ctx context.Context, interval time.Duration, batch int) {
	if e.llm == nil {
		slog.WarnContext(ctx, "feedback enricher disabled: no llm client")
		return
	}
	slog.InfoContext(ctx, "feedback enricher started", "interval", interval, "batch", batch)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.EnrichPending(ctx, batch)
		}
	}
}

// buildSnapshot, parseEnrichJSON, classifyErrResult, truncate moved to
// enricher_parse.go (Sprint 1.3) to keep this file under attune ≤300-
// line rule after the Triage split.
//
// persistIgnored, persistFromTriage moved to enricher_helpers.go for the
// same reason.
