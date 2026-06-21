package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

// persistEnriched flips user_feedback to done, records semantic extraction
// diagnostics when present, and queues outbox rows when configured.
func (e *Enricher) persistEnriched(
	ctx context.Context,
	s domain.Snapshot,
	enriched domain.Enriched,
	run *feedback.SemanticExtractionRun,
) error {
	const where = "service.Enricher.persistEnriched"
	// Fast path: skip transaction if no semantic run, no outbox, AND no embedding task.
	// Must check embeddingTask too, otherwise embedding clustering is silently skipped.
	if run == nil && (e.outbox == nil || e.targets == nil) && e.embeddingTask == nil && e.draftTask == nil {
		return e.repo.MarkDone(ctx, s.ID, enriched, feedback.EnrichmentMetadata{
			Language:      s.Language,
			DisplayLocale: s.DisplayLocale,
		})
	}
	plan, err := e.buildOutboxPlan(ctx, s, where)
	if err != nil {
		return err
	}
	return e.persistEnrichedTx(ctx, s, enriched, run, plan)
}

type outboxPlan struct {
	traceID string
	targets []notifytarget.NotifyTarget
	payload []byte
}

func (e *Enricher) buildOutboxPlan(
	ctx context.Context,
	s domain.Snapshot,
	where string,
) (outboxPlan, error) {
	// Look up active destinations BEFORE opening tx — list query
	// doesn't need atomicity with the UPDATE/INSERT.
	plan := outboxPlan{traceID: extractTraceID(ctx)}
	if e.outbox == nil || e.targets == nil {
		return plan, nil
	}
	allTargets, err := e.targets.ListActiveByTenant(ctx, s.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list notify targets failed,tenant_id:%s,err:%+v",
			where, s.TenantID, err.Error())
		return outboxPlan{}, fmt.Errorf("list notify targets: %w", err)
	}
	plan.targets = selectOutboxTargets(allTargets, s)
	plan.payload, err = buildOutboxEnvelope(s, plan.traceID)
	if err != nil {
		logext.Errorf(ctx, "[%s] build envelope failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return outboxPlan{}, fmt.Errorf("build outbox envelope: %w", err)
	}
	return plan, nil
}

func (e *Enricher) persistEnrichedTx(
	ctx context.Context,
	s domain.Snapshot,
	enriched domain.Enriched,
	run *feedback.SemanticExtractionRun,
	plan outboxPlan,
) error {
	const where = "service.Enricher.persistEnrichedTx"
	tx, err := e.repo.BeginTx(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] begin tx failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := e.repo.MarkDoneTx(ctx, tx, s.ID, enriched, feedback.EnrichmentMetadata{
		Language:      s.Language,
		DisplayLocale: s.DisplayLocale,
	}); err != nil {
		logext.Errorf(ctx, "[%s] MarkDoneTx failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return err
	}
	if err := e.insertSemanticRunTx(ctx, tx, s.ID, run, where); err != nil {
		return err
	}
	if err := e.insertOutboxRows(ctx, tx, s, plan, where); err != nil {
		return err
	}
	if err := e.insertEmbeddingTask(ctx, tx, s, where); err != nil {
		return err
	}
	if err := e.insertDraftTask(ctx, tx, s, where); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit tx failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return fmt.Errorf("commit enrich tx: %w", err)
	}
	if len(plan.targets) > 0 {
		logext.Infof(ctx,
			"[%s] outbox rows queued,inbound_trace_id:%s,tenant_id:%s,feedback_id:%d,count:%d",
			where, plan.traceID, s.TenantID, s.ID, len(plan.targets))
	}
	return nil
}

func (e *Enricher) insertSemanticRunTx(
	ctx context.Context,
	tx pgx.Tx,
	feedbackID int64,
	run *feedback.SemanticExtractionRun,
	where string,
) error {
	if run == nil {
		return nil
	}
	if _, err := e.repo.InsertSemanticExtractionRunTx(ctx, tx, ptrext.Indirect(run)); err != nil {
		logext.Errorf(ctx, "[%s] semantic run insert failed,feedback_id:%d,err:%+v",
			where, feedbackID, err.Error())
		return err
	}
	return nil
}

func (e *Enricher) insertOutboxRows(
	ctx context.Context,
	tx pgx.Tx,
	s domain.Snapshot,
	plan outboxPlan,
	where string,
) error {
	for _, t := range plan.targets {
		if _, err := e.outbox.Insert(ctx, tx, outboxrepo.OutboxRow{
			FeedbackID:        s.ID,
			TenantID:          s.TenantID,
			DestinationType:   t.DestinationType,
			DestinationTarget: t.URL,
			Audience:          t.Audience,
			Payload:           plan.payload,
			TraceID:           plan.traceID,
		}); err != nil {
			logext.Errorf(ctx, "[%s] outbox insert failed,feedback_id:%d,dest_type:%s,err:%+v",
				where, s.ID, t.DestinationType, err.Error())
			return fmt.Errorf("queue outbox: %w", err)
		}
	}
	return nil
}

