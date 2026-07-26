// SPDX-License-Identifier: Apache-2.0

// Package gdpr owns tenant-scoped data-subject export/delete queries.
package gdpr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
)

var ErrSubjectNotFound = errors.New("gdpr subject not found")

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

type Counts struct {
	FeedbackCount             int
	TagAssignmentCount        int
	FeedbackAuditCount        int
	LLMAuditCount             int
	OutboxCount               int
	ReplyDraftCount           int
	ReplyDraftRevisionCount   int
	ReplyDraftEventCount      int
	ReplyDeliveryAttemptCount int
	// CustomerLinkCount / VoteCount are customer-request rows carrying
	// the subject's identity (email/name); erasure anonymizes them
	// in place instead of deleting so request aggregates survive.
	CustomerLinkCount int
	VoteCount         int
}

type ExportData struct {
	SubjectKey               string
	SubjectDisplay           string
	GeneratedAt              time.Time
	FeedbackRows             []json.RawMessage
	FeedbackTagRows          []json.RawMessage
	FeedbackAuditRows        []json.RawMessage
	LLMAuditRows             []json.RawMessage
	ReplyDraftRows           []json.RawMessage
	ReplyDraftRevisionRows   []json.RawMessage
	ReplyDraftEventRows      []json.RawMessage
	ReplyDeliveryAttemptRows []json.RawMessage
	Counts                   Counts
}

type DeleteResult struct {
	RequestID    string
	SubjectKey   string
	Counts       Counts
	Status       RequestStatus
	ExecuteAfter *time.Time
}

func (r *Repo) Export(ctx context.Context, tenantID, subjectKey string) (*ExportData, error) {
	info, err := r.subjectInfo(ctx, tenantID, subjectKey)
	if err != nil {
		return nil, err
	}
	rows, err := r.exportSubjectRows(ctx, tenantID, subjectKey)
	if err != nil {
		return nil, err
	}

	return ptrext.Of(ExportData{
		SubjectKey:               subjectKey,
		SubjectDisplay:           info.subjectDisplay,
		GeneratedAt:              time.Now().UTC(),
		FeedbackRows:             rows.feedback,
		FeedbackTagRows:          rows.tags,
		FeedbackAuditRows:        rows.feedbackAudit,
		LLMAuditRows:             rows.llmAudit,
		ReplyDraftRows:           rows.replyDrafts,
		ReplyDraftRevisionRows:   rows.replyDraftRevisions,
		ReplyDraftEventRows:      rows.replyDraftEvents,
		ReplyDeliveryAttemptRows: rows.replyDeliveryAttempts,
		Counts:                   rows.counts(),
	}), nil
}

type subjectExportRows struct {
	feedback              []json.RawMessage
	tags                  []json.RawMessage
	feedbackAudit         []json.RawMessage
	llmAudit              []json.RawMessage
	replyDrafts           []json.RawMessage
	replyDraftRevisions   []json.RawMessage
	replyDraftEvents      []json.RawMessage
	replyDeliveryAttempts []json.RawMessage
}

func (rows subjectExportRows) counts() Counts {
	return Counts{
		FeedbackCount:             len(rows.feedback),
		TagAssignmentCount:        len(rows.tags),
		FeedbackAuditCount:        len(rows.feedbackAudit),
		LLMAuditCount:             len(rows.llmAudit),
		ReplyDraftCount:           len(rows.replyDrafts),
		ReplyDraftRevisionCount:   len(rows.replyDraftRevisions),
		ReplyDraftEventCount:      len(rows.replyDraftEvents),
		ReplyDeliveryAttemptCount: len(rows.replyDeliveryAttempts),
	}
}

