package enrich

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	embeddingrepo "github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

// attrsSizeBytes returns the JSON-encoded size of attrs without
// persisting the bytes. Cheap (a few KiB of allocation per row) and
// mirrors exactly what repo.feedback.marshalAttrs would compute.
func attrsSizeBytes(a map[string]any) int {
	if a == nil {
		return 0
	}
	b, err := json.Marshal(a)
	if err != nil {
		return 0
	}
	return len(b)
}

const (
	enrichmentSystemUser = "system-feedback-enricher"
)

type feedbackEnrichRepo interface {
	TryClaim(context.Context, int64) (bool, error)
	LoadForEnrich(context.Context, int64) (*feedback.EnrichInput, error)
	MarkFailed(context.Context, int64, string, ...feedback.EnrichmentFailureSnapshot) (bool, string)
	MarkDone(context.Context, int64, domain.Enriched, ...feedback.EnrichmentMetadata) error
	BeginTx(context.Context) (pgx.Tx, error)
	MarkDoneTx(context.Context, pgx.Tx, int64, domain.Enriched, ...feedback.EnrichmentMetadata) error
	InsertSemanticExtractionRunTx(context.Context, pgx.Tx, feedback.SemanticExtractionRun) (int64, error)
	ListPending(context.Context, int) ([]int64, error)
}

// Enricher classifies user_feedback rows via the LLM gateway. It owns
// the claim-then-update loop but holds no SQL itself — repo does.
//
// Wiring: raw-webhook → outbox row in same tx as MarkDone (at-least-once).
// The #34 outbound adapter framework handles delivery via OutboxWorker.
type Enricher struct {
	repo          feedbackEnrichRepo
	llm           llmclient.LLMClient
	model         string                         // resolved from config; "" → enricher rejects with 400-like error
	outbox        *outboxrepo.OutboxRepo         // optional outbox writer
	targets       *notifytarget.NotifyTargetRepo // optional, paired with outbox
	embeddingTask *embeddingrepo.TaskRepo        // optional embedding task outbox
	draftTask     *replydraftrepo.DraftTaskRepo  // optional reply-draft task outbox
	sources       domain.SourceSet               // injected union; resolves the envelope's source_display label
}

type classifyResult struct {
	Enriched          domain.Enriched
	DropDiagnostics   []domain.AttrDropDiagnostic
	DroppedAttrsAudit map[string]any
	Route             llmclient.RouteMetadata
}

// ClassifyResult is the public result type for ClassifyWithDiagnostics.
// It exposes the diagnostics for eval to capture off-list values.
type ClassifyResult struct {
	Enriched        domain.Enriched
	DropDiagnostics []domain.AttrDropDiagnostic
}

// NewEnricher takes an optional legacy model id. New deployments leave it empty
// and let the DB-managed LLM router resolve model/channel by purpose.
func NewEnricher(r *feedback.FeedbackRepo, llm llmclient.LLMClient, model string) *Enricher {
	return ptrext.Of(Enricher{repo: r, llm: llm, model: model})
}

// SetSourceSet injects the registry-derived SourceSet used to stamp the
// envelope's source_display label. Unset (or nil) falls back to
// domain.DefaultSourceSet, so an enricher built for eval/tests still renders.
func (e *Enricher) SetSourceSet(sources domain.SourceSet) { e.sources = sources }

// sourceDisplay resolves a source's human label via the injected set, falling
// back to the default set when unset. Never empty (raw key for an unknown
// source), so a retired/unknown token still renders downstream.
func (e *Enricher) sourceDisplay(source string) string {
	set := e.sources
	if set == nil {
		set = domain.DefaultSourceSet()
	}
	return set.Display(source)
}

// SetOutbox wires at-least-once delivery for raw-webhook destinations.
// When set, every enrich success inserts one outbox row per active
// matching destination inside the same tx as MarkDone. Both repos
// must be non-nil or this is a no-op.
func (e *Enricher) SetOutbox(outbox *outboxrepo.OutboxRepo, targets *notifytarget.NotifyTargetRepo) {
	e.outbox = outbox
	e.targets = targets
}

