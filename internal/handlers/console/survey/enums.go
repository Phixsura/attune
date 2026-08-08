// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"strings"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func surveyTypeToRepo(value attunev1.SurveyType) string {
	switch value {
	case attunev1.SurveyType_SURVEY_TYPE_CSAT:
		return repo.TypeCSAT
	case attunev1.SurveyType_SURVEY_TYPE_CES:
		return repo.TypeCES
	case attunev1.SurveyType_SURVEY_TYPE_NPS:
		return repo.TypeNPS
	default:
		return ""
	}
}

func surveyTypeToProto(value string) attunev1.SurveyType {
	switch value {
	case repo.TypeCSAT:
		return attunev1.SurveyType_SURVEY_TYPE_CSAT
	case repo.TypeCES:
		return attunev1.SurveyType_SURVEY_TYPE_CES
	case repo.TypeNPS:
		return attunev1.SurveyType_SURVEY_TYPE_NPS
	default:
		return attunev1.SurveyType_SURVEY_TYPE_UNSPECIFIED
	}
}

func campaignStatusToRepo(value attunev1.SurveyCampaignStatus) string {
	switch value {
	case attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_DRAFT:
		return repo.StatusDraft
	case attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE:
		return repo.StatusActive
	case attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ARCHIVED:
		return repo.StatusArchived
	default:
		return ""
	}
}

func campaignStatusToProto(value string) attunev1.SurveyCampaignStatus {
	switch value {
	case repo.StatusDraft:
		return attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_DRAFT
	case repo.StatusActive:
		return attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE
	case repo.StatusArchived:
		return attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ARCHIVED
	default:
		return attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_UNSPECIFIED
	}
}

func campaignStatusFromQuery(raw string) (attunev1.SurveyCampaignStatus, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.StatusDraft:
		return attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_DRAFT, true
	case repo.StatusActive:
		return attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE, true
	case repo.StatusArchived:
		return attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ARCHIVED, true
	default:
		return attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_UNSPECIFIED, false
	}
}

func triggerEventToRepo(value attunev1.SurveyTriggerEvent) string {
	switch value {
	case attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION:
		return repo.TriggerWorkflowTransition
	case attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_REPLY_SENT:
		return repo.TriggerReplySent
	case attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_MANUAL_LINK:
		return repo.TriggerManualLink
	case attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_REQUEST_RESOLVED:
		return repo.TriggerRequestResolved
	case attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_SCHEDULED_RUN:
		return repo.TriggerScheduledRun
	default:
		return ""
	}
}

func triggerEventToProto(value string) attunev1.SurveyTriggerEvent {
	switch value {
	case repo.TriggerWorkflowTransition:
		return attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION
	case repo.TriggerReplySent:
		return attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_REPLY_SENT
	case repo.TriggerManualLink:
		return attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_MANUAL_LINK
	case repo.TriggerRequestResolved:
		return attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_REQUEST_RESOLVED
	case repo.TriggerScheduledRun:
		return attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_SCHEDULED_RUN
	default:
		return attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_UNSPECIFIED
	}
}

func distributionModeToRepo(value attunev1.SurveyDistributionMode) string {
	switch value {
	case attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL:
		return repo.DistributionContactEmail
	case attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_SOURCE_LINK:
		return repo.DistributionSourceLink
	default:
		return ""
	}
}

func distributionModeToProto(value string) attunev1.SurveyDistributionMode {
	switch value {
	case repo.DistributionContactEmail:
		return attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL
	case repo.DistributionSourceLink:
		return attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_SOURCE_LINK
	default:
		return attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_UNSPECIFIED
	}
}

func dedupePolicyToRepo(value attunev1.SurveyDedupePolicy) string {
	switch value {
	case attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_SOURCE:
		return repo.DedupeOnePerSource
	case attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION:
		return repo.DedupeOnePerResolution
	case attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_TRIGGER:
		return repo.DedupeOnePerTrigger
	case attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_RUN:
		return repo.DedupeOnePerRun
	default:
		return ""
	}
}

func dedupePolicyToProto(value string) attunev1.SurveyDedupePolicy {
	switch value {
	case repo.DedupeOnePerSource:
		return attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_SOURCE
	case repo.DedupeOnePerResolution:
		return attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION
	case repo.DedupeOnePerTrigger:
		return attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_TRIGGER
	case repo.DedupeOnePerRun:
		return attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_RUN
	default:
		return attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_UNSPECIFIED
	}
}