func (r *Repo) exportSubjectRows(ctx context.Context, tenantID, subjectKey string) (subjectExportRows, error) {
	var rows subjectExportRows
	var err error
	if rows.feedback, err = r.exportFeedbackRows(ctx, tenantID, subjectKey); err != nil {
		return subjectExportRows{}, err
	}
	if rows.tags, err = r.exportFeedbackTagRows(ctx, tenantID, subjectKey); err != nil {
		return subjectExportRows{}, err
	}
	if rows.feedbackAudit, err = r.exportFeedbackAuditRows(ctx, tenantID, subjectKey); err != nil {
		return subjectExportRows{}, err
	}
	if rows.llmAudit, err = r.exportLLMAuditRows(ctx, tenantID, subjectKey); err != nil {
		return subjectExportRows{}, err
	}
	if rows.replyDrafts, err = r.exportReplyDraftRows(ctx, tenantID, subjectKey); err != nil {
		return subjectExportRows{}, err
	}
	if rows.replyDraftRevisions, err = r.exportReplyDraftRevisionRows(ctx, tenantID, subjectKey); err != nil {
		return subjectExportRows{}, err
	}
	if rows.replyDraftEvents, err = r.exportReplyDraftEventRows(ctx, tenantID, subjectKey); err != nil {
		return subjectExportRows{}, err
	}
	if rows.replyDeliveryAttempts, err = r.exportReplyDeliveryAttemptRows(ctx, tenantID, subjectKey); err != nil {
		return subjectExportRows{}, err
	}
	return rows, nil
}

func (r *Repo) exportFeedbackRows(ctx context.Context, tenantID, subjectKey string) ([]json.RawMessage, error) {
	subjectFilter := subjectMatchClause(2)
	feedbackRows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT id, tenant_id, user_id, subject_key, subject_display, source, source_meta,
			       type, content, page_url, attachments, language, enriched_title,
			       enriched_display_title, enriched_display_locale, enriched_attrs, is_urgent,
			       classification_confidence, enrichment_status, enrichment_error, enriched_at,
			       enriched_rationale, enriched_display_rationale, reply_draft,
			       reply_draft_generated_at, embedding_model, embedding_dims, embedded_at,
			       cluster_id, cluster_label, cluster_assigned_at, workflow_state_id,
			       workflow_updated_at, created_at, updated_at, deleted_at
			FROM user_feedback
			WHERE tenant_id = $1 AND `+subjectFilter+`
			ORDER BY id
		) t`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query feedback export rows: %w", err)
	}
	return feedbackRows, nil
}

func (r *Repo) exportFeedbackTagRows(ctx context.Context, tenantID, subjectKey string) ([]json.RawMessage, error) {
	subjectFilter := subjectMatchClause(2)
	tagRows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT fta.feedback_id, fta.tag_id, fta.created_by, fta.created_at,
			       t.name AS tag_name, t.color AS tag_color, t.description AS tag_description,
			       t.exclusive_scope, t.archived_at AS tag_archived_at
			FROM feedback_tag_assignments fta
			JOIN tenant_feedback_tags t ON t.id = fta.tag_id
			JOIN user_feedback uf ON uf.id = fta.feedback_id
			WHERE uf.tenant_id = $1 AND `+subjectFilter+`
			ORDER BY fta.feedback_id, fta.created_at, fta.tag_id
		) t`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query feedback tag export rows: %w", err)
	}
	return tagRows, nil
}

func (r *Repo) exportFeedbackAuditRows(ctx context.Context, tenantID, subjectKey string) ([]json.RawMessage, error) {
	subjectFilter := subjectMatchClause(2)
	feedbackAuditRows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT fal.id, fal.tenant_id, fal.feedback_id, fal.entity_type, fal.field_name,
			       fal.old_value, fal.new_value, fal.comment, fal.changed_by, fal.created_at
			FROM feedback_audit_log fal
			JOIN user_feedback uf ON uf.id = fal.feedback_id
			WHERE uf.tenant_id = $1 AND `+subjectFilter+`
			ORDER BY fal.feedback_id, fal.id
		) t`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query feedback audit export rows: %w", err)
	}
	return feedbackAuditRows, nil
}

func (r *Repo) exportLLMAuditRows(ctx context.Context, tenantID, subjectKey string) ([]json.RawMessage, error) {
	subjectFilter := subjectMatchClause(2)
	llmAuditRows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT la.id, la.tenant_id, la.feedback_id, la.inbound_trace_id, la.otel_trace_id,
			       la.model_id, la.purpose, la.prompt_tokens, la.completion_tokens, la.cost_usd,
			       la.status, la.error, la.latency_ms, la.created_at
			FROM llm_audit la
			JOIN user_feedback uf ON uf.id = la.feedback_id
			WHERE uf.tenant_id = $1 AND `+subjectFilter+`
			ORDER BY la.feedback_id, la.id
		) t`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query llm audit export rows: %w", err)
	}
	return llmAuditRows, nil
}

