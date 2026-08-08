// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

type triggerTarget struct {
	SourceType        string
	SourceID          string
	RequestID         *uuid.UUID
	ContactID         *uuid.UUID
	LastActivityAt    time.Time
	RecipientSnapshot map[string]any
	ActorID           string
	SuppressionReason string
}

func (s *Service) RecordWorkflowTransition(ctx context.Context, in WorkflowTransitionInput) (int, error) {
	if strings.TrimSpace(in.TenantID) == "" || in.FeedbackID <= 0 {
		return 0, ErrValidation
	}
	if strings.TrimSpace(in.ToStateCategory) != "closed" {
		return 0, nil
	}
	contextRow, err := s.repo.FeedbackTriggerContext(ctx, in.TenantID, in.FeedbackID)
	if err != nil {
		return 0, mapRepoError(err)
	}
	attrs := workflowTriggerAttrs(in, contextRow)
	return s.createFeedbackTriggeredInvitations(ctx, repo.TriggerWorkflowTransition, contextRow, attrs,
		"workflow_transition", fmt.Sprintf("%d", in.FeedbackID), strings.TrimSpace(in.ActorID))
}

func (s *Service) RecordReplySent(ctx context.Context, in ReplySentInput) (int, error) {
	if strings.TrimSpace(in.TenantID) == "" || in.FeedbackID <= 0 {
		return 0, ErrValidation
	}
	contextRow, err := s.repo.FeedbackTriggerContext(ctx, in.TenantID, in.FeedbackID)
	if err != nil {
		return 0, mapRepoError(err)
	}
	sourceID := firstNonEmpty(in.AttemptID, in.DraftID, fmt.Sprintf("%d", in.FeedbackID))
	attrs := replyTriggerAttrs(in, contextRow)
	return s.createFeedbackTriggeredInvitations(ctx, repo.TriggerReplySent, contextRow, attrs,
		"reply_sent", sourceID, strings.TrimSpace(in.ActorID))
}

func (s *Service) RecordRequestResolved(ctx context.Context, in RequestResolvedInput) (int, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" || in.RequestID == uuid.Nil {
		return 0, ErrValidation
	}
	if !requestResolutionStatus(in.NewStatus) || strings.TrimSpace(in.OldStatus) == strings.TrimSpace(in.NewStatus) {
		return 0, nil
	}
	campaigns, err := s.repo.ListActiveCampaignsByTrigger(ctx, tenantID, repo.TriggerRequestResolved)
	if err != nil {
		return 0, mapRepoError(err)
	}
	recipients, err := s.repo.RequestRecipients(ctx, tenantID, in.RequestID)
	if err != nil {
		return 0, mapRepoError(err)
	}
	attrs := requestTriggerAttrs(in)
	count := 0
	for _, campaign := range campaigns {
		created, err := s.createRequestInvitationsForCampaign(ctx, campaign, in, recipients, attrs)
		if err != nil {
			return count, err
		}
		count += created
	}
	return count, nil
}

func (s *Service) createFeedbackTriggeredInvitations(
	ctx context.Context,
	triggerEvent string,
	contextRow repo.TriggerContext,
	attrs map[string]string,
	sourceType string,
	sourceID string,
	actorID string,
) (int, error) {
	campaigns, err := s.repo.ListActiveCampaignsByTrigger(ctx, contextRow.TenantID, triggerEvent)
	if err != nil {
		return 0, mapRepoError(err)
	}
	count := 0
	for _, campaign := range campaigns {
		target := feedbackInvitationTarget(campaign, contextRow, attrs, sourceType, sourceID, actorID)
		created, err := s.createTriggeredInvitation(ctx, campaign, target, attrs)
		if err != nil {
			return count, err
		}
		if created {
			count++
		}
	}
	return count, nil
}

func (s *Service) createRequestInvitationsForCampaign(
	ctx context.Context,
	campaign repo.Campaign,
	in RequestResolvedInput,
	recipients []repo.RequestRecipient,
	attrs map[string]string,
) (int, error) {
	if !campaignMatchesTrigger(campaign, attrs) {
		return 0, nil
	}
	if campaign.DistributionMode == repo.DistributionSourceLink || len(recipients) == 0 {
		target := requestInvitationTarget(campaign, in, nil, "no_eligible_recipient")
		if campaign.DistributionMode == repo.DistributionSourceLink {
			target.SuppressionReason = ""
		}
		created, err := s.createTriggeredInvitation(ctx, campaign, target, attrs)
		return boolInt(created), err
	}
	count := 0
	for _, recipient := range recipients {
		target := requestInvitationTarget(campaign, in, ptrext.Of(recipient), "")
		created, err := s.createTriggeredInvitation(ctx, campaign, target, attrs)
		if err != nil {
			return count, err
		}
		if created {
			count++
		}
	}
	return count, nil
}

