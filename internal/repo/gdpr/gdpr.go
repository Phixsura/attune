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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
)

var ErrSubjectNotFound = errors.New("gdpr subject not found")

type Repo struct {
	pool dbPool
}

// dbPool is the slice of *pgxpool.Pool the repo actually uses — an
// interface so unit tests can drive the erasure transaction against a
// scripted Tx (PR #122 pattern; the real flow needs a live database).
type dbPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
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
	CustomerLinkCount               int
	VoteCount                       int
	SurveyInvitationCount           int
	SurveyResponseCount             int
	SurveyLowScoreReviewCount       int
	SurveyProviderEventCount        int
	SurveyRecoveryNotificationCount int
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
	// Customer-request rows carrying the subject's identity — the same
	// rows the delete path anonymizes (Art. 15 must cover Art. 17's scope).
	CustomerLinkRows               []json.RawMessage
	VoteRows                       []json.RawMessage
	SurveyInvitationRows           []json.RawMessage
	SurveyResponseRows             []json.RawMessage
	SurveyLowScoreReviewRows       []json.RawMessage
	SurveyProviderEventRows        []json.RawMessage
	SurveyRecoveryNotificationRows []json.RawMessage
	Counts                         Counts
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
		SubjectKey:                     subjectKey,
		SubjectDisplay:                 info.subjectDisplay,
		GeneratedAt:                    time.Now().UTC(),
		FeedbackRows:                   rows.feedback,
		FeedbackTagRows:                rows.tags,
		FeedbackAuditRows:              rows.feedbackAudit,
		LLMAuditRows:                   rows.llmAudit,
		ReplyDraftRows:                 rows.replyDrafts,
		ReplyDraftRevisionRows:         rows.replyDraftRevisions,
		ReplyDraftEventRows:            rows.replyDraftEvents,
		ReplyDeliveryAttemptRows:       rows.replyDeliveryAttempts,
		CustomerLinkRows:               rows.customerLinks,
		VoteRows:                       rows.votes,
		SurveyInvitationRows:           rows.surveyInvitations,
		SurveyResponseRows:             rows.surveyResponses,
		SurveyLowScoreReviewRows:       rows.surveyLowScoreReviews,
		SurveyProviderEventRows:        rows.surveyProviderEvents,
		SurveyRecoveryNotificationRows: rows.surveyRecoveryNotifications,
		Counts:                         rows.counts(),
	}), nil
}

type subjectExportRows struct {
	feedback                    []json.RawMessage
	tags                        []json.RawMessage
	feedbackAudit               []json.RawMessage
	llmAudit                    []json.RawMessage
	replyDrafts                 []json.RawMessage
	replyDraftRevisions         []json.RawMessage
	replyDraftEvents            []json.RawMessage
	replyDeliveryAttempts       []json.RawMessage
	customerLinks               []json.RawMessage
	votes                       []json.RawMessage
	surveyInvitations           []json.RawMessage
	surveyResponses             []json.RawMessage
	surveyLowScoreReviews       []json.RawMessage
	surveyProviderEvents        []json.RawMessage
	surveyRecoveryNotifications []json.RawMessage
}

func (rows subjectExportRows) counts() Counts {
	return Counts{
		FeedbackCount:                   len(rows.feedback),
		TagAssignmentCount:              len(rows.tags),
		FeedbackAuditCount:              len(rows.feedbackAudit),
		LLMAuditCount:                   len(rows.llmAudit),
		ReplyDraftCount:                 len(rows.replyDrafts),
		ReplyDraftRevisionCount:         len(rows.replyDraftRevisions),
		ReplyDraftEventCount:            len(rows.replyDraftEvents),
		ReplyDeliveryAttemptCount:       len(rows.replyDeliveryAttempts),
		CustomerLinkCount:               len(rows.customerLinks),
		VoteCount:                       len(rows.votes),
		SurveyInvitationCount:           len(rows.surveyInvitations),
		SurveyResponseCount:             len(rows.surveyResponses),
		SurveyLowScoreReviewCount:       len(rows.surveyLowScoreReviews),
		SurveyProviderEventCount:        len(rows.surveyProviderEvents),
		SurveyRecoveryNotificationCount: len(rows.surveyRecoveryNotifications),
	}
}

