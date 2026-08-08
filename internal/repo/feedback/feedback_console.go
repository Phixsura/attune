// Package repo — console-scoped projections of user_feedback. Kept
// separate from feedback.go so the ingest-path SQL stays decoupled
// from the read-path SQL (only the console reads the latter). The
// analytics/aggregate queries live in feedback_console_stats.go.
package feedback

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
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

// AccountContext is the account/company projection derived from source metadata.
type AccountContext struct {
	AccountKey     string
	AccountDisplay string
	Source         string
}

// ConsoleListOpts is the filter set the console UI sends. Each field
// is optional; empty means no filter. Limit is bounded by the handler.
type ConsoleListOpts struct {
	Attrs              []AttrFilter // per-dim filters, AND-composed via JSONB containment
	Urgent             *bool        // nil = no filter; true = is_urgent only; false = not urgent only
	Q                  string       // ILIKE on content + native/display titles
	Cursor             int64        // last id seen; 0 = first page
	Limit              int
	Source             *string
	Type               *string
	TagID              *string // UUID string; nil = no filter
	WorkflowStateID    *string // UUID string; nil = no filter
	WorkflowCategory   *string // "open"/"active"/"closed"; nil = no filter
	EnrichmentStatus   *string // "pending" | "enriching" | "done" | "failed"; nil = no filter
	TerminalFailedOnly *bool   // true = failed AND attempts >= maxEnrichmentAttempts AND next_retry_at IS NULL
	IDs                []int64
	ConfidenceLTE      *float64
	CreatedFrom        *time.Time
	CreatedTo          *time.Time
	EnrichedFrom       *time.Time
	EnrichedTo         *time.Time
	QualitySignal      *string
	CohortID           *string // UUID string; nil = no filter; filter feedback by cohort membership
	AccountKey         *string
}

// ConsoleListRow is the projection sent to the console list view.
// EnrichedAttrs is returned as a raw JSONB byte slice — the handler
// decodes it into a structpb.Struct for the proto wire shape.
type ConsoleListRow struct {
	ID                               int64
	Content                          string
	Source                           string
	Type                             string
	UserID                           string
	Language                         string
	PageURL                          string
	EnrichedTitle                    string
	EnrichedDisplayTitle             string
	EnrichedDisplayLocale            string
	EnrichedAttrs                    []byte // raw JSONB
	IsUrgent                         bool
	ClassificationConfidence         *float64
	EnrichmentStatus                 string
	CreatedAt                        time.Time
	WorkflowStateID                  *string
	EnrichmentAttempts               int        // 0-5; number of enrichment attempts made
	EnrichmentNextRetryAt            *time.Time // nil if terminal or not failed
	TerminalFailureReasonClass       string
	TerminalFailureModel             string
	TerminalFailureChannelID         string
	TerminalFailureChannelName       string
	TerminalFailureConfigFingerprint string
	TerminalFailurePromptVersion     string
	Assignment                       Assignment
	AccountContext                   AccountContext
}

func feedbackSourceMetaSQL(alias string) string {
	if alias == "" {
		return "source_meta"
	}
	return alias + ".source_meta"
}

func feedbackAccountKeySQL(alias string) string {
	meta := feedbackSourceMetaSQL(alias)
	return "COALESCE(" +
		"NULLIF(" + meta + "->>'account_key', ''), " +
		"NULLIF(" + meta + "->>'accountKey', ''), " +
		"NULLIF(" + meta + "->>'company_id', ''), " +
		"NULLIF(" + meta + "->>'companyId', ''), " +
		"NULLIF(" + meta + " #>> '{account,key}', ''), " +
		"NULLIF(" + meta + " #>> '{account,id}', ''), " +
		"NULLIF(" + meta + " #>> '{account,account_key}', ''), " +
		"NULLIF(" + meta + " #>> '{account,accountKey}', ''), " +
		"NULLIF(" + meta + " #>> '{company,key}', ''), " +
		"NULLIF(" + meta + " #>> '{company,id}', '')" +
		")"
}

func feedbackAccountDisplaySQL(alias string) string {
	meta := feedbackSourceMetaSQL(alias)
	return "COALESCE(" +
		"NULLIF(" + meta + "->>'account_display', ''), " +
		"NULLIF(" + meta + "->>'accountDisplay', ''), " +
		"NULLIF(" + meta + "->>'company_name', ''), " +
		"NULLIF(" + meta + "->>'companyName', ''), " +
		"NULLIF(" + meta + " #>> '{account,name}', ''), " +
		"NULLIF(" + meta + " #>> '{account,display}', ''), " +
		"NULLIF(" + meta + " #>> '{company,name}', ''), " +
		"NULLIF(" + meta + " #>> '{company,display}', ''), " +
		feedbackAccountKeySQL(alias) +
		")"
}

