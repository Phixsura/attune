// Package repo — console-scoped projections of user_feedback. Kept
// separate from feedback.go so the ingest-path SQL stays decoupled
// from the read-path SQL (only the console reads the latter). The
// analytics/aggregate queries live in feedback_console_stats.go.
package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/logext"
)

// AttrFilter is one per-dim filter in a console list query. The Dim
// is the stable Dimension.Name; the Value is the stable
// Taxonomy.Value. For multi-kind dims, the SQL is
// `enriched_attrs @> '{"<dim>": ["<value>"]}'`; for single-kind dims,
// `enriched_attrs @> '{"<dim>": "<value>"}'`. The caller knows the
// kind via the tenant's DimensionSet.
type AttrFilter struct {
	Dim   string
	Value string
	Multi bool
}

// ConsoleListOpts is the filter set the console UI sends. Each field
// is optional; empty means no filter. Limit is bounded by the handler.
type ConsoleListOpts struct {
	Attrs  []AttrFilter // per-dim filters, AND-composed via JSONB containment
	Urgent *bool        // nil = no filter; true = is_urgent only; false = not urgent only
	Q      string       // ILIKE on content + enriched_title
	Cursor int64        // last id seen; 0 = first page
	Limit  int
}

// ConsoleListRow is the projection sent to the console list view.
// EnrichedAttrs is returned as a raw JSONB byte slice — the handler
// decodes it into a structpb.Struct for the proto wire shape.
type ConsoleListRow struct {
	ID               int64
	Content          string
	Source           string
	Type             string
	UserID           string
	PageURL          string
	EnrichedTitle    string
	EnrichedAttrs    []byte // raw JSONB
	IsUrgent         bool
	EnrichmentStatus string
	CreatedAt        time.Time
}

// ListForConsole returns one page newest-first, scoped to tenant. Uses
// the idx_user_feedback_tenant_created index for the ORDER BY and the
// jsonb_path_ops GIN index for any AttrFilter containment.
func (r *FeedbackRepo) ListForConsole(
	ctx context.Context, tenantID string, opts ConsoleListOpts,
) ([]ConsoleListRow, error) {
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 50
	}
	args := []any{tenantID}
	where := "WHERE tenant_id = $1"
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if opts.Cursor > 0 {
		where += " AND id < " + addArg(opts.Cursor)
	}
	for _, f := range opts.Attrs {
		if f.Dim == "" || f.Value == "" {
			continue
		}
		clause, err := containmentClause(f)
		if err != nil {
			return nil, err
		}
		where += " AND enriched_attrs @> " + addArg(clause) + "::jsonb"
	}
	if opts.Urgent != nil {
		where += " AND is_urgent = " + addArg(*opts.Urgent)
	}
	if opts.Q != "" {
		p := addArg("%" + opts.Q + "%")
		where += " AND (content ILIKE " + p + " OR enriched_title ILIKE " + p + ")"
	}
	sql := `
		SELECT id, content, source, type, user_id, page_url,
		 COALESCE(enriched_title, ''),
		 COALESCE(enriched_attrs, '{}'::jsonb),
		 is_urgent,
		 enrichment_status,
		 created_at
		 FROM user_feedback
		 ` + where + `
		 ORDER BY id DESC
		 LIMIT ` + addArg(opts.Limit)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		const where = "repo.FeedbackRepo.ListForConsole"
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return nil, fmt.Errorf("list feedback for console: %w", err)
	}
	defer rows.Close()
	var out []ConsoleListRow
	for rows.Next() {
		var row ConsoleListRow
		if err := rows.Scan(
			&row.ID, &row.Content, &row.Source, &row.Type, &row.UserID, &row.PageURL,
			&row.EnrichedTitle, &row.EnrichedAttrs, &row.IsUrgent,
			&row.EnrichmentStatus, &row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan feedback row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// containmentClause builds a JSONB fragment for the `@>` operator from
// one AttrFilter. We marshal through encoding/json so any Unicode in
// the dim name / value is escaped correctly — never concatenate raw.
func containmentClause(f AttrFilter) ([]byte, error) {
	var payload map[string]any
	if f.Multi {
		payload = map[string]any{f.Dim: []string{f.Value}}
	} else {
		payload = map[string]any{f.Dim: f.Value}
	}
	return json.Marshal(payload)
}

// ConsoleDetailRow extends ConsoleListRow with full content (source_meta,
// attachments, enrichment_error, enriched_at, enriched_rationale).
type ConsoleDetailRow struct {
	ConsoleListRow
	SourceMeta        []byte // raw JSONB
	Attachments       []byte // raw JSONB
	EnrichmentError   string
	EnrichedAt        *time.Time
	EnrichedRationale string // LLM's short "why these values"; empty when not classified
}

// ErrFeedbackNotFound returned by GetForConsole when id doesn't match
// the tenant (cross-tenant query safety).
var ErrFeedbackNotFound = errors.New("feedback not found")

func (r *FeedbackRepo) GetForConsole(
	ctx context.Context, tenantID string, id int64,
) (*ConsoleDetailRow, error) {
	var row ConsoleDetailRow
	err := r.pool.QueryRow(
		ctx, `
		SELECT id, content, source, type, user_id, page_url,
		 COALESCE(enriched_title, ''),
		 COALESCE(enriched_attrs, '{}'::jsonb),
		 is_urgent,
		 enrichment_status, created_at,
		 source_meta, attachments,
		 COALESCE(enrichment_error, ''),
		 enriched_at,
		 COALESCE(enriched_rationale, '')
		 FROM user_feedback
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	).Scan(
		&row.ID, &row.Content, &row.Source, &row.Type, &row.UserID, &row.PageURL,
		&row.EnrichedTitle, &row.EnrichedAttrs, &row.IsUrgent,
		&row.EnrichmentStatus, &row.CreatedAt,
		&row.SourceMeta, &row.Attachments,
		&row.EnrichmentError, &row.EnrichedAt,
		&row.EnrichedRationale,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFeedbackNotFound
	}
	if err != nil {
		const where = "repo.FeedbackRepo.GetForConsole"
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,id:%d,err:%+v",
			where, tenantID, id, err.Error())
		return nil, fmt.Errorf("get feedback for console: %w", err)
	}
	return &row, nil
}
