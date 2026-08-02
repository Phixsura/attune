// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/survey"
	svc "github.com/Phixsura/attune/internal/service/survey"
)

func campaignToProto(item repo.Campaign) *attunev1.SurveyCampaign {
	return ptrext.Of(attunev1.SurveyCampaign{
		Id:                            item.ID.String(),
		Name:                          item.Name,
		SurveyType:                    surveyTypeToProto(item.SurveyType),
		Status:                        campaignStatusToProto(item.Status),
		TriggerEvent:                  triggerEventToProto(item.TriggerEvent),
		DistributionMode:              distributionModeToProto(item.DistributionMode),
		DedupePolicy:                  dedupePolicyToProto(item.DedupePolicy),
		TriggerFilter:                 mapStruct(item.TriggerFilter),
		Content:                       mapStruct(item.Content),
		Locale:                        item.Locale,
		ContentVersion:                int32(item.ContentVersion),
		SamplingPercent:               item.SamplingPercent,
		MinDaysBetweenContact:         int32(item.MinDaysBetweenContact),
		ExpiresAfterDays:              int32(item.ExpiresAfterDays),
		MaxDailyInvitations:           int32(item.MaxDailyInvitations),
		LowScoreThreshold:             int32(item.LowScoreThreshold),
		RequireRecentCustomerActivity: item.RequireRecentCustomerActivity,
		RecentActivityDays:            int32(item.RecentActivityDays),
		SuppressAutoResolved:          item.SuppressAutoResolved,
		CreatedBy:                     item.CreatedBy,
		UpdatedBy:                     item.UpdatedBy,
		CreatedAt:                     timeString(item.CreatedAt),
		UpdatedAt:                     timeString(item.UpdatedAt),
		ArchivedAt:                    optionalTimeString(item.ArchivedAt),
	})
}

func invitationToProto(item repo.Invitation) *attunev1.SurveyInvitation {
	out := ptrext.Of(attunev1.SurveyInvitation{
		Id:                item.ID.String(),
		CampaignId:        item.CampaignID.String(),
		SourceType:        item.SourceType,
		SourceId:          item.SourceID,
		DistributionMode:  distributionModeToProto(item.DistributionMode),
		DeliveryStatus:    deliveryStatusToProto(item.DeliveryStatus),
		ResponseStatus:    responseStatusToProto(item.ResponseStatus),
		SuppressionStatus: suppressionStatusToProto(item.SuppressionStatus),
		SuppressionReason: item.SuppressionReason,
		CampaignSnapshot:  mapStruct(item.CampaignSnapshot),
		RecipientSnapshot: mapStruct(item.RecipientSnapshot),
		CreatedAt:         timeString(item.CreatedAt),
		UpdatedAt:         timeString(item.UpdatedAt),
		DeliveryRetryable: deliveryRetryable(item),
		ExpiresAt:         optionalTimeString(item.ExpiresAt),
		DeliveredAt:       optionalTimeString(item.DeliveredAt),
		OpenedAt:          optionalTimeString(item.OpenedAt),
		RespondedAt:       optionalTimeString(item.RespondedAt),
	})
	if item.RequestID != nil {
		out.RequestId = ptrext.Of(item.RequestID.String())
	}
	if item.ContactID != nil {
		out.ContactId = ptrext.Of(item.ContactID.String())
	}
	if item.PublicURL != "" {
		out.PublicUrl = ptrext.Of(item.PublicURL)
	}
	return out
}

func deliveryRetryable(item repo.Invitation) bool {
	if item.DistributionMode != repo.DistributionContactEmail ||
		item.SuppressionStatus == repo.SuppressionSuppressed ||
		item.ResponseStatus == repo.ResponseCompleted ||
		len(item.DeliverySecret) == 0 {
		return false
	}
	return item.DeliveryStatus == repo.DeliveryPending || item.DeliveryStatus == repo.DeliveryDelayed
}