func npsBucketToProto(value string) attunev1.NpsBucket {
	switch value {
	case repo.NPSBucketDetractor:
		return attunev1.NpsBucket_NPS_BUCKET_DETRACTOR
	case repo.NPSBucketPassive:
		return attunev1.NpsBucket_NPS_BUCKET_PASSIVE
	case repo.NPSBucketPromoter:
		return attunev1.NpsBucket_NPS_BUCKET_PROMOTER
	default:
		return attunev1.NpsBucket_NPS_BUCKET_UNSPECIFIED
	}
}

func npsCampaignRunStatusToProto(value string) attunev1.NpsCampaignRunStatus {
	switch value {
	case repo.NPSRunScheduled:
		return attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_SCHEDULED
	case repo.NPSRunEvaluating:
		return attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_EVALUATING
	case repo.NPSRunCollecting:
		return attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_COLLECTING
	case repo.NPSRunClosed:
		return attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_CLOSED
	case repo.NPSRunFailed:
		return attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_FAILED
	case repo.NPSRunCancelled:
		return attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_CANCELLED
	default:
		return attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_UNSPECIFIED
	}
}

func npsMeasurementReadinessToProto(value string) attunev1.NpsMeasurementReadiness {
	switch value {
	case repo.NPSMeasurementPending:
		return attunev1.NpsMeasurementReadiness_NPS_MEASUREMENT_READINESS_PENDING
	case repo.NPSMeasurementPreliminary:
		return attunev1.NpsMeasurementReadiness_NPS_MEASUREMENT_READINESS_PRELIMINARY
	case repo.NPSMeasurementDirectional:
		return attunev1.NpsMeasurementReadiness_NPS_MEASUREMENT_READINESS_DIRECTIONAL
	case repo.NPSMeasurementQualified:
		return attunev1.NpsMeasurementReadiness_NPS_MEASUREMENT_READINESS_QUALIFIED
	case repo.NPSMeasurementRedacted:
		return attunev1.NpsMeasurementReadiness_NPS_MEASUREMENT_READINESS_REDACTED
	case repo.NPSMeasurementUnavailable:
		return attunev1.NpsMeasurementReadiness_NPS_MEASUREMENT_READINESS_UNAVAILABLE
	default:
		return attunev1.NpsMeasurementReadiness_NPS_MEASUREMENT_READINESS_UNSPECIFIED
	}
}

func deliveryStatusToProto(value string) attunev1.SurveyDeliveryStatus {
	switch value {
	case repo.DeliveryPending:
		return attunev1.SurveyDeliveryStatus_SURVEY_DELIVERY_STATUS_PENDING
	case repo.DeliveryAccepted:
		return attunev1.SurveyDeliveryStatus_SURVEY_DELIVERY_STATUS_ACCEPTED
	case repo.DeliveryDelivered:
		return attunev1.SurveyDeliveryStatus_SURVEY_DELIVERY_STATUS_DELIVERED
	case repo.DeliveryRejected:
		return attunev1.SurveyDeliveryStatus_SURVEY_DELIVERY_STATUS_REJECTED
	case repo.DeliveryBounced:
		return attunev1.SurveyDeliveryStatus_SURVEY_DELIVERY_STATUS_BOUNCED
	case repo.DeliveryComplained:
		return attunev1.SurveyDeliveryStatus_SURVEY_DELIVERY_STATUS_COMPLAINED
	case repo.DeliveryDelayed:
		return attunev1.SurveyDeliveryStatus_SURVEY_DELIVERY_STATUS_DELAYED
	case repo.DeliveryNotApplicable:
		return attunev1.SurveyDeliveryStatus_SURVEY_DELIVERY_STATUS_NOT_APPLICABLE
	default:
		return attunev1.SurveyDeliveryStatus_SURVEY_DELIVERY_STATUS_UNSPECIFIED
	}
}

func responseStatusToRepo(value attunev1.SurveyResponseStatus) string {
	switch value {
	case attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_NOT_STARTED:
		return repo.ResponseNotStarted
	case attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_OPENED:
		return repo.ResponseOpened
	case attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_STARTED:
		return repo.ResponseStarted
	case attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_COMPLETED:
		return repo.ResponseCompleted
	case attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_EXPIRED:
		return repo.ResponseExpired
	default:
		return ""
	}
}

