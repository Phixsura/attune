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
)

var ErrSubjectNotFound = errors.New("gdpr subject not found")

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

type Counts struct {
	FeedbackCount      int
	TagAssignmentCount int
	FeedbackAuditCount int
	LLMAuditCount      int
	OutboxCount        int
}

type ExportData struct {
	SubjectKey        string
	SubjectDisplay    string
	GeneratedAt       time.Time
	FeedbackRows      []json.RawMessage
	FeedbackTagRows   []json.RawMessage
	FeedbackAuditRows []json.RawMessage
	LLMAuditRows      []json.RawMessage
	Counts            Counts
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

	return ptrext.Of(ExportData{
		SubjectKey:        subjectKey,
		SubjectDisplay:    info.subjectDisplay,
		GeneratedAt:       time.Now().UTC(),
		FeedbackRows:      feedbackRows,
		FeedbackTagRows:   tagRows,
		FeedbackAuditRows: feedbackAuditRows,
		LLMAuditRows:      llmAuditRows,
		Counts: Counts{
			FeedbackCount:      len(feedbackRows),
			TagAssignmentCount: len(tagRows),
			FeedbackAuditCount: len(feedbackAuditRows),
			LLMAuditCount:      len(llmAuditRows),
		},
	}), nil
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

	counts, err := deleteLockedSubject(ctx, tx, tenantID, info.feedbackIDs)
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
	counts, err := deleteLockedSubject(ctx, tx, tenantID, info.feedbackIDs)
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

func deleteLockedSubject(ctx context.Context, tx pgx.Tx, tenantID string, feedbackIDs []int64) (Counts, error) {
	var counts Counts
	if err := tx.QueryRow(
		ctx, `
		SELECT
			(SELECT COUNT(*) FROM feedback_tag_assignments WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM feedback_audit_log WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM llm_audit WHERE feedback_id = ANY($1)),
			(SELECT COUNT(*) FROM notify_outbox WHERE feedback_id = ANY($1))`,
		feedbackIDs,
	).Scan(&counts.TagAssignmentCount, &counts.FeedbackAuditCount, &counts.LLMAuditCount, &counts.OutboxCount); err != nil {
		return Counts{}, fmt.Errorf("count subject-linked rows: %w", err)
	}
	counts.FeedbackCount = len(feedbackIDs)

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
	return counts, nil
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