func recipientPreviewResultToProto(item svc.RecipientPreviewResult) *attunev1.PreviewSurveyRecipientsResponse {
	out := ptrext.Of(attunev1.PreviewSurveyRecipientsResponse{
		CampaignId:                    item.CampaignID.String(),
		TriggerMatched:                item.TriggerMatched,
		SampleIncluded:                item.SampleIncluded,
		MatchedCount:                  int32(item.MatchedCount),
		EligibleCount:                 int32(item.EligibleCount),
		SuppressedCount:               int32(item.SuppressedCount),
		Recipients:                    make([]*attunev1.SurveyRecipientPreview, 0, len(item.Recipients)),
		SuppressionReasonDistribution: make([]*attunev1.SurveySuppressionReasonBucket, 0, len(item.SuppressionReasons)),
		DeliveryReady:                 item.DeliveryReady,
		DeliveryBlocker:               item.DeliveryBlocker,
	})
	for _, recipient := range item.Recipients {
		out.Recipients = append(out.Recipients, recipientPreviewToProto(recipient))
	}
	for _, bucket := range item.SuppressionReasons {
		out.SuppressionReasonDistribution = append(out.SuppressionReasonDistribution, ptrext.Of(
			attunev1.SurveySuppressionReasonBucket{
				Reason: bucket.Reason,
				Count:  int32(bucket.Count),
			},
		))
	}
	return out
}

func recipientPreviewToProto(item svc.RecipientPreview) *attunev1.SurveyRecipientPreview {
	out := ptrext.Of(attunev1.SurveyRecipientPreview{
		SourceType:        item.SourceType,
		SourceId:          item.SourceID,
		Channel:           item.Channel,
		DisplayName:       item.DisplayName,
		SubjectDisplay:    item.SubjectDisplay,
		Eligible:          item.Eligible,
		SuppressionReason: item.SuppressionReason,
		RecipientSnapshot: mapStruct(item.RecipientSnapshot),
		LastActivityAt:    optionalTimeString(item.LastActivityAt),
	})
	if item.RequestID != nil {
		out.RequestId = ptrext.Of(item.RequestID.String())
	}
	if item.ContactID != nil {
		out.ContactId = ptrext.Of(item.ContactID.String())
	}
	return out
}

func campaignHealthToProto(item svc.CampaignHealth) *attunev1.SurveyCampaignHealth {
	out := ptrext.Of(attunev1.SurveyCampaignHealth{
		CampaignId:                    item.CampaignID.String(),
		Status:                        campaignHealthStatusToProto(item.Status),
		ReadinessScore:                int32(item.ReadinessScore),
		Funnel:                        campaignHealthFunnelToProto(item.Funnel),
		Checks:                        make([]*attunev1.SurveyCampaignHealthCheck, 0, len(item.Checks)),
		SuppressionReasonDistribution: make([]*attunev1.SurveySuppressionReasonBucket, 0, len(item.SuppressionReasons)),
		GeneratedAt:                   timeString(item.GeneratedAt),
	})
	for _, check := range item.Checks {
		out.Checks = append(out.Checks, campaignHealthCheckToProto(check))
	}
	for _, bucket := range item.SuppressionReasons {
		out.SuppressionReasonDistribution = append(out.SuppressionReasonDistribution, ptrext.Of(
			attunev1.SurveySuppressionReasonBucket{
				Reason: bucket.Reason,
				Count:  int32(bucket.Count),
			},
		))
	}
	return out
}

func campaignHealthFunnelToProto(item svc.CampaignHealthFunnel) *attunev1.SurveyCampaignHealthFunnel {
	return ptrext.Of(attunev1.SurveyCampaignHealthFunnel{
		InvitationCount:            int32(item.InvitationCount),
		PendingCount:               int32(item.PendingCount),
		DelayedCount:               int32(item.DelayedCount),
		DeliveredCount:             int32(item.DeliveredCount),
		OpenedCount:                int32(item.OpenedCount),
		CompletedCount:             int32(item.CompletedCount),
		SuppressedCount:            int32(item.SuppressedCount),
		ExpiredCount:               int32(item.ExpiredCount),
		RejectedCount:              int32(item.RejectedCount),
		LowScoreCount:              int32(item.LowScoreCount),
		OpenLowScoreReviewCount:    int32(item.OpenLowScoreReviewCount),
		OverdueLowScoreReviewCount: int32(item.OverdueLowScoreReviewCount),
		DeliveryRate:               item.DeliveryRate,
		OpenRate:                   item.OpenRate,
		ResponseRate:               item.ResponseRate,
		SuppressionRate:            item.SuppressionRate,
		ExpiredRate:                item.ExpiredRate,
		RecoveryOverdueRate:        item.RecoveryOverdueRate,
	})
}