// SetEmbeddingTask wires embedding task creation after enrichment.
// When set, every enrich success inserts an embedding_task row in the
// same tx as MarkDone, enabling the embedding worker to process it.
func (e *Enricher) SetEmbeddingTask(repo *embeddingrepo.TaskRepo) {
	e.embeddingTask = repo
}

// SetDraftTask wires reply-draft task creation after enrichment. When set,
// every enrich success inserts a reply_draft_task row in the same tx as
// MarkDone — gated by the repo SQL on the tenant's reply_draft_enabled and
// confidence threshold — so the reply-draft worker can process it.
func (e *Enricher) SetDraftTask(repo *replydraftrepo.DraftTaskRepo) {
	e.draftTask = repo
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
		metrics.EnrichDuration.WithLabelValues("unknown", "freeform", "db_err").Observe(0)
		logext.Errorf(ctx, "[%s] load failed,feedback_id:%d,err:%+v",
			where, id, err.Error())
		return err
	}
	logext.Infof(ctx, "[%s] start,feedback_id:%d,tenant_id:%s,content_len:%d",
		where, id, row.TenantID, len(row.Content))

	// Triage gate in front of the LLM call. Cheap rule-based filter
	// that diverts noise to the ignore path (no LLM cost, no dispatch)
	// and a future fast-path slot for per-tenant deterministic rules.
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
		return e.persistFromTriage(ctx, id, row, ptrext.Indirect(decision.FastEnriched))
	default:
		return e.runFullEnrich(ctx, id, row)
	}
}

// runFullEnrich is the pre-Sprint-1.3 path — call the LLM, build the
// snapshot, persist, fan out. Extracted so the triage switch in
// EnrichOne stays one expression per branch.
func (e *Enricher) runFullEnrich(ctx context.Context, id int64, row *feedback.EnrichInput) error {
	const where = "service.Enricher.runFullEnrich"
	start := time.Now()
	row.Language = detectRowLanguage(row.Content)
	row.DisplayLocale = displayLocaleForTenantLocale(row.DisplayLocale)
	cfg := classifyConfigFromRow(row)
	mode := dimsMode(cfg)
	result, err := e.classify(ctx, id, row.Content, cfg)
	if err != nil {
		metrics.EnrichDuration.WithLabelValues(row.TenantID, mode, classifyErrResult(err)).
			Observe(time.Since(start).Seconds())
		return err
	}
	enriched := result.Enriched
	// Observe attrs payload size before persistence so the histogram
	// captures every classification attempt, even ones rejected by the
	// per-row size cap downstream.
	if size := attrsSizeBytes(enriched.Attrs); size > 0 {
		metrics.EnrichAttrsSizeBytes.WithLabelValues(row.TenantID).Observe(float64(size))
	}
	snapshot := buildSnapshot(id, row, enriched, time.Now())
	run := e.semanticRun(snapshot, cfg, result)
	if err := e.persistEnriched(ctx, snapshot, enriched, ptrext.Of(run)); err != nil {
		if errors.Is(err, feedback.ErrAttrsTooLarge) {
			metrics.EnrichAttrsRejectedTotal.WithLabelValues(row.TenantID).Inc()
		}
		metrics.EnrichDuration.WithLabelValues(row.TenantID, mode, "db_err").
			Observe(time.Since(start).Seconds())
		e.markFailed(ctx, id, err.Error(), e.failureSnapshot(cfg, result.Route, err))
		return err
	}
	metrics.EnrichDuration.WithLabelValues(row.TenantID, mode, "ok").
		Observe(time.Since(start).Seconds())
	logext.Infof(ctx,
		"[%s] feedback enriched,inbound_trace_id:%s,tenant_id:%s,feedback_id:%d,is_urgent:%t,title:%s,attrs:%s",
		where, trace.FromContext(ctx), row.TenantID, id, enriched.IsUrgent, enriched.Title,
		logext.AsLogParam(enriched.Attrs))
	return nil
}