func (e *Enricher) insertEmbeddingTask(
	ctx context.Context,
	tx pgx.Tx,
	s domain.Snapshot,
	where string,
) error {
	if e.embeddingTask == nil {
		return nil
	}
	if err := e.embeddingTask.CreateTaskTx(ctx, tx, s.ID, s.TenantID); err != nil {
		logext.Errorf(ctx, "[%s] embedding task insert failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return fmt.Errorf("queue embedding task: %w", err)
	}
	return nil
}

// insertDraftTask queues a reply-draft task in the same tx as MarkDone. The
// repo SQL gates on the tenant's reply_draft_enabled + confidence threshold,
// so this is a no-op for opted-out or low-confidence rows. A later draft-LLM
// failure is isolated in the worker and never affects this committed
// classification.
func (e *Enricher) insertDraftTask(
	ctx context.Context,
	tx pgx.Tx,
	s domain.Snapshot,
	where string,
) error {
	if e.draftTask == nil {
		return nil
	}
	if err := e.draftTask.CreateTaskTx(ctx, tx, s.ID, s.TenantID, s.ClassificationConfidence, extractTraceID(ctx)); err != nil {
		logext.Errorf(ctx, "[%s] reply draft task insert failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return fmt.Errorf("queue reply draft task: %w", err)
	}
	return nil
}

func (e *Enricher) semanticRun(
	s domain.Snapshot,
	cfg ClassifyConfig,
	result classifyResult,
) feedback.SemanticExtractionRun {
	policy := resolvePromptPolicy(cfg)
	promptLang := policy.TemplateLanguage
	detectedLang := normalizeLanguage(s.Language)
	displayLocale := s.DisplayLocale
	rationale := map[string]any{
		"summary":  result.Enriched.Rationale,
		"language": detectedLang,
	}
	if displayLocale != "" {
		rationale["display_summary"] = result.Enriched.DisplayRationale
		rationale["display_locale"] = displayLocale
	}
	languageGuard := map[string]any{
		"detected":        detectedLang,
		"detector":        languageDetectorVersion,
		"prompt_language": promptLang,
	}
	if displayLocale != "" {
		languageGuard["display_locale"] = displayLocale
	}
	model := strings.TrimSpace(result.Route.ProviderModel)
	if model == "" {
		model = strings.TrimSpace(e.model)
	}
	guardSummary := map[string]any{
		"language":      languageGuard,
		"prompt_policy": promptPolicyGuard(policy),
	}
	if cfg.PromptVersionID != "" {
		guardSummary["prompt_version_id"] = cfg.PromptVersionID
	}
	if route := semanticRouteGuard(result.Route); len(route) > 0 {
		guardSummary["routing"] = route
	}
	return feedback.SemanticExtractionRun{
		TenantID:        s.TenantID,
		SubjectType:     feedback.SemanticSubjectFeedback,
		SubjectID:       s.ID,
		DomainPack:      feedback.DefaultDomainPack,
		SchemaVersion:   feedback.DefaultSchemaVersion,
		PromptVersion:   policy.PromptVersion,
		PromptVersionID: cfg.PromptVersionID,
		Model:           model,
		GuardSummary:    guardSummary,
		Attrs:           result.Enriched.Attrs,
		Confidence:      confidenceAudit(result.Enriched),
		Rationale:       rationale,
		DroppedAttrs:    result.DroppedAttrsAudit,
	}
}

func semanticRouteGuard(route llmclient.RouteMetadata) map[string]any {
	out := map[string]any{}
	if route.LogicalModel != "" {
		out["logical_model"] = route.LogicalModel
	}
	if route.ProviderModel != "" {
		out["provider_model"] = route.ProviderModel
	}
	if route.ChannelID != "" {
		out["channel_id"] = route.ChannelID
	}
	if route.ChannelName != "" {
		out["channel_name"] = route.ChannelName
	}
	if route.Protocol != "" {
		out["protocol"] = route.Protocol
	}
	return out
}

func confidenceAudit(enriched domain.Enriched) map[string]any {
	if enriched.ClassificationConfidence == nil {
		return map[string]any{}
	}
	return map[string]any{
		"overall": ptrext.Indirect(enriched.ClassificationConfidence),
		"source":  "llm_self_report",
	}
}

// outboxDestTypes lists destination_type values that go through the
// outbox + Transport pipeline (rather than the inline-fanout slot kept
// for the #34 outbound adapter SDK). New outbox-routed dispatchers add
// themselves here and grow a sender in
// service/outbox/outbox_worker.go's sendByDestType.
var outboxDestTypes = map[string]bool{
	notifytarget.DestRawWebhook:  true,
	notifytarget.DestGitHubIssue: true,
	notifytarget.DestSlack:       true,
	notifytarget.DestLark:        true,
	notifytarget.DestDiscord:     true,
}

// selectOutboxTargets returns the destination rows that should receive
// an outbox row for this snapshot. The audience semantics (pool / radar /
// all) apply uniformly across dest types; only the set of "outbox-aware"
// dest types is filtered here.
func selectOutboxTargets(targets []notifytarget.NotifyTarget, s domain.Snapshot) []notifytarget.NotifyTarget {
	out := make([]notifytarget.NotifyTarget, 0, len(targets))
	for _, t := range targets {
		if !outboxDestTypes[t.DestinationType] {
			continue
		}
		switch t.Audience {
		case notifytarget.AudienceAll:
			out = append(out, t)
		case notifytarget.AudiencePool:
			out = append(out, t)
		case notifytarget.AudienceRadar:
			if s.IsUrgent {
				out = append(out, t)
			}
		}
	}
	return out
}

// extractTraceID pulls the trace ID from ctx (chi RequestID middleware
// or an explicit trace.WithID set by ingestor.fireEnrich). Falls back to
// a freshly generated id when ctx has neither — keeps observability
// unbroken on background poller paths.
func extractTraceID(ctx context.Context) string {
	if id := trace.FromContext(ctx); id != "" {
		return id
	}
	return trace.New()
}

// buildOutboxEnvelope marshals the v1 envelope JSON that the outbox
// worker will POST verbatim. Keeping the envelope shape here (rather
// than re-deriving it in the worker) means destination URL/secret
// rotations don't retroactively change what customers receive.
//
// traceID is embedded as the top-level "trace_id" field so customers
// can correlate the received envelope with their own logs.
//
// The #34 outbound framework uses content-hash signing which is field-order
// independent. json.Marshal preserves struct field order for readability.
func buildOutboxEnvelope(s domain.Snapshot, traceID string) ([]byte, error) {
	type enrichedOut struct {
		Title            string         `json:"title"`
		DisplayTitle     string         `json:"display_title,omitempty"`
		DisplayLocale    string         `json:"display_locale,omitempty"`
		Attrs            map[string]any `json:"attrs"`
		IsUrgent         bool           `json:"is_urgent"`
		Rationale        string         `json:"rationale"`
		DisplayRationale string         `json:"display_rationale,omitempty"`
		EnrichedAt       string         `json:"enriched_at"`
	}
	type feedbackOut struct {
		ID          int64       `json:"id"`
		TenantID    string      `json:"tenant_id"`
		Content     string      `json:"content"`
		Source      string      `json:"source"`
		UserID      string      `json:"user_id"`
		Language    string      `json:"language,omitempty"`
		SubmittedAt string      `json:"submitted_at"`
		Enriched    enrichedOut `json:"enriched"`
	}
	type envelopeOut struct {
		Version     string      `json:"version"`
		EventType   string      `json:"event_type"`
		DeliveredAt string      `json:"delivered_at"`
		TraceID     string      `json:"trace_id,omitempty"`
		Feedback    feedbackOut `json:"feedback"`
	}
	at := s.EnrichedAt.UTC().Format(time.RFC3339)
	submittedAt := s.SubmittedAt.UTC().Format(time.RFC3339)
	attrs := s.Attrs
	if attrs == nil {
		attrs = map[string]any{}
	}
	env := envelopeOut{
		Version:     "2", // E3 metadata-driven dims: enriched = {title, attrs, is_urgent, rationale}
		EventType:   "feedback.enriched",
		DeliveredAt: at,
		TraceID:     traceID,
		Feedback: feedbackOut{
			ID:          s.ID,
			TenantID:    s.TenantID,
			Content:     s.Content,
			Source:      s.Source,
			UserID:      s.UserID,
			Language:    s.Language,
			SubmittedAt: submittedAt, // #82: actual ingest time, not enrichment time
			Enriched: enrichedOut{
				Title:            s.Title,
				DisplayTitle:     s.DisplayTitle,
				DisplayLocale:    s.DisplayLocale,
				Attrs:            attrs,
				IsUrgent:         s.IsUrgent,
				Rationale:        s.Rationale,
				DisplayRationale: s.DisplayRationale,
				EnrichedAt:       at,
			},
		},
	}
	return json.Marshal(env)
}
