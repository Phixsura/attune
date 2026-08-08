// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

const responseColumns = `
	id, tenant_id, campaign_id, survey_type, invitation_id, request_id, contact_id,
	source_type, source_id, score, nps_bucket, follow_up_consent, comment, locale, metadata,
	user_agent_hash, ip_hash, submitted_at, created_at`

const qualifiedResponseColumns = `
	sr.id, sr.tenant_id, sr.campaign_id, sr.survey_type, sr.invitation_id, sr.request_id, sr.contact_id,
	sr.source_type, sr.source_id, sr.score, sr.nps_bucket, sr.follow_up_consent, sr.comment, sr.locale, sr.metadata,
	sr.user_agent_hash, sr.ip_hash, sr.submitted_at, sr.created_at`

const qualifiedResponseFeedbackIDColumn = `srfl.feedback_id`

const lowScoreReviewColumns = `
	response_id, tenant_id, campaign_id, status, severity, owner_member_id,
	root_cause, action_taken, customer_contacted, due_at, initial_due_at, customer_contacted_at, first_terminal_at, reviewed_at,
	updated_by, created_at, updated_at`

const qualifiedLowScoreReviewColumns = `
	lsr.response_id, lsr.tenant_id, lsr.campaign_id, lsr.status, lsr.severity, lsr.owner_member_id,
	lsr.root_cause, lsr.action_taken, lsr.customer_contacted, lsr.due_at, lsr.initial_due_at, lsr.customer_contacted_at, lsr.first_terminal_at, lsr.reviewed_at,
	lsr.updated_by, lsr.created_at, lsr.updated_at`

const qualifiedRecoveryNotificationSummaryColumns = `
	COALESCE(srn.status, ''), COALESCE(srn.reason, ''), srn.delivered_at,
	COALESCE(srn.last_error, '')`

const responseInvitationJoin = `
	JOIN survey_invitations si
	  ON si.tenant_id = sr.tenant_id AND si.id = sr.invitation_id`

const responseFeedbackLinkJoin = `
	LEFT JOIN survey_response_feedback_links srfl
	  ON srfl.tenant_id = sr.tenant_id AND srfl.response_id = sr.id`

func (r *Repo) CreateResponse(ctx context.Context, response Response, review *LowScoreReviewSeed) (Response, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := createResponseTx(ctx, tx, response, review)
	if err != nil {
		if errors.Is(err, ErrInvitationExpired) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return Response{}, commitErr
			}
		}
		return Response{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Response{}, err
	}
	return item, nil
}

// CreateResponseTx persists a response and its review inside the caller's
// transaction. It is used by NPS so a comment-derived feedback signal can be
// committed with the response, never ahead of it or after it.
func (r *Repo) CreateResponseTx(ctx context.Context, tx pgx.Tx, response Response, review *LowScoreReviewSeed) (Response, error) {
	return createResponseTx(ctx, tx, response, review)
}

func createResponseTx(ctx context.Context, tx pgx.Tx, response Response, review *LowScoreReviewSeed) (Response, error) {
	if err := lockResponseInvitation(ctx, tx, response); err != nil {
		return Response{}, err
	}
	var err error
	response, err = populateResponseSurveyType(ctx, tx, response)
	if err != nil {
		return Response{}, err
	}
	metadataRaw, err := jsonObject(response.Metadata)
	if err != nil {
		return Response{}, err
	}
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO survey_responses (
			id, tenant_id, campaign_id, survey_type, invitation_id, request_id, contact_id,
			source_type, source_id, score, nps_bucket, follow_up_consent, comment, locale, metadata,
			user_agent_hash, ip_hash, submitted_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
		RETURNING %s`, responseColumns),
		response.ID,
		strings.TrimSpace(response.TenantID),
		response.CampaignID,
		response.SurveyType,
		response.InvitationID,
		nullableUUID(response.RequestID),
		nullableUUID(response.ContactID),
		response.SourceType,
		response.SourceID,
		response.Score,
		response.NPSBucket,
		response.FollowUpConsent,
		response.Comment,
		response.Locale,
		metadataRaw,
		response.UserAgentHash,
		response.IPHash,
		response.SubmittedAt,
	)
	item, err := scanResponse(row)
	if err != nil {
		return Response{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE survey_invitations
		SET response_status = 'completed',
		    responded_at = $3
		WHERE tenant_id = $1 AND id = $2`,
		item.TenantID,
		item.InvitationID,
		item.SubmittedAt,
	); err != nil {
		return Response{}, mapWriteError(err)
	}
	if review != nil {
		created, err := createLowScoreReviewTx(ctx, tx, item, ptrext.Indirect(review))
		if err != nil {
			return Response{}, err
		}
		item.Review = ptrext.Of(created)
	}
	return item, nil
}

// lockResponseInvitation makes the invitation deadline part of the response
// transaction. The database clock is read after the row lock is held so a
// request that waits across its deadline cannot be counted as a response.
func lockResponseInvitation(ctx context.Context, tx pgx.Tx, response Response) error {
	invitationCampaignID := ptrext.Of(uuid.Nil)
	responseStatus := ptrext.Of("")
	suppressionStatus := ptrext.Of("")
	expiresAt := ptrext.Of(time.Time{})
	hasExpiry := ptrext.Of(false)
	campaignStatus := ptrext.Of("")
	if err := tx.QueryRow(ctx, `
		SELECT si.campaign_id,
		       si.response_status,
		       si.suppression_status,
		       COALESCE(si.expires_at, 'epoch'::timestamptz),
		       si.expires_at IS NOT NULL,
		       sc.status
		FROM survey_invitations si
		JOIN survey_campaigns sc
		  ON sc.tenant_id = si.tenant_id AND sc.id = si.campaign_id
		WHERE si.tenant_id = $1 AND si.id = $2
		FOR UPDATE OF si
		FOR SHARE OF sc`,
		strings.TrimSpace(response.TenantID),
		response.InvitationID,
	).Scan(invitationCampaignID, responseStatus, suppressionStatus, expiresAt, hasExpiry, campaignStatus); err != nil {
		return mapNotFound(err)
	}
	if ptrext.Indirect(invitationCampaignID) != response.CampaignID {
		return ErrInvalidInput
	}
	switch ptrext.Indirect(responseStatus) {
	case ResponseCompleted:
		return ErrConflict
	case ResponseExpired:
		return ErrInvitationExpired
	}
	if ptrext.Indirect(suppressionStatus) != SuppressionNotSuppressed {
		return ErrCampaignNotActive
	}
	databaseNow := ptrext.Of(time.Time{})
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(databaseNow); err != nil {
		return fmt.Errorf("read survey response deadline clock: %w", err)
	}
	if !ptrext.Indirect(hasExpiry) || ptrext.Indirect(databaseNow).Before(ptrext.Indirect(expiresAt)) {
		if ptrext.Indirect(campaignStatus) == StatusActive {
			return nil
		}
		return ErrCampaignNotActive
	}
	if _, err := tx.Exec(ctx, `
		UPDATE survey_invitations
		SET response_status = 'expired',
		    delivery_status = CASE
		        WHEN delivery_status IN ('pending', 'delayed') THEN 'not_applicable'
		        ELSE delivery_status
		    END,
		    delivery_secret = NULL,
		    suppression_reason = 'expired',
		    claimed_at = NULL,
		    claimed_by = ''
		WHERE tenant_id = $1 AND id = $2`,
		strings.TrimSpace(response.TenantID),
		response.InvitationID,
	); err != nil {
		return mapWriteError(err)
	}
	return ErrInvitationExpired
}

func populateResponseSurveyType(ctx context.Context, tx pgx.Tx, response Response) (Response, error) {
	if strings.TrimSpace(response.SurveyType) != "" {
		return response, nil
	}
	surveyType := ptrext.Of("")
	if err := tx.QueryRow(ctx, `
		SELECT survey_type
		FROM survey_campaigns
		WHERE tenant_id = $1 AND id = $2`, response.TenantID, response.CampaignID).Scan(surveyType); err != nil {
		return Response{}, mapNotFound(err)
	}
	response.SurveyType = ptrext.Indirect(surveyType)
	return response, nil
}

