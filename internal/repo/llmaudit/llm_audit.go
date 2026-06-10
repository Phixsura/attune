package llmaudit

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Repo owns llm_audit writes and aggregate reads.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

// Row is one provider call audit fact.
type Row struct {
	TenantID         string
	FeedbackID       int64
	InboundTraceID   string
	OtelTraceID      string
	ModelID          string
	Purpose          string
	PromptTokens     int32
	CompletionTokens int32
	CostUSD          float64
	Status           string
	Error            string
	LatencyMS        int
}

func (r *Repo) Insert(ctx context.Context, row Row) error {
	if row.Status == "" {
		row.Status = "ok"
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO llm_audit
		 (tenant_id, feedback_id, inbound_trace_id, otel_trace_id, model_id, purpose, prompt_tokens,
		  completion_tokens, cost_usd, status, error, latency_ms)
		VALUES ($1, NULLIF($2, 0), $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		row.TenantID, nonNegativeInt64(row.FeedbackID), row.InboundTraceID, row.OtelTraceID,
		row.ModelID, row.Purpose, nonNegativeI32(row.PromptTokens),
		nonNegativeI32(row.CompletionTokens), row.CostUSD, row.Status, row.Error,
		nonNegativeInt(row.LatencyMS),
	)
	if err != nil {
		return fmt.Errorf("insert llm audit: %w", err)
	}
	return nil
}

type UsageGranularity string

const (
	GranularityDay   UsageGranularity = "day"
	GranularityWeek  UsageGranularity = "week"
	GranularityMonth UsageGranularity = "month"
)

type UsageBucket struct {
	Bucket           time.Time
	TenantID         string
	ModelID          string
	PromptTokens     int64
	CompletionTokens int64
	CostUSD          float64
	Calls            int64
	Errors           int64
}

func (r *Repo) UsageByTenant(
	ctx context.Context,
	tenantID string,
	granularity UsageGranularity,
	from, to time.Time,
) ([]UsageBucket, error) {
	if granularity == "" {
		granularity = GranularityWeek
	}
	if !validGranularity(granularity) {
		return nil, fmt.Errorf("invalid granularity %q", granularity)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT date_trunc($4, created_at) AS bucket,
		 tenant_id,
		 model_id,
		 COALESCE(SUM(prompt_tokens), 0),
		 COALESCE(SUM(completion_tokens), 0),
		 COALESCE(SUM(cost_usd), 0)::float8,
		 COUNT(*),
		 COUNT(*) FILTER (WHERE status = 'error')
		 FROM llm_audit
		 WHERE tenant_id = $1
		 AND created_at >= $2
		 AND created_at < $3
		 GROUP BY bucket, tenant_id, model_id
		 ORDER BY bucket ASC, model_id ASC`,
		tenantID, from, to, string(granularity),
	)
	if err != nil {
		return nil, fmt.Errorf("llm usage by tenant: %w", err)
	}
	defer rows.Close()
	var out []UsageBucket
	for rows.Next() {
		var b UsageBucket
		if err := rows.Scan(
			&b.Bucket, &b.TenantID, &b.ModelID, &b.PromptTokens,
			&b.CompletionTokens, &b.CostUSD, &b.Calls, &b.Errors,
		); err != nil {
			return nil, fmt.Errorf("scan llm usage bucket: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func validGranularity(g UsageGranularity) bool {
	return g == GranularityDay || g == GranularityWeek || g == GranularityMonth
}

func nonNegativeI32(n int32) int32 {
	if n < 0 {
		return 0
	}
	return n
}

func nonNegativeInt(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func nonNegativeInt64(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}