func feedbackAccountSourceSQL(alias string) string {
	return "CASE WHEN " + feedbackAccountKeySQL(alias) + " IS NULL THEN '' ELSE 'source_meta' END"
}

type queryBuilder struct {
	args  []any
	where string
}

func newQueryBuilder(tenantID string) *queryBuilder {
	return ptrext.Of(queryBuilder{args: []any{tenantID}, where: "WHERE tenant_id = $1"})
}

func (qb *queryBuilder) addArg(v any) string {
	qb.args = append(qb.args, v)
	return fmt.Sprintf("$%d", len(qb.args))
}

func (qb *queryBuilder) and(clause string) { qb.where += " AND " + clause }

func (qb *queryBuilder) applyFilters(opts ConsoleListOpts) error {
	if err := qb.applyIdentityFilters(opts); err != nil {
		return err
	}
	qb.applyStateFilters(opts)
	qb.applyQualityFilters(opts)
	return nil
}

func (qb *queryBuilder) applyIdentityFilters(opts ConsoleListOpts) error {
	if opts.Cursor > 0 {
		qb.and("id < " + qb.addArg(opts.Cursor))
	}
	if len(opts.IDs) > 0 {
		qb.and("id = ANY(" + qb.addArg(uniquePositiveIDs(opts.IDs, 50)) + ")")
	}
	for _, f := range opts.Attrs {
		if f.Dim == "" || f.Value == "" {
			continue
		}
		clause, err := containmentClause(f)
		if err != nil {
			return err
		}
		qb.and("enriched_attrs @> " + qb.addArg(clause) + "::jsonb")
	}
	if opts.AccountKey != nil {
		if accountKey := strings.TrimSpace(ptrext.Indirect(opts.AccountKey)); accountKey != "" {
			qb.and(feedbackAccountKeySQL("") + " = " + qb.addArg(accountKey))
		}
	}
	return nil
}

func (qb *queryBuilder) applyStateFilters(opts ConsoleListOpts) {
	if opts.Source != nil {
		qb.and("source = " + qb.addArg(ptrext.Indirect(opts.Source)))
	}
	if opts.Type != nil {
		qb.and("type = " + qb.addArg(ptrext.Indirect(opts.Type)))
	}
	if opts.Urgent != nil {
		qb.and("is_urgent = " + qb.addArg(ptrext.Indirect(opts.Urgent)))
	}
	if opts.Q != "" {
		p := qb.addArg("%" + opts.Q + "%")
		qb.and("(content ILIKE " + p +
			" OR enriched_title ILIKE " + p +
			" OR enriched_display_title ILIKE " + p + ")")
	}
	if opts.TagID != nil {
		qb.and("EXISTS (SELECT 1 FROM feedback_tag_assignments fta WHERE fta.feedback_id = id AND fta.tag_id = " + qb.addArg(ptrext.Indirect(opts.TagID)) + "::uuid)")
	}
	if opts.CohortID != nil {
		qb.and("subject_key <> '' AND EXISTS (SELECT 1 FROM cohort_memberships cm WHERE cm.tenant_id = $1 AND cm.cohort_id = " + qb.addArg(ptrext.Indirect(opts.CohortID)) + "::uuid AND cm.external_user_id = subject_key AND cm.left_at IS NULL)")
	}
	if opts.WorkflowStateID != nil {
		qb.and("workflow_state_id = " + qb.addArg(ptrext.Indirect(opts.WorkflowStateID)) + "::uuid")
	}
	if opts.WorkflowCategory != nil {
		qb.and("workflow_state_id IN (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND category = " + qb.addArg(ptrext.Indirect(opts.WorkflowCategory)) + ")")
	}
	if opts.EnrichmentStatus != nil {
		qb.and("enrichment_status = " + qb.addArg(ptrext.Indirect(opts.EnrichmentStatus)))
	}
	if opts.TerminalFailedOnly != nil && ptrext.Indirect(opts.TerminalFailedOnly) {
		qb.andTerminalFailure()
	}
}

