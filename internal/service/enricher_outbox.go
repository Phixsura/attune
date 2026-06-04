package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Phixsura/listen/internal/domain"
	"github.com/Phixsura/listen/internal/infra/trace"
	"github.com/Phixsura/listen/internal/logext"
	"github.com/Phixsura/listen/internal/repo"
)

// persistEnriched flips user_feedback to 'done' and (when outbox is
// wired) inserts one outbox row per active raw-webhook destination,
// all in a single tx. If outbox isn't wired, falls back to the
// single-statement MarkDone — preserves the Wave 1.1 path for dev
// environments without outbox setup.
func (e *Enricher) persistEnriched(
	ctx context.Context,
	s domain.Snapshot,
	enriched domain.Enriched,
) error {
	const where = "service.Enricher.persistEnriched"
	if e.outbox == nil || e.targets == nil {
		return e.repo.MarkDone(ctx, s.ID, enriched)
	}

	// Look up active destinations BEFORE opening tx — list query
	// doesn't need atomicity with the UPDATE/INSERT.
	allTargets, err := e.targets.ListActiveByTenant(ctx, s.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list notify targets failed,tenant_id:%s,err:%+v",
			where, s.TenantID, err.Error())
		return fmt.Errorf("list notify targets: %w", err)
	}
	selected := selectOutboxTargets(allTargets, s)
	traceID := extractTraceID(ctx)
	payload, err := buildOutboxEnvelope(s, traceID)
	if err != nil {
		logext.Errorf(ctx, "[%s] build envelope failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return fmt.Errorf("build outbox envelope: %w", err)
	}

	tx, err := e.repo.BeginTx(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] begin tx failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := e.repo.MarkDoneTx(ctx, tx, s.ID, enriched); err != nil {
		logext.Errorf(ctx, "[%s] MarkDoneTx failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return err
	}
	for _, t := range selected {
		if _, err := e.outbox.Insert(ctx, tx, repo.OutboxRow{
			FeedbackID:        s.ID,
			TenantID:          s.TenantID,
			DestinationType:   t.DestinationType,
			DestinationTarget: t.URL,
			Audience:          t.Audience,
			Payload:           payload,
			TraceID:           traceID,
		}); err != nil {
			logext.Errorf(ctx, "[%s] outbox insert failed,feedback_id:%d,dest_type:%s,err:%+v",
				where, s.ID, t.DestinationType, err.Error())
			return fmt.Errorf("queue outbox: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit tx failed,feedback_id:%d,err:%+v",
			where, s.ID, err.Error())
		return fmt.Errorf("commit enrich tx: %w", err)
	}
	if len(selected) > 0 {
		slog.InfoContext(ctx, "outbox rows queued",
			"inbound_trace_id", traceID,
			"tenant_id", s.TenantID,
			"feedback_id", s.ID,
			"count", len(selected))
	}
	return nil
}

// outboxDestTypes lists destination_type values that go through the
// outbox + Transport pipeline (rather than inline fan-out like lark-bot).
// New outbox-routed dispatchers add themselves here and grow a sender in
// service/outbox_worker.go's sendByDestType. Sprint 1 adds github-issue.
var outboxDestTypes = map[string]bool{
	repo.DestRawWebhook:  true,
	repo.DestGitHubIssue: true,
}

// selectOutboxTargets returns the destination rows that should receive
// an outbox row for this snapshot. The audience semantics (pool / radar /
// all) apply uniformly across dest types; only the set of "outbox-aware"
// dest types is filtered here.
func selectOutboxTargets(targets []repo.NotifyTarget, s domain.Snapshot) []repo.NotifyTarget {
	out := make([]repo.NotifyTarget, 0, len(targets))
	for _, t := range targets {
		if !outboxDestTypes[t.DestinationType] {
			continue
		}
		switch t.Audience {
		case repo.AudienceAll:
			out = append(out, t)
		case repo.AudiencePool:
			out = append(out, t)
		case repo.AudienceRadar:
			if s.IsHighSeverity() {
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
// Field order + names MUST match notify/raw_webhook.go's inline path
// because customer verifiers may rely on canonical JSON. json.Marshal
// preserves struct field order — change with caution.
func buildOutboxEnvelope(s domain.Snapshot, traceID string) ([]byte, error) {
	type enrichedOut struct {
		Title      string   `json:"title"`
		Kind       string   `json:"kind"`
		Severity   string   `json:"severity"`
		Modules    []string `json:"modules"`
		Priority   float64  `json:"priority"`
		Rationale  string   `json:"rationale"`
		EnrichedAt string   `json:"enriched_at"`
	}
	type feedbackOut struct {
		ID          int64       `json:"id"`
		TenantID    string      `json:"tenant_id"`
		Content     string      `json:"content"`
		Source      string      `json:"source"`
		UserID      string      `json:"user_id"`
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
	env := envelopeOut{
		Version:     "1",
		EventType:   "feedback.enriched",
		DeliveredAt: at,
		TraceID:     traceID,
		Feedback: feedbackOut{
			ID:          s.ID,
			TenantID:    s.TenantID,
			Content:     s.Content,
			Source:      s.Source,
			UserID:      s.UserID,
			SubmittedAt: at,
			Enriched: enrichedOut{
				Title:      s.Title,
				Kind:       s.Kind,
				Severity:   s.Severity,
				Modules:    s.Modules,
				Priority:   s.Priority,
				EnrichedAt: at,
			},
		},
	}
	return json.Marshal(env)
}
