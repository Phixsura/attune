// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

const (
	recipientPreviewDefaultLimit = 10
	recipientPreviewMaxLimit     = 50
)

type RecipientPreviewInput struct {
	TenantID   string
	CampaignID uuid.UUID
	SourceType string
	SourceID   string
	RequestID  *uuid.UUID
	Context    map[string]any
	Limit      int
}

type RecipientPreviewResult struct {
	CampaignID         uuid.UUID
	TriggerMatched     bool
	SampleIncluded     bool
	MatchedCount       int
	EligibleCount      int
	SuppressedCount    int
	Recipients         []RecipientPreview
	SuppressionReasons []repo.SuppressionReasonBucket
	DeliveryReady      bool
	DeliveryBlocker    string
}

type RecipientPreview struct {
	SourceType        string
	SourceID          string
	RequestID         *uuid.UUID
	ContactID         *uuid.UUID
	Channel           string
	DisplayName       string
	SubjectDisplay    string
	Eligible          bool
	SuppressionReason string
	RecipientSnapshot map[string]any
	LastActivityAt    *time.Time
}

func (s *Service) PreviewRecipients(ctx context.Context, in RecipientPreviewInput) (RecipientPreviewResult, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" || in.CampaignID == uuid.Nil {
		return RecipientPreviewResult{}, ErrValidation
	}
	campaign, err := s.repo.GetCampaign(ctx, tenantID, in.CampaignID)
	if err != nil {
		return RecipientPreviewResult{}, mapRepoError(err)
	}
	if campaign.Status == repo.StatusArchived {
		return RecipientPreviewResult{}, ErrDisabled
	}
	targets, attrs, err := s.previewTargets(ctx, campaign, in)
	if err != nil {
		return RecipientPreviewResult{}, err
	}
	deliveryReady, deliveryBlocker, err := s.previewDeliveryReadiness(ctx, campaign)
	if err != nil {
		return RecipientPreviewResult{}, err
	}
	limit := boundedRecipientPreviewLimit(in.Limit)
	result := RecipientPreviewResult{
		CampaignID:      campaign.ID,
		TriggerMatched:  campaignMatchesTrigger(campaign, attrs),
		SampleIncluded:  true,
		DeliveryReady:   deliveryReady,
		DeliveryBlocker: deliveryBlocker,
		Recipients:      make([]RecipientPreview, 0, minInt(len(targets), limit)),
	}
	reasonCounts := map[string]int{}
	for _, target := range targets {
		preview, err := s.previewRecipient(ctx, campaign, target, attrs, result.TriggerMatched)
		if err != nil {
			return RecipientPreviewResult{}, err
		}
		result.MatchedCount++
		if preview.Eligible {
			result.EligibleCount++
		} else {
			result.SuppressedCount++
			result.SampleIncluded = result.SampleIncluded && preview.SuppressionReason != "sampled_out"
			reasonCounts[preview.SuppressionReason]++
		}
		if len(result.Recipients) < limit {
			result.Recipients = append(result.Recipients, preview)
		}
	}
	result.SuppressionReasons = previewSuppressionBuckets(reasonCounts)
	return result, nil
}

func (s *Service) previewDeliveryReadiness(ctx context.Context, campaign repo.Campaign) (bool, string, error) {
	return s.campaignDeliveryReadiness(ctx, campaign)
}

func (s *Service) previewTargets(
	ctx context.Context,
	campaign repo.Campaign,
	in RecipientPreviewInput,
) ([]triggerTarget, map[string]string, error) {
	switch campaign.TriggerEvent {
	case repo.TriggerWorkflowTransition:
		return s.previewWorkflowTargets(ctx, campaign, in)
	case repo.TriggerReplySent:
		return s.previewReplyTargets(ctx, campaign, in)
	case repo.TriggerRequestResolved:
		return s.previewRequestTargets(ctx, campaign, in)
	case repo.TriggerManualLink:
		return previewManualTargets(campaign, in), normalizeStringContext(in.Context), nil
	default:
		return nil, nil, ErrValidation
	}
}

func (s *Service) previewWorkflowTargets(
	ctx context.Context,
	campaign repo.Campaign,
	in RecipientPreviewInput,
) ([]triggerTarget, map[string]string, error) {
	feedbackID, err := previewFeedbackID(in)
	if err != nil {
		return nil, nil, err
	}
	contextRow, err := s.repo.FeedbackTriggerContext(ctx, campaign.TenantID, feedbackID)
	if err != nil {
		return nil, nil, mapRepoError(err)
	}
	attrs := workflowTriggerAttrs(WorkflowTransitionInput{
		TenantID:          campaign.TenantID,
		FeedbackID:        feedbackID,
		FromStateID:       previewContextString(in.Context, "from_state_id"),
		FromStateName:     previewContextString(in.Context, "from_state_name"),
		FromStateCategory: previewContextString(in.Context, "from_state_category"),
		ToStateID:         previewContextString(in.Context, "to_state_id"),
		ToStateName:       previewContextString(in.Context, "to_state_name"),
		ToStateCategory:   firstNonEmpty(previewContextString(in.Context, "workflow_category"), previewContextString(in.Context, "to_state_category"), "closed"),
		AutoResolved:      previewContextBoolValue(in.Context, "auto_resolved"),
		AutoResolvedSet:   previewContextHas(in.Context, "auto_resolved"),
	}, contextRow)
	target := feedbackInvitationTarget(campaign, contextRow, attrs,
		repo.TriggerWorkflowTransition, firstNonEmpty(strings.TrimSpace(in.SourceID), fmt.Sprintf("%d", feedbackID)), "preview")
	return []triggerTarget{target}, attrs, nil
}

