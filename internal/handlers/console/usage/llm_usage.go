package usage

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/llmaudit"
)

func BindLLMUsageRequest(r *http.Request, req *attunev1.GetLLMUsageRequest) error {
	q := r.URL.Query()
	granularity, err := bindUsageGranularity(q.Get("granularity"))
	if err != nil {
		return err
	}
	rangeExpr, err := bindUsageRange(q.Get("range"))
	if err != nil {
		return err
	}
	req.Granularity = granularity
	req.Range = rangeExpr
	return nil
}

func (h *UsageHandler) GetLLMUsage(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetLLMUsageRequest,
) (dispatcher.Result[*attunev1.GetLLMUsageResponse], error) {
	const where = "console.UsageHandler.GetLLMUsage"
	if h.llm == nil {
		return dispatcher.Fail[*attunev1.GetLLMUsageResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"llm usage repository not configured",
		)
	}
	granularity := repoGranularity(req.GetGranularity())
	now := time.Now().UTC()
	from, err := rangeStart(req.GetRange(), now)
	if err != nil {
		return dispatcher.Fail[*attunev1.GetLLMUsageResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
	}
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s,granularity:%s,from:%s,to:%s",
		where, auth.TenantID, granularity, from.Format(time.RFC3339), now.Format(time.RFC3339))

	rows, err := h.llm.UsageByTenant(ctx, auth.TenantID, granularity, from, now)
	if err != nil {
		logext.Errorf(ctx, "[%s] llm.UsageByTenant failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetLLMUsageResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read llm usage")
	}

	resp := ptrext.Of(attunev1.GetLLMUsageResponse{
		PeriodStart: from.Format(time.RFC3339),
		PeriodEnd:   now.Format(time.RFC3339),
		Granularity: string(granularity),
		Series:      make([]*attunev1.LLMUsageBucket, 0, len(rows)),
	})
	for _, row := range rows {
		resp.Series = append(resp.Series, ptrext.Of(attunev1.LLMUsageBucket{
			Bucket:           row.Bucket.UTC().Format(time.RFC3339),
			TenantId:         row.TenantID,
			ModelId:          row.ModelID,
			PromptTokens:     row.PromptTokens,
			CompletionTokens: row.CompletionTokens,
			CostUsd:          row.CostUSD,
			Calls:            row.Calls,
			Errors:           row.Errors,
		}))
		resp.PromptTokens += row.PromptTokens
		resp.CompletionTokens += row.CompletionTokens
		resp.CostUsd += row.CostUSD
		resp.Calls += row.Calls
		resp.Errors += row.Errors
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,calls:%d,cost_usd:%f",
		where, auth.TenantID, resp.Calls, resp.CostUsd)
	return dispatcher.OK(resp)
}

func bindUsageGranularity(raw string) (attunev1.UsageGranularity, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "week", "usage_granularity_week":
		return attunev1.UsageGranularity_USAGE_GRANULARITY_WEEK, nil
	case "day", "usage_granularity_day":
		return attunev1.UsageGranularity_USAGE_GRANULARITY_DAY, nil
	case "month", "usage_granularity_month":
		return attunev1.UsageGranularity_USAGE_GRANULARITY_MONTH, nil
	default:
		return attunev1.UsageGranularity_USAGE_GRANULARITY_UNSPECIFIED,
			dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "granularity must be day, week, or month")
	}
}

func repoGranularity(granularity attunev1.UsageGranularity) llmaudit.UsageGranularity {
	switch granularity {
	case attunev1.UsageGranularity_USAGE_GRANULARITY_DAY:
		return llmaudit.GranularityDay
	case attunev1.UsageGranularity_USAGE_GRANULARITY_MONTH:
		return llmaudit.GranularityMonth
	default:
		return llmaudit.GranularityWeek
	}
}

type usageRangeSpec struct {
	expr  string
	start func(time.Time) time.Time
}

var usageRangeSpecs = map[string]usageRangeSpec{
	"":        {expr: "now-30d", start: daysBefore(30)},
	"7d":      {expr: "now-7d", start: daysBefore(7)},
	"30d":     {expr: "now-30d", start: daysBefore(30)},
	"90d":     {expr: "now-90d", start: daysBefore(90)},
	"month":   {expr: "now/M", start: startOfMonth},
	"now-7d":  {expr: "now-7d", start: daysBefore(7)},
	"now-30d": {expr: "now-30d", start: daysBefore(30)},
	"now-90d": {expr: "now-90d", start: daysBefore(90)},
	"now/M":   {expr: "now/M", start: startOfMonth},
}

func bindUsageRange(raw string) (string, error) {
	spec, ok := lookupUsageRange(raw)
	if !ok {
		return "", dispatcher.NewError(
			http.StatusBadRequest,
			attunev1.ErrorCode_BAD_REQUEST,
			"range must be now-7d, now-30d, now-90d, or now/M",
		)
	}
	return spec.expr, nil
}

func rangeStart(raw string, now time.Time) (time.Time, error) {
	spec, ok := lookupUsageRange(raw)
	if !ok {
		return time.Time{}, fmt.Errorf("range must be now-7d, now-30d, now-90d, or now/M")
	}
	return spec.start(now), nil
}

func lookupUsageRange(raw string) (usageRangeSpec, bool) {
	token := strings.TrimSpace(raw)
	if spec, ok := usageRangeSpecs[token]; ok {
		return spec, true
	}
	spec, ok := usageRangeSpecs[strings.ToLower(token)]
	return spec, ok
}

func daysBefore(days int) func(time.Time) time.Time {
	return func(now time.Time) time.Time {
		return now.AddDate(0, 0, -days)
	}
}

func startOfMonth(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}
