package service

// enricher_helpers.go — the non-LLM triage persist paths extracted from
// enricher.go so the main file stays under the listen ≤300-line rule
// (CLAUDE.md 律 2). Both share runFullEnrich's downstream behavior
// (persist + best-effort fan-out) but skip the LLM call.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wanmuchengchuan/listen/internal/domain"
	"github.com/wanmuchengchuan/listen/internal/infra/trace"
	"github.com/wanmuchengchuan/listen/internal/repo"
)

// persistIgnored marks the row done with a sentinel "ignored" Enriched
// so downstream queries can filter it out. Does NOT fan out — silence
// is the right outcome for noise. Observability still records the row
// (via the triage_decisions counter) so PMs can audit the ignore rate.
func (e *Enricher) persistIgnored(ctx context.Context, id int64, row *repo.EnrichInput, reason string) error {
	enriched := domain.Enriched{
		Title:     "[triage-ignored]",
		Kind:      "other",
		Severity:  "P3",
		Modules:   []string{},
		Rationale: "triage v0: " + reason,
		Priority:  domain.SeverityWeight["P3"],
	}
	if err := e.repo.MarkDone(ctx, id, enriched); err != nil {
		return fmt.Errorf("mark ignored row done: %w", err)
	}
	slog.InfoContext(ctx, "feedback ignored by triage",
		"inbound_trace_id", trace.FromContext(ctx),
		"tenant_id", row.TenantID,
		"feedback_id", id,
		"reason", reason)
	return nil
}

// persistFromTriage handles the fast-path case where triage matched a
// per-tenant rule and produced a full Enriched without consulting the
// LLM. Same downstream behavior as runFullEnrich (persist + fan out),
// just without the LLM call.
func (e *Enricher) persistFromTriage(ctx context.Context, id int64, row *repo.EnrichInput, enriched domain.Enriched) error {
	enriched.Priority = domain.SeverityWeight[enriched.Severity]
	snapshot := buildSnapshot(id, row, enriched, time.Now())
	if err := e.persistEnriched(ctx, snapshot, enriched); err != nil {
		return err
	}
	slog.InfoContext(ctx, "feedback enriched via fast-path",
		"inbound_trace_id", trace.FromContext(ctx),
		"tenant_id", row.TenantID,
		"feedback_id", id,
		"kind", enriched.Kind,
		"severity", enriched.Severity)
	if e.notifier != nil {
		go e.fanOut(snapshot)
	}
	return nil
}