func (s *Service) createTriggeredInvitation(
	ctx context.Context,
	campaign repo.Campaign,
	target triggerTarget,
	attrs map[string]string,
) (bool, error) {
	if !campaignMatchesTrigger(campaign, attrs) || !sampleIncluded(campaign, target.SourceType, target.SourceID) {
		return false, nil
	}
	reason, err := s.suppressionReason(ctx, campaign, target, attrs)
	if err != nil {
		return false, err
	}
	if target.SuppressionReason == "" {
		target.SuppressionReason = reason
	}
	invitation, err := s.buildTriggeredInvitation(campaign, target)
	if err != nil {
		return false, err
	}
	if target.ContactID != nil && invitation.SuppressionStatus == repo.SuppressionNotSuppressed {
		var cooldownSince *time.Time
		if campaign.MinDaysBetweenContact > 0 {
			since := s.now().UTC().Add(-time.Duration(campaign.MinDaysBetweenContact) * 24 * time.Hour)
			cooldownSince = ptrext.Of(since)
		}
		_, skipReason, err := s.repo.CreateInvitationWithContactCooldown(ctx, invitation, cooldownSince)
		if err != nil {
			if errors.Is(mapRepoError(err), ErrConflict) {
				return false, nil
			}
			return false, mapRepoError(err)
		}
		if skipReason == "" {
			return true, nil
		}
		target.SuppressionReason = skipReason
		invitation, err = s.buildTriggeredInvitation(campaign, target)
		if err != nil {
			return false, err
		}
	}
	if _, err := s.repo.CreateInvitation(ctx, invitation); err != nil {
		if errors.Is(mapRepoError(err), ErrConflict) {
			return false, nil
		}
		return false, mapRepoError(err)
	}
	return true, nil
}

func (s *Service) suppressionReason(
	ctx context.Context,
	campaign repo.Campaign,
	target triggerTarget,
	attrs map[string]string,
) (string, error) {
	if campaign.SuppressAutoResolved && strings.EqualFold(attrs["auto_resolved"], "true") {
		return "auto_resolved", nil
	}
	if reason := recentActivitySuppression(campaign, target, s.now()); reason != "" {
		return reason, nil
	}
	if campaign.MaxDailyInvitations > 0 {
		count, err := s.repo.CountCampaignInvitationsSince(ctx, campaign.TenantID, campaign.ID, startOfUTCDay(s.now()))
		if err != nil {
			return "", mapRepoError(err)
		}
		if count >= campaign.MaxDailyInvitations {
			return "campaign_daily_limit", nil
		}
	}
	if target.ContactID == nil || campaign.MinDaysBetweenContact <= 0 {
		return "", nil
	}
	since := s.now().UTC().Add(-time.Duration(campaign.MinDaysBetweenContact) * 24 * time.Hour)
	count, err := s.repo.CountContactInvitationsSince(ctx, campaign.TenantID, ptrext.Indirect(target.ContactID), since)
	if err != nil {
		return "", mapRepoError(err)
	}
	if count > 0 {
		return "contact_cooldown", nil
	}
	return "", nil
}

func (s *Service) buildTriggeredInvitation(campaign repo.Campaign, target triggerTarget) (repo.Invitation, error) {
	token, err := newToken()
	if err != nil {
		return repo.Invitation{}, err
	}
	now := s.now().UTC()
	expiresAt := now.Add(time.Duration(campaign.ExpiresAfterDays) * 24 * time.Hour)
	status := repo.SuppressionNotSuppressed
	deliveryStatus := repo.DeliveryNotApplicable
	var deliverySecret []byte
	if target.SuppressionReason != "" {
		status = repo.SuppressionSuppressed
	} else if campaign.DistributionMode == repo.DistributionContactEmail {
		deliveryStatus = repo.DeliveryPending
		deliverySecret, err = s.encryptDeliverySecret(s.publicSurveyURL(token))
		if err != nil {
			return repo.Invitation{}, err
		}
	}
	invitation := repo.Invitation{
		ID:                     uuid.New(),
		TenantID:               campaign.TenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       campaignSnapshot(campaign),
		DedupeKey:              dedupeKeyAt(campaign, target.SourceType, target.SourceID, target.RequestID, now),
		SourceType:             target.SourceType,
		SourceID:               target.SourceID,
		RequestID:              target.RequestID,
		ContactID:              target.ContactID,
		DistributionMode:       campaign.DistributionMode,
		TokenHash:              tokenHash(token),
		DeliveryStatus:         deliveryStatus,
		ResponseStatus:         repo.ResponseNotStarted,
		SuppressionStatus:      status,
		SuppressionReason:      target.SuppressionReason,
		RecipientSnapshot:      normalizeObject(target.RecipientSnapshot),
		DeliverySecret:         deliverySecret,
		ExpiresAt:              ptrext.Of(expiresAt),
		CreatedBy:              defaultActor(target.ActorID),
	}
	if campaign.DistributionMode == repo.DistributionSourceLink && target.SuppressionReason == "" {
		invitation.PublicURL = s.publicSurveyURL(token)
	}
	return invitation, nil
}

