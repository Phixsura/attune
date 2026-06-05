package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/repo"
)

// Ingestor is the business-layer entry point for "a new feedback row
// just arrived". It validates, persists via repo, and triggers async
// enrichment. Handlers depend on this concrete type (not an interface)
// because there is one ingest pipeline today.
type Ingestor struct {
	repo     *repo.FeedbackRepo
	enricher *Enricher
}

func NewIngestor(r *repo.FeedbackRepo, e *Enricher) *Ingestor {
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
		// Capture inbound trace_id (业务: Lark / customer webhook trace_id)
		// + OTel SpanContext(自家 trace_id),让 async enrich goroutine
		// 同时继承"客户 trace"和"自家 OTel trace",enricher 调 gateway
		// LLM 时 traceparent 透传,SLS 上 trace_id 串联 attune + gateway。
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
// traceID(业务): inbound webhook trace id,透传到 customer envelope。
// inboundCtx: 原始 HTTP ctx,我们从中提取 OTel SpanContext,detach 后接到
// 新的 timeout ctx 上,让 enricher 调 gateway 时 OTel trace_id 串联。
//
// Errors are only logged — the row stays 'pending' and the background
// poller will pick it up on the next tick.
func (i *Ingestor) fireEnrich(inboundCtx context.Context, id int64, traceID string) {
	// Detach OTel SpanContext 到新的 bounded ctx:
	//   - 新 ctx 不受 inbound HTTP request 关闭影响(60s timeout)
	//   - OTel SpanContext 跟着走,trace_id 保持串联
	//   - 业务 trace_id (Lark / customer 来源) 仍通过 trace.WithID 传
	span := oteltrace.SpanFromContext(inboundCtx)
	ctx, cancel := context.WithTimeout(
		oteltrace.ContextWithSpanContext(context.Background(), span.SpanContext()),
		60*time.Second,
	)
	defer cancel()
	ctx = trace.WithID(ctx, traceID)
	if err := i.enricher.EnrichOne(ctx, id); err != nil {
		slog.WarnContext(ctx, "inline enrich failed",
			"id", id, "inbound_trace_id", traceID, "err", err)
	}
}