func campaignHealthCheckToProto(item svc.CampaignHealthCheck) *attunev1.SurveyCampaignHealthCheck {
	return ptrext.Of(attunev1.SurveyCampaignHealthCheck{
		Id:                item.ID,
		Status:            campaignHealthCheckStatusToProto(item.Status),
		Title:             item.Title,
		Summary:           item.Summary,
		RecommendedAction: item.RecommendedAction,
		Evidence:          item.Evidence,
	})
}

func campaignHealthStatusToProto(value string) attunev1.SurveyCampaignHealthStatus {
	switch value {
	case svc.CampaignHealthHealthy:
		return attunev1.SurveyCampaignHealthStatus_SURVEY_CAMPAIGN_HEALTH_STATUS_HEALTHY
	case svc.CampaignHealthNeedsAttention:
		return attunev1.SurveyCampaignHealthStatus_SURVEY_CAMPAIGN_HEALTH_STATUS_NEEDS_ATTENTION
	case svc.CampaignHealthBlocked:
		return attunev1.SurveyCampaignHealthStatus_SURVEY_CAMPAIGN_HEALTH_STATUS_BLOCKED
	default:
		return attunev1.SurveyCampaignHealthStatus_SURVEY_CAMPAIGN_HEALTH_STATUS_UNSPECIFIED
	}
}

func campaignHealthCheckStatusToProto(value string) attunev1.SurveyCampaignHealthCheckStatus {
	switch value {
	case svc.CampaignHealthCheckPass:
		return attunev1.SurveyCampaignHealthCheckStatus_SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_PASS
	case svc.CampaignHealthCheckWarn:
		return attunev1.SurveyCampaignHealthCheckStatus_SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_WARN
	case svc.CampaignHealthCheckFail:
		return attunev1.SurveyCampaignHealthCheckStatus_SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_FAIL
	default:
		return attunev1.SurveyCampaignHealthCheckStatus_SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_UNSPECIFIED
	}
}

func responseToProto(item repo.Response) *attunev1.SurveyResponse {
	out := ptrext.Of(attunev1.SurveyResponse{
		Id:           item.ID.String(),
		CampaignId:   item.CampaignID.String(),
		InvitationId: item.InvitationID.String(),
		SourceType:   item.SourceType,
		SourceId:     item.SourceID,
		Score:        int32(item.Score),
		Comment:      item.Comment,
		Locale:       item.Locale,
		LowScore:     item.Review != nil,
		SubmittedAt:  timeString(item.SubmittedAt),
	})
	if account := surveyAccountContextToProto(item.Account); account != nil {
		out.AccountContext = account
	}
	if item.RequestID != nil {
		out.RequestId = ptrext.Of(item.RequestID.String())
	}
	if item.ContactID != nil {
		out.ContactId = ptrext.Of(item.ContactID.String())
	}
	if item.Review != nil {
		out.LowScoreReview = reviewToProto(ptrext.Indirect(item.Review))
	}
	return out
}

func surveyAccountContextToProto(ctx repo.AccountContext) *attunev1.SurveyAccountContext {
	accountKey := strings.TrimSpace(ctx.AccountKey)
	if accountKey == "" {
		return nil
	}
	accountDisplay := strings.TrimSpace(ctx.AccountDisplay)
	if accountDisplay == "" {
		accountDisplay = accountKey
	}
	return ptrext.Of(attunev1.SurveyAccountContext{
		AccountKey:     accountKey,
		AccountDisplay: accountDisplay,
		Source:         strings.TrimSpace(ctx.Source),
	})
}