func (r *Repo) exportReplyDraftRows(ctx context.Context, tenantID, subjectKey string) ([]json.RawMessage, error) {
	subjectFilter := subjectMatchClause(2)
	rows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT d.id, d.tenant_id, d.feedback_id, d.cycle_no, d.status,
			       d.active_revision_id, d.approved_revision_id, d.sent_revision_id,
			       d.approved_hook_id, d.approved_hook_fingerprint, d.sent_hook_id,
			       d.source_fingerprint, d.source_meta, d.last_blocker,
			       d.external_delivery_status, d.external_message_id,
			       d.generated_at, d.generated_by, d.edited_at, d.edited_by,
			       d.approved_at, d.approved_by, d.rejected_at, d.rejected_by,
			       d.sent_at, d.sent_by, d.revision, d.created_at, d.updated_at
			FROM reply_drafts d
			JOIN user_feedback uf ON uf.id = d.feedback_id
			WHERE uf.tenant_id = $1 AND `+subjectFilter+`
			ORDER BY d.feedback_id, d.cycle_no, d.id
		) t`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query reply draft export rows: %w", err)
	}
	return rows, nil
}

func (r *Repo) exportReplyDraftRevisionRows(ctx context.Context, tenantID, subjectKey string) ([]json.RawMessage, error) {
	subjectFilter := subjectMatchClause(2)
	rows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT rr.id, rr.draft_id, rr.tenant_id, rr.feedback_id, rr.cycle_no,
			       rr.revision_no, rr.origin, rr.content, encode(rr.content_sha256, 'hex') AS content_sha256,
			       rr.source_fingerprint, rr.created_by, rr.created_at
			FROM reply_draft_revisions rr
			JOIN user_feedback uf ON uf.id = rr.feedback_id
			WHERE uf.tenant_id = $1 AND `+subjectFilter+`
			ORDER BY rr.feedback_id, rr.cycle_no, rr.revision_no, rr.id
		) t`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query reply draft revision export rows: %w", err)
	}
	return rows, nil
}

func (r *Repo) exportReplyDraftEventRows(ctx context.Context, tenantID, subjectKey string) ([]json.RawMessage, error) {
	subjectFilter := subjectMatchClause(2)
	rows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT e.id, e.draft_id, e.tenant_id, e.feedback_id, e.revision_id,
			       e.hook_id, e.event_type, e.actor_type, e.actor_id, e.blocker,
			       e.metadata, e.created_at
			FROM reply_draft_events e
			JOIN user_feedback uf ON uf.id = e.feedback_id
			WHERE uf.tenant_id = $1 AND `+subjectFilter+`
			ORDER BY e.feedback_id, e.created_at, e.id
		) t`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query reply draft event export rows: %w", err)
	}
	return rows, nil
}

func (r *Repo) exportReplyDeliveryAttemptRows(ctx context.Context, tenantID, subjectKey string) ([]json.RawMessage, error) {
	subjectFilter := subjectMatchClause(2)
	rows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT a.id, a.tenant_id, a.draft_id, a.feedback_id, a.hook_id,
			       a.revision_id, a.event_type, a.idempotency_key, a.status,
			       a.http_status, a.attempts, a.max_attempts, a.next_retry_at,
			       a.request_fingerprint, a.external_message_id, a.error,
			       a.response_meta, a.requested_by_type, a.requested_by,
			       a.requested_at, a.completed_at, a.created_at, a.updated_at
			FROM reply_delivery_attempts a
			JOIN user_feedback uf ON uf.id = a.feedback_id
			WHERE uf.tenant_id = $1 AND `+subjectFilter+`
			ORDER BY a.feedback_id, a.created_at, a.id
		) t`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query reply delivery attempt export rows: %w", err)
	}
	return rows, nil
}

func (r *Repo) Delete(ctx context.Context, tenantID, subjectKey string) (*DeleteResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	info, err := subjectInfoTx(ctx, tx, tenantID, subjectKey)
	if err != nil {
		return nil, err
	}

	counts, err := deleteLockedSubject(ctx, tx, tenantID, subjectKey, info.feedbackIDs)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return ptrext.Of(DeleteResult{
		SubjectKey: subjectKey,
		Counts:     counts,
	}), nil
}

