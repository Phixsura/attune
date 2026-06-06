// Package repo — console-scoped projections of user_feedback.
// Kept separate from feedback.go to honor the attune ≤300-line file
// rule (CLAUDE.md 律 2) and to keep "ingest path SQL" decoupled from
// "read path SQL" — the latter only the console reads.
//
// This file holds the list + detail read path. The analytics/aggregate
// queries (usage-by-day, kind counts, top modules) live in
// feedback_console_stats.go to keep both files under the line cap.
package feedback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/logext"
)

// ConsoleListOpts is the filter set the console UI sends. Each field
// is optional; empty means no filter. Limit is bounded by the handler.
type ConsoleListOpts struct {
	Kind     string // enriched_kind exact match
	Severity string // enriched_severity exact match
	Q        string // ILIKE on content + enriched_title
	Cursor   int64  // last id seen; 0 = first page
	Limit    int
}

// ConsoleListRow is the projection sent to the console list view.
type ConsoleListRow struct {
	ID               int64
	Content          string
	Source           string
	Type             string
	UserID           string
	PageURL          string
	EnrichedTitle    string
	EnrichedKind     string
	EnrichedSeverity string
	EnrichedModules  []byte // raw JSONB; DTO unmarshals to []string
	EnrichedPriority *float64
	EnrichmentStatus string
	CreatedAt        time.Time
}

// ListForConsole returns one page newest-first, scoped to tenant. Uses
// the idx_user_feedback_tenant_created index. Filters compose
// conjunctively; empty string = no filter.
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
	if opts.Kind != "" {
		where += " AND enriched_kind = " + addArg(opts.Kind)
	}
	if opts.Severity != "" {
		where += " AND enriched_severity = " + addArg(opts.Severity)
	}
	if opts.Q != "" {
		p := addArg("%" + opts.Q + "%")
		where += " AND (content ILIKE " + p + " OR enriched_title ILIKE " + p + ")"
	}
	sql := `
		SELECT id, content, source, type, user_id, page_url,
		       COALESCE(enriched_title, ''),
		       COALESCE(enriched_kind, ''),
		       COALESCE(enriched_severity, ''),
		       enriched_modules,
		       enriched_priority,
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
			&row.EnrichedTitle, &row.EnrichedKind, &row.EnrichedSeverity,
			&row.EnrichedModules, &row.EnrichedPriority,
			&row.EnrichmentStatus, &row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan feedback row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ConsoleDetailRow extends ConsoleListRow with full content (source_meta,
// attachments, enrichment_error, enriched_at).
type ConsoleDetailRow struct {
	ConsoleListRow
	SourceMeta      []byte // raw JSONB
	Attachments     []byte // raw JSONB
	EnrichmentError string
	EnrichedAt      *time.Time
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
		       COALESCE(enriched_kind, ''),
		       COALESCE(enriched_severity, ''),
		       enriched_modules, enriched_priority,
		       enrichment_status, created_at,
		       source_meta, attachments,
		       COALESCE(enrichment_error, ''),
		       enriched_at
		  FROM user_feedback
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	).Scan(
		&row.ID, &row.Content, &row.Source, &row.Type, &row.UserID, &row.PageURL,
		&row.EnrichedTitle, &row.EnrichedKind, &row.EnrichedSeverity,
		&row.EnrichedModules, &row.EnrichedPriority,
		&row.EnrichmentStatus, &row.CreatedAt,
		&row.SourceMeta, &row.Attachments,
		&row.EnrichmentError, &row.EnrichedAt,
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