func reviewToProto(item repo.LowScoreReview) *attunev1.SurveyLowScoreReview {
	plan := reviewRecoveryPlan(item, time.Now().UTC())
	out := ptrext.Of(attunev1.SurveyLowScoreReview{
		ResponseId:                      item.ResponseID.String(),
		Status:                          reviewStatusToProto(item.Status),
		Severity:                        severityToProto(item.Severity),
		RootCause:                       item.RootCause,
		ActionTaken:                     item.ActionTaken,
		CustomerContacted:               item.CustomerContacted,
		UpdatedBy:                       item.UpdatedBy,
		CreatedAt:                       timeString(item.CreatedAt),
		UpdatedAt:                       timeString(item.UpdatedAt),
		DueAt:                           optionalTimeString(item.DueAt),
		ReviewedAt:                      optionalTimeString(item.ReviewedAt),
		SlaStatus:                       recoverySLAStatusToProto(plan.SLAStatus),
		BlockerReason:                   plan.BlockerReason,
		NextBestAction:                  plan.NextBestAction,
		RiskScore:                       int32(plan.RiskScore),
		RecoveryNotificationStatus:      recoveryNotificationStatusToProto(item.RecoveryNotificationStatus),
		RecoveryNotificationReason:      item.RecoveryNotificationReason,
		RecoveryNotificationDeliveredAt: optionalTimeString(item.RecoveryNotificationDeliveredAt),
		RecoveryNotificationLastError:   item.RecoveryNotificationLastError,
	})
	if item.OwnerMemberID != nil {
		out.OwnerMemberId = item.OwnerMemberID.String()
	}
	return out
}

func assignmentResultToProto(item svc.AssignmentResult) *attunev1.AssignSurveyLowScoreReviewsResponse {
	out := ptrext.Of(attunev1.AssignSurveyLowScoreReviewsResponse{
		Reviews:   make([]*attunev1.SurveyLowScoreReview, 0, len(item.Reviews)),
		Decisions: make([]*attunev1.SurveyRecoveryAssignmentDecision, 0, len(item.Decisions)),
	})
	for _, review := range item.Reviews {
		out.Reviews = append(out.Reviews, reviewToProto(review))
	}
	for _, decision := range item.Decisions {
		out.Decisions = append(out.Decisions, assignmentDecisionToProto(decision))
	}
	return out
}

func assignmentDecisionToProto(item svc.AssignmentDecision) *attunev1.SurveyRecoveryAssignmentDecision {
	out := ptrext.Of(attunev1.SurveyRecoveryAssignmentDecision{
		ResponseId:          item.ResponseID.String(),
		OwnerMemberId:       item.OwnerMemberID.String(),
		DueAt:               timeString(item.DueAt),
		Severity:            severityToProto(item.Severity),
		Escalated:           item.Escalated,
		Reason:              item.Reason,
		WorkloadScoreBefore: int32(item.WorkloadScoreBefore),
		WorkloadScoreAfter:  int32(item.WorkloadScoreAfter),
	})
	if item.PreviousOwnerMemberID != nil {
		out.PreviousOwnerMemberId = ptrext.Of(item.PreviousOwnerMemberID.String())
	}
	return out
}

func escalationResultToProto(item svc.EscalationResult) *attunev1.EscalateSurveyLowScoreReviewsResponse {
	out := ptrext.Of(attunev1.EscalateSurveyLowScoreReviewsResponse{
		Reviews:   make([]*attunev1.SurveyLowScoreReview, 0, len(item.Reviews)),
		Decisions: make([]*attunev1.SurveyRecoveryEscalationDecision, 0, len(item.Decisions)),
	})
	for _, review := range item.Reviews {
		out.Reviews = append(out.Reviews, reviewToProto(review))
	}
	for _, decision := range item.Decisions {
		out.Decisions = append(out.Decisions, escalationDecisionToProto(decision))
	}
	return out
}

func escalationDecisionToProto(item svc.EscalationDecision) *attunev1.SurveyRecoveryEscalationDecision {
	out := ptrext.Of(attunev1.SurveyRecoveryEscalationDecision{
		ResponseId:       item.ResponseID.String(),
		PreviousSeverity: severityToProto(item.PreviousSeverity),
		Severity:         severityToProto(item.Severity),
		DueAt:            timeString(item.DueAt),
		OwnerMissing:     item.OwnerMissing,
		DueAtChanged:     item.DueAtChanged,
		Reason:           item.Reason,
		ActionTaken:      item.ActionTaken,
	})
	if item.PreviousDueAt != nil {
		out.PreviousDueAt = optionalTimeString(item.PreviousDueAt)
	}
	return out
}

type recoveryPlan struct {
	SLAStatus      string
	BlockerReason  string
	NextBestAction string
	RiskScore      int
}

const (
	recoveryActionMonitor   = "monitor_recovery"
	recoveryActionOverdue   = "resolve_overdue"
	recoveryActionAssign    = "assign_owner"
	recoveryActionDue       = "set_due_date"
	recoveryActionContact   = "contact_customer"
	recoveryActionRootCause = "capture_root_cause"
	recoveryActionRecord    = "record_action"
	recoveryActionStart     = "start_review"
	recoveryActionComplete  = "complete_review"
)