func (r *Repo) ExecuteDeleteRequest(ctx context.Context, requestID string) (*DeleteResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID, subjectKey string
	if err := tx.QueryRow(
		ctx, `
		SELECT tenant_id, subject_key
		FROM gdpr_requests
		WHERE id = $1 AND request_type = 'delete'
		FOR UPDATE`,
		requestID,
	).Scan(&tenantID, &subjectKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRequestNotFound
		}
		return nil, fmt.Errorf("load gdpr delete request: %w", err)
	}

	info, err := subjectInfoTx(ctx, tx, tenantID, subjectKey)
	if err != nil {
		return nil, err
	}
	counts, err := deleteLockedSubject(ctx, tx, tenantID, subjectKey, info.feedbackIDs)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ptrext.Of(DeleteResult{
		RequestID:  requestID,
		SubjectKey: subjectKey,
		Counts:     counts,
		Status:     RequestStatusCompleted,
	}), nil
}

func deleteLockedSubject(ctx context.Context, tx pgx.Tx, tenantID, subjectKey string, feedbackIDs []int64) (Counts, error) {
	var counts Counts
	if err := tx.QueryRow(
		ctx, `
		SELECT
			(SELECT COUNT(*) FROM feedback_tag_assignments WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM feedback_audit_log WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM llm_audit WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM notify_outbox WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM reply_drafts WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM reply_draft_revisions WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM reply_draft_events WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM reply_delivery_attempts WHERE feedback_id = ANY($1))`,
		feedbackIDs,
	).Scan(
		&counts.TagAssignmentCount,
		&counts.FeedbackAuditCount,
		&counts.LLMAuditCount,
		&counts.OutboxCount,
		&counts.ReplyDraftCount,
		&counts.ReplyDraftRevisionCount,
		&counts.ReplyDraftEventCount,
		&counts.ReplyDeliveryAttemptCount,
	); err != nil {
		return Counts{}, fmt.Errorf("count subject-linked rows: %w", err)
	}
	counts.FeedbackCount = len(feedbackIDs)

	if _, err := tx.Exec(ctx, `DELETE FROM reply_delivery_attempts WHERE feedback_id = ANY($1)`, feedbackIDs); err != nil {
		return Counts{}, fmt.Errorf("delete reply_delivery_attempts rows: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM llm_audit WHERE feedback_id = ANY($1)`, feedbackIDs); err != nil {
		return Counts{}, fmt.Errorf("delete llm_audit rows: %w", err)
	}
	// notify_outbox.feedback_id is a NOT NULL FK with no ON DELETE action and
	// its payload JSONB holds the feedback content verbatim. Purge it before
	// user_feedback, or the erasure aborts on an FK violation (and would leave
	// PII behind even if it didn't). feedback_tag_assignments + feedback_audit_log
	// cascade on the user_feedback delete; llm_audit + notify_outbox do not.
	if _, err := tx.Exec(ctx, `DELETE FROM notify_outbox WHERE feedback_id = ANY($1)`, feedbackIDs); err != nil {
		return Counts{}, fmt.Errorf("delete notify_outbox rows: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_feedback WHERE tenant_id = $1 AND id = ANY($2)`, tenantID, feedbackIDs); err != nil {
		return Counts{}, fmt.Errorf("delete user_feedback rows: %w", err)
	}
	linkCount, voteCount, err := anonymizeCustomerRequestSubject(ctx, tx, tenantID, subjectKey)
	if err != nil {
		return Counts{}, err
	}
	counts.CustomerLinkCount = linkCount
	counts.VoteCount = voteCount
	return counts, nil
}

// anonymizeCustomerRequestSubject scrubs the subject's identity from
// customer-request links and votes. These tables carry the subject's
// email (subject_key) and name (subject_display) — copied there by
// manual linking and by the promote-time auto-attribution — but have no
// FK to user_feedback, so the feedback purge never reaches them.
// Anonymize in place: the per-tenant subject_hash keeps rows unique and
// keeps request aggregates (vote counts, customer counts) intact
// without retaining the raw identity.
func anonymizeCustomerRequestSubject(ctx context.Context, tx pgx.Tx, tenantID, subjectKey string) (linkCount, voteCount int, err error) {
	subjectHash := subjectkey.Hash(tenantID, subjectKey)
	for _, table := range []string{"customer_request_customer_links", "customer_request_votes"} {
		count, tableErr := anonymizeSubjectRowsInTable(ctx, tx, table, tenantID, subjectKey, subjectHash)
		if tableErr != nil {
			return 0, 0, tableErr
		}
		if table == "customer_request_customer_links" {
			linkCount = count
		} else {
			voteCount = count
		}
	}
	return linkCount, voteCount, nil
}

// anonymizeSubjectRowsInTable scrubs one table's rows for the subject.
// All the subject's rows on one request+account collapse to a single
// anonymized tuple — the unique constraint requires keeping exactly one
// per group before scrubbing.
func anonymizeSubjectRowsInTable(ctx context.Context, tx pgx.Tx, table, tenantID, subjectKey, subjectHash string) (int, error) {
	if _, err := tx.Exec(ctx, `
		DELETE FROM `+table+`
		WHERE tenant_id = $1
		  AND (subject_key = $2 OR (subject_key = '' AND subject_hash = $3))
		  AND id NOT IN (
			SELECT (array_agg(id ORDER BY created_at, id))[1]
			FROM `+table+`
			WHERE tenant_id = $1
			  AND (subject_key = $2 OR (subject_key = '' AND subject_hash = $3))
			GROUP BY request_id, account_key
		  )`,
		tenantID, subjectKey, subjectHash,
	); err != nil {
		return 0, fmt.Errorf("dedup %s rows: %w", table, err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE `+table+`
		SET subject_key = '', subject_display = '', note = '',
		    subject_hash = $3
		WHERE tenant_id = $1
		  AND subject_key = $2`,
		tenantID, subjectKey, subjectHash,
	)
	if err != nil {
		return 0, fmt.Errorf("anonymize %s rows: %w", table, err)
	}
	return int(tag.RowsAffected()), nil
}

type subjectMetadata struct {
	feedbackIDs    []int64
	subjectDisplay string
}

func (r *Repo) subjectInfo(ctx context.Context, tenantID, subjectKey string) (*subjectMetadata, error) {
	return subjectInfoTx(ctx, r.pool, tenantID, subjectKey)
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func subjectInfoTx(ctx context.Context, q queryer, tenantID, subjectKey string) (*subjectMetadata, error) {
	subjectFilter := subjectMatchClause(2)
	rows, err := q.Query(ctx, `
		SELECT id, COALESCE(NULLIF(subject_display, ''), subject_key)
		FROM user_feedback
		WHERE tenant_id = $1 AND `+subjectFilter+`
		ORDER BY id
		FOR UPDATE`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query subject feedback ids: %w", err)
	}
	defer rows.Close()

	info := ptrext.Of(subjectMetadata{})
	for rows.Next() {
		var id int64
		var subjectDisplay string
		if err := rows.Scan(&id, &subjectDisplay); err != nil {
			return nil, fmt.Errorf("scan subject feedback ids: %w", err)
		}
		info.feedbackIDs = append(info.feedbackIDs, id)
		if info.subjectDisplay == "" {
			info.subjectDisplay = subjectDisplay
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subject feedback ids: %w", err)
	}
	if len(info.feedbackIDs) == 0 {
		return nil, ErrSubjectNotFound
	}
	return info, nil
}

func (r *Repo) queryJSONLines(ctx context.Context, query string, args ...any) ([]json.RawMessage, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []json.RawMessage
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		out = append(out, json.RawMessage(raw))
	}
	return out, rows.Err()
}

func subjectMatchClause(subjectArgPos int) string {
	subjectRef := fmt.Sprintf("$%d", subjectArgPos)
	return strings.Join([]string{
		"(",
		"subject_key = " + subjectRef,
		"OR (",
		"subject_key = ''",
		"AND (",
		"(user_id LIKE 'ext_%:%' AND split_part(user_id, ':', 2) = " + subjectRef + ")",
		"OR (user_id LIKE 'ext_%' AND user_id NOT LIKE 'ext_%:%' AND user_id = " + subjectRef + ")",
		"OR (user_id NOT LIKE 'ext_%' AND user_id = " + subjectRef + ")",
		")",
		")",
		")",
	}, " ")
}
