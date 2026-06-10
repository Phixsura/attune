package llmaudit

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	biztrace "github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditrepo "github.com/Phixsura/attune/internal/repo/llmaudit"
)

const auditInsertTimeout = 2 * time.Second

type recorder interface {
	Insert(ctx context.Context, row auditrepo.Row) error
}

// Client records provider-call token/cost facts around an LLMClient.
type Client struct {
	next     llmclient.LLMClient
	recorder recorder
}

func NewClient(next llmclient.LLMClient, recorder recorder) *Client {
	return ptrext.Of(Client{next: next, recorder: recorder})
}

func (c *Client) Complete(ctx context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	const where = "service.llmaudit.Client.Complete"
	if c == nil || c.next == nil {
		return llmclient.CompletionResponse{}, fmt.Errorf("llm audit: nil client")
	}
	start := time.Now()
	resp, err := c.next.Complete(ctx, req)
	latency := time.Since(start)
	if c.recorder != nil {
		row := auditRow(ctx, req, resp, err, latency)
		recordMetrics(row)
		insertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditInsertTimeout)
		defer cancel()
		if insertErr := c.recorder.Insert(insertCtx, row); insertErr != nil {
			logext.Warnf(ctx, "[%s] audit insert failed,tenant_id:%s,feedback_id:%d,purpose:%s,inbound_trace_id:%s,otel_trace_id:%s,model:%s,err:%+v",
				where, row.TenantID, row.FeedbackID, row.Purpose, row.InboundTraceID,
				row.OtelTraceID, row.ModelID, insertErr.Error())
		}
	} else {
		recordMetrics(auditRow(ctx, req, resp, err, latency))
	}
	return resp, err
}

func (c *Client) Close() error {
	if c == nil || c.next == nil {
		return nil
	}
	return c.next.Close()
}

func recordMetrics(row auditrepo.Row) {
	model := metricModelLabel(row.ModelID)
	metrics.LLMCallsTotal.WithLabelValues(row.TenantID, model, row.Status).Inc()
	metrics.LLMTokensTotal.WithLabelValues(row.TenantID, model, "prompt").
		Add(float64(nonNegativeI32(row.PromptTokens)))
	metrics.LLMTokensTotal.WithLabelValues(row.TenantID, model, "completion").
		Add(float64(nonNegativeI32(row.CompletionTokens)))
	if row.CostUSD > 0 {
		metrics.LLMCostUSDTotal.WithLabelValues(row.TenantID, model).Add(row.CostUSD)
	}
}

func nonNegativeI32(n int32) int32 {
	if n < 0 {
		return 0
	}
	return n
}

func auditRow(
	ctx context.Context,
	req llmclient.CompletionRequest,
	resp llmclient.CompletionResponse,
	err error,
	latency time.Duration,
) auditrepo.Row {
	status := "ok"
	errText := ""
	if err != nil {
		status = "error"
		errText = safeError(err)
	}
	price := llmclient.PriceUsage(req.Model, resp.Usage)
	return auditrepo.Row{
		TenantID:         labelOrUnknown(req.Guard.TenantID),
		FeedbackID:       req.Guard.FeedbackID,
		InboundTraceID:   biztrace.FromContext(ctx),
		OtelTraceID:      otelTraceID(ctx),
		ModelID:          strings.TrimSpace(req.Model),
		Purpose:          labelOrUnknown(req.Guard.Purpose),
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		CostUSD:          price.CostUSD,
		Status:           status,
		Error:            errText,
		LatencyMS:        int(latency.Milliseconds()),
	}
}

func labelOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return strings.TrimSpace(s)
}

func truncateError(s string) string {
	const limit = 512
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "...(truncated)"
}

func otelTraceID(ctx context.Context) string {
	span := oteltrace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().TraceID().String()
}

var statusCodeRE = regexp.MustCompile(`(?i)\bstatus(?: code)?[:= ]+([0-9]{3})\b`)

func safeError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case strings.Contains(msg, "llm guard blocked request"):
		return "guard_blocked"
	}
	if match := statusCodeRE.FindStringSubmatch(msg); len(match) == 2 {
		return "provider_status_" + match[1]
	}
	if looksLikeProviderBody(msg) {
		return "provider_error"
	}
	return truncateError(msg)
}

func looksLikeProviderBody(msg string) bool {
	return strings.ContainsAny(msg, "{}\n\r")
}

func metricModelLabel(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return "unknown"
	}
	if _, ok := llmclient.LookupModelPrice(trimmed); ok {
		return trimmed
	}
	return "custom"
}
