package enrich

// enricher_helpers.go — the non-LLM triage persist paths extracted from
// enricher.go so the main file stays under the no-grab-bag-files guidance
//. Both share runFullEnrich's downstream behavior
// (persist + best-effort fan-out) but skip the LLM call.

import (
	"context"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// persistIgnored marks the row done with a sentinel "ignored" Enriched
// so downstream queries can filter it out. Does NOT fan out — silence
// is the right outcome for noise. Observability still records the row
// (via the triage_decisions counter) so PMs can audit the ignore rate.
func (e *Enricher) persistIgnored(ctx context.Context, id int64, row *feedback.EnrichInput, reason string) error {
	enriched := domain.Enriched{
		Title:     "[triage-ignored]",
		Attrs:     map[string]any{},
		IsUrgent:  false,
		Rationale: "triage v0: " + reason,
	}
	if err := e.repo.MarkDone(ctx, id, enriched); err != nil {
		return fmt.Errorf("mark ignored row done: %w", err)
	}
	logext.Infof(ctx,
		"[service.Enricher.persistIgnored] feedback ignored by triage,inbound_trace_id:%s,tenant_id:%s,feedback_id:%d,reason:%s",
		trace.FromContext(ctx), row.TenantID, id, reason)
	return nil
}

// persistFromTriage handles the fast-path case where triage matched a
// per-tenant rule and produced a full Enriched without consulting the
// LLM. Same downstream behavior as runFullEnrich (persist + fan out),
// just without the LLM call.
func (e *Enricher) persistFromTriage(ctx context.Context, id int64, row *feedback.EnrichInput, enriched domain.Enriched) error {
	snapshot := buildSnapshot(id, row, enriched, time.Now())
	if err := e.persistEnriched(ctx, snapshot, enriched); err != nil {
		return err
	}
	logext.Infof(ctx,
		"[service.Enricher.persistFromTriage] feedback enriched via fast-path,inbound_trace_id:%s,tenant_id:%s,feedback_id:%d,is_urgent:%t,attrs:%s",
		trace.FromContext(ctx), row.TenantID, id, enriched.IsUrgent,
		logext.AsLogParam(enriched.Attrs))
	if n := e.notifier.Load(); n != nil {
		go e.fanOut(snapshot, ptrext.Indirect(n))
	}
	return nil
}