func createLowScoreReviewTx(ctx context.Context, tx pgx.Tx, response Response, seed LowScoreReviewSeed) (LowScoreReview, error) {
	severity := strings.TrimSpace(seed.Severity)
	if severity == "" {
		severity = SeverityMedium
	}
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO survey_low_score_reviews (
			response_id, tenant_id, campaign_id, status, severity, owner_member_id, due_at, initial_due_at, updated_by
		) VALUES ($1, $2, $3, 'open', $4, $5, $6, $7, $8)
		RETURNING %s`, lowScoreReviewColumns),
		response.ID,
		response.TenantID,
		response.CampaignID,
		severity,
		nullableUUID(seed.OwnerMemberID),
		seed.DueAt,
		seed.DueAt,
		strings.TrimSpace(seed.UpdatedBy),
	)
	review, err := scanLowScoreReview(row)
	if err != nil {
		return LowScoreReview{}, mapWriteError(err)
	}
	return review, nil
}

func (r *Repo) ListResponses(ctx context.Context, filter ResponseFilter) ([]Response, error) {
	where := []string{"sr.tenant_id = $1"}
	args := []any{strings.TrimSpace(filter.TenantID)}
	lowScoreOnly := responseListRequiresReview(filter)
	if filter.CampaignID != nil {
		where, args = appendFilter(where, args, "sr.campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if filter.SubmittedFrom != nil {
		where, args = appendFilter(where, args, "sr.submitted_at >= $%d", ptrext.Indirect(filter.SubmittedFrom))
	}
	if filter.SubmittedTo != nil {
		where, args = appendFilter(where, args, "sr.submitted_at < $%d", ptrext.Indirect(filter.SubmittedTo))
	}
	if filter.ReviewSeverity != "" {
		where, args = appendFilter(where, args, "lsr.severity = $%d", filter.ReviewSeverity)
	}
	if filter.OwnerMemberID != nil {
		where, args = appendFilter(where, args, "lsr.owner_member_id = $%d", ptrext.Indirect(filter.OwnerMemberID))
	}
	if accountKey := strings.TrimSpace(filter.AccountKey); accountKey != "" {
		where, args = appendFilter(where, args, "("+surveyResponseAccountKeySQL()+") = $%d", accountKey)
	}
	where = appendRecoveryResponseFilters(where, filter)
	args = append(args, boundedLimit(filter.Limit))
	query := fmt.Sprintf(`
		SELECT %s
		FROM survey_responses sr
		%s
		%s
		%s
		%s
		%s
		LIMIT $%d`, responseListColumns(lowScoreOnly), responseInvitationJoin, responseFeedbackLinkJoin, lowScoreReviewJoin(lowScoreOnly), whereClause(where), responseListOrder(lowScoreOnly), len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list survey responses: %w", err)
	}
	defer rows.Close()
	var items []Response
	for rows.Next() {
		item, err := r.scanListedResponse(ctx, rows, lowScoreOnly)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list survey responses rows: %w", err)
	}
	return items, nil
}

func responseListRequiresReview(filter ResponseFilter) bool {
	if filter.LowScoreOnly != nil && ptrext.Indirect(filter.LowScoreOnly) {
		return true
	}
	return filter.RecoverySLAStatus != "" ||
		filter.RecoveryBlockerReason != "" ||
		filter.ReviewSeverity != "" ||
		filter.OwnerMemberID != nil
}

func responseListColumns(lowScoreOnly bool) string {
	if !lowScoreOnly {
		return qualifiedResponseColumns + ", " + qualifiedResponseFeedbackIDColumn + ", " + surveyResponseAccountColumns()
	}
	return qualifiedResponseColumns + ", " + qualifiedResponseFeedbackIDColumn + ", " + qualifiedLowScoreReviewColumns + ", " +
		qualifiedRecoveryNotificationSummaryColumns + ", " + surveyResponseAccountColumns()
}

func (r *Repo) scanListedResponse(ctx context.Context, row pgx.Row, lowScoreOnly bool) (Response, error) {
	if lowScoreOnly {
		return scanResponseWithLowScoreReviewAndAccount(row)
	}
	item, err := scanResponseWithAccount(row)
	if err != nil {
		return Response{}, err
	}
	review, err := r.GetLowScoreReview(ctx, item.TenantID, item.ID)
	if err == nil {
		item.Review = ptrext.Of(review)
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Response{}, err
	}
	return item, nil
}

func surveyResponseAccountColumns() string {
	return surveyResponseAccountKeySQL() + ", " +
		surveyResponseAccountDisplaySQL() + ", " +
		surveyResponseAccountSourceSQL()
}

func surveyResponseAccountKeySQL() string {
	return "COALESCE(" +
		surveyAccountKeyCandidates("sr.metadata") + ", " +
		surveyAccountKeyCandidates("si.recipient_snapshot") + ", " +
		"''" +
		")"
}

func surveyResponseAccountDisplaySQL() string {
	key := surveyResponseAccountKeySQL()
	return "COALESCE(" +
		surveyAccountDisplayCandidates("sr.metadata") + ", " +
		surveyAccountDisplayCandidates("si.recipient_snapshot") + ", " +
		key +
		")"
}

func surveyResponseAccountSourceSQL() string {
	return "CASE " +
		"WHEN COALESCE(" + surveyAccountKeyCandidates("sr.metadata") + ") IS NOT NULL THEN 'response_metadata' " +
		"WHEN COALESCE(" + surveyAccountKeyCandidates("si.recipient_snapshot") + ") IS NOT NULL THEN 'recipient_snapshot' " +
		"ELSE '' END"
}

func surveyAccountKeyCandidates(expr string) string {
	return strings.Join([]string{
		"NULLIF(BTRIM(" + expr + "->>'account_key'), '')",
		"NULLIF(BTRIM(" + expr + "->>'accountKey'), '')",
		"NULLIF(BTRIM(" + expr + "->>'company_id'), '')",
		"NULLIF(BTRIM(" + expr + "->>'companyId'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{account,key}'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{account,id}'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{account,account_key}'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{account,accountKey}'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{company,key}'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{company,id}'), '')",
	}, ", ")
}

func surveyAccountDisplayCandidates(expr string) string {
	return strings.Join([]string{
		"NULLIF(BTRIM(" + expr + "->>'account_display'), '')",
		"NULLIF(BTRIM(" + expr + "->>'accountDisplay'), '')",
		"NULLIF(BTRIM(" + expr + "->>'company_name'), '')",
		"NULLIF(BTRIM(" + expr + "->>'companyName'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{account,name}'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{account,display}'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{company,name}'), '')",
		"NULLIF(BTRIM(" + expr + " #>> '{company,display}'), '')",
	}, ", ")
}

func lowScoreReviewJoin(enabled bool) string {
	if !enabled {
		return ""
	}
	return `JOIN survey_low_score_reviews lsr
		  ON lsr.tenant_id = sr.tenant_id AND lsr.response_id = sr.id
		LEFT JOIN LATERAL (
			SELECT status, reason, delivered_at, last_error
			FROM survey_recovery_notifications srn
			WHERE srn.tenant_id = sr.tenant_id
			  AND srn.response_id = sr.id
			ORDER BY srn.created_at DESC, srn.id DESC
			LIMIT 1
		) srn ON TRUE`
}

func responseListOrder(lowScoreOnly bool) string {
	if !lowScoreOnly {
		return "ORDER BY sr.submitted_at DESC, sr.id DESC"
	}
	return `ORDER BY
			CASE WHEN lsr.status IN ('resolved', 'dismissed') THEN 1 ELSE 0 END,
			CASE
				WHEN lsr.status IN ('open', 'in_review') AND lsr.due_at IS NOT NULL AND lsr.due_at < NOW() THEN 0
				ELSE 1
			END,
			lsr.due_at ASC NULLS LAST,
			CASE lsr.severity
				WHEN 'critical' THEN 0
				WHEN 'high' THEN 1
				WHEN 'medium' THEN 2
				WHEN 'low' THEN 3
				ELSE 4
			END,
			sr.submitted_at DESC,
			sr.id DESC`
}

const (
	activeReviewWhere        = "lsr.status IN ('open', 'in_review')"
	terminalReviewWhere      = "lsr.status IN ('resolved', 'dismissed')"
	overdueReviewWhere       = activeReviewWhere + " AND lsr.due_at IS NOT NULL AND lsr.due_at < NOW()"
	notOverdueReviewWhere    = "(lsr.due_at IS NULL OR lsr.due_at >= NOW())"
	ownerAssignedWhere       = "lsr.owner_member_id IS NOT NULL"
	dueAssignedWhere         = "lsr.due_at IS NOT NULL"
	customerContactedWhere   = "lsr.customer_contacted"
	rootCauseMissingWhere    = "btrim(COALESCE(lsr.root_cause, '')) = ''"
	rootCauseCapturedWhere   = "btrim(COALESCE(lsr.root_cause, '')) <> ''"
	actionTakenMissingWhere  = "btrim(COALESCE(lsr.action_taken, '')) = ''"
	actionTakenCapturedWhere = "btrim(COALESCE(lsr.action_taken, '')) <> ''"
)

func appendRecoveryResponseFilters(where []string, filter ResponseFilter) []string {
	if condition := recoverySLAWhere(filter.RecoverySLAStatus); condition != "" {
		where = append(where, condition)
	}
	if condition := recoveryBlockerWhere(filter.RecoveryBlockerReason); condition != "" {
		where = append(where, condition)
	}
	return where
}

func recoverySLAWhere(value string) string {
	switch value {
	case RecoverySLAOnTrack:
		return "(" + activeReviewWhere + " AND (lsr.due_at IS NULL OR lsr.due_at >= NOW() + INTERVAL '24 hours'))"
	case RecoverySLADueSoon:
		return "(" + activeReviewWhere + " AND lsr.due_at IS NOT NULL AND lsr.due_at >= NOW() AND lsr.due_at < NOW() + INTERVAL '24 hours')"
	case RecoverySLAOverdue:
		return "(" + overdueReviewWhere + ")"
	case RecoverySLAClosed:
		return "(" + terminalReviewWhere + ")"
	default:
		return ""
	}
}

func recoveryBlockerWhere(value string) string {
	switch value {
	case RecoveryBlockerNone:
		return "(" + terminalReviewWhere + " OR (" + activeUnblockedReviewWhere() + "))"
	case RecoveryBlockerOverdue:
		return "(" + overdueReviewWhere + ")"
	case RecoveryBlockerOwner:
		return "(" + activeNotOverdueReviewWhere() + " AND lsr.owner_member_id IS NULL)"
	case RecoveryBlockerDue:
		return "(" + activeReviewWhere + " AND " + ownerAssignedWhere + " AND lsr.due_at IS NULL)"
	case RecoveryBlockerContact:
		return "(" + activeNotOverdueReviewWhere() + " AND " + ownerAssignedWhere + " AND " +
			dueAssignedWhere + " AND NOT lsr.customer_contacted)"
	case RecoveryBlockerRootCause:
		return "(" + activeNotOverdueReviewWhere() + " AND " + ownerAssignedWhere + " AND " +
			dueAssignedWhere + " AND " + customerContactedWhere + " AND " + rootCauseMissingWhere + ")"
	case RecoveryBlockerAction:
		return "(" + activeNotOverdueReviewWhere() + " AND " + ownerAssignedWhere + " AND " +
			dueAssignedWhere + " AND " + customerContactedWhere + " AND " + rootCauseCapturedWhere +
			" AND " + actionTakenMissingWhere + ")"
	default:
		return ""
	}
}

func activeNotOverdueReviewWhere() string {
	return activeReviewWhere + " AND " + notOverdueReviewWhere
}

func activeUnblockedReviewWhere() string {
	return activeNotOverdueReviewWhere() + " AND " + ownerAssignedWhere + " AND " + dueAssignedWhere +
		" AND " + customerContactedWhere + " AND " + rootCauseCapturedWhere + " AND " +
		actionTakenCapturedWhere
}

func (r *Repo) GetResponseByInvitation(ctx context.Context, tenantID string, invitationID uuid.UUID) (Response, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s, %s
		FROM survey_responses sr
		%s
		WHERE sr.tenant_id = $1 AND sr.invitation_id = $2`,
		qualifiedResponseColumns,
		qualifiedResponseFeedbackIDColumn,
		responseFeedbackLinkJoin,
	),
		strings.TrimSpace(tenantID),
		invitationID,
	)
	item, err := scanResponseWithFeedbackID(row)
	if err != nil {
		return Response{}, err
	}
	review, err := r.GetLowScoreReview(ctx, item.TenantID, item.ID)
	if err == nil {
		item.Review = ptrext.Of(review)
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Response{}, err
	}
	return item, nil
}