func reviewRecoveryPlan(item repo.LowScoreReview, now time.Time) recoveryPlan {
	if reviewTerminal(item.Status) {
		return recoveryPlan{
			SLAStatus:      repo.RecoverySLAClosed,
			BlockerReason:  repo.RecoveryBlockerNone,
			NextBestAction: recoveryActionMonitor,
		}
	}
	status := reviewSLAStatus(item, now)
	blocker, action := reviewBlockerAndAction(item, status)
	return recoveryPlan{
		SLAStatus:      status,
		BlockerReason:  blocker,
		NextBestAction: action,
		RiskScore:      reviewRiskScore(item, status),
	}
}

func reviewSLAStatus(item repo.LowScoreReview, now time.Time) string {
	if item.DueAt == nil {
		return repo.RecoverySLAOnTrack
	}
	dueAt := ptrext.Indirect(item.DueAt)
	if dueAt.Before(now) {
		return repo.RecoverySLAOverdue
	}
	if dueAt.Before(now.Add(24 * time.Hour)) {
		return repo.RecoverySLADueSoon
	}
	return repo.RecoverySLAOnTrack
}

func reviewBlockerAndAction(item repo.LowScoreReview, slaStatus string) (string, string) {
	switch {
	case slaStatus == repo.RecoverySLAOverdue:
		return repo.RecoveryBlockerOverdue, recoveryActionOverdue
	case item.OwnerMemberID == nil:
		return repo.RecoveryBlockerOwner, recoveryActionAssign
	case item.DueAt == nil:
		return repo.RecoveryBlockerDue, recoveryActionDue
	case !item.CustomerContacted:
		return repo.RecoveryBlockerContact, recoveryActionContact
	case strings.TrimSpace(item.RootCause) == "":
		return repo.RecoveryBlockerRootCause, recoveryActionRootCause
	case strings.TrimSpace(item.ActionTaken) == "":
		return repo.RecoveryBlockerAction, recoveryActionRecord
	case item.Status == repo.ReviewOpen:
		return repo.RecoveryBlockerNone, recoveryActionStart
	default:
		return repo.RecoveryBlockerNone, recoveryActionComplete
	}
}

func reviewRiskScore(item repo.LowScoreReview, slaStatus string) int {
	score := 0
	if slaStatus == repo.RecoverySLAOverdue {
		score += 30
	}
	if slaStatus == repo.RecoverySLADueSoon {
		score += 12
	}
	score += reviewSeverityRisk(item.Severity)
	if item.OwnerMemberID == nil {
		score += 20
	}
	if item.DueAt == nil {
		score += 10
	}
	if !item.CustomerContacted {
		score += 15
	}
	if strings.TrimSpace(item.RootCause) == "" {
		score += 10
	}
	if strings.TrimSpace(item.ActionTaken) == "" {
		score += 5
	}
	if score > 100 {
		return 100
	}
	return score
}

func reviewSeverityRisk(severity string) int {
	switch severity {
	case repo.SeverityCritical:
		return 25
	case repo.SeverityHigh:
		return 15
	case repo.SeverityMedium:
		return 8
	default:
		return 0
	}
}

func reviewTerminal(status string) bool {
	return status == repo.ReviewResolved || status == repo.ReviewDismissed
}

func recoverySLAStatusToProto(value string) attunev1.SurveyRecoverySlaStatus {
	switch value {
	case repo.RecoverySLAOnTrack:
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_ON_TRACK
	case repo.RecoverySLADueSoon:
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_DUE_SOON
	case repo.RecoverySLAOverdue:
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_OVERDUE
	case repo.RecoverySLAClosed:
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_CLOSED
	default:
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_UNSPECIFIED
	}
}

