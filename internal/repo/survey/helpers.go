// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

const maxListLimit = 200

func mapWriteError(err error) error {
	switch {
	case pgxutil.IsUniqueViolation(err):
		return ErrConflict
	case pgxutil.IsForeignKeyViolation(err), pgxutil.IsCheckViolation(err):
		return ErrInvalidInput
	default:
		return err
	}
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func jsonObject(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeObject(raw []byte) (map[string]any, error) {
	out := map[string]any{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil { // ptrext:allow unmarshal-out-param
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

func DestinationHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return ptrext.Indirect(id)
}

func uuidFromPg(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return ptrext.Of(id)
}

func int64FromPg(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	return ptrext.Of(value.Int64)
}

func nextRetryAtArg(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func scanCampaign(row pgx.Row) (Campaign, error) {
	var c Campaign
	var triggerRaw []byte
	var contentRaw []byte
	err := row.Scan(
		&c.ID,
		&c.TenantID,
		&c.Name,
		&c.SurveyType,
		&c.Status,
		&c.TriggerEvent,
		&c.DistributionMode,
		&c.DedupePolicy,
		&triggerRaw,
		&contentRaw,
		&c.Locale,
		&c.ContentVersion,
		&c.SamplingPercent,
		&c.MinDaysBetweenContact,
		&c.ExpiresAfterDays,
		&c.MaxDailyInvitations,
		&c.LowScoreThreshold,
		&c.RequireRecentCustomerActivity,
		&c.RecentActivityDays,
		&c.SuppressAutoResolved,
		&c.CreatedBy,
		&c.UpdatedBy,
		&c.ArchivedAt,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return Campaign{}, mapNotFound(err)
	}
	triggerFilter, err := decodeObject(triggerRaw)
	if err != nil {
		return Campaign{}, err
	}
	content, err := decodeObject(contentRaw)
	if err != nil {
		return Campaign{}, err
	}
	c.TriggerFilter = triggerFilter
	c.Content = content
	return c, nil
}

func scanInvitation(row pgx.Row) (Invitation, error) {
	var i Invitation
	var campaignRaw []byte
	var recipientRaw []byte
	var runID pgtype.UUID
	var requestID pgtype.UUID
	var contactID pgtype.UUID
	err := row.Scan(
		&i.ID,
		&i.TenantID,
		&i.CampaignID,
		&runID,
		&i.CampaignContentVersion,
		&campaignRaw,
		&i.DedupeKey,
		&i.SourceType,
		&i.SourceID,
		&requestID,
		&contactID,
		&i.DistributionMode,
		&i.TokenHash,
		&i.DeliveryStatus,
		&i.ResponseStatus,
		&i.SuppressionStatus,
		&i.SuppressionReason,
		&recipientRaw,
		&i.DeliverySecret,
		&i.Provider,
		&i.ProviderMessageID,
		&i.Attempts,
		&i.FailureKind,
		&i.HTTPStatus,
		&i.LastError,
		&i.ClaimedAt,
		&i.ClaimedBy,
		&i.NextRetryAt,
		&i.DeliveredAt,
		&i.OpenedAt,
		&i.RespondedAt,
		&i.ExpiresAt,
		&i.CreatedBy,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	if err != nil {
		return Invitation{}, mapNotFound(err)
	}
	campaignSnapshot, err := decodeObject(campaignRaw)
	if err != nil {
		return Invitation{}, err
	}
	recipientSnapshot, err := decodeObject(recipientRaw)
	if err != nil {
		return Invitation{}, err
	}
	i.RunID = uuidFromPg(runID)
	i.RequestID = uuidFromPg(requestID)
	i.ContactID = uuidFromPg(contactID)
	i.CampaignSnapshot = campaignSnapshot
	i.RecipientSnapshot = recipientSnapshot
	return i, nil
}

func scanResponse(row pgx.Row) (Response, error) {
	var response Response
	var metadataRaw []byte
	var requestID pgtype.UUID
	var contactID pgtype.UUID
	err := row.Scan(
		&response.ID,
		&response.TenantID,
		&response.CampaignID,
		&response.SurveyType,
		&response.InvitationID,
		&requestID,
		&contactID,
		&response.SourceType,
		&response.SourceID,
		&response.Score,
		&response.NPSBucket,
		&response.FollowUpConsent,
		&response.Comment,
		&response.Locale,
		&metadataRaw,
		&response.UserAgentHash,
		&response.IPHash,
		&response.SubmittedAt,
		&response.CreatedAt,
	)
	if err != nil {
		return Response{}, mapNotFound(err)
	}
	metadata, err := decodeObject(metadataRaw)
	if err != nil {
		return Response{}, err
	}
	response.RequestID = uuidFromPg(requestID)
	response.ContactID = uuidFromPg(contactID)
	response.Metadata = metadata
	return response, nil
}

func scanResponseWithFeedbackID(row pgx.Row) (Response, error) {
	var response Response
	var metadataRaw []byte
	var requestID pgtype.UUID
	var contactID pgtype.UUID
	var feedbackID pgtype.Int8
	err := row.Scan(
		&response.ID,
		&response.TenantID,
		&response.CampaignID,
		&response.SurveyType,
		&response.InvitationID,
		&requestID,
		&contactID,
		&response.SourceType,
		&response.SourceID,
		&response.Score,
		&response.NPSBucket,
		&response.FollowUpConsent,
		&response.Comment,
		&response.Locale,
		&metadataRaw,
		&response.UserAgentHash,
		&response.IPHash,
		&response.SubmittedAt,
		&response.CreatedAt,
		&feedbackID,
	)
	if err != nil {
		return Response{}, mapNotFound(err)
	}
	metadata, err := decodeObject(metadataRaw)
	if err != nil {
		return Response{}, err
	}
	response.RequestID = uuidFromPg(requestID)
	response.ContactID = uuidFromPg(contactID)
	response.Metadata = metadata
	response.FeedbackID = int64FromPg(feedbackID)
	return response, nil
}

func scanResponseWithAccount(row pgx.Row) (Response, error) {
	var response Response
	var metadataRaw []byte
	var requestID pgtype.UUID
	var contactID pgtype.UUID
	var feedbackID pgtype.Int8
	err := row.Scan(
		&response.ID,
		&response.TenantID,
		&response.CampaignID,
		&response.SurveyType,
		&response.InvitationID,
		&requestID,
		&contactID,
		&response.SourceType,
		&response.SourceID,
		&response.Score,
		&response.NPSBucket,
		&response.FollowUpConsent,
		&response.Comment,
		&response.Locale,
		&metadataRaw,
		&response.UserAgentHash,
		&response.IPHash,
		&response.SubmittedAt,
		&response.CreatedAt,
		&feedbackID,
		&response.Account.AccountKey,
		&response.Account.AccountDisplay,
		&response.Account.Source,
	)
	if err != nil {
		return Response{}, mapNotFound(err)
	}
	metadata, err := decodeObject(metadataRaw)
	if err != nil {
		return Response{}, err
	}
	response.RequestID = uuidFromPg(requestID)
	response.ContactID = uuidFromPg(contactID)
	response.Metadata = metadata
	response.FeedbackID = int64FromPg(feedbackID)
	return response, nil
}

func scanResponseWithLowScoreReviewAndAccount(row pgx.Row) (Response, error) {
	var response Response
	var review LowScoreReview
	var metadataRaw []byte
	var requestID pgtype.UUID
	var contactID pgtype.UUID
	var feedbackID pgtype.Int8
	var ownerMemberID pgtype.UUID
	err := row.Scan(
		&response.ID,
		&response.TenantID,
		&response.CampaignID,
		&response.SurveyType,
		&response.InvitationID,
		&requestID,
		&contactID,
		&response.SourceType,
		&response.SourceID,
		&response.Score,
		&response.NPSBucket,
		&response.FollowUpConsent,
		&response.Comment,
		&response.Locale,
		&metadataRaw,
		&response.UserAgentHash,
		&response.IPHash,
		&response.SubmittedAt,
		&response.CreatedAt,
		&feedbackID,
		&review.ResponseID,
		&review.TenantID,
		&review.CampaignID,
		&review.Status,
		&review.Severity,
		&ownerMemberID,
		&review.RootCause,
		&review.ActionTaken,
		&review.CustomerContacted,
		&review.DueAt,
		&review.InitialDueAt,
		&review.CustomerContactedAt,
		&review.FirstTerminalAt,
		&review.ReviewedAt,
		&review.UpdatedBy,
		&review.CreatedAt,
		&review.UpdatedAt,
		&review.RecoveryNotificationStatus,
		&review.RecoveryNotificationReason,
		&review.RecoveryNotificationDeliveredAt,
		&review.RecoveryNotificationLastError,
		&response.Account.AccountKey,
		&response.Account.AccountDisplay,
		&response.Account.Source,
	)
	if err != nil {
		return Response{}, mapNotFound(err)
	}
	metadata, err := decodeObject(metadataRaw)
	if err != nil {
		return Response{}, err
	}
	response.RequestID = uuidFromPg(requestID)
	response.ContactID = uuidFromPg(contactID)
	response.Metadata = metadata
	response.FeedbackID = int64FromPg(feedbackID)
	review.OwnerMemberID = uuidFromPg(ownerMemberID)
	response.Review = ptrext.Of(review)
	return response, nil
}

func scanLowScoreReview(row pgx.Row) (LowScoreReview, error) {
	var r LowScoreReview
	var ownerMemberID pgtype.UUID
	err := row.Scan(
		&r.ResponseID,
		&r.TenantID,
		&r.CampaignID,
		&r.Status,
		&r.Severity,
		&ownerMemberID,
		&r.RootCause,
		&r.ActionTaken,
		&r.CustomerContacted,
		&r.DueAt,
		&r.InitialDueAt,
		&r.CustomerContactedAt,
		&r.FirstTerminalAt,
		&r.ReviewedAt,
		&r.UpdatedBy,
		&r.CreatedAt,
		&r.UpdatedAt,
	)
	if err != nil {
		return LowScoreReview{}, mapNotFound(err)
	}
	r.OwnerMemberID = uuidFromPg(ownerMemberID)
	return r, nil
}

func appendFilter(where []string, args []any, condition string, arg any) ([]string, []any) {
	args = append(args, arg)
	return append(where, fmt.Sprintf(condition, len(args))), args
}

func whereClause(where []string) string {
	if len(where) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(where, " AND ")
}
