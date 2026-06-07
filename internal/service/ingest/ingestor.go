package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Ingestor is the business-layer entry point for "a new feedback row
// just arrived". It validates, persists via repo, and triggers async
// enrichment. Handlers depend on this concrete type (not an interface)
// because there is one ingest pipeline today.
type Ingestor struct {
	repo     *feedback.FeedbackRepo
	enricher *enrich.Enricher
}

func NewIngestor(r *feedback.FeedbackRepo, e *enrich.Enricher) *Ingestor {
	return &Ingestor{repo: r, enricher: e}
}

// IngestRow validates input, persists it, and fires off best-effort
// enrichment. Returns the new row id. tenantID is the TEXT tenants.id;
// keyID is the UUID of the api key used to authenticate (uuid.Nil for
// non-API-key sources like the Lark webhook).
func (i *Ingestor) IngestRow(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
	const where = "service.Ingestor.IngestRow"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,key_id:%s,source:%s,content_len:%d",
		where, tenantID, keyID, in.Source, len(in.Content))
	if err := in.Validate(); err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,source:%s,err:%s",
			where, tenantID, in.Source, err.Error())
		return 0, err
	}
	userID := composeUserID(keyID, in.SourceUser)
	id, err := i.repo.Insert(ctx, tenantID, userID, in)
	if err != nil {
		logext.Errorf(ctx, "[%s] repo.Insert failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return 0, fmt.Errorf("repo insert: %w", err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d", where, tenantID, id)
	if i.enricher != nil {
		// Capture the inbound trace_id (Lark / customer webhook) and
		// the OTel SpanContext (attune's own trace) so the async
		// enrich goroutine inherits both. Downstream the enricher
		// propagates traceparent on its LLM call, which lets the
		// trace backend join attune's spans with the gateway's.
		go i.fireEnrich(ctx, id, trace.FromContext(ctx))
	}
	return id, nil
}

// composeUserID prefixes every external-source row with the api key
// uuid so we can trace which key submitted what, while preserving any
// upstream user id (e.g. Lark open_id) for later support lookups.
func composeUserID(keyID uuid.UUID, sourceUser string) string {
	uid := "ext_" + keyID.String()
	if sourceUser != "" {
		uid = uid + ":" + sourceUser
	}
	return uid
}

// fireEnrich runs enrichment in a fresh, bounded context so a slow LLM
// call cannot pin the inbound HTTP request goroutine.
//
// traceID is the inbound webhook trace id; we propagate it onto the
// customer envelope downstream.
// inboundCtx is the original HTTP request ctx. We extract the OTel
// SpanContext, detach it from the HTTP cancellation, and reattach it
// onto a fresh timeout ctx so the enricher's outbound call shares the
// inbound trace.
//
// Errors are only logged — the row stays 'pending' and the background
// poller will pick it up on the next tick.
func (i *Ingestor) fireEnrich(inboundCtx context.Context, id int64, traceID string) {
	// Detach the OTel SpanContext onto a fresh bounded ctx:
	// - the new ctx survives the inbound HTTP request closing (60s timeout);
	// - the OTel SpanContext rides along, keeping trace_id stitched;
	// - the inbound business trace_id (Lark / customer) is propagated via trace.WithID.
	span := oteltrace.SpanFromContext(inboundCtx)
	ctx, cancel := context.WithTimeout(
		oteltrace.ContextWithSpanContext(context.Background(), span.SpanContext()),
		90*time.Second,
	)
	defer cancel()
	ctx = trace.WithID(ctx, traceID)
	if err := i.enricher.EnrichOne(ctx, id); err != nil {
		slog.WarnContext(ctx, "inline enrich failed",
			"id", id, "inbound_trace_id", traceID, "err", err)
	}
}
