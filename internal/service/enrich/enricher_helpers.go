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
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// markFailed records an enrichment failure on the row and, when the row has
// exhausted its retries, counts it in attune_enrichment_terminal_failures_total
// so terminal failures are no longer invisible (#81).
func (e *Enricher) markFailed(ctx context.Context, id int64, msg string) {
	if terminal, tenant := e.repo.MarkFailed(ctx, id, msg); terminal {
		metrics.EnrichmentTerminalFailuresTotal.WithLabelValues(tenant).Inc()
	}
}

// persistIgnored marks the row done with a sentinel "ignored" Enriched
// so downstream queries can filter it out. Does NOT fan out — silence
// is the right outcome for noise. Observability still records the row
// (via the triage_decisions counter) so PMs can audit the ignore rate.
func (e *Enricher) persistIgnored(ctx context.Context, id int64, row *feedback.EnrichInput, reason string) error {
	const where = "service.Enricher.persistIgnored"
	row.Language = detectRowLanguage(row.Content)
	row.DisplayLocale = displayLocaleForTenantLocale(row.DisplayLocale)
	enriched := domain.Enriched{
		Title:            "[triage-ignored]",
		DisplayTitle:     "[triage-ignored]",
		Attrs:            map[string]any{},
		IsUrgent:         false,
		Rationale:        "triage v0: " + reason,
		DisplayRationale: "triage v0: " + reason,
	}
	if err := e.repo.MarkDone(ctx, id, enriched, feedback.EnrichmentMetadata{
		Language:      row.Language,
		DisplayLocale: row.DisplayLocale,
	}); err != nil {
		e.markFailed(ctx, id, err.Error())
		return fmt.Errorf("mark ignored row done: %w", err)
	}
	logext.Infof(ctx,
		"[%s] feedback ignored by triage,inbound_trace_id:%s,tenant_id:%s,feedback_id:%d,reason:%s",
		where, trace.FromContext(ctx), row.TenantID, id, reason)
	return nil
}

// persistFromTriage handles the fast-path case where triage matched a
// per-tenant rule and produced a full Enriched without consulting the
// LLM. Same downstream behavior as runFullEnrich (persist + fan out),
// just without the LLM call.
func (e *Enricher) persistFromTriage(ctx context.Context, id int64, row *feedback.EnrichInput, enriched domain.Enriched) error {
	const where = "service.Enricher.persistFromTriage"
	row.Language = detectRowLanguage(row.Content)
	row.DisplayLocale = displayLocaleForTenantLocale(row.DisplayLocale)
	enriched = normalizeEnrichedDisplay(enriched, row.Language, row.DisplayLocale)
	snapshot := buildSnapshot(id, row, enriched, time.Now())
	if err := e.persistEnriched(ctx, snapshot, enriched, nil); err != nil {
		e.markFailed(ctx, id, err.Error())
		return err
	}
	logext.Infof(ctx,
		"[%s] feedback enriched via fast-path,inbound_trace_id:%s,tenant_id:%s,feedback_id:%d,is_urgent:%t,attrs:%s",
		where, trace.FromContext(ctx), row.TenantID, id, enriched.IsUrgent,
		logext.AsLogParam(enriched.Attrs))
	return nil
}