func (s *Service) previewReplyTargets(
	ctx context.Context,
	campaign repo.Campaign,
	in RecipientPreviewInput,
) ([]triggerTarget, map[string]string, error) {
	feedbackID, err := previewFeedbackID(in)
	if err != nil {
		return nil, nil, err
	}
	contextRow, err := s.repo.FeedbackTriggerContext(ctx, campaign.TenantID, feedbackID)
	if err != nil {
		return nil, nil, mapRepoError(err)
	}
	attrs := replyTriggerAttrs(ReplySentInput{
		TenantID:          campaign.TenantID,
		FeedbackID:        feedbackID,
		DraftID:           previewContextString(in.Context, "draft_id"),
		AttemptID:         previewContextString(in.Context, "attempt_id"),
		RevisionID:        previewContextString(in.Context, "revision_id"),
		ExternalMessageID: previewContextString(in.Context, "external_message_id"),
	}, contextRow)
	sourceID := firstNonEmpty(attrs["attempt_id"], attrs["draft_id"], strings.TrimSpace(in.SourceID), fmt.Sprintf("%d", feedbackID))
	target := feedbackInvitationTarget(campaign, contextRow, attrs, repo.TriggerReplySent, sourceID, "preview")
	return []triggerTarget{target}, attrs, nil
}

func (s *Service) previewRequestTargets(
	ctx context.Context,
	campaign repo.Campaign,
	in RecipientPreviewInput,
) ([]triggerTarget, map[string]string, error) {
	requestID, err := previewRequestID(in)
	if err != nil {
		return nil, nil, err
	}
	title := firstNonEmpty(previewContextString(in.Context, "title"), previewContextString(in.Context, "request_title"))
	attrs := requestTriggerAttrs(RequestResolvedInput{
		TenantID:  campaign.TenantID,
		RequestID: requestID,
		OldStatus: previewContextString(in.Context, "old_status"),
		NewStatus: firstNonEmpty(previewContextString(in.Context, "request_status"), previewContextString(in.Context, "new_status"), "shipped"),
		Title:     title,
		ActorID:   "preview",
	})
	recipients, err := s.repo.RequestRecipients(ctx, campaign.TenantID, requestID)
	if err != nil {
		return nil, nil, mapRepoError(err)
	}
	input := RequestResolvedInput{
		TenantID:  campaign.TenantID,
		RequestID: requestID,
		OldStatus: attrs["old_status"],
		NewStatus: attrs["new_status"],
		Title:     title,
		ActorID:   "preview",
	}
	if campaign.DistributionMode == repo.DistributionSourceLink || len(recipients) == 0 {
		target := requestInvitationTarget(campaign, input, nil, "no_eligible_recipient")
		if campaign.DistributionMode == repo.DistributionSourceLink {
			target.SuppressionReason = ""
		}
		return []triggerTarget{target}, attrs, nil
	}
	targets := make([]triggerTarget, 0, len(recipients))
	for _, recipient := range recipients {
		targets = append(targets, requestInvitationTarget(campaign, input, ptrext.Of(recipient), ""))
	}
	return targets, attrs, nil
}

func previewManualTargets(campaign repo.Campaign, in RecipientPreviewInput) []triggerTarget {
	sourceType := firstNonEmpty(strings.TrimSpace(in.SourceType), "manual")
	sourceID := firstNonEmpty(strings.TrimSpace(in.SourceID), "preview")
	target := triggerTarget{
		SourceType:        sourceType,
		SourceID:          sourceID,
		RequestID:         in.RequestID,
		RecipientSnapshot: normalizeObject(in.Context),
		ActorID:           "preview",
	}
	if campaign.DistributionMode == repo.DistributionContactEmail {
		target.SuppressionReason = "no_eligible_recipient"
	}
	return []triggerTarget{target}
}

func (s *Service) previewRecipient(
	ctx context.Context,
	campaign repo.Campaign,
	target triggerTarget,
	attrs map[string]string,
	triggerMatched bool,
) (RecipientPreview, error) {
	reason, err := s.previewSuppressionReason(ctx, campaign, target, attrs, triggerMatched)
	if err != nil {
		return RecipientPreview{}, err
	}
	return recipientPreviewFromTarget(campaign, target, reason), nil
}