func analyticsToProto(item repo.Analytics) *attunev1.SurveyAnalytics {
	out := ptrext.Of(attunev1.SurveyAnalytics{
		InvitationCount:            int32(item.InvitationCount),
		DeliveredCount:             int32(item.DeliveredCount),
		SuppressedCount:            int32(item.SuppressedCount),
		NotStartedCount:            int32(item.NotStartedCount),
		OpenedCount:                int32(item.OpenedCount),
		ExpiredCount:               int32(item.ExpiredCount),
		PendingDeliveryCount:       int32(item.PendingDeliveryCount),
		DelayedDeliveryCount:       int32(item.DelayedDeliveryCount),
		RejectedDeliveryCount:      int32(item.RejectedDeliveryCount),
		CompletedCount:             int32(item.CompletedCount),
		LowScoreCount:              int32(item.LowScoreCount),
		PositiveScoreCount:         int32(item.PositiveScoreCount),
		OpenLowScoreReviewCount:    int32(item.OpenLowScoreReviewCount),
		OverdueLowScoreReviewCount: int32(item.OverdueLowScoreReviewCount),
		UnassignedLowScoreReviewCount: int32(
			item.UnassignedLowScoreReviewCount,
		),
		CriticalLowScoreReviewCount: int32(item.CriticalLowScoreReviewCount),
		PendingCustomerContactReviewCount: int32(
			item.PendingCustomerContactReviewCount,
		),
		OverdueRecoveryQueueCount:          int32(item.OverdueRecoveryQueueCount),
		UnassignedRecoveryQueueCount:       int32(item.UnassignedRecoveryQueueCount),
		PendingContactRecoveryQueueCount:   int32(item.PendingContactRecoveryQueueCount),
		MissingRootCauseRecoveryQueueCount: int32(item.MissingRootCauseRecoveryQueueCount),
		MissingActionRecoveryQueueCount:    int32(item.MissingActionRecoveryQueueCount),
		AverageScore:                       item.AverageScore,
		ResponseRate:                       item.ResponseRate,
		PositiveScoreRate:                  item.PositiveScoreRate,
		AverageResponseSeconds:             item.AverageResponseSeconds,
		ScoreDistribution:                  make([]*attunev1.SurveyScoreBucket, 0, len(item.ScoreDistribution)),
		SuppressionReasonDistribution: make(
			[]*attunev1.SurveySuppressionReasonBucket,
			0,
			len(item.SuppressionReasons),
		),
		OwnerRecoveryLoads: make([]*attunev1.SurveyRecoveryOwnerLoad, 0, len(item.OwnerRecoveryLoads)),
	})
	if item.CampaignID != nil {
		out.CampaignId = ptrext.Of(item.CampaignID.String())
	}
	out.OldestOpenLowScoreReviewDueAt = optionalTimeString(item.OldestOpenLowScoreReviewDueAt)
	for _, bucket := range item.ScoreDistribution {
		out.ScoreDistribution = append(out.ScoreDistribution, ptrext.Of(attunev1.SurveyScoreBucket{
			Score: int32(bucket.Score),
			Count: int32(bucket.Count),
		}))
	}
	for _, bucket := range item.SuppressionReasons {
		out.SuppressionReasonDistribution = append(out.SuppressionReasonDistribution, ptrext.Of(
			attunev1.SurveySuppressionReasonBucket{
				Reason: bucket.Reason,
				Count:  int32(bucket.Count),
			},
		))
	}
	for _, load := range item.OwnerRecoveryLoads {
		out.OwnerRecoveryLoads = append(out.OwnerRecoveryLoads, ownerRecoveryLoadToProto(load))
	}
	return out
}

func ownerRecoveryLoadToProto(item repo.RecoveryOwnerLoad) *attunev1.SurveyRecoveryOwnerLoad {
	return ptrext.Of(attunev1.SurveyRecoveryOwnerLoad{
		OwnerMemberId:       item.OwnerMemberID.String(),
		OpenCount:           int32(item.OpenCount),
		OverdueCount:        int32(item.OverdueCount),
		DueSoonCount:        int32(item.DueSoonCount),
		CriticalCount:       int32(item.CriticalCount),
		PendingContactCount: int32(item.PendingContactCount),
		OldestOpenDueAt:     optionalTimeString(item.OldestOpenDueAt),
		WorkloadScore:       int32(item.WorkloadScore),
	})
}