func (qb *queryBuilder) applyQualityFilters(opts ConsoleListOpts) {
	if opts.ConfidenceLTE != nil {
		qb.and("classification_confidence IS NOT NULL")
		qb.and("classification_confidence <= " + qb.addArg(ptrext.Indirect(opts.ConfidenceLTE)))
	}
	if opts.CreatedFrom != nil {
		qb.and("created_at >= " + qb.addArg(ptrext.Indirect(opts.CreatedFrom)))
	}
	if opts.CreatedTo != nil {
		qb.and("created_at < " + qb.addArg(ptrext.Indirect(opts.CreatedTo)))
	}
	if opts.EnrichedFrom != nil {
		qb.and("enriched_at >= " + qb.addArg(ptrext.Indirect(opts.EnrichedFrom)))
	}
	if opts.EnrichedTo != nil {
		qb.and("enriched_at < " + qb.addArg(ptrext.Indirect(opts.EnrichedTo)))
	}
	if opts.QualitySignal != nil {
		qb.applyQualitySignal(ptrext.Indirect(opts.QualitySignal), opts)
	}
}

func (qb *queryBuilder) applyQualitySignal(signal string, opts ConsoleListOpts) {
	switch signal {
	case "low_confidence":
		if opts.ConfidenceLTE == nil {
			qb.and("classification_confidence IS NOT NULL")
			qb.and("classification_confidence <= " + qb.addArg(0.60))
		}
	case "terminal_failure":
		qb.andTerminalFailure()
	case "off_list", "parse_failure":
		// These signals live in quality facts; dashboard links should pass ids.
		if len(opts.IDs) == 0 {
			qb.and("FALSE")
		}
	}
}

func (qb *queryBuilder) andTerminalFailure() {
	qb.and("enrichment_status = 'failed'")
	qb.and("enrichment_attempts >= " + qb.addArg(maxEnrichmentAttempts))
	qb.and("enrichment_next_retry_at IS NULL")
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
	qb := newQueryBuilder(tenantID)
	if err := qb.applyFilters(opts); err != nil {
		return nil, err
	}
	query := `
			SELECT id, content, source, type, user_id, COALESCE(language, ''), page_url,
		 COALESCE(enriched_title, ''),
		 COALESCE(enriched_display_title, ''),
		 COALESCE(enriched_display_locale, ''),
		 COALESCE(enriched_attrs, '{}'::jsonb),
		 is_urgent,
		 classification_confidence,
		 enrichment_status,
		 created_at,
		 workflow_state_id,
		 enrichment_attempts,
		 enrichment_next_retry_at,
		 COALESCE(enrichment_failure_reason_class, ''),
		 COALESCE(enrichment_failure_model, ''),
		 COALESCE(enrichment_failure_channel_id, ''),
		 COALESCE(enrichment_failure_channel_name, ''),
		 COALESCE(enrichment_failure_config_fingerprint, ''),
		 COALESCE(enrichment_failure_prompt_version, ''),
		 owner_member_id::text,
		 owner_assigned_at,
		 COALESCE(owner_assigned_by, ''),
		 feedback_sla_due_at,
		 COALESCE(owner_assignment_note, ''),
		 COALESCE((SELECT tm.member_type FROM tenant_members tm WHERE tm.id = user_feedback.owner_member_id), ''),
		 COALESCE((SELECT tm.user_id FROM tenant_members tm WHERE tm.id = user_feedback.owner_member_id), ''),
		 COALESCE((SELECT tm.email FROM tenant_members tm WHERE tm.id = user_feedback.owner_member_id), ''),
		 COALESCE((SELECT tm.role FROM tenant_members tm WHERE tm.id = user_feedback.owner_member_id), ''),
		 COALESCE(` + feedbackAccountKeySQL("user_feedback") + `, ''),
		 COALESCE(` + feedbackAccountDisplaySQL("user_feedback") + `, ''),
		 ` + feedbackAccountSourceSQL("user_feedback") + `
		 FROM user_feedback
		 ` + qb.where + `
		 ORDER BY id DESC
		 LIMIT ` + qb.addArg(opts.Limit)
	rows, err := r.pool.Query(ctx, query, qb.args...)
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
		var confidence sql.NullFloat64
		var wsID sql.NullString    // ptrext:allow scan-target
		var nextRetry sql.NullTime // ptrext:allow scan-target
		var ownerID sql.NullString // ptrext:allow scan-target
		var ownerAssignedAt sql.NullTime
		var slaDueAt sql.NullTime
		if err := rows.Scan(
			&row.ID, &row.Content, &row.Source, &row.Type, &row.UserID, &row.Language, &row.PageURL, // ptrext:allow scan-target
			&row.EnrichedTitle, &row.EnrichedDisplayTitle, &row.EnrichedDisplayLocale, // ptrext:allow scan-target
			&row.EnrichedAttrs, &row.IsUrgent, &confidence, // ptrext:allow scan-target
			&row.EnrichmentStatus, &row.CreatedAt, // ptrext:allow scan-target
			&wsID,                                                             // ptrext:allow scan-target
			&row.EnrichmentAttempts,                                           // ptrext:allow scan-target
			&nextRetry,                                                        // ptrext:allow scan-target
			&row.TerminalFailureReasonClass,                                   // ptrext:allow scan-target
			&row.TerminalFailureModel,                                         // ptrext:allow scan-target
			&row.TerminalFailureChannelID,                                     // ptrext:allow scan-target
			&row.TerminalFailureChannelName,                                   // ptrext:allow scan-target
			&row.TerminalFailureConfigFingerprint,                             // ptrext:allow scan-target
			&row.TerminalFailurePromptVersion,                                 // ptrext:allow scan-target
			&ownerID, &ownerAssignedAt, &row.Assignment.AssignedBy, &slaDueAt, // ptrext:allow scan-target
			&row.Assignment.Note, &row.Assignment.OwnerMemberType, // ptrext:allow scan-target
			&row.Assignment.OwnerUserID, &row.Assignment.OwnerEmail, &row.Assignment.OwnerRole, // ptrext:allow scan-target
			&row.AccountContext.AccountKey, &row.AccountContext.AccountDisplay, &row.AccountContext.Source, // ptrext:allow scan-target
		); err != nil {
			return nil, fmt.Errorf("scan feedback row: %w", err)
		}
		row.ClassificationConfidence = nullFloatPtr(confidence)
		if wsID.Valid {
			row.WorkflowStateID = ptrext.Of(wsID.String)
		}
		if nextRetry.Valid {
			row.EnrichmentNextRetryAt = ptrext.Of(nextRetry.Time)
		}
		row.Assignment = hydrateAssignmentTimes(row.Assignment, row.ID, ownerID, ownerAssignedAt, slaDueAt)
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
	SourceMeta               []byte // raw JSONB
	Attachments              []byte // raw JSONB
	EnrichmentError          string
	EnrichedAt               *time.Time
	EnrichedRationale        string // LLM's short "why these values"; empty when not classified
	EnrichedDisplayRationale string
	ReplyDraft               string // operator-facing LLM draft (#26); empty when none
	ReplyDraftGeneratedAt    *time.Time
	ReplyDraftEnabled        bool // tenant opt-in flag (joined from tenants)
}