func (s *Service) previewSuppressionReason(
	ctx context.Context,
	campaign repo.Campaign,
	target triggerTarget,
	attrs map[string]string,
	triggerMatched bool,
) (string, error) {
	if !triggerMatched {
		return "trigger_filter_mismatch", nil
	}
	if !sampleIncluded(campaign, target.SourceType, target.SourceID) {
		return "sampled_out", nil
	}
	if target.SuppressionReason != "" {
		return target.SuppressionReason, nil
	}
	reason, err := s.suppressionReason(ctx, campaign, target, attrs)
	if err != nil || reason != "" {
		return reason, err
	}
	return s.previewDedupeReason(ctx, campaign, target)
}

func (s *Service) previewDedupeReason(ctx context.Context, campaign repo.Campaign, target triggerTarget) (string, error) {
	if campaign.DedupePolicy == repo.DedupeOnePerTrigger {
		return "", nil
	}
	dedupeKey := dedupeKeyAt(campaign, target.SourceType, target.SourceID, target.RequestID, s.now().UTC())
	exists, err := s.repo.InvitationExistsByDedupeKey(ctx, campaign.TenantID, campaign.ID, dedupeKey)
	if err != nil {
		return "", mapRepoError(err)
	}
	if exists {
		return "dedupe_conflict", nil
	}
	return "", nil
}

func recipientPreviewFromTarget(campaign repo.Campaign, target triggerTarget, reason string) RecipientPreview {
	preview := RecipientPreview{
		SourceType:        target.SourceType,
		SourceID:          target.SourceID,
		RequestID:         target.RequestID,
		ContactID:         target.ContactID,
		Channel:           recipientPreviewChannel(campaign),
		DisplayName:       snapshotString(target.RecipientSnapshot, "contact_display"),
		SubjectDisplay:    snapshotString(target.RecipientSnapshot, "subject_display"),
		Eligible:          reason == "",
		SuppressionReason: reason,
		RecipientSnapshot: normalizeObject(target.RecipientSnapshot),
	}
	if !target.LastActivityAt.IsZero() {
		lastActivityAt := target.LastActivityAt.UTC()
		preview.LastActivityAt = ptrext.Of(lastActivityAt)
	}
	return preview
}

func recipientPreviewChannel(campaign repo.Campaign) string {
	if campaign.DistributionMode == repo.DistributionContactEmail {
		return "email"
	}
	return "hosted_link"
}

func previewFeedbackID(in RecipientPreviewInput) (int64, error) {
	if value, ok := previewContextInt(in.Context, "feedback_id"); ok && value > 0 {
		return value, nil
	}
	sourceID := strings.TrimSpace(in.SourceID)
	if sourceID == "" {
		return 0, ErrValidation
	}
	id, err := strconv.ParseInt(sourceID, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrValidation
	}
	return id, nil
}

func previewRequestID(in RecipientPreviewInput) (uuid.UUID, error) {
	if in.RequestID != nil {
		return ptrext.Indirect(in.RequestID), nil
	}
	if value := previewContextString(in.Context, "request_id"); value != "" {
		id, err := uuid.Parse(value)
		if err != nil {
			return uuid.Nil, ErrValidation
		}
		return id, nil
	}
	sourceID := strings.TrimSpace(in.SourceID)
	if sourceID == "" {
		return uuid.Nil, ErrValidation
	}
	id, err := uuid.Parse(sourceID)
	if err != nil {
		return uuid.Nil, ErrValidation
	}
	return id, nil
}

func previewContextString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			return boundedString(text, 512)
		}
	}
	return ""
}

func previewContextBoolValue(values map[string]any, key string) bool {
	value, ok := values[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && parsed
	default:
		return false
	}
}

func previewContextHas(values map[string]any, key string) bool {
	_, ok := values[key]
	return ok
}

func previewContextInt(values map[string]any, key string) (int64, bool) {
	value, ok := values[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func normalizeStringContext(values map[string]any) map[string]string {
	out := map[string]string{}
	for key, value := range normalizeObject(values) {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			out[key] = boundedString(text, 512)
		}
	}
	return out
}

func snapshotString(snapshot map[string]any, key string) string {
	value, ok := snapshot[key]
	if !ok {
		return ""
	}
	return boundedString(strings.TrimSpace(fmt.Sprint(value)), 256)
}

func boundedRecipientPreviewLimit(limit int) int {
	if limit <= 0 {
		return recipientPreviewDefaultLimit
	}
	if limit > recipientPreviewMaxLimit {
		return recipientPreviewMaxLimit
	}
	return limit
}

func previewSuppressionBuckets(counts map[string]int) []repo.SuppressionReasonBucket {
	if len(counts) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}
	sort.Strings(reasons)
	out := make([]repo.SuppressionReasonBucket, 0, len(reasons))
	for _, reason := range reasons {
		out = append(out, repo.SuppressionReasonBucket{Reason: reason, Count: counts[reason]})
	}
	return out
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
