package feedback

import (
	"context"
	"encoding/json"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// FeedbackHandler serves /fb/v1/console/feedback. All queries scope to the
// session's tenant via FromContext — never taking tenant_id from query params.
//
// The handler also reads the tenant's EnrichConfig to translate the
// stable wire query (`?type=bug&severity=critical&labels=payment`)
// into the per-dim AttrFilter shape the repo's JSONB containment
// queries expect.
type FeedbackHandler struct {
	repo         feedbackRepo
	tenants      tenantConfigRepo
	drafter      Drafter            // optional reply-draft generator; nil disables Regenerate
	regenLimiter *ratelimit.Limiter // optional per-tenant rate limit on Regenerate; nil disables
}

// Drafter regenerates a reply draft synchronously, sharing the worker's
// Generate core. Optional — nil disables the Regenerate endpoint. Precheck
// supplies the tenant-scoped guard inputs (ownership / enrichment status /
// opt-in / last-drafted time) so the handler enforces them — including the
// per-row cooldown — before spending an LLM call.
type Drafter interface {
	Precheck(ctx context.Context, feedbackID int64, tenantID string) (status string, enabled, found bool, lastGeneratedAt *time.Time, err error)
	Generate(ctx context.Context, feedbackID int64, tenantID string) (string, time.Time, error)
}

// SetDrafter wires the reply-draft generator used by Regenerate.
func (h *FeedbackHandler) SetDrafter(d Drafter) { h.drafter = d }

// SetRegenLimiter wires the per-tenant token-bucket that backstops the
// synchronous Regenerate endpoint: the per-row cooldown caps one row, this caps
// a tenant's total regenerations so a script can't spread unbounded LLM spend
// across many owned rows. nil leaves Regenerate unlimited.
func (h *FeedbackHandler) SetRegenLimiter(l *ratelimit.Limiter) { h.regenLimiter = l }

type feedbackRepo interface {
	ListForConsole(ctx context.Context, tenantID string, opts feedback.ConsoleListOpts) ([]feedback.ConsoleListRow, error)
	GetForConsole(ctx context.Context, tenantID string, id int64) (*feedback.ConsoleDetailRow, error)
	UsageByDay(ctx context.Context, tenantID string, from, to time.Time) ([]feedback.UsageBucket, error)
	UrgentCount(ctx context.Context, tenantID string, from, to time.Time) (int64, error)
	TopValuesByDim(ctx context.Context, tenantID, dim string, multi bool, from, to time.Time, limit int) ([]feedback.ValueCount, error)
}

type tenantConfigRepo interface {
	GetEnrichConfig(ctx context.Context, tenantID string) (tenant.EnrichConfig, error)
}

func NewFeedbackHandler(r *feedback.FeedbackRepo, t *tenant.TenantRepo) *FeedbackHandler {
	return ptrext.Of(FeedbackHandler{repo: r, tenants: t})
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return ptrext.Of(s)
}

// attrsToStruct decodes the raw JSONB attrs payload into a structpb
// for proto wire output. Empty / missing returns an empty Struct so
// the SPA can rely on the field always being present.
func attrsToStruct(raw []byte) *structpb.Struct {
	if len(raw) == 0 {
		s, _ := structpb.NewStruct(map[string]any{})
		return s
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		s, _ := structpb.NewStruct(map[string]any{})
		return s
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		s2, _ := structpb.NewStruct(map[string]any{})
		return s2
	}
	return s
}

func toProtoFeedback(row feedback.ConsoleListRow) *attunev1.Feedback {
	return ptrext.Of(attunev1.Feedback{
		Id:                       row.ID,
		Content:                  row.Content,
		Source:                   row.Source,
		Type:                     row.Type,
		UserId:                   row.UserID,
		Language:                 nullableString(row.Language),
		PageUrl:                  row.PageURL,
		EnrichedTitle:            nullableString(row.EnrichedTitle),
		EnrichedDisplayTitle:     nullableString(row.EnrichedDisplayTitle),
		EnrichedDisplayLocale:    nullableString(row.EnrichedDisplayLocale),
		EnrichedAttrs:            attrsToStruct(row.EnrichedAttrs),
		IsUrgent:                 row.IsUrgent,
		ClassificationConfidence: row.ClassificationConfidence,
		EnrichmentStatus:         row.EnrichmentStatus,
		CreatedAt:                row.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// extractAttrFilters walks the query string for keys matching a
// Dimension.Name in the tenant's set, building one AttrFilter per
// value. Each per-dim filter is AND-composed downstream; multiple
// values for the same dim become multiple filters (also AND-composed,
// effectively requiring all of them).
func extractAttrFilters(q map[string][]string, dims []domain.Dimension) []feedback.AttrFilter {
	if len(dims) == 0 || len(q) == 0 {
		return nil
	}
	dimIdx := make(map[string]domain.Dimension, len(dims))
	for _, d := range dims {
		dimIdx[d.Name] = d
	}
	var out []feedback.AttrFilter
	for k, vs := range q {
		d, ok := dimIdx[k]
		if !ok {
			continue
		}
		for _, v := range vs {
			if v == "" {
				continue
			}
			out = append(out, feedback.AttrFilter{
				Dim:   d.Name,
				Value: v,
				Multi: d.Kind == domain.DimMulti,
			})
		}
	}
	return out
}

func attrFiltersFromProto(filters []*attunev1.AttrFilter, dims []domain.Dimension) []feedback.AttrFilter {
	if len(filters) == 0 {
		return nil
	}
	q := make(map[string][]string, len(filters))
	for _, f := range filters {
		q[f.GetDim()] = append(q[f.GetDim()], f.GetValue())
	}
	return extractAttrFilters(q, dims)
}