// IdentityReviewRow is the bounded raw input for the console identity-review
// projection. The handler owns evidence extraction so the review model stays
// aligned with detail-page identity assessment.
type IdentityReviewRow struct {
	ID         int64
	Source     string
	UserID     string
	Content    string
	SourceMeta []byte
	CreatedAt  time.Time
}

// ErrFeedbackNotFound returned by GetForConsole when id doesn't match
// the tenant (cross-tenant query safety).
var ErrFeedbackNotFound = errors.New("feedback not found")

func (r *FeedbackRepo) GetForConsole(
	ctx context.Context, tenantID string, id int64,
) (*ConsoleDetailRow, error) {
	var row ConsoleDetailRow
	var confidence sql.NullFloat64
	var wsID sql.NullString    // ptrext:allow scan-target
	var nextRetry sql.NullTime // ptrext:allow scan-target
	var ownerID sql.NullString // ptrext:allow scan-target
	var ownerAssignedAt sql.NullTime
	var slaDueAt sql.NullTime
	err := r.pool.QueryRow(
		ctx, `
			SELECT id, content, source, type, user_id, COALESCE(language, ''), page_url,
		 COALESCE(enriched_title, ''),
		 COALESCE(enriched_display_title, ''),
		 COALESCE(enriched_display_locale, ''),
		 COALESCE(enriched_attrs, '{}'::jsonb),
		 is_urgent,
		 classification_confidence,
		 enrichment_status, created_at,
		 source_meta, attachments,
		 COALESCE(enrichment_error, ''),
		 enriched_at,
		 COALESCE(enriched_rationale, ''),
		 COALESCE(enriched_display_rationale, ''),
		 COALESCE(reply_draft, ''),
		 reply_draft_generated_at,
		 COALESCE((SELECT reply_draft_enabled FROM tenants WHERE id = $2), FALSE),
		 workflow_state_id,
		 enrichment_attempts,
		 enrichment_next_retry_at,
		 COALESCE(enrichment_failure_reason_class, ''),
		 COALESCE(enrichment_failure_model, ''),
		 COALESCE(enrichment_failure_channel_id, ''),
		 COALESCE(enrichment_failure_channel_name, ''),
		 COALESCE(enrichment_failure_config_fingerprint, ''),
		 COALESCE(enrichment_failure_prompt_version, ''),
		 owner_member_id::text,
		 owner_assigned_at,
		 COALESCE(owner_assigned_by, ''),
		 feedback_sla_due_at,
		 COALESCE(owner_assignment_note, ''),
		 COALESCE((SELECT tm.member_type FROM tenant_members tm WHERE tm.id = user_feedback.owner_member_id), ''),
		 COALESCE((SELECT tm.user_id FROM tenant_members tm WHERE tm.id = user_feedback.owner_member_id), ''),
		 COALESCE((SELECT tm.email FROM tenant_members tm WHERE tm.id = user_feedback.owner_member_id), ''),
		 COALESCE((SELECT tm.role FROM tenant_members tm WHERE tm.id = user_feedback.owner_member_id), ''),
		 COALESCE(`+feedbackAccountKeySQL("user_feedback")+`, ''),
		 COALESCE(`+feedbackAccountDisplaySQL("user_feedback")+`, ''),
		 `+feedbackAccountSourceSQL("user_feedback")+`
		 FROM user_feedback
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	).Scan(
		&row.ID, &row.Content, &row.Source, &row.Type, &row.UserID, &row.Language, &row.PageURL, // ptrext:allow scan-target
		&row.EnrichedTitle, &row.EnrichedDisplayTitle, &row.EnrichedDisplayLocale, // ptrext:allow scan-target
		&row.EnrichedAttrs, &row.IsUrgent, &confidence, // ptrext:allow scan-target
		&row.EnrichmentStatus, &row.CreatedAt, // ptrext:allow scan-target
		&row.SourceMeta, &row.Attachments, // ptrext:allow scan-target
		&row.EnrichmentError, &row.EnrichedAt, // ptrext:allow scan-target
		&row.EnrichedRationale, &row.EnrichedDisplayRationale, // ptrext:allow scan-target
		&row.ReplyDraft, &row.ReplyDraftGeneratedAt, &row.ReplyDraftEnabled, // ptrext:allow scan-target
		&wsID,                                                             // ptrext:allow scan-target
		&row.EnrichmentAttempts,                                           // ptrext:allow scan-target
		&nextRetry,                                                        // ptrext:allow scan-target
		&row.TerminalFailureReasonClass,                                   // ptrext:allow scan-target
		&row.TerminalFailureModel,                                         // ptrext:allow scan-target
		&row.TerminalFailureChannelID,                                     // ptrext:allow scan-target
		&row.TerminalFailureChannelName,                                   // ptrext:allow scan-target
		&row.TerminalFailureConfigFingerprint,                             // ptrext:allow scan-target
		&row.TerminalFailurePromptVersion,                                 // ptrext:allow scan-target
		&ownerID, &ownerAssignedAt, &row.Assignment.AssignedBy, &slaDueAt, // ptrext:allow scan-target
		&row.Assignment.Note, &row.Assignment.OwnerMemberType, // ptrext:allow scan-target
		&row.Assignment.OwnerUserID, &row.Assignment.OwnerEmail, &row.Assignment.OwnerRole, // ptrext:allow scan-target
		&row.AccountContext.AccountKey, &row.AccountContext.AccountDisplay, &row.AccountContext.Source, // ptrext:allow scan-target
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
	row.ClassificationConfidence = nullFloatPtr(confidence)
	if wsID.Valid {
		row.WorkflowStateID = ptrext.Of(wsID.String)
	}
	if nextRetry.Valid {
		row.EnrichmentNextRetryAt = ptrext.Of(nextRetry.Time)
	}
	row.Assignment = hydrateAssignmentTimes(row.Assignment, row.ID, ownerID, ownerAssignedAt, slaDueAt)
	return ptrext.Of(row), nil
}

func (r *FeedbackRepo) IdentityReviewRows(
	ctx context.Context, tenantID string, limit int,
) ([]IdentityReviewRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 250
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, source, user_id, content, source_meta, created_at
		FROM user_feedback
		WHERE tenant_id = $1
		ORDER BY id DESC
		LIMIT $2`,
		tenantID, limit,
	)
	if err != nil {
		const where = "repo.FeedbackRepo.IdentityReviewRows"
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return nil, fmt.Errorf("identity review rows: %w", err)
	}
	defer rows.Close()
	out := make([]IdentityReviewRow, 0, limit)
	for rows.Next() {
		var row IdentityReviewRow
		if err := rows.Scan(
			&row.ID, &row.Source, &row.UserID, &row.Content, &row.SourceMeta, &row.CreatedAt, // ptrext:allow scan-target
		); err != nil {
			return nil, fmt.Errorf("scan identity review row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func nullFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return ptrext.Of(v.Float64)
}