// Classify is the public, side-effect-free LLM classification entry
// point. `attune eval` re-runs historical content through this without
// touching the DB. The internal classify wraps it with MarkFailed.
//
// IsUrgent is derived deterministically from the configured Dimension
// set: for every dim, if any value the LLM picked falls into that
// dim's UrgentSet, the row is urgent. The LLM never decides urgency
// — that keeps routing under operator control and deterministic
// across retries.
func (e *Enricher) Classify(ctx context.Context, content string, cfg ClassifyConfig) (domain.Enriched, error) {
	if cfg.Language == "" {
		cfg.Language = detectRowLanguage(content)
	}
	if cfg.DisplayLocale == "" {
		cfg.DisplayLocale = classifyDisplayLocale(cfg)
	}
	result, err := e.classifyWithDiagnostics(ctx, content, cfg)
	if err != nil {
		return domain.Enriched{}, err
	}
	return result.Enriched, nil
}

// ClassifyWithDiagnostics is like Classify but also returns drop diagnostics.
// Used by eval to capture off-list values that were filtered out.
func (e *Enricher) ClassifyWithDiagnostics(ctx context.Context, content string, cfg ClassifyConfig) (ClassifyResult, error) {
	if cfg.Language == "" {
		cfg.Language = detectRowLanguage(content)
	}
	if cfg.DisplayLocale == "" {
		cfg.DisplayLocale = classifyDisplayLocale(cfg)
	}
	result, err := e.classifyWithDiagnostics(ctx, content, cfg)
	if err != nil {
		return ClassifyResult{}, err
	}
	return ClassifyResult{
		Enriched:        result.Enriched,
		DropDiagnostics: result.DropDiagnostics,
	}, nil
}

func (e *Enricher) classifyWithDiagnostics(ctx context.Context, content string, cfg ClassifyConfig) (classifyResult, error) {
	const where = "service.Enricher.Classify"
	prompt := renderPrompt(cfg, content)
	req := llmclient.CompletionRequest{
		Model:       e.model,
		Prompt:      prompt,
		Temperature: 0.0,
		MaxTokens:   512,
		UserID:      enrichmentSystemUser,
		Guard: llmclient.GuardMetadata{
			TenantID:   cfg.TenantID,
			FeedbackID: cfg.FeedbackID,
			Channel:    cfg.Channel,
			SourceID:   cfg.SourceID,
			SourceTags: cfg.SourceTags,
			Purpose:    classifyPurpose(cfg),
		},
	}
	if cfg.HasConstrained() {
		req.Schema = buildEnrichSchema(cfg.Dimensions)
	}
	resp, err := e.llm.Complete(ctx, req)
	if err != nil {
		logext.Errorf(ctx, "[%s] llm.Complete failed,model:%s,err:%+v",
			where, e.model, err.Error())
		return classifyResult{Route: resp.Route}, fmt.Errorf("llm: %w", err)
	}
	parsed, err := parseEnrichJSON(resp.Text)
	if err != nil {
		logext.Warnf(ctx, "[%s] parse failed,err:%s", where, err.Error())
		return classifyResult{Route: resp.Route}, fmt.Errorf("parse: %w", err)
	}
	parsed = normalizeEnrichedDisplay(parsed, cfg.Language, cfg.DisplayLocale)
	tenant := cfg.TenantID
	if tenant == "" {
		tenant = "unknown"
	}
	kept, dropped := applyAttrsGate(ctx, tenant, parsed.Attrs, cfg.Dimensions)
	parsed.Attrs = kept
	parsed.IsUrgent = domain.ComputeIsUrgent(parsed.Attrs, cfg.Dimensions)
	return classifyResult{
		Enriched:          parsed,
		DropDiagnostics:   dropped,
		DroppedAttrsAudit: droppedAttrsAudit(dropped),
		Route:             resp.Route,
	}, nil
}

func classifyPurpose(cfg ClassifyConfig) string {
	if cfg.Purpose != "" {
		return cfg.Purpose
	}
	return "enrich"
}