func responseStatusToProto(value string) attunev1.SurveyResponseStatus {
	switch value {
	case repo.ResponseNotStarted:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_NOT_STARTED
	case repo.ResponseOpened:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_OPENED
	case repo.ResponseStarted:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_STARTED
	case repo.ResponseCompleted:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_COMPLETED
	case repo.ResponseExpired:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_EXPIRED
	default:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_UNSPECIFIED
	}
}

func responseStatusFromQuery(raw string) (attunev1.SurveyResponseStatus, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.ResponseNotStarted:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_NOT_STARTED, true
	case repo.ResponseOpened:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_OPENED, true
	case repo.ResponseStarted:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_STARTED, true
	case repo.ResponseCompleted:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_COMPLETED, true
	case repo.ResponseExpired:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_EXPIRED, true
	default:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_UNSPECIFIED, false
	}
}

func suppressionStatusToRepo(value attunev1.SurveySuppressionStatus) string {
	switch value {
	case attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED:
		return repo.SuppressionNotSuppressed
	case attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_SUPPRESSED:
		return repo.SuppressionSuppressed
	default:
		return ""
	}
}

func recoveryNotificationStatusToProto(value string) attunev1.SurveyRecoveryNotificationStatus {
	switch value {
	case repo.RecoveryNotificationPending:
		return attunev1.SurveyRecoveryNotificationStatus_SURVEY_RECOVERY_NOTIFICATION_STATUS_PENDING
	case repo.RecoveryNotificationDelivered:
		return attunev1.SurveyRecoveryNotificationStatus_SURVEY_RECOVERY_NOTIFICATION_STATUS_DELIVERED
	case repo.RecoveryNotificationFailed:
		return attunev1.SurveyRecoveryNotificationStatus_SURVEY_RECOVERY_NOTIFICATION_STATUS_FAILED
	case repo.RecoveryNotificationDead:
		return attunev1.SurveyRecoveryNotificationStatus_SURVEY_RECOVERY_NOTIFICATION_STATUS_DEAD
	case repo.RecoveryNotificationSuppressed:
		return attunev1.SurveyRecoveryNotificationStatus_SURVEY_RECOVERY_NOTIFICATION_STATUS_SUPPRESSED
	default:
		return attunev1.SurveyRecoveryNotificationStatus_SURVEY_RECOVERY_NOTIFICATION_STATUS_UNSPECIFIED
	}
}

func suppressionStatusToProto(value string) attunev1.SurveySuppressionStatus {
	switch value {
	case repo.SuppressionNotSuppressed:
		return attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED
	case repo.SuppressionSuppressed:
		return attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_SUPPRESSED
	default:
		return attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_UNSPECIFIED
	}
}

func suppressionStatusFromQuery(raw string) (attunev1.SurveySuppressionStatus, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.SuppressionNotSuppressed:
		return attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED, true
	case repo.SuppressionSuppressed:
		return attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_SUPPRESSED, true
	default:
		return attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_UNSPECIFIED, false
	}
}

func analyticsSegmentDimensionToRepo(value attunev1.SurveyAnalyticsSegmentDimension) string {
	switch value {
	case attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE:
		return repo.SegmentSourceType
	case attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_CAMPAIGN:
		return repo.SegmentCampaign
	case attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_DISTRIBUTION_MODE:
		return repo.SegmentDistributionMode
	case attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_TRIGGER_EVENT:
		return repo.SegmentTriggerEvent
	default:
		return ""
	}
}

func analyticsSegmentDimensionToProto(value string) attunev1.SurveyAnalyticsSegmentDimension {
	switch value {
	case repo.SegmentSourceType:
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE
	case repo.SegmentCampaign:
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_CAMPAIGN
	case repo.SegmentDistributionMode:
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_DISTRIBUTION_MODE
	case repo.SegmentTriggerEvent:
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_TRIGGER_EVENT
	default:
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_UNSPECIFIED
	}
}