func (r *Repo) GetLowScoreReview(ctx context.Context, tenantID string, responseID uuid.UUID) (LowScoreReview, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		FROM survey_low_score_reviews
		WHERE tenant_id = $1 AND response_id = $2`, lowScoreReviewColumns),
		strings.TrimSpace(tenantID),
		responseID,
	)
	return scanLowScoreReview(row)
}

func (r *Repo) UpdateLowScoreReview(ctx context.Context, review LowScoreReview) (LowScoreReview, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE survey_low_score_reviews
		SET status = $3,
		    severity = $4,
		    owner_member_id = $5,
		    root_cause = $6,
		    action_taken = $7,
		    customer_contacted = $8,
		    customer_contacted_at = CASE
	        WHEN NOT customer_contacted AND $8 THEN COALESCE(customer_contacted_at, NOW())
	        ELSE customer_contacted_at
	    END,
		    due_at = $9,
		    reviewed_at = CASE
	        WHEN $3 IN ('resolved', 'dismissed') THEN COALESCE(reviewed_at, NOW())
	        ELSE reviewed_at
	    END,
		    first_terminal_at = CASE
	        WHEN status NOT IN ('resolved', 'dismissed')
	             AND $3 IN ('resolved', 'dismissed')
	             AND NOT terminal_timeliness_unknown THEN COALESCE(first_terminal_at, NOW())
	        ELSE first_terminal_at
	    END,
		    claimed_at = NULL,
		    claimed_by = '',
		    updated_by = $10
		WHERE tenant_id = $1 AND response_id = $2
		RETURNING %s`, lowScoreReviewColumns),
		strings.TrimSpace(review.TenantID),
		review.ResponseID,
		review.Status,
		review.Severity,
		nullableUUID(review.OwnerMemberID),
		review.RootCause,
		review.ActionTaken,
		review.CustomerContacted,
		review.DueAt,
		review.UpdatedBy,
	)
	item, err := scanLowScoreReview(row)
	if err != nil {
		return LowScoreReview{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) ClaimLowScoreReviewsForRecoveryAutomation(
	ctx context.Context,
	limit int,
	owner string,
) ([]LowScoreReview, error) {
	rows, err := r.pool.Query(ctx, claimLowScoreReviewsForRecoveryAutomationQuery(),
		boundedLimit(limit),
		pgxutil.Truncate(strings.TrimSpace(owner), 256),
	)
	if err != nil {
		return nil, fmt.Errorf("claim survey low-score recovery automation: %w", err)
	}
	defer rows.Close()
	var items []LowScoreReview
	for rows.Next() {
		item, err := scanLowScoreReview(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim survey low-score recovery automation rows: %w", err)
	}
	return items, nil
}

func claimLowScoreReviewsForRecoveryAutomationQuery() string {
	return fmt.Sprintf(`
		UPDATE survey_low_score_reviews
		   SET claimed_at = NOW(),
		       claimed_by = $2
		 WHERE response_id IN (
			SELECT response_id
			  FROM survey_low_score_reviews
			 WHERE status IN ('open', 'in_review')
			   AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
			   AND POSITION('automation=survey_recovery_worker' IN action_taken) = 0
			   AND (
			      due_at IS NULL
			      OR due_at < NOW()
			      OR severity = 'critical'
			      OR (owner_member_id IS NULL AND created_at <= NOW() - INTERVAL '24 hours')
			   )
			 ORDER BY
			   CASE WHEN due_at IS NOT NULL AND due_at < NOW() THEN 0 ELSE 1 END,
			   CASE WHEN severity = 'critical' THEN 0 ELSE 1 END,
			   CASE WHEN owner_member_id IS NULL THEN 0 ELSE 1 END,
			   due_at ASC NULLS FIRST,
			   updated_at ASC,
			   response_id ASC
			 LIMIT $1
			 FOR UPDATE SKIP LOCKED
		 )
		RETURNING %s`, lowScoreReviewColumns)
}

func (r *Repo) Analytics(ctx context.Context, filter AnalyticsFilter) (Analytics, error) {
	invitations, err := r.invitationCounts(ctx, filter)
	if err != nil {
		return Analytics{}, err
	}
	responses, err := r.responseCounts(ctx, filter)
	if err != nil {
		return Analytics{}, err
	}
	distribution, err := r.scoreDistribution(ctx, filter)
	if err != nil {
		return Analytics{}, err
	}
	suppressionReasons, err := r.suppressionReasonDistribution(ctx, filter)
	if err != nil {
		return Analytics{}, err
	}
	reviews, err := r.lowScoreReviewCounts(ctx, filter)
	if err != nil {
		return Analytics{}, err
	}
	ownerLoads, err := r.lowScoreOwnerLoads(ctx, filter)
	if err != nil {
		return Analytics{}, err
	}
	out := Analytics{
		CampaignID:                         filter.CampaignID,
		InvitationCount:                    invitations.InvitationCount,
		DeliveredCount:                     invitations.DeliveredCount,
		SuppressedCount:                    invitations.SuppressedCount,
		NotStartedCount:                    invitations.NotStartedCount,
		OpenedCount:                        invitations.OpenedCount,
		StartedCount:                       invitations.StartedCount,
		ExpiredCount:                       invitations.ExpiredCount,
		PendingDeliveryCount:               invitations.PendingDeliveryCount,
		DelayedDeliveryCount:               invitations.DelayedDeliveryCount,
		RejectedDeliveryCount:              invitations.RejectedDeliveryCount,
		CompletedCount:                     responses.CompletedCount,
		LowScoreCount:                      responses.LowScoreCount,
		PositiveScoreCount:                 responses.PositiveScoreCount,
		QualityFlaggedResponseCount:        responses.QualityFlaggedResponseCount,
		OpenLowScoreReviewCount:            reviews.OpenCount,
		OverdueLowScoreReviewCount:         reviews.OverdueCount,
		UnassignedLowScoreReviewCount:      reviews.UnassignedCount,
		CriticalLowScoreReviewCount:        reviews.CriticalCount,
		PendingCustomerContactReviewCount:  reviews.PendingCustomerContactCount,
		OldestOpenLowScoreReviewDueAt:      reviews.OldestOpenDueAt,
		OverdueRecoveryQueueCount:          reviews.OverdueQueueCount,
		UnassignedRecoveryQueueCount:       reviews.UnassignedQueueCount,
		PendingContactRecoveryQueueCount:   reviews.PendingContactQueueCount,
		MissingRootCauseRecoveryQueueCount: reviews.MissingRootCauseQueueCount,
		MissingActionRecoveryQueueCount:    reviews.MissingActionQueueCount,
		RecoveryOutcome:                    reviews.Outcome,
		AverageScore:                       responses.AverageScore,
		AverageResponseSeconds:             responses.AverageResponseSeconds,
		ScoreDistribution:                  distribution,
		SuppressionReasons:                 suppressionReasons,
		OwnerRecoveryLoads:                 ownerLoads,
	}
	npsMetrics, err := r.npsMetrics(ctx, filter)
	if err != nil {
		return Analytics{}, err
	}
	out.NPS = npsMetrics.Score
	out.NPSAvailable = npsMetrics.Available
	out.DetractorCount = npsMetrics.DetractorCount
	out.PassiveCount = npsMetrics.PassiveCount
	out.PromoterCount = npsMetrics.PromoterCount
	out.RedactedResponseCount = npsMetrics.RedactedResponseCount
	if out.InvitationCount > 0 {
		out.ResponseRate = float64(out.CompletedCount) / float64(out.InvitationCount)
		out.StartRate = float64(out.StartedCount) / float64(out.InvitationCount)
	}
	if out.StartedCount > 0 {
		out.CompletionRate = float64(out.CompletedCount) / float64(out.StartedCount)
	}
	if out.CompletedCount > 0 {
		out.PositiveScoreRate = float64(out.PositiveScoreCount) / float64(out.CompletedCount)
	}
	return out, nil
}

func (r *Repo) AnalyticsTrend(ctx context.Context, filter AnalyticsFilter) ([]AnalyticsTrendBucket, error) {
	from := ptrext.Indirect(filter.From)
	to := ptrext.Indirect(filter.To)
	args := []any{strings.TrimSpace(filter.TenantID), from, to}
	invitationCampaignFilter := ""
	responseCampaignFilter := ""
	if filter.CampaignID != nil {
		args = append(args, ptrext.Indirect(filter.CampaignID))
		invitationCampaignFilter = fmt.Sprintf("AND campaign_id = $%d", len(args))
		responseCampaignFilter = fmt.Sprintf("AND sr.campaign_id = $%d", len(args))
	}
	if filter.RunID != nil {
		args = append(args, ptrext.Indirect(filter.RunID))
		invitationCampaignFilter += fmt.Sprintf(" AND run_id = $%d", len(args))
		responseCampaignFilter += fmt.Sprintf(" AND si.run_id = $%d", len(args))
	}
	rows, err := r.pool.Query(ctx, analyticsTrendQuery(invitationCampaignFilter, responseCampaignFilter), args...)
	if err != nil {
		return nil, fmt.Errorf("survey analytics trend: %w", err)
	}
	defer rows.Close()
	var out []AnalyticsTrendBucket
	for rows.Next() {
		var bucket AnalyticsTrendBucket
		if err := rows.Scan(
			&bucket.Date,
			&bucket.InvitationCount,
			&bucket.DeliveredCount,
			&bucket.SuppressedCount,
			&bucket.CompletedCount,
			&bucket.LowScoreCount,
			&bucket.PositiveScoreCount,
			&bucket.AverageScore,
			&bucket.ResponseRate,
			&bucket.NotStartedCount,
			&bucket.OpenedCount,
			&bucket.StartedCount,
			&bucket.ExpiredCount,
			&bucket.NPS,
			&bucket.NPSAvailable,
			&bucket.DetractorCount,
			&bucket.PassiveCount,
			&bucket.PromoterCount,
			&bucket.RedactedResponseCount,
			&bucket.QualityFlaggedResponseCount,
		); err != nil {
			return nil, err
		}
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("survey analytics trend rows: %w", err)
	}
	return out, nil
}

func analyticsTrendQuery(invitationCampaignFilter string, responseCampaignFilter string) string {
	return fmt.Sprintf(`
		WITH days AS (
			SELECT generate_series(
				($2::timestamptz AT TIME ZONE 'UTC')::date,
				(($3::timestamptz - interval '1 microsecond') AT TIME ZONE 'UTC')::date,
				interval '1 day'
			)::date AS day
		),
		invitation_daily AS (
			SELECT
				(created_at AT TIME ZONE 'UTC')::date AS day,
				COUNT(*) AS invitation_count,
				COUNT(*) FILTER (WHERE delivery_status IN ('accepted', 'delivered')) AS delivered_count,
				COUNT(*) FILTER (WHERE suppression_status = 'suppressed') AS suppressed_count,
				COUNT(*) FILTER (WHERE response_status = 'not_started') AS not_started_count,
				COUNT(*) FILTER (WHERE response_status = 'opened' OR opened_at IS NOT NULL) AS opened_count,
				COUNT(*) FILTER (WHERE response_status IN ('started', 'completed')) AS started_count,
				COUNT(*) FILTER (WHERE response_status = 'expired') AS expired_count
			FROM survey_invitations
			WHERE tenant_id = $1
			  AND created_at >= $2
			  AND created_at < $3
			  %s
			GROUP BY day
		),
		response_daily AS (
			SELECT
				(sr.submitted_at AT TIME ZONE 'UTC')::date AS day,
				COUNT(*) AS completed_count,
				COUNT(*) FILTER (WHERE sr.score <= sc.low_score_threshold) AS low_score_count,
				COUNT(*) FILTER (WHERE sr.score >= CASE WHEN sc.survey_type = 'ces' THEN 6 WHEN sc.survey_type = 'nps' THEN 9 ELSE 4 END) AS positive_score_count,
				AVG(sr.score) AS average_score,
				COUNT(*) FILTER (WHERE sr.survey_type = 'nps' AND sr.nps_bucket = 'detractor') AS detractor_count,
				COUNT(*) FILTER (WHERE sr.survey_type = 'nps' AND sr.nps_bucket = 'passive') AS passive_count,
				COUNT(*) FILTER (WHERE sr.survey_type = 'nps' AND sr.nps_bucket = 'promoter') AS promoter_count,
				COUNT(*) FILTER (WHERE sr.metadata->>'response_quality_status' = 'flagged') AS quality_flagged_response_count
			FROM survey_responses sr
			JOIN survey_campaigns sc
			  ON sc.tenant_id = sr.tenant_id AND sc.id = sr.campaign_id
			JOIN survey_invitations si
			  ON si.tenant_id = sr.tenant_id AND si.id = sr.invitation_id
			WHERE sr.tenant_id = $1
			  AND sr.submitted_at >= $2
			  AND sr.submitted_at < $3
			  %s
			GROUP BY day
		)
		SELECT
			to_char(days.day, 'YYYY-MM-DD'),
			COALESCE(invitation_daily.invitation_count, 0),
			COALESCE(invitation_daily.delivered_count, 0),
			COALESCE(invitation_daily.suppressed_count, 0),
			COALESCE(response_daily.completed_count, 0),
			COALESCE(response_daily.low_score_count, 0),
			COALESCE(response_daily.positive_score_count, 0),
			COALESCE(response_daily.average_score, 0),
			CASE
				WHEN COALESCE(invitation_daily.invitation_count, 0) > 0
					THEN COALESCE(response_daily.completed_count, 0)::float / invitation_daily.invitation_count
				ELSE 0
			END,
			COALESCE(invitation_daily.not_started_count, 0),
			COALESCE(invitation_daily.opened_count, 0),
			COALESCE(invitation_daily.started_count, 0),
			COALESCE(invitation_daily.expired_count, 0),
			CASE
				WHEN COALESCE(response_daily.detractor_count, 0) + COALESCE(response_daily.passive_count, 0) + COALESCE(response_daily.promoter_count, 0) > 0
					THEN 100.0 * (COALESCE(response_daily.promoter_count, 0) - COALESCE(response_daily.detractor_count, 0)) /
						(COALESCE(response_daily.detractor_count, 0) + COALESCE(response_daily.passive_count, 0) + COALESCE(response_daily.promoter_count, 0))
				ELSE 0
				END,
				CASE
					WHEN COALESCE(response_daily.detractor_count, 0) + COALESCE(response_daily.passive_count, 0) + COALESCE(response_daily.promoter_count, 0) > 0
						THEN TRUE
					ELSE FALSE
				END,
				COALESCE(response_daily.detractor_count, 0),
			COALESCE(response_daily.passive_count, 0),
			COALESCE(response_daily.promoter_count, 0),
			0,
			COALESCE(response_daily.quality_flagged_response_count, 0)
		FROM days
		LEFT JOIN invitation_daily ON invitation_daily.day = days.day
		LEFT JOIN response_daily ON response_daily.day = days.day
		ORDER BY days.day ASC`, invitationCampaignFilter, responseCampaignFilter)
}

func (r *Repo) AnalyticsSegments(ctx context.Context, filter AnalyticsSegmentFilter) ([]AnalyticsSegment, error) {
	expressions, ok := analyticsSegmentExpressions(filter.Dimension)
	if !ok {
		return nil, ErrInvalidInput
	}
	args := []any{
		strings.TrimSpace(filter.TenantID),
		ptrext.Indirect(filter.From),
		ptrext.Indirect(filter.To),
	}
	campaignFilter := ""
	if filter.CampaignID != nil {
		args = append(args, ptrext.Indirect(filter.CampaignID))
		campaignFilter = fmt.Sprintf("AND si.campaign_id = $%d", len(args))
	}
	limitPlaceholder := len(args) + 1
	args = append(args, boundedLimit(filter.Limit))
	rows, err := r.pool.Query(ctx, analyticsSegmentsQuery(expressions, campaignFilter, limitPlaceholder), args...)
	if err != nil {
		return nil, fmt.Errorf("survey analytics segments: %w", err)
	}
	defer rows.Close()
	var out []AnalyticsSegment
	for rows.Next() {
		item, err := scanAnalyticsSegment(rows, filter.Dimension)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("survey analytics segments rows: %w", err)
	}
	return out, nil
}

type analyticsSegmentExpr struct {
	Key        string
	Label      string
	CampaignID string
}

func analyticsSegmentExpressions(dimension string) (analyticsSegmentExpr, bool) {
	switch strings.TrimSpace(dimension) {
	case SegmentSourceType:
		sourceType := "COALESCE(NULLIF(BTRIM(si.source_type), ''), 'unknown')"
		return analyticsSegmentExpr{Key: sourceType, Label: sourceType, CampaignID: "NULL::text"}, true
	case SegmentCampaign:
		return analyticsSegmentExpr{Key: "sc.id::text", Label: "sc.name", CampaignID: "sc.id::text"}, true
	case SegmentDistributionMode:
		return analyticsSegmentExpr{Key: "si.distribution_mode", Label: "si.distribution_mode", CampaignID: "NULL::text"}, true
	case SegmentTriggerEvent:
		return analyticsSegmentExpr{Key: "sc.trigger_event", Label: "sc.trigger_event", CampaignID: "NULL::text"}, true
	default:
		return analyticsSegmentExpr{}, false
	}
}

func analyticsSegmentsQuery(expr analyticsSegmentExpr, campaignFilter string, limitPlaceholder int) string {
	return fmt.Sprintf(`
		WITH invitation_cohort AS (
			SELECT
				si.tenant_id,
				si.id,
				si.created_at,
				si.delivery_status,
				si.response_status,
				si.suppression_status,
				%s AS segment_key,
				%s AS segment_label,
				%s AS segment_campaign_id,
				sc.survey_type AS campaign_survey_type,
				sc.low_score_threshold
			FROM survey_invitations si
			JOIN survey_campaigns sc
			  ON sc.tenant_id = si.tenant_id AND sc.id = si.campaign_id
			WHERE si.tenant_id = $1
			  AND si.created_at >= $2
			  AND si.created_at < $3
			  %s
		)
		SELECT
			segment_key,
			segment_label,
			segment_campaign_id,
			COUNT(*),
			COUNT(*) FILTER (WHERE delivery_status IN ('accepted', 'delivered')),
			COUNT(*) FILTER (WHERE suppression_status = 'suppressed'),
			COUNT(sr.id),
			COUNT(sr.id) FILTER (WHERE sr.score <= low_score_threshold),
			COUNT(sr.id) FILTER (
				WHERE sr.score >= CASE
					WHEN campaign_survey_type = 'ces' THEN 6
					WHEN campaign_survey_type = 'nps' THEN 9
					ELSE 4
				END
			),
			COUNT(*) FILTER (WHERE response_status = 'expired'),
			COALESCE(AVG(sr.score), 0),
			CASE WHEN COUNT(*) > 0 THEN COUNT(sr.id)::float / COUNT(*) ELSE 0 END,
			CASE WHEN COUNT(sr.id) > 0
				THEN (COUNT(sr.id) FILTER (WHERE sr.score <= low_score_threshold))::float / COUNT(sr.id)
				ELSE 0
			END,
			CASE WHEN COUNT(sr.id) > 0
				THEN (
					COUNT(sr.id) FILTER (
						WHERE sr.score >= CASE
							WHEN campaign_survey_type = 'ces' THEN 6
							WHEN campaign_survey_type = 'nps' THEN 9
							ELSE 4
						END
					)
				)::float / COUNT(sr.id)
				ELSE 0
			END,
			CASE WHEN COUNT(*) > 0
				THEN (COUNT(*) FILTER (WHERE suppression_status = 'suppressed'))::float / COUNT(*)
				ELSE 0
			END,
			COALESCE(AVG(GREATEST(EXTRACT(EPOCH FROM (sr.submitted_at - invitation_cohort.created_at)), 0)), 0),
			(
				(COUNT(sr.id) FILTER (WHERE sr.score <= low_score_threshold)) * 3
				+ (COUNT(*) FILTER (WHERE response_status = 'expired')) * 2
				+ (COUNT(*) FILTER (WHERE suppression_status = 'suppressed'))
			)::float AS attention_score
		FROM invitation_cohort
		LEFT JOIN survey_responses sr
		  ON sr.tenant_id = invitation_cohort.tenant_id AND sr.invitation_id = invitation_cohort.id
		GROUP BY segment_key, segment_label, segment_campaign_id
		ORDER BY attention_score DESC, COUNT(*) DESC, segment_label ASC
		LIMIT $%d`, expr.Key, expr.Label, expr.CampaignID, campaignFilter, limitPlaceholder)
}

func scanAnalyticsSegment(row pgx.Row, dimension string) (AnalyticsSegment, error) {
	var item AnalyticsSegment
	var campaignID sql.NullString
	if err := row.Scan(
		&item.Key,
		&item.Label,
		&campaignID,
		&item.InvitationCount,
		&item.DeliveredCount,
		&item.SuppressedCount,
		&item.CompletedCount,
		&item.LowScoreCount,
		&item.PositiveScoreCount,
		&item.ExpiredCount,
		&item.AverageScore,
		&item.ResponseRate,
		&item.LowScoreRate,
		&item.PositiveScoreRate,
		&item.SuppressionRate,
		&item.AverageResponseSeconds,
		&item.AttentionScore,
	); err != nil {
		return AnalyticsSegment{}, err
	}
	item.Dimension = dimension
	if campaignID.Valid {
		id, err := uuid.Parse(campaignID.String)
		if err != nil {
			return AnalyticsSegment{}, fmt.Errorf("survey analytics segment campaign id: %w", err)
		}
		item.CampaignID = ptrext.Of(id)
	}
	return item, nil
}

func (r *Repo) suppressionReasonDistribution(ctx context.Context, filter AnalyticsFilter) ([]SuppressionReasonBucket, error) {
	where := []string{"tenant_id = $1", "suppression_status = 'suppressed'"}
	args := []any{strings.TrimSpace(filter.TenantID)}
	if filter.CampaignID != nil {
		where, args = appendFilter(where, args, "campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if filter.From != nil {
		where, args = appendFilter(where, args, "created_at >= $%d", ptrext.Indirect(filter.From))
	}
	if filter.To != nil {
		where, args = appendFilter(where, args, "created_at < $%d", ptrext.Indirect(filter.To))
	}
	if filter.RunID != nil {
		where, args = appendFilter(where, args, "run_id = $%d", ptrext.Indirect(filter.RunID))
	}
	query := fmt.Sprintf(`
		SELECT COALESCE(NULLIF(BTRIM(suppression_reason), ''), 'unknown') AS reason, COUNT(*)
		FROM survey_invitations
		%s
		GROUP BY reason
		ORDER BY COUNT(*) DESC, reason ASC`, whereClause(where))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("survey suppression reason distribution: %w", err)
	}
	defer rows.Close()
	var out []SuppressionReasonBucket
	for rows.Next() {
		var bucket SuppressionReasonBucket
		if err := rows.Scan(&bucket.Reason, &bucket.Count); err != nil {
			return nil, err
		}
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("survey suppression reason distribution rows: %w", err)
	}
	return out, nil
}

type lowScoreReviewCountResult struct {
	OpenCount                   int
	OverdueCount                int
	UnassignedCount             int
	CriticalCount               int
	PendingCustomerContactCount int
	OldestOpenDueAt             *time.Time
	OverdueQueueCount           int
	UnassignedQueueCount        int
	PendingContactQueueCount    int
	MissingRootCauseQueueCount  int
	MissingActionQueueCount     int
	Outcome                     RecoveryOutcome
}

type lowScoreReviewCountScan struct {
	reviewCount                      *int
	resolvedCount                    *int
	dismissedCount                   *int
	customerContactedCount           *int
	rootCauseRecordedCount           *int
	actionRecordedCount              *int
	contactedTimelinessEvidenceCount *int
	contactedOnTimeCount             *int
	contactedLateCount               *int
	terminalTimelinessEvidenceCount  *int
	terminalOnTimeCount              *int
	terminalLateCount                *int
	openCount                        *int
	overdueCount                     *int
	unassignedCount                  *int
	criticalCount                    *int
	pendingCustomerContactCount      *int
	oldestOpenDueAt                  **time.Time
	overdueQueueCount                *int
	unassignedQueueCount             *int
	pendingContactQueueCount         *int
	missingRootCauseQueueCount       *int
	missingActionQueueCount          *int
}

func (r *Repo) lowScoreReviewCounts(ctx context.Context, filter AnalyticsFilter) (lowScoreReviewCountResult, error) {
	where, args := analyticsLowScoreReviewWhere(filter)
	scan := newLowScoreReviewCountScan()
	if err := r.pool.QueryRow(ctx, lowScoreReviewCountsQuery(where), args...).Scan(
		scan.destinations()...,
	); err != nil {
		return lowScoreReviewCountResult{}, fmt.Errorf("survey low score review counts: %w", err)
	}
	return scan.result(), nil
}

func analyticsLowScoreReviewWhere(filter AnalyticsFilter) ([]string, []any) {
	where := []string{"lsr.tenant_id = $1"}
	args := []any{strings.TrimSpace(filter.TenantID)}
	if filter.CampaignID != nil {
		where, args = appendFilter(where, args, "lsr.campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if filter.RunID != nil {
		where, args = appendFilter(where, args, "si.run_id = $%d", ptrext.Indirect(filter.RunID))
	}
	if filter.From != nil {
		where, args = appendFilter(where, args, "sr.submitted_at >= $%d", ptrext.Indirect(filter.From))
	}
	if filter.To != nil {
		where, args = appendFilter(where, args, "sr.submitted_at < $%d", ptrext.Indirect(filter.To))
	}
	return where, args
}

func lowScoreReviewCountsQuery(where []string) string {
	return fmt.Sprintf(`
			SELECT
				COUNT(*),
				COUNT(*) FILTER (WHERE lsr.status = 'resolved'),
				COUNT(*) FILTER (WHERE lsr.status = 'dismissed'),
				COUNT(*) FILTER (WHERE lsr.customer_contacted),
				COUNT(*) FILTER (WHERE BTRIM(lsr.root_cause) <> ''),
				COUNT(*) FILTER (WHERE BTRIM(lsr.action_taken) <> ''),
				COUNT(*) FILTER (
					WHERE lsr.initial_due_at IS NOT NULL
					  AND lsr.customer_contacted_at IS NOT NULL
				),
				COUNT(*) FILTER (
					WHERE lsr.initial_due_at IS NOT NULL
					  AND lsr.customer_contacted_at IS NOT NULL
					  AND lsr.customer_contacted_at <= lsr.initial_due_at
				),
				COUNT(*) FILTER (
					WHERE lsr.initial_due_at IS NOT NULL
					  AND lsr.customer_contacted_at IS NOT NULL
					  AND lsr.customer_contacted_at > lsr.initial_due_at
				),
				COUNT(*) FILTER (
					WHERE lsr.initial_due_at IS NOT NULL
					  AND lsr.first_terminal_at IS NOT NULL
				),
				COUNT(*) FILTER (
					WHERE lsr.initial_due_at IS NOT NULL
					  AND lsr.first_terminal_at IS NOT NULL
					  AND lsr.first_terminal_at <= lsr.initial_due_at
				),
				COUNT(*) FILTER (
					WHERE lsr.initial_due_at IS NOT NULL
					  AND lsr.first_terminal_at IS NOT NULL
					  AND lsr.first_terminal_at > lsr.initial_due_at
				),
				COUNT(*) FILTER (WHERE lsr.status IN ('open', 'in_review')),
				COUNT(*) FILTER (
					WHERE lsr.status IN ('open', 'in_review')
					  AND lsr.due_at IS NOT NULL
					  AND lsr.due_at < NOW()
				),
				COUNT(*) FILTER (
					WHERE lsr.status IN ('open', 'in_review')
					  AND lsr.owner_member_id IS NULL
				),
				COUNT(*) FILTER (
					WHERE lsr.status IN ('open', 'in_review')
					  AND lsr.severity = 'critical'
				),
				COUNT(*) FILTER (
					WHERE lsr.status IN ('open', 'in_review')
					  AND NOT lsr.customer_contacted
				),
				MIN(lsr.due_at) FILTER (
					WHERE lsr.status IN ('open', 'in_review')
					  AND lsr.due_at IS NOT NULL
				),
				COUNT(*) FILTER (WHERE %s),
				COUNT(*) FILTER (WHERE %s),
				COUNT(*) FILTER (WHERE %s),
				COUNT(*) FILTER (WHERE %s),
				COUNT(*) FILTER (WHERE %s)
			FROM survey_low_score_reviews lsr
			JOIN survey_responses sr
			  ON sr.tenant_id = lsr.tenant_id AND sr.id = lsr.response_id
			JOIN survey_invitations si
			  ON si.tenant_id = sr.tenant_id AND si.id = sr.invitation_id
			%s`,
		recoveryBlockerWhere(RecoveryBlockerOverdue),
		recoveryBlockerWhere(RecoveryBlockerOwner),
		recoveryBlockerWhere(RecoveryBlockerContact),
		recoveryBlockerWhere(RecoveryBlockerRootCause),
		recoveryBlockerWhere(RecoveryBlockerAction),
		whereClause(where),
	)
}

func newLowScoreReviewCountScan() lowScoreReviewCountScan {
	return lowScoreReviewCountScan{
		reviewCount: ptrext.Of(0), resolvedCount: ptrext.Of(0), dismissedCount: ptrext.Of(0),
		customerContactedCount: ptrext.Of(0), rootCauseRecordedCount: ptrext.Of(0), actionRecordedCount: ptrext.Of(0),
		contactedTimelinessEvidenceCount: ptrext.Of(0), contactedOnTimeCount: ptrext.Of(0), contactedLateCount: ptrext.Of(0),
		terminalTimelinessEvidenceCount: ptrext.Of(0), terminalOnTimeCount: ptrext.Of(0), terminalLateCount: ptrext.Of(0),
		openCount: ptrext.Of(0), overdueCount: ptrext.Of(0), unassignedCount: ptrext.Of(0), criticalCount: ptrext.Of(0),
		pendingCustomerContactCount: ptrext.Of(0), oldestOpenDueAt: ptrext.Of[*time.Time](nil),
		overdueQueueCount: ptrext.Of(0), unassignedQueueCount: ptrext.Of(0), pendingContactQueueCount: ptrext.Of(0),
		missingRootCauseQueueCount: ptrext.Of(0), missingActionQueueCount: ptrext.Of(0),
	}
}

func (scan lowScoreReviewCountScan) destinations() []any {
	return []any{
		scan.reviewCount, scan.resolvedCount, scan.dismissedCount, scan.customerContactedCount,
		scan.rootCauseRecordedCount, scan.actionRecordedCount,
		scan.contactedTimelinessEvidenceCount, scan.contactedOnTimeCount, scan.contactedLateCount,
		scan.terminalTimelinessEvidenceCount, scan.terminalOnTimeCount, scan.terminalLateCount,
		scan.openCount, scan.overdueCount, scan.unassignedCount, scan.criticalCount,
		scan.pendingCustomerContactCount, scan.oldestOpenDueAt,
		scan.overdueQueueCount, scan.unassignedQueueCount, scan.pendingContactQueueCount,
		scan.missingRootCauseQueueCount, scan.missingActionQueueCount,
	}
}

func (scan lowScoreReviewCountScan) result() lowScoreReviewCountResult {
	return lowScoreReviewCountResult{
		OpenCount:                   ptrext.Indirect(scan.openCount),
		OverdueCount:                ptrext.Indirect(scan.overdueCount),
		UnassignedCount:             ptrext.Indirect(scan.unassignedCount),
		CriticalCount:               ptrext.Indirect(scan.criticalCount),
		PendingCustomerContactCount: ptrext.Indirect(scan.pendingCustomerContactCount),
		OldestOpenDueAt:             ptrext.Indirect(scan.oldestOpenDueAt),
		OverdueQueueCount:           ptrext.Indirect(scan.overdueQueueCount),
		UnassignedQueueCount:        ptrext.Indirect(scan.unassignedQueueCount),
		PendingContactQueueCount:    ptrext.Indirect(scan.pendingContactQueueCount),
		MissingRootCauseQueueCount:  ptrext.Indirect(scan.missingRootCauseQueueCount),
		MissingActionQueueCount:     ptrext.Indirect(scan.missingActionQueueCount),
		Outcome: RecoveryOutcome{
			ReviewCount:                      ptrext.Indirect(scan.reviewCount),
			ResolvedCount:                    ptrext.Indirect(scan.resolvedCount),
			DismissedCount:                   ptrext.Indirect(scan.dismissedCount),
			CustomerContactedCount:           ptrext.Indirect(scan.customerContactedCount),
			RootCauseRecordedCount:           ptrext.Indirect(scan.rootCauseRecordedCount),
			ActionRecordedCount:              ptrext.Indirect(scan.actionRecordedCount),
			ContactedTimelinessEvidenceCount: ptrext.Indirect(scan.contactedTimelinessEvidenceCount),
			ContactedOnTimeCount:             ptrext.Indirect(scan.contactedOnTimeCount),
			ContactedLateCount:               ptrext.Indirect(scan.contactedLateCount),
			TerminalTimelinessEvidenceCount:  ptrext.Indirect(scan.terminalTimelinessEvidenceCount),
			TerminalOnTimeCount:              ptrext.Indirect(scan.terminalOnTimeCount),
			TerminalLateCount:                ptrext.Indirect(scan.terminalLateCount),
		},
	}
}

type npsMetricResult struct {
	Score                 float64
	Available             bool
	DetractorCount        int
	PassiveCount          int
	PromoterCount         int
	RedactedResponseCount int
}

func (r *Repo) npsMetrics(ctx context.Context, filter AnalyticsFilter) (npsMetricResult, error) {
	where := []string{"sr.tenant_id = $1", "sr.survey_type = 'nps'"}
	args := []any{strings.TrimSpace(filter.TenantID)}
	if filter.CampaignID != nil {
		where, args = appendFilter(where, args, "sr.campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if filter.From != nil {
		where, args = appendFilter(where, args, "sr.submitted_at >= $%d", ptrext.Indirect(filter.From))
	}
	if filter.To != nil {
		where, args = appendFilter(where, args, "sr.submitted_at < $%d", ptrext.Indirect(filter.To))
	}
	if filter.RunID != nil {
		where, args = appendFilter(where, args, "si.run_id = $%d", ptrext.Indirect(filter.RunID))
	}
	var out npsMetricResult
	err := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE sr.nps_bucket = 'detractor'),
			COUNT(*) FILTER (WHERE sr.nps_bucket = 'passive'),
			COUNT(*) FILTER (WHERE sr.nps_bucket = 'promoter')
		FROM survey_responses sr
		JOIN survey_invitations si
		  ON si.tenant_id = sr.tenant_id AND si.id = sr.invitation_id
		%s`, whereClause(where)), args...).Scan(&out.DetractorCount, &out.PassiveCount, &out.PromoterCount)
	if err != nil {
		return npsMetricResult{}, fmt.Errorf("survey NPS metrics: %w", err)
	}
	total := out.DetractorCount + out.PassiveCount + out.PromoterCount
	if total > 0 {
		out.Available = true
		out.Score = 100 * float64(out.PromoterCount-out.DetractorCount) / float64(total)
	}
	runWhere := []string{"tenant_id = $1"}
	runArgs := []any{strings.TrimSpace(filter.TenantID)}
	if filter.CampaignID != nil {
		runWhere, runArgs = appendFilter(runWhere, runArgs, "campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if filter.RunID != nil {
		runWhere, runArgs = appendFilter(runWhere, runArgs, "id = $%d", ptrext.Indirect(filter.RunID))
	}
	// Redaction evidence is retained only at run granularity. Its analytics
	// window therefore follows the run's immutable measurement start rather than
	// a deleted response's submission time.
	if filter.From != nil {
		runWhere, runArgs = appendFilter(runWhere, runArgs, "opened_at >= $%d", ptrext.Indirect(filter.From))
	}
	if filter.To != nil {
		runWhere, runArgs = appendFilter(runWhere, runArgs, "opened_at < $%d", ptrext.Indirect(filter.To))
	}
	if err := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COALESCE(SUM(redacted_response_count), 0)
		FROM survey_campaign_runs
		%s`, whereClause(runWhere)), runArgs...).Scan(&out.RedactedResponseCount); err != nil {
		return npsMetricResult{}, fmt.Errorf("survey NPS redaction metrics: %w", err)
	}
	return out, nil
}

const maxOwnerRecoveryLoads = 8

func (r *Repo) lowScoreOwnerLoads(ctx context.Context, filter AnalyticsFilter) ([]RecoveryOwnerLoad, error) {
	where, args := lowScoreOwnerLoadWhere(filter)
	rows, err := r.pool.Query(ctx, lowScoreOwnerLoadQuery(whereClause(where)), args...)
	if err != nil {
		return nil, fmt.Errorf("survey low score owner loads: %w", err)
	}
	defer rows.Close()
	var out []RecoveryOwnerLoad
	for rows.Next() {
		item, err := scanRecoveryOwnerLoad(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("survey low score owner load rows: %w", err)
	}
	return out, nil
}

func lowScoreOwnerLoadWhere(filter AnalyticsFilter) ([]string, []any) {
	where := []string{
		"lsr.tenant_id = $1",
		activeReviewWhere,
		"lsr.owner_member_id IS NOT NULL",
	}
	args := []any{strings.TrimSpace(filter.TenantID)}
	if filter.CampaignID != nil {
		where, args = appendFilter(where, args, "lsr.campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if filter.RunID != nil {
		where, args = appendFilter(where, args, "si.run_id = $%d", ptrext.Indirect(filter.RunID))
	}
	if filter.From != nil {
		where, args = appendFilter(where, args, "sr.submitted_at >= $%d", ptrext.Indirect(filter.From))
	}
	if filter.To != nil {
		where, args = appendFilter(where, args, "sr.submitted_at < $%d", ptrext.Indirect(filter.To))
	}
	args = append(args, maxOwnerRecoveryLoads)
	return where, args
}

func lowScoreOwnerLoadQuery(where string) string {
	return fmt.Sprintf(`
		SELECT
			lsr.owner_member_id::text,
			COUNT(*),
			COUNT(*) FILTER (WHERE lsr.due_at IS NOT NULL AND lsr.due_at < NOW()),
			COUNT(*) FILTER (
				WHERE lsr.due_at IS NOT NULL
				  AND lsr.due_at >= NOW()
				  AND lsr.due_at < NOW() + INTERVAL '24 hours'
			),
			COUNT(*) FILTER (WHERE lsr.severity = 'critical'),
			COUNT(*) FILTER (WHERE NOT lsr.customer_contacted),
			MIN(lsr.due_at) FILTER (WHERE lsr.due_at IS NOT NULL),
			(
				COUNT(*) * 3
				+ COUNT(*) FILTER (WHERE lsr.due_at IS NOT NULL AND lsr.due_at < NOW()) * 30
				+ COUNT(*) FILTER (
					WHERE lsr.due_at IS NOT NULL
					  AND lsr.due_at >= NOW()
					  AND lsr.due_at < NOW() + INTERVAL '24 hours'
				) * 12
				+ COUNT(*) FILTER (WHERE lsr.severity = 'critical') * 20
				+ COUNT(*) FILTER (WHERE NOT lsr.customer_contacted) * 8
			)::int
		FROM survey_low_score_reviews lsr
		JOIN survey_responses sr
		  ON sr.tenant_id = lsr.tenant_id AND sr.id = lsr.response_id
		JOIN survey_invitations si
		  ON si.tenant_id = sr.tenant_id AND si.id = sr.invitation_id
		%s
		GROUP BY lsr.owner_member_id
		ORDER BY 8 DESC, 7 ASC NULLS LAST, lsr.owner_member_id ASC
		LIMIT $%d`, where, strings.Count(where, "$")+1)
}

func scanRecoveryOwnerLoad(row pgx.Row) (RecoveryOwnerLoad, error) {
	var item RecoveryOwnerLoad
	var ownerMemberID string
	if err := row.Scan(
		&ownerMemberID,
		&item.OpenCount,
		&item.OverdueCount,
		&item.DueSoonCount,
		&item.CriticalCount,
		&item.PendingContactCount,
		&item.OldestOpenDueAt,
		&item.WorkloadScore,
	); err != nil {
		return RecoveryOwnerLoad{}, err
	}
	id, err := uuid.Parse(ownerMemberID)
	if err != nil {
		return RecoveryOwnerLoad{}, fmt.Errorf("survey low score owner load owner id: %w", err)
	}
	item.OwnerMemberID = id
	return item, nil
}

type invitationCountResult struct {
	InvitationCount       int
	DeliveredCount        int
	SuppressedCount       int
	NotStartedCount       int
	OpenedCount           int
	StartedCount          int
	ExpiredCount          int
	PendingDeliveryCount  int
	DelayedDeliveryCount  int
	RejectedDeliveryCount int
}

func (r *Repo) invitationCounts(ctx context.Context, filter AnalyticsFilter) (invitationCountResult, error) {
	where := []string{"tenant_id = $1"}
	args := []any{strings.TrimSpace(filter.TenantID)}
	if filter.CampaignID != nil {
		where, args = appendFilter(where, args, "campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if filter.From != nil {
		where, args = appendFilter(where, args, "created_at >= $%d", ptrext.Indirect(filter.From))
	}
	if filter.To != nil {
		where, args = appendFilter(where, args, "created_at < $%d", ptrext.Indirect(filter.To))
	}
	if filter.RunID != nil {
		where, args = appendFilter(where, args, "run_id = $%d", ptrext.Indirect(filter.RunID))
	}
	query := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE delivery_status IN ('accepted', 'delivered')),
			COUNT(*) FILTER (WHERE suppression_status = 'suppressed'),
			COUNT(*) FILTER (WHERE response_status = 'not_started'),
			COUNT(*) FILTER (WHERE response_status = 'opened' OR opened_at IS NOT NULL),
			COUNT(*) FILTER (WHERE response_status IN ('started', 'completed')),
			COUNT(*) FILTER (WHERE response_status = 'expired'),
			COUNT(*) FILTER (WHERE delivery_status = 'pending'),
			COUNT(*) FILTER (WHERE delivery_status = 'delayed'),
			COUNT(*) FILTER (WHERE delivery_status IN ('rejected', 'bounced', 'complained'))
		FROM survey_invitations
		%s`, whereClause(where))
	var out invitationCountResult
	if err := r.pool.QueryRow(ctx, query, args...).Scan(
		&out.InvitationCount,
		&out.DeliveredCount,
		&out.SuppressedCount,
		&out.NotStartedCount,
		&out.OpenedCount,
		&out.StartedCount,
		&out.ExpiredCount,
		&out.PendingDeliveryCount,
		&out.DelayedDeliveryCount,
		&out.RejectedDeliveryCount,
	); err != nil {
		return invitationCountResult{}, fmt.Errorf("survey invitation counts: %w", err)
	}
	return out, nil
}

type responseCountResult struct {
	CompletedCount              int
	LowScoreCount               int
	PositiveScoreCount          int
	QualityFlaggedResponseCount int
	AverageScore                float64
	AverageResponseSeconds      float64
}

func (r *Repo) responseCounts(ctx context.Context, filter AnalyticsFilter) (responseCountResult, error) {
	where := []string{"sr.tenant_id = $1"}
	args := []any{strings.TrimSpace(filter.TenantID)}
	if filter.CampaignID != nil {
		where, args = appendFilter(where, args, "sr.campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if filter.From != nil {
		where, args = appendFilter(where, args, "sr.submitted_at >= $%d", ptrext.Indirect(filter.From))
	}
	if filter.To != nil {
		where, args = appendFilter(where, args, "sr.submitted_at < $%d", ptrext.Indirect(filter.To))
	}
	if filter.RunID != nil {
		where, args = appendFilter(where, args, "si.run_id = $%d", ptrext.Indirect(filter.RunID))
	}
	query := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE sr.score <= sc.low_score_threshold),
			COUNT(*) FILTER (WHERE sr.score >= CASE WHEN sc.survey_type = 'ces' THEN 6 WHEN sc.survey_type = 'nps' THEN 9 ELSE 4 END),
			COUNT(*) FILTER (WHERE sr.metadata->>'response_quality_status' = 'flagged'),
			AVG(sr.score),
			AVG(GREATEST(EXTRACT(EPOCH FROM (sr.submitted_at - si.created_at)), 0))
		FROM survey_responses sr
		JOIN survey_campaigns sc
		  ON sc.tenant_id = sr.tenant_id AND sc.id = sr.campaign_id
		JOIN survey_invitations si
		  ON si.tenant_id = sr.tenant_id AND si.id = sr.invitation_id
		%s`, whereClause(where))
	var out responseCountResult
	var avg sql.NullFloat64
	var avgResponse sql.NullFloat64
	if err := r.pool.QueryRow(ctx, query, args...).Scan(
		&out.CompletedCount,
		&out.LowScoreCount,
		&out.PositiveScoreCount,
		&out.QualityFlaggedResponseCount,
		&avg,
		&avgResponse,
	); err != nil {
		return responseCountResult{}, fmt.Errorf("survey response counts: %w", err)
	}
	if avg.Valid {
		out.AverageScore = avg.Float64
	}
	if avgResponse.Valid {
		out.AverageResponseSeconds = avgResponse.Float64
	}
	return out, nil
}

func (r *Repo) scoreDistribution(ctx context.Context, filter AnalyticsFilter) ([]ScoreBucket, error) {
	where := []string{"sr.tenant_id = $1"}
	args := []any{strings.TrimSpace(filter.TenantID)}
	if filter.CampaignID != nil {
		where, args = appendFilter(where, args, "sr.campaign_id = $%d", ptrext.Indirect(filter.CampaignID))
	}
	if filter.From != nil {
		where, args = appendFilter(where, args, "sr.submitted_at >= $%d", ptrext.Indirect(filter.From))
	}
	if filter.To != nil {
		where, args = appendFilter(where, args, "sr.submitted_at < $%d", ptrext.Indirect(filter.To))
	}
	if filter.RunID != nil {
		where, args = appendFilter(where, args, "si.run_id = $%d", ptrext.Indirect(filter.RunID))
	}
	query := fmt.Sprintf(`
		SELECT score, COUNT(*)
		FROM survey_responses sr
		JOIN survey_invitations si
		  ON si.tenant_id = sr.tenant_id AND si.id = sr.invitation_id
		%s
		GROUP BY score
		ORDER BY score ASC`, whereClause(where))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("survey score distribution: %w", err)
	}
	defer rows.Close()
	var out []ScoreBucket
	for rows.Next() {
		var bucket ScoreBucket
		if err := rows.Scan(&bucket.Score, &bucket.Count); err != nil {
			return nil, err
		}
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("survey score distribution rows: %w", err)
	}
	return out, nil
}