// classify calls Classify and, on failure, marks the row 'failed' so the
// next sweep won't retry immediately. Used by the main enricher loop.
func (e *Enricher) classify(ctx context.Context, id int64, content string, cfg ClassifyConfig) (classifyResult, error) {
	cfg.FeedbackID = id
	parsed, err := e.classifyWithDiagnostics(ctx, content, cfg)
	if err != nil {
		if shouldMarkEnrichFailed(err) {
			e.markFailed(ctx, id, err.Error(), e.failureSnapshot(cfg, parsed.Route, err))
		}
		return parsed, err
	}
	return parsed, nil
}

func shouldMarkEnrichFailed(err error) bool {
	return !errors.Is(err, llmclient.ErrRateLimitWaitCanceled)
}

func classifyConfigFromRow(row *feedback.EnrichInput) ClassifyConfig {
	return ClassifyConfig{
		TenantID:        row.TenantID,
		Channel:         row.Source,
		SourceID:        row.InboundSourceID,
		SourceTags:      row.SourceTags,
		Language:        row.Language,
		DisplayLocale:   displayLocaleForTenantLocale(row.DisplayLocale),
		PromptTemplate:  row.PromptTemplate,
		PolicyConfig:    row.PromptPolicy,
		PromptVersionID: row.PromptVersionID,
		Dimensions:      row.Dimensions,
	}
}

// applyAttrsGate runs gate (2) per dim and records observability for
// off-list values. Returns the canonical kept attrs.
func applyAttrsGate(
	ctx context.Context,
	tenantID string,
	produced map[string]any,
	dims domain.DimensionSet,
) (map[string]any, []domain.AttrDropDiagnostic) {
	const where = "service.Enricher.applyAttrsGate"
	kept, diagnostics, suggested := domain.FilterAttrsWithDiagnostics(produced, dims)
	if len(diagnostics) == 0 {
		return kept, nil
	}
	for _, diag := range diagnostics {
		if diag.Reason == domain.AttrDropUnknownDimension {
			continue
		}
		for i := 0; i < diag.Count; i++ {
			metrics.EnrichAttrsDroppedTotal.WithLabelValues(tenantID, diag.Dim).Inc()
		}
	}
	seen := make(map[string]bool, len(suggested))
	for _, dim := range suggested {
		if seen[dim] {
			continue
		}
		seen[dim] = true
		metrics.EnrichSuggestedAttrsTotal.WithLabelValues(tenantID, dim).Inc()
	}
	logext.Infof(ctx, "[%s] suggested,tenant_id:%s,dropped:%v",
		where, tenantID, diagnostics)
	return kept, diagnostics
}

func droppedAttrsAudit(diagnostics []domain.AttrDropDiagnostic) map[string]any {
	if len(diagnostics) == 0 {
		return nil
	}
	return map[string]any{"diagnostics": diagnostics}
}

// fanOut pushes a freshly enriched snapshot to the snapshot of `notifier`
// EnrichPending sweeps up to n rows that need classification.
func (e *Enricher) EnrichPending(ctx context.Context, n int) {
	const where = "service.Enricher.EnrichPending"
	ids, err := e.repo.ListPending(ctx, n)
	if err != nil {
		logext.Errorf(ctx, "[%s] list pending failed,err:%+v", where, err.Error())
		return
	}
	for _, id := range ids {
		rowCtx := trace.WithID(ctx, trace.New())
		if err := e.EnrichOne(rowCtx, id); err != nil {
			logext.Warnf(rowCtx, "[%s] enrich failed,id:%d,err:%+v", where, id, err.Error())
		}
	}
}

// RunBackground polls pending rows on `interval`. A nil-llm enricher
// logs once and returns so it doesn't busy-spin in misconfigured envs.
func (e *Enricher) RunBackground(ctx context.Context, interval time.Duration, batch int) {
	const where = "service.Enricher.RunBackground"
	if e.llm == nil {
		logext.Warnf(ctx, "[%s] disabled: no llm client", where)
		return
	}
	logext.Infof(ctx, "[%s] started,interval:%s,batch:%d", where, interval, batch)
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