func analyticsSegmentDimensionFromQuery(raw string) (attunev1.SurveyAnalyticsSegmentDimension, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", repo.SegmentSourceType, "survey_analytics_segment_dimension_source_type":
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE, true
	case repo.SegmentCampaign, "survey_analytics_segment_dimension_campaign":
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_CAMPAIGN, true
	case repo.SegmentDistributionMode, "survey_analytics_segment_dimension_distribution_mode":
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_DISTRIBUTION_MODE, true
	case repo.SegmentTriggerEvent, "survey_analytics_segment_dimension_trigger_event":
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_TRIGGER_EVENT, true
	default:
		return attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_UNSPECIFIED, false
	}
}

func recoverySLAStatusToRepo(value attunev1.SurveyRecoverySlaStatus) string {
	switch value {
	case attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_ON_TRACK:
		return repo.RecoverySLAOnTrack
	case attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_DUE_SOON:
		return repo.RecoverySLADueSoon
	case attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_OVERDUE:
		return repo.RecoverySLAOverdue
	case attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_CLOSED:
		return repo.RecoverySLAClosed
	default:
		return ""
	}
}

func recoverySLAStatusFromQuery(raw string) (attunev1.SurveyRecoverySlaStatus, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.RecoverySLAOnTrack, "survey_recovery_sla_status_on_track":
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_ON_TRACK, true
	case repo.RecoverySLADueSoon, "survey_recovery_sla_status_due_soon":
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_DUE_SOON, true
	case repo.RecoverySLAOverdue, "survey_recovery_sla_status_overdue":
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_OVERDUE, true
	case repo.RecoverySLAClosed, "survey_recovery_sla_status_closed":
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_CLOSED, true
	default:
		return attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_UNSPECIFIED, false
	}
}

func recoveryBlockerReasonFromQuery(raw string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.RecoveryBlockerNone:
		return repo.RecoveryBlockerNone, true
	case repo.RecoveryBlockerOverdue:
		return repo.RecoveryBlockerOverdue, true
	case repo.RecoveryBlockerOwner:
		return repo.RecoveryBlockerOwner, true
	case repo.RecoveryBlockerDue:
		return repo.RecoveryBlockerDue, true
	case repo.RecoveryBlockerContact:
		return repo.RecoveryBlockerContact, true
	case repo.RecoveryBlockerRootCause:
		return repo.RecoveryBlockerRootCause, true
	case repo.RecoveryBlockerAction:
		return repo.RecoveryBlockerAction, true
	default:
		return "", false
	}
}

func reviewStatusToRepo(value attunev1.SurveyLowScoreReviewStatus) string {
	switch value {
	case attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_OPEN:
		return repo.ReviewOpen
	case attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW:
		return repo.ReviewInReview
	case attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED:
		return repo.ReviewResolved
	case attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_DISMISSED:
		return repo.ReviewDismissed
	default:
		return ""
	}
}

func reviewStatusToProto(value string) attunev1.SurveyLowScoreReviewStatus {
	switch value {
	case repo.ReviewOpen:
		return attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_OPEN
	case repo.ReviewInReview:
		return attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW
	case repo.ReviewResolved:
		return attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED
	case repo.ReviewDismissed:
		return attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_DISMISSED
	default:
		return attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_UNSPECIFIED
	}
}

func severityToRepo(value attunev1.SurveyLowScoreSeverity) string {
	switch value {
	case attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_LOW:
		return repo.SeverityLow
	case attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_MEDIUM:
		return repo.SeverityMedium
	case attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_HIGH:
		return repo.SeverityHigh
	case attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_CRITICAL:
		return repo.SeverityCritical
	default:
		return ""
	}
}

func severityToProto(value string) attunev1.SurveyLowScoreSeverity {
	switch value {
	case repo.SeverityLow:
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_LOW
	case repo.SeverityMedium:
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_MEDIUM
	case repo.SeverityHigh:
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_HIGH
	case repo.SeverityCritical:
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_CRITICAL
	default:
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_UNSPECIFIED
	}
}

func severityFromQuery(raw string) (attunev1.SurveyLowScoreSeverity, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.SeverityLow, "survey_low_score_severity_low":
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_LOW, true
	case repo.SeverityMedium, "survey_low_score_severity_medium":
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_MEDIUM, true
	case repo.SeverityHigh, "survey_low_score_severity_high":
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_HIGH, true
	case repo.SeverityCritical, "survey_low_score_severity_critical":
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_CRITICAL, true
	default:
		return attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_UNSPECIFIED, false
	}
}