func analyticsTrendToProto(items []repo.AnalyticsTrendBucket) *attunev1.GetSurveyAnalyticsTrendResponse {
	out := ptrext.Of(attunev1.GetSurveyAnalyticsTrendResponse{
		Buckets: make([]*attunev1.SurveyAnalyticsTrendBucket, 0, len(items)),
	})
	for _, item := range items {
		out.Buckets = append(out.Buckets, ptrext.Of(attunev1.SurveyAnalyticsTrendBucket{
			Date:               item.Date,
			InvitationCount:    int32(item.InvitationCount),
			DeliveredCount:     int32(item.DeliveredCount),
			SuppressedCount:    int32(item.SuppressedCount),
			CompletedCount:     int32(item.CompletedCount),
			LowScoreCount:      int32(item.LowScoreCount),
			PositiveScoreCount: int32(item.PositiveScoreCount),
			AverageScore:       item.AverageScore,
			ResponseRate:       item.ResponseRate,
			NotStartedCount:    int32(item.NotStartedCount),
			OpenedCount:        int32(item.OpenedCount),
			ExpiredCount:       int32(item.ExpiredCount),
		}))
	}
	return out
}

func analyticsSegmentsToProto(items []repo.AnalyticsSegment) *attunev1.GetSurveyAnalyticsSegmentsResponse {
	out := ptrext.Of(attunev1.GetSurveyAnalyticsSegmentsResponse{
		Segments: make([]*attunev1.SurveyAnalyticsSegment, 0, len(items)),
	})
	for _, item := range items {
		segment := ptrext.Of(attunev1.SurveyAnalyticsSegment{
			Dimension:              analyticsSegmentDimensionToProto(item.Dimension),
			Key:                    item.Key,
			Label:                  item.Label,
			InvitationCount:        int32(item.InvitationCount),
			DeliveredCount:         int32(item.DeliveredCount),
			SuppressedCount:        int32(item.SuppressedCount),
			CompletedCount:         int32(item.CompletedCount),
			LowScoreCount:          int32(item.LowScoreCount),
			PositiveScoreCount:     int32(item.PositiveScoreCount),
			ExpiredCount:           int32(item.ExpiredCount),
			AverageScore:           item.AverageScore,
			ResponseRate:           item.ResponseRate,
			LowScoreRate:           item.LowScoreRate,
			PositiveScoreRate:      item.PositiveScoreRate,
			SuppressionRate:        item.SuppressionRate,
			AverageResponseSeconds: item.AverageResponseSeconds,
			AttentionScore:         item.AttentionScore,
		})
		if item.CampaignID != nil {
			segment.CampaignId = ptrext.Of(item.CampaignID.String())
		}
		out.Segments = append(out.Segments, segment)
	}
	return out
}

func analyticsInsightsToProto(items []svc.AnalyticsInsight) *attunev1.GetSurveyAnalyticsInsightsResponse {
	out := ptrext.Of(attunev1.GetSurveyAnalyticsInsightsResponse{
		Insights: make([]*attunev1.SurveyAnalyticsInsight, 0, len(items)),
	})
	for _, item := range items {
		insight := ptrext.Of(attunev1.SurveyAnalyticsInsight{
			Id:                item.ID,
			Severity:          insightSeverityToProto(item.Severity),
			Title:             item.Title,
			Summary:           item.Summary,
			Metric:            item.Metric,
			Value:             item.Value,
			Threshold:         item.Threshold,
			SegmentDimension:  analyticsSegmentDimensionToProto(item.SegmentDimension),
			RecommendedAction: item.RecommendedAction,
			Rank:              int32(item.Rank),
		})
		if item.SegmentKey != "" {
			insight.SegmentKey = ptrext.Of(item.SegmentKey)
		}
		if item.SegmentLabel != "" {
			insight.SegmentLabel = ptrext.Of(item.SegmentLabel)
		}
		out.Insights = append(out.Insights, insight)
	}
	return out
}

func insightSeverityToProto(value string) attunev1.SurveyAnalyticsInsightSeverity {
	switch value {
	case svc.InsightSeverityInfo:
		return attunev1.SurveyAnalyticsInsightSeverity_SURVEY_ANALYTICS_INSIGHT_SEVERITY_INFO
	case svc.InsightSeverityWarning:
		return attunev1.SurveyAnalyticsInsightSeverity_SURVEY_ANALYTICS_INSIGHT_SEVERITY_WARNING
	case svc.InsightSeverityCritical:
		return attunev1.SurveyAnalyticsInsightSeverity_SURVEY_ANALYTICS_INSIGHT_SEVERITY_CRITICAL
	default:
		return attunev1.SurveyAnalyticsInsightSeverity_SURVEY_ANALYTICS_INSIGHT_SEVERITY_UNSPECIFIED
	}
}