func (r *Repo) exportSubjectRows(ctx context.Context, tenantID, subjectKey string) (subjectExportRows, error) {
	rows, err := r.exportFeedbackSubjectRows(ctx, tenantID, subjectKey)
	if err != nil {
		return subjectExportRows{}, err
	}
	surveyRows, err := r.exportSurveySubjectRows(ctx, tenantID, subjectKey)
	if err != nil {
		return subjectExportRows{}, err
	}
	rows.surveyInvitations = surveyRows.surveyInvitations
	rows.surveyResponses = surveyRows.surveyResponses
	rows.surveyLowScoreReviews = surveyRows.surveyLowScoreReviews
	rows.surveyProviderEvents = surveyRows.surveyProviderEvents
	rows.surveyRecoveryNotifications = surveyRows.surveyRecoveryNotifications
	return rows, nil
}

func (r *Repo) exportFeedbackSubjectRows(ctx context.Context, tenantID, subjectKey string) (subjectExportRows, error) {
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
	if rows.customerLinks, err = r.exportCustomerRequestSubjectRows(ctx, tenantID, subjectKey, "customer_request_customer_links"); err != nil {
		return rows, err
	}
	if rows.votes, err = r.exportCustomerRequestSubjectRows(ctx, tenantID, subjectKey, "customer_request_votes"); err != nil {
		return rows, err
	}
	if rows.replyDeliveryAttempts, err = r.exportReplyDeliveryAttemptRows(ctx, tenantID, subjectKey); err != nil {
		return subjectExportRows{}, err
	}
	return rows, nil
}