func (s *Service) encryptDeliverySecret(publicURL string) ([]byte, error) {
	return s.encryptSurveyDeliverySecret(surveyDeliverySecret{PublicURL: publicURL})
}

func (s *Service) encryptSurveyDeliverySecret(secret surveyDeliverySecret) ([]byte, error) {
	if s.secrets == nil {
		return nil, ErrDisabled
	}
	secret.PublicURL = strings.TrimSpace(secret.PublicURL)
	secret.UnsubscribeURL = strings.TrimSpace(secret.UnsubscribeURL)
	if secret.PublicURL == "" {
		return nil, ErrValidation
	}
	if secret.UnsubscribeExpiresAt != nil {
		expiresAt := ptrext.Indirect(secret.UnsubscribeExpiresAt).UTC()
		secret.UnsubscribeExpiresAt = ptrext.Of(expiresAt)
	}
	raw, err := json.Marshal(secret)
	if err != nil {
		return nil, err
	}
	return s.secrets.Encrypt(raw)
}

func feedbackInvitationTarget(
	campaign repo.Campaign,
	contextRow repo.TriggerContext,
	attrs map[string]string,
	sourceType string,
	sourceID string,
	actorID string,
) triggerTarget {
	target := triggerTarget{
		SourceType:        sourceType,
		SourceID:          sourceID,
		RequestID:         contextRow.RequestID,
		ContactID:         contextRow.ContactID,
		LastActivityAt:    feedbackLastActivityAt(contextRow),
		RecipientSnapshot: feedbackRecipientSnapshot(contextRow, attrs),
		ActorID:           actorID,
	}
	if campaign.DistributionMode == repo.DistributionContactEmail && contextRow.ContactID == nil {
		target.SuppressionReason = "no_eligible_recipient"
	}
	return target
}

func requestInvitationTarget(
	campaign repo.Campaign,
	in RequestResolvedInput,
	recipient *repo.RequestRecipient,
	suppression string,
) triggerTarget {
	requestID := in.RequestID
	target := triggerTarget{
		SourceType:        repo.TriggerRequestResolved,
		SourceID:          requestSourceID(in.RequestID, recipient),
		RequestID:         ptrext.Of(requestID),
		RecipientSnapshot: requestRecipientSnapshot(in, recipient),
		ActorID:           in.ActorID,
		SuppressionReason: suppression,
	}
	if recipient != nil {
		contactID := recipient.ContactID
		target.ContactID = ptrext.Of(contactID)
		target.LastActivityAt = recipient.LastActivityAt
	}
	if campaign.DistributionMode == repo.DistributionContactEmail && recipient == nil {
		target.SuppressionReason = "no_eligible_recipient"
	}
	return target
}

func feedbackRecipientSnapshot(contextRow repo.TriggerContext, attrs map[string]string) map[string]any {
	snapshot := map[string]any{
		"feedback_id":     contextRow.FeedbackID,
		"source":          contextRow.Source,
		"subject_display": contextRow.SubjectDisplay,
		"trigger":         attrs,
	}
	if contextRow.RequestID != nil {
		snapshot["request_id"] = contextRow.RequestID.String()
		snapshot["request_title"] = contextRow.RequestTitle
		snapshot["request_status"] = contextRow.RequestStatus
	}
	if contextRow.ContactID != nil {
		snapshot["contact_id"] = contextRow.ContactID.String()
		snapshot["contact_display"] = contextRow.ContactDisplay
	}
	if lastActivityAt := feedbackLastActivityAt(contextRow); !lastActivityAt.IsZero() {
		snapshot["last_activity_at"] = lastActivityAt.UTC().Format(time.RFC3339Nano)
	}
	return snapshot
}

func requestRecipientSnapshot(in RequestResolvedInput, recipient *repo.RequestRecipient) map[string]any {
	snapshot := map[string]any{
		"request_id": in.RequestID.String(),
		"title":      strings.TrimSpace(in.Title),
		"old_status": strings.TrimSpace(in.OldStatus),
		"new_status": strings.TrimSpace(in.NewStatus),
	}
	if recipient != nil {
		snapshot["contact_id"] = recipient.ContactID.String()
		snapshot["contact_display"] = recipient.DisplayName
		snapshot["organization"] = recipient.Organization
		snapshot["subject_display"] = recipient.SubjectDisplay
		if !recipient.LastActivityAt.IsZero() {
			snapshot["last_activity_at"] = recipient.LastActivityAt.UTC().Format(time.RFC3339Nano)
		}
	}
	return snapshot
}