func (r *Repo) exportSurveySubjectRows(ctx context.Context, tenantID, subjectKey string) (subjectExportRows, error) {
	info, err := r.subjectInfo(ctx, tenantID, subjectKey)
	if err != nil {
		return subjectExportRows{}, err
	}
	var rows subjectExportRows
	if rows.surveyInvitations, err = r.exportSurveyInvitationRows(ctx, tenantID, info); err != nil {
		return subjectExportRows{}, err
	}
	if rows.surveyResponses, err = r.exportSurveyResponseRows(ctx, tenantID, info); err != nil {
		return subjectExportRows{}, err
	}
	if rows.surveyLowScoreReviews, err = r.exportSurveyLowScoreReviewRows(ctx, tenantID, info); err != nil {
		return subjectExportRows{}, err
	}
	if rows.surveyProviderEvents, err = r.exportSurveyProviderEventRows(ctx, tenantID, info); err != nil {
		return subjectExportRows{}, err
	}
	if rows.surveyRecoveryNotifications, err = r.exportSurveyRecoveryNotificationRows(ctx, tenantID, info); err != nil {
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

// exportCustomerRequestSubjectRows exports the subject's identity rows
// from one customer-request table (links or votes) — the same rows the
// delete path anonymizes. Matches by subject_key, or by the tenant-scoped
// subject_hash for rows already anonymized in a previous erasure. `table`
// is a compile-time constant at every call site, never user input.
func (r *Repo) exportCustomerRequestSubjectRows(ctx context.Context, tenantID, subjectKey, table string) ([]json.RawMessage, error) {
	subjectHash := subjectkey.Hash(tenantID, subjectKey)
	rows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT x.*, cr.display_id AS request_display_id, cr.title AS request_title
			FROM `+table+` x
			JOIN customer_requests cr ON cr.tenant_id = x.tenant_id AND cr.id = x.request_id
			WHERE x.tenant_id = $1
			  AND (x.subject_key = $2 OR (x.subject_key = '' AND x.subject_hash = $3))
			ORDER BY x.created_at, x.id
		) t`, tenantID, subjectKey, subjectHash)
	if err != nil {
		return nil, fmt.Errorf("query %s export rows: %w", table, err)
	}
	return rows, nil
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

func (r *Repo) exportSurveyInvitationRows(ctx context.Context, tenantID string, info *subjectMetadata) ([]json.RawMessage, error) {
	rows, err := r.queryJSONLines(ctx, `
		SELECT row_to_json(t)
		FROM (
			SELECT si.id, si.tenant_id, si.campaign_id, si.campaign_content_version,
			       si.campaign_snapshot, si.dedupe_key, si.source_type, si.source_id,
			       si.request_id, si.contact_id, si.distribution_mode, si.token_hash,
			       si.delivery_status, si.response_status, si.suppression_status,
			       si.suppression_reason, si.recipient_snapshot,
			       encode(si.delivery_secret, 'hex') AS delivery_secret_hex,
			       si.provider, si.provider_message_id, si.attempts, si.failure_kind,
			       si.http_status, si.last_error, si.delivered_at, si.opened_at,
			       si.responded_at, si.expires_at, si.created_by, si.created_at,
			       si.updated_at
			FROM survey_invitations si
			WHERE si.tenant_id = $1 AND `+subjectSurveyInvitationClause(2, 3, 4)+`
			ORDER BY si.created_at, si.id
		) t`,
		tenantID, info.feedbackIDTexts, info.subjectKey, info.subjectHashes)
	if err != nil {
		return nil, fmt.Errorf("query survey invitation export rows: %w", err)
	}
	return rows, nil
}

func (r *Repo) exportSurveyResponseRows(ctx context.Context, tenantID string, info *subjectMetadata) ([]json.RawMessage, error) {
	rows, err := r.queryJSONLines(ctx, `
		WITH subject_invitations AS (
			SELECT si.id
			FROM survey_invitations si
			WHERE si.tenant_id = $1 AND `+subjectSurveyInvitationClause(2, 3, 4)+`
		)
		SELECT row_to_json(t)
		FROM (
			SELECT sr.id, sr.tenant_id, sr.campaign_id, sr.invitation_id,
			       sr.request_id, sr.contact_id, sr.source_type, sr.source_id,
			       sr.score, sr.comment, sr.locale, sr.metadata, sr.user_agent_hash,
			       sr.ip_hash, sr.submitted_at, sr.created_at
			FROM survey_responses sr
			JOIN subject_invitations si ON si.id = sr.invitation_id
			WHERE sr.tenant_id = $1
			ORDER BY sr.submitted_at, sr.id
		) t`,
		tenantID, info.feedbackIDTexts, info.subjectKey, info.subjectHashes)
	if err != nil {
		return nil, fmt.Errorf("query survey response export rows: %w", err)
	}
	return rows, nil
}

func (r *Repo) exportSurveyLowScoreReviewRows(ctx context.Context, tenantID string, info *subjectMetadata) ([]json.RawMessage, error) {
	rows, err := r.queryJSONLines(ctx, `
		WITH subject_invitations AS (
			SELECT si.id
			FROM survey_invitations si
			WHERE si.tenant_id = $1 AND `+subjectSurveyInvitationClause(2, 3, 4)+`
		)
		SELECT row_to_json(t)
		FROM (
			SELECT lsr.response_id, lsr.tenant_id, lsr.campaign_id, lsr.status,
			       lsr.severity, lsr.owner_member_id, lsr.root_cause,
			       lsr.action_taken, lsr.customer_contacted, lsr.due_at,
			       lsr.reviewed_at, lsr.updated_by, lsr.created_at,
			       lsr.updated_at
			FROM survey_low_score_reviews lsr
			JOIN survey_responses sr
			  ON sr.tenant_id = lsr.tenant_id
			 AND sr.id = lsr.response_id
			JOIN subject_invitations si ON si.id = sr.invitation_id
			WHERE lsr.tenant_id = $1
			ORDER BY lsr.updated_at, lsr.response_id
		) t`,
		tenantID, info.feedbackIDTexts, info.subjectKey, info.subjectHashes)
	if err != nil {
		return nil, fmt.Errorf("query survey low-score review export rows: %w", err)
	}
	return rows, nil
}

func (r *Repo) exportSurveyProviderEventRows(ctx context.Context, tenantID string, info *subjectMetadata) ([]json.RawMessage, error) {
	rows, err := r.queryJSONLines(ctx, `
		WITH subject_invitations AS (
			SELECT si.id
			FROM survey_invitations si
			WHERE si.tenant_id = $1 AND `+subjectSurveyInvitationClause(2, 3, 4)+`
		)
		SELECT row_to_json(t)
		FROM (
			SELECT spe.id, spe.tenant_id, spe.invitation_id, spe.provider,
			       spe.provider_event_type, spe.provider_message_id,
			       spe.provider_event_key, spe.payload, spe.occurred_at,
			       spe.created_at
			FROM survey_provider_events spe
			JOIN subject_invitations si ON si.id = spe.invitation_id
			WHERE spe.tenant_id = $1
			ORDER BY spe.created_at, spe.id
		) t`,
		tenantID, info.feedbackIDTexts, info.subjectKey, info.subjectHashes)
	if err != nil {
		return nil, fmt.Errorf("query survey provider event export rows: %w", err)
	}
	return rows, nil
}

func (r *Repo) exportSurveyRecoveryNotificationRows(
	ctx context.Context,
	tenantID string,
	info *subjectMetadata,
) ([]json.RawMessage, error) {
	rows, err := r.queryJSONLines(ctx, `
		WITH subject_invitations AS (
			SELECT si.id
			FROM survey_invitations si
			WHERE si.tenant_id = $1 AND `+subjectSurveyInvitationClause(2, 3, 4)+`
		)
		SELECT row_to_json(t)
		FROM (
			SELECT srn.id, srn.tenant_id, srn.response_id, srn.owner_member_id,
			       srn.channel, srn.status, srn.reason, srn.destination_hash,
			       srn.payload, srn.provider, srn.provider_message_id,
			       srn.attempts, srn.failure_kind, srn.http_status,
			       srn.last_error, srn.next_retry_at, srn.delivered_at,
			       srn.created_at, srn.updated_at
			FROM survey_recovery_notifications srn
			JOIN survey_responses sr
			  ON sr.tenant_id = srn.tenant_id
			 AND sr.id = srn.response_id
			JOIN subject_invitations si ON si.id = sr.invitation_id
			WHERE srn.tenant_id = $1
			ORDER BY srn.created_at, srn.id
		) t`,
		tenantID, info.feedbackIDTexts, info.subjectKey, info.subjectHashes)
	if err != nil {
		return nil, fmt.Errorf("query survey recovery notification export rows: %w", err)
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

	// Cohort memberships: delete by external_user_id matching the subject key.
	// This runs before feedback deletion to remove PII (email, display_name)
	// stored by the cohort sync subsystem (#233).
	if _, err := tx.Exec(ctx, `DELETE FROM cohort_memberships WHERE tenant_id = $1 AND external_user_id = $2`, tenantID, subjectKey); err != nil {
		return nil, fmt.Errorf("delete cohort memberships: %w", err)
	}

	counts, err := deleteLockedSubject(ctx, tx, tenantID, info)
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

	// Match Delete's contact-then-membership lock order. Survey materialization
	// takes the same contact lock before validating cohort membership, so a
	// scheduled erasure cannot deadlock with a claimed NPS run.
	if _, err := tx.Exec(ctx, `DELETE FROM cohort_memberships WHERE tenant_id = $1 AND external_user_id = $2`, tenantID, subjectKey); err != nil {
		return nil, fmt.Errorf("delete cohort memberships: %w", err)
	}
	counts, err := deleteLockedSubject(ctx, tx, tenantID, info)
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

func deleteLockedSubject(ctx context.Context, tx pgx.Tx, tenantID string, info *subjectMetadata) (Counts, error) {
	// A public response holds a row lock on its invitation while it writes the
	// response and any derived feedback. Take the same invitation locks before
	// counting redactions so a response can neither commit between the count and
	// deletion nor survive an erase transaction through a stale statement snapshot.
	if err := lockSubjectSurveyInvitations(ctx, tx, tenantID, info); err != nil {
		return Counts{}, err
	}
	counts, err := countLockedSubject(ctx, tx, tenantID, info)
	if err != nil {
		return Counts{}, err
	}
	if err := deleteSurveySubjectRows(ctx, tx, tenantID, info); err != nil {
		return Counts{}, err
	}

	feedbackIDs := info.feedbackIDs
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
	linkCount, voteCount, err := anonymizeCustomerRequestSubject(ctx, tx, tenantID, info.subjectKey)
	if err != nil {
		return Counts{}, err
	}
	counts.CustomerLinkCount = linkCount
	counts.VoteCount = voteCount
	return counts, nil
}

func lockSubjectSurveyInvitations(ctx context.Context, tx pgx.Tx, tenantID string, info *subjectMetadata) error {
	rows, err := tx.Query(ctx, `
		SELECT si.id
		FROM survey_invitations si
		WHERE si.tenant_id = $1 AND `+subjectSurveyInvitationClause(2, 3, 4)+`
		FOR UPDATE`,
		tenantID,
		info.feedbackIDTexts,
		info.subjectKey,
		info.subjectHashes,
	)
	if err != nil {
		return fmt.Errorf("lock survey invitations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("lock survey invitations rows: %w", err)
	}
	return nil
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

func countLockedSubject(ctx context.Context, tx pgx.Tx, tenantID string, info *subjectMetadata) (Counts, error) {
	var counts Counts
	if err := tx.QueryRow(
		ctx, `
		WITH subject_invitations AS (
			SELECT si.id
			FROM survey_invitations si
			WHERE si.tenant_id = $1 AND `+subjectSurveyInvitationClause(3, 4, 5)+`
		)
		SELECT
			(SELECT COUNT(*) FROM feedback_tag_assignments WHERE feedback_id = ANY($2)),
			(SELECT COUNT(*) FROM feedback_audit_log WHERE feedback_id = ANY($2)),
			(SELECT COUNT(*) FROM llm_audit WHERE feedback_id = ANY($2)),
			(SELECT COUNT(*) FROM notify_outbox WHERE feedback_id = ANY($2)),
			(SELECT COUNT(*) FROM reply_drafts WHERE feedback_id = ANY($2)),
			(SELECT COUNT(*) FROM reply_draft_revisions WHERE feedback_id = ANY($2)),
			(SELECT COUNT(*) FROM reply_draft_events WHERE feedback_id = ANY($2)),
			(SELECT COUNT(*) FROM reply_delivery_attempts WHERE feedback_id = ANY($2)),
			(SELECT COUNT(*) FROM subject_invitations),
			(SELECT COUNT(*)
			 FROM survey_responses sr
			 JOIN subject_invitations si ON si.id = sr.invitation_id
			 WHERE sr.tenant_id = $1),
			(SELECT COUNT(*)
			 FROM survey_low_score_reviews lsr
			 JOIN survey_responses sr
			   ON sr.tenant_id = lsr.tenant_id
			  AND sr.id = lsr.response_id
			 JOIN subject_invitations si ON si.id = sr.invitation_id
			 WHERE lsr.tenant_id = $1),
			(SELECT COUNT(*)
			 FROM survey_provider_events spe
			 JOIN subject_invitations si ON si.id = spe.invitation_id
			 WHERE spe.tenant_id = $1),
			(SELECT COUNT(*)
			 FROM survey_recovery_notifications srn
			 JOIN survey_responses sr
			   ON sr.tenant_id = srn.tenant_id
			  AND sr.id = srn.response_id
			 JOIN subject_invitations si ON si.id = sr.invitation_id
			 WHERE srn.tenant_id = $1)`,
		tenantID,
		info.feedbackIDs,
		info.feedbackIDTexts,
		info.subjectKey,
		info.subjectHashes,
	).Scan(
		&counts.TagAssignmentCount,
		&counts.FeedbackAuditCount,
		&counts.LLMAuditCount,
		&counts.OutboxCount,
		&counts.ReplyDraftCount,
		&counts.ReplyDraftRevisionCount,
		&counts.ReplyDraftEventCount,
		&counts.ReplyDeliveryAttemptCount,
		&counts.SurveyInvitationCount,
		&counts.SurveyResponseCount,
		&counts.SurveyLowScoreReviewCount,
		&counts.SurveyProviderEventCount,
		&counts.SurveyRecoveryNotificationCount,
	); err != nil {
		return Counts{}, fmt.Errorf("count subject-linked rows: %w", err)
	}
	counts.FeedbackCount = len(info.feedbackIDs)
	return counts, nil
}

func deleteSurveySubjectRows(ctx context.Context, tx pgx.Tx, tenantID string, info *subjectMetadata) error {
	args := []any{tenantID, info.feedbackIDTexts, info.subjectKey, info.subjectHashes}
	if _, err := tx.Exec(ctx, incrementNPSRunRedactionCountsSQL(), args...); err != nil {
		return fmt.Errorf("record survey campaign run redactions: %w", err)
	}
	for _, stmt := range []struct {
		name string
		sql  string
	}{
		{name: "survey_recovery_notifications", sql: deleteSurveyRecoveryNotificationsSQL()},
		{name: "survey_low_score_reviews", sql: deleteSurveyLowScoreReviewsSQL()},
		{name: "survey_provider_events", sql: deleteSurveyProviderEventsSQL()},
		{name: "survey_responses", sql: deleteSurveyResponsesSQL()},
		{name: "survey_invitations", sql: deleteSurveyInvitationsSQL()},
	} {
		if _, err := tx.Exec(ctx, stmt.sql, args...); err != nil {
			return fmt.Errorf("delete %s rows: %w", stmt.name, err)
		}
	}
	return nil
}

// incrementNPSRunRedactionCountsSQL preserves the aggregate interpretation of
// a completed NPS run after GDPR removes an individual's response and its
// feedback bridge through the response foreign key cascade.
func incrementNPSRunRedactionCountsSQL() string {
	return subjectSurveyInvitationCTE() + `,
	redacted_runs AS (
		SELECT si.run_id, COUNT(*) AS response_count
		FROM survey_responses sr
		JOIN subject_invitations si ON si.id = sr.invitation_id
		WHERE sr.tenant_id = $1
		  AND sr.survey_type = 'nps'
		  AND si.run_id IS NOT NULL
		GROUP BY si.run_id
	)
	UPDATE survey_campaign_runs run
	SET redacted_response_count = run.redacted_response_count + redacted_runs.response_count
	FROM redacted_runs
	WHERE run.tenant_id = $1
	  AND run.id = redacted_runs.run_id`
}

func subjectSurveyInvitationClause(feedbackIDsArg, subjectKeyArg, subjectHashesArg int) string {
	return fmt.Sprintf(`(
		(
				si.source_type IN ('workflow_transition', 'reply_sent', 'manual_link', 'request_resolved')
			AND si.source_id = ANY($%d)
		)
		OR si.recipient_snapshot->>'feedback_id' = ANY($%d)
		OR EXISTS (
			SELECT 1
			FROM customer_notification_contacts c
			WHERE c.tenant_id = si.tenant_id
			  AND c.id = si.contact_id
			  AND (
				(c.subject_key <> '' AND c.subject_key = $%d)
				OR (c.subject_hash <> '' AND c.subject_hash = ANY($%d))
			  )
		)
	)`, feedbackIDsArg, feedbackIDsArg, subjectKeyArg, subjectHashesArg)
}

func subjectSurveyInvitationCTE() string {
	return `WITH subject_invitations AS (
		SELECT si.id, si.run_id
		FROM survey_invitations si
		WHERE si.tenant_id = $1 AND ` + subjectSurveyInvitationClause(2, 3, 4) + `
	)`
}

func deleteSurveyRecoveryNotificationsSQL() string {
	return subjectSurveyInvitationCTE() + `
	DELETE FROM survey_recovery_notifications srn
	USING survey_responses sr, subject_invitations si
	WHERE srn.tenant_id = $1
	  AND sr.tenant_id = srn.tenant_id
	  AND sr.id = srn.response_id
	  AND sr.invitation_id = si.id`
}

func deleteSurveyLowScoreReviewsSQL() string {
	return subjectSurveyInvitationCTE() + `
	DELETE FROM survey_low_score_reviews lsr
	USING survey_responses sr, subject_invitations si
	WHERE lsr.tenant_id = $1
	  AND sr.tenant_id = lsr.tenant_id
	  AND sr.id = lsr.response_id
	  AND sr.invitation_id = si.id`
}

func deleteSurveyProviderEventsSQL() string {
	return subjectSurveyInvitationCTE() + `
	DELETE FROM survey_provider_events spe
	USING subject_invitations si
	WHERE spe.tenant_id = $1
	  AND spe.invitation_id = si.id`
}

func deleteSurveyResponsesSQL() string {
	return subjectSurveyInvitationCTE() + `
	DELETE FROM survey_responses sr
	USING subject_invitations si
	WHERE sr.tenant_id = $1
	  AND sr.invitation_id = si.id`
}

func deleteSurveyInvitationsSQL() string {
	return subjectSurveyInvitationCTE() + `
	DELETE FROM survey_invitations si
	USING subject_invitations subject_si
	WHERE si.tenant_id = $1
	  AND si.id = subject_si.id`
}

type subjectMetadata struct {
	feedbackIDs     []int64
	feedbackIDTexts []string
	subjectKey      string
	subjectHashes   []string
	subjectDisplay  string
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
		WITH matching_feedback AS (
			SELECT id AS feedback_id,
			       COALESCE(NULLIF(subject_display, ''), subject_key) AS subject_display,
			       subject_hash
			FROM user_feedback
			WHERE tenant_id = $1 AND `+subjectFilter+`
			FOR UPDATE
		), matching_contacts AS (
			SELECT 0::BIGINT AS feedback_id,
			       COALESCE(NULLIF(display_name, ''), subject_key) AS subject_display,
			       subject_hash
			FROM customer_notification_contacts
			WHERE tenant_id = $1
			  AND subject_key <> ''
			  AND subject_key = $2
			FOR UPDATE
		)
		SELECT feedback_id, subject_display, subject_hash
		FROM (
			SELECT feedback_id, subject_display, subject_hash FROM matching_feedback
			UNION ALL
			SELECT feedback_id, subject_display, subject_hash FROM matching_contacts
		) subject_rows
		ORDER BY CASE WHEN feedback_id = 0 THEN 1 ELSE 0 END, feedback_id`, tenantID, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("query subject identity rows: %w", err)
	}
	defer rows.Close()

	info := ptrext.Of(subjectMetadata{})
	info.subjectKey = strings.TrimSpace(subjectKey)
	found := false
	for rows.Next() {
		found = true
		var id int64
		var subjectDisplay string
		var subjectHash string
		if err := rows.Scan(&id, &subjectDisplay, &subjectHash); err != nil {
			return nil, fmt.Errorf("scan subject feedback ids: %w", err)
		}
		if id > 0 {
			info.feedbackIDs = append(info.feedbackIDs, id)
			info.feedbackIDTexts = append(info.feedbackIDTexts, fmt.Sprintf("%d", id))
		}
		info.subjectHashes = appendUniqueNonEmpty(info.subjectHashes, subjectHash)
		if info.subjectDisplay == "" {
			info.subjectDisplay = subjectDisplay
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subject feedback ids: %w", err)
	}
	if !found {
		return nil, ErrSubjectNotFound
	}
	if info.subjectDisplay == "" {
		info.subjectDisplay = info.subjectKey
	}
	return info, nil
}

func appendUniqueNonEmpty(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
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