func workflowTriggerAttrs(in WorkflowTransitionInput, contextRow repo.TriggerContext) map[string]string {
	attrs := map[string]string{
		"source":              contextRow.Source,
		"from_state_id":       in.FromStateID,
		"from_state_name":     in.FromStateName,
		"from_state_category": in.FromStateCategory,
		"to_state_id":         in.ToStateID,
		"to_state_name":       in.ToStateName,
		"to_state_category":   in.ToStateCategory,
		"workflow_category":   in.ToStateCategory,
	}
	addCommonAttrs(attrs, contextRow)
	if in.AutoResolvedSet {
		attrs["auto_resolved"] = fmt.Sprintf("%t", in.AutoResolved)
	}
	return attrs
}

func replyTriggerAttrs(in ReplySentInput, contextRow repo.TriggerContext) map[string]string {
	attrs := map[string]string{
		"source":              contextRow.Source,
		"draft_id":            in.DraftID,
		"attempt_id":          in.AttemptID,
		"revision_id":         in.RevisionID,
		"external_message_id": in.ExternalMessageID,
	}
	addCommonAttrs(attrs, contextRow)
	return attrs
}

func requestTriggerAttrs(in RequestResolvedInput) map[string]string {
	return map[string]string{
		"request_id":     in.RequestID.String(),
		"old_status":     strings.TrimSpace(in.OldStatus),
		"new_status":     strings.TrimSpace(in.NewStatus),
		"request_status": strings.TrimSpace(in.NewStatus),
		"has_request":    "true",
	}
}

func addCommonAttrs(attrs map[string]string, contextRow repo.TriggerContext) {
	attrs["has_request"] = fmt.Sprintf("%t", contextRow.RequestID != nil)
	attrs["request_status"] = contextRow.RequestStatus
	attrs["subject_hash"] = contextRow.SubjectHash
}

func campaignMatchesTrigger(campaign repo.Campaign, attrs map[string]string) bool {
	for key, raw := range campaign.TriggerFilter {
		if !filterValueMatches(key, raw, attrs) {
			return false
		}
	}
	return true
}

func filterValueMatches(key string, raw any, attrs map[string]string) bool {
	key = strings.TrimSpace(key)
	value, ok := attrs[key]
	if !ok {
		return false
	}
	switch typed := raw.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), value)
	case bool:
		return strings.EqualFold(fmt.Sprintf("%t", typed), value)
	case []any:
		return anyFilterValueMatches(typed, value)
	default:
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(typed)), value)
	}
}

func anyFilterValueMatches(values []any, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), target) {
			return true
		}
	}
	return false
}

func sampleIncluded(campaign repo.Campaign, sourceType string, sourceID string) bool {
	switch {
	case campaign.SamplingPercent >= 100:
		return true
	case campaign.SamplingPercent <= 0:
		return false
	default:
		sum := sha256.Sum256([]byte(campaign.ID.String() + ":" + sourceType + ":" + sourceID))
		bucket := binary.BigEndian.Uint64(sum[:8]) % 10000
		return bucket < uint64(campaign.SamplingPercent*100)
	}
}

func recentActivitySuppression(campaign repo.Campaign, target triggerTarget, now time.Time) string {
	if !campaign.RequireRecentCustomerActivity || campaign.RecentActivityDays <= 0 {
		return ""
	}
	if target.ContactID == nil && campaign.DistributionMode != repo.DistributionContactEmail {
		return ""
	}
	if target.LastActivityAt.IsZero() {
		return "no_recent_customer_activity"
	}
	cutoff := now.UTC().Add(-time.Duration(campaign.RecentActivityDays) * 24 * time.Hour)
	if target.LastActivityAt.UTC().Before(cutoff) {
		return "no_recent_customer_activity"
	}
	return ""
}

func feedbackLastActivityAt(contextRow repo.TriggerContext) time.Time {
	if !contextRow.LastActivityAt.IsZero() {
		return contextRow.LastActivityAt
	}
	return contextRow.CreatedAt
}

func requestResolutionStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "shipped", "cancelled":
		return true
	default:
		return false
	}
}

func requestSourceID(requestID uuid.UUID, recipient *repo.RequestRecipient) string {
	if recipient == nil {
		return requestID.String()
	}
	return requestID.String() + ":" + recipient.ContactID.String()
}

func startOfUTCDay(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func defaultActor(actorID string) string {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return "system"
	}
	return actorID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
