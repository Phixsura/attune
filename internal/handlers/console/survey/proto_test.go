// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/survey"
	svc "github.com/Phixsura/attune/internal/service/survey"
)

func TestReviewRecoveryPlanPrioritizesOverdueSLA(t *testing.T) {
	t.Parallel()

	dueAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	plan := reviewRecoveryPlan(repo.LowScoreReview{
		ResponseID:        uuid.New(),
		Status:            repo.ReviewOpen,
		Severity:          repo.SeverityHigh,
		CustomerContacted: false,
		DueAt:             ptrext.Of(dueAt),
	}, time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC))

	if plan.SLAStatus != repo.RecoverySLAOverdue {
		t.Fatalf("SLAStatus = %q, want %q", plan.SLAStatus, repo.RecoverySLAOverdue)
	}
	if plan.BlockerReason != repo.RecoveryBlockerOverdue || plan.NextBestAction != recoveryActionOverdue {
		t.Fatalf("plan = %#v, want overdue blocker/action", plan)
	}
	if plan.RiskScore != 95 {
		t.Fatalf("RiskScore = %d, want 95", plan.RiskScore)
	}
}

func TestReviewRecoveryPlanAssignsOwnerBeforeContact(t *testing.T) {
	t.Parallel()

	dueAt := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	plan := reviewRecoveryPlan(repo.LowScoreReview{
		ResponseID:        uuid.New(),
		Status:            repo.ReviewInReview,
		Severity:          repo.SeverityMedium,
		CustomerContacted: false,
		DueAt:             ptrext.Of(dueAt),
	}, time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC))

	if plan.SLAStatus != repo.RecoverySLAOnTrack {
		t.Fatalf("SLAStatus = %q, want %q", plan.SLAStatus, repo.RecoverySLAOnTrack)
	}
	if plan.BlockerReason != repo.RecoveryBlockerOwner || plan.NextBestAction != recoveryActionAssign {
		t.Fatalf("plan = %#v, want owner blocker/action", plan)
	}
}

func TestReviewRecoveryPlanClosesTerminalReviews(t *testing.T) {
	t.Parallel()

	plan := reviewRecoveryPlan(repo.LowScoreReview{
		ResponseID:        uuid.New(),
		Status:            repo.ReviewResolved,
		Severity:          repo.SeverityCritical,
		CustomerContacted: true,
	}, time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC))

	if plan.SLAStatus != repo.RecoverySLAClosed || plan.RiskScore != 0 {
		t.Fatalf("plan = %#v, want closed with zero risk", plan)
	}
	if plan.NextBestAction != recoveryActionMonitor {
		t.Fatalf("NextBestAction = %q, want %q", plan.NextBestAction, recoveryActionMonitor)
	}
}

func TestOwnerRecoveryLoadToProto(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	oldestDue := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	got := ownerRecoveryLoadToProto(repo.RecoveryOwnerLoad{
		OwnerMemberID:       ownerID,
		OpenCount:           5,
		OverdueCount:        2,
		DueSoonCount:        1,
		CriticalCount:       1,
		PendingContactCount: 3,
		OldestOpenDueAt:     ptrext.Of(oldestDue),
		WorkloadScore:       123,
	})

	if got.GetOwnerMemberId() != ownerID.String() || got.GetOpenCount() != 5 || got.GetWorkloadScore() != 123 {
		t.Fatalf("owner load proto = %#v, want owner/open/score", got)
	}
	if got.GetOldestOpenDueAt() != "2026-07-31T09:00:00Z" {
		t.Fatalf("OldestOpenDueAt = %q, want RFC3339 due", got.GetOldestOpenDueAt())
	}
}

func TestSurveyEnumMappingsCoverKnownAndUnknownValues(t *testing.T) {
	t.Parallel()

	requireEqual(t, surveyTypeToRepo(attunev1.SurveyType_SURVEY_TYPE_CSAT), repo.TypeCSAT)
	requireEqual(t, surveyTypeToRepo(attunev1.SurveyType_SURVEY_TYPE_CES), repo.TypeCES)
	requireEqual(t, surveyTypeToRepo(attunev1.SurveyType_SURVEY_TYPE_UNSPECIFIED), "")
	requireEqual(t, surveyTypeToProto(repo.TypeCSAT), attunev1.SurveyType_SURVEY_TYPE_CSAT)
	requireEqual(t, surveyTypeToProto(repo.TypeCES), attunev1.SurveyType_SURVEY_TYPE_CES)
	requireEqual(t, surveyTypeToProto("unexpected"), attunev1.SurveyType_SURVEY_TYPE_UNSPECIFIED)

	requireEqual(t, campaignStatusToRepo(attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_DRAFT), repo.StatusDraft)
	requireEqual(t, campaignStatusToRepo(attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE), repo.StatusActive)
	requireEqual(t, campaignStatusToRepo(attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ARCHIVED), repo.StatusArchived)
	requireEqual(t, campaignStatusToRepo(attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_UNSPECIFIED), "")
	requireEqual(t, campaignStatusToProto(repo.StatusDraft), attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_DRAFT)
	requireEqual(t, campaignStatusToProto(repo.StatusActive), attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE)
	requireEqual(t, campaignStatusToProto(repo.StatusArchived), attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ARCHIVED)
	requireEqual(t, campaignStatusToProto("unexpected"), attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_UNSPECIFIED)
}

func TestSurveyWorkflowEnumMappingsCoverKnownAndUnknownValues(t *testing.T) {
	t.Parallel()

	requireEqual(t, triggerEventToRepo(attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION), repo.TriggerWorkflowTransition)
	requireEqual(t, triggerEventToRepo(attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_REPLY_SENT), repo.TriggerReplySent)
	requireEqual(t, triggerEventToRepo(attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_MANUAL_LINK), repo.TriggerManualLink)
	requireEqual(t, triggerEventToRepo(attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_REQUEST_RESOLVED), repo.TriggerRequestResolved)
	requireEqual(t, triggerEventToRepo(attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_UNSPECIFIED), "")
	requireEqual(t, triggerEventToProto(repo.TriggerWorkflowTransition), attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION)
	requireEqual(t, triggerEventToProto(repo.TriggerReplySent), attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_REPLY_SENT)
	requireEqual(t, triggerEventToProto(repo.TriggerManualLink), attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_MANUAL_LINK)
	requireEqual(t, triggerEventToProto(repo.TriggerRequestResolved), attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_REQUEST_RESOLVED)
	requireEqual(t, triggerEventToProto("unexpected"), attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_UNSPECIFIED)
	requireEqual(t, distributionModeToRepo(attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL), repo.DistributionContactEmail)
	requireEqual(t, distributionModeToRepo(attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_SOURCE_LINK), repo.DistributionSourceLink)
	requireEqual(t, distributionModeToRepo(attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_UNSPECIFIED), "")
	requireEqual(t, distributionModeToProto(repo.DistributionContactEmail), attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL)
	requireEqual(t, distributionModeToProto(repo.DistributionSourceLink), attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_SOURCE_LINK)
	requireEqual(t, distributionModeToProto("unexpected"), attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_UNSPECIFIED)
}

func TestSurveyPolicyAndStatusEnumMappingsCoverKnownAndUnknownValues(t *testing.T) {
	t.Parallel()

	requireEqual(t, dedupePolicyToRepo(attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_SOURCE), repo.DedupeOnePerSource)
	requireEqual(t, dedupePolicyToRepo(attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION), repo.DedupeOnePerResolution)
	requireEqual(t, dedupePolicyToRepo(attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_TRIGGER), repo.DedupeOnePerTrigger)
	requireEqual(t, dedupePolicyToRepo(attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_UNSPECIFIED), "")
	requireEqual(t, dedupePolicyToProto(repo.DedupeOnePerSource), attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_SOURCE)
	requireEqual(t, dedupePolicyToProto(repo.DedupeOnePerResolution), attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION)
	requireEqual(t, dedupePolicyToProto(repo.DedupeOnePerTrigger), attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_TRIGGER)
	requireEqual(t, dedupePolicyToProto("unexpected"), attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_UNSPECIFIED)
	requireEqual(t, responseStatusToRepo(attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_COMPLETED), repo.ResponseCompleted)
	requireEqual(t, responseStatusToRepo(attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_UNSPECIFIED), "")
	requireEqual(t, responseStatusToProto(repo.ResponseNotStarted), attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_NOT_STARTED)
	requireEqual(t, responseStatusToProto(repo.ResponseOpened), attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_OPENED)
	requireEqual(t, responseStatusToProto(repo.ResponseStarted), attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_STARTED)
	requireEqual(t, responseStatusToProto(repo.ResponseCompleted), attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_COMPLETED)
	requireEqual(t, responseStatusToProto(repo.ResponseExpired), attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_EXPIRED)
	requireEqual(t, responseStatusToProto("unexpected"), attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_UNSPECIFIED)
}

func TestSurveyRecoveryEnumMappingsCoverKnownAndUnknownValues(t *testing.T) {
	t.Parallel()

	requireEqual(t, suppressionStatusToRepo(attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED), repo.SuppressionNotSuppressed)
	requireEqual(t, suppressionStatusToRepo(attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_SUPPRESSED), repo.SuppressionSuppressed)
	requireEqual(t, suppressionStatusToRepo(attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_UNSPECIFIED), "")
	requireEqual(t, suppressionStatusToProto(repo.SuppressionNotSuppressed), attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED)
	requireEqual(t, suppressionStatusToProto(repo.SuppressionSuppressed), attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_SUPPRESSED)
	requireEqual(t, suppressionStatusToProto("unexpected"), attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_UNSPECIFIED)
	requireEqual(t, reviewStatusToRepo(attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_OPEN), repo.ReviewOpen)
	requireEqual(t, reviewStatusToRepo(attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW), repo.ReviewInReview)
	requireEqual(t, reviewStatusToRepo(attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED), repo.ReviewResolved)
	requireEqual(t, reviewStatusToRepo(attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_DISMISSED), repo.ReviewDismissed)
	requireEqual(t, reviewStatusToRepo(attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_UNSPECIFIED), "")
	requireEqual(t, reviewStatusToProto(repo.ReviewOpen), attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_OPEN)
	requireEqual(t, reviewStatusToProto(repo.ReviewInReview), attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW)
	requireEqual(t, reviewStatusToProto(repo.ReviewResolved), attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED)
	requireEqual(t, reviewStatusToProto(repo.ReviewDismissed), attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_DISMISSED)
	requireEqual(t, reviewStatusToProto("unexpected"), attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_UNSPECIFIED)
}

func TestSurveyAnalyticsEnumMappingsCoverKnownAndUnknownValues(t *testing.T) {
	t.Parallel()

	requireEqual(t, analyticsSegmentDimensionToRepo(attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE), repo.SegmentSourceType)
	requireEqual(t, analyticsSegmentDimensionToRepo(attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_CAMPAIGN), repo.SegmentCampaign)
	requireEqual(t, analyticsSegmentDimensionToRepo(attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_DISTRIBUTION_MODE), repo.SegmentDistributionMode)
	requireEqual(t, analyticsSegmentDimensionToRepo(attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_TRIGGER_EVENT), repo.SegmentTriggerEvent)
	requireEqual(t, analyticsSegmentDimensionToRepo(attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_UNSPECIFIED), "")
	requireEqual(t, analyticsSegmentDimensionToProto(repo.SegmentSourceType), attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE)
	requireEqual(t, analyticsSegmentDimensionToProto(repo.SegmentCampaign), attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_CAMPAIGN)
	requireEqual(t, analyticsSegmentDimensionToProto(repo.SegmentDistributionMode), attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_DISTRIBUTION_MODE)
	requireEqual(t, analyticsSegmentDimensionToProto(repo.SegmentTriggerEvent), attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_TRIGGER_EVENT)
	requireEqual(t, analyticsSegmentDimensionToProto("unexpected"), attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_UNSPECIFIED)
	requireEqual(t, insightSeverityToProto(svc.InsightSeverityInfo), attunev1.SurveyAnalyticsInsightSeverity_SURVEY_ANALYTICS_INSIGHT_SEVERITY_INFO)
	requireEqual(t, insightSeverityToProto(svc.InsightSeverityWarning), attunev1.SurveyAnalyticsInsightSeverity_SURVEY_ANALYTICS_INSIGHT_SEVERITY_WARNING)
	requireEqual(t, insightSeverityToProto(svc.InsightSeverityCritical), attunev1.SurveyAnalyticsInsightSeverity_SURVEY_ANALYTICS_INSIGHT_SEVERITY_CRITICAL)
	requireEqual(t, insightSeverityToProto("unexpected"), attunev1.SurveyAnalyticsInsightSeverity_SURVEY_ANALYTICS_INSIGHT_SEVERITY_UNSPECIFIED)
}

func TestResponseToProtoIncludesSurveyAccountContext(t *testing.T) {
	t.Parallel()

	responseID := uuid.New()
	campaignID := uuid.New()
	invitationID := uuid.New()
	got := responseToProto(repo.Response{
		ID:           responseID,
		CampaignID:   campaignID,
		InvitationID: invitationID,
		SourceType:   "feedback",
		SourceID:     "101",
		Score:        2,
		SubmittedAt:  time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
		Account: repo.AccountContext{
			AccountKey:     " acct:acme ",
			AccountDisplay: " Acme Corp ",
			Source:         " recipient_snapshot ",
		},
	})

	if got.GetAccountContext().GetAccountKey() != "acct:acme" ||
		got.GetAccountContext().GetAccountDisplay() != "Acme Corp" ||
		got.GetAccountContext().GetSource() != "recipient_snapshot" {
		t.Fatalf("account context = %#v, want trimmed Acme context", got.GetAccountContext())
	}
}

func requireEqual[T comparable](t *testing.T, got T, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAssignmentDecisionToProto(t *testing.T) {
	t.Parallel()

	responseID := uuid.New()
	ownerID := uuid.New()
	previousOwnerID := uuid.New()
	got := assignmentDecisionToProto(svc.AssignmentDecision{
		ResponseID:            responseID,
		OwnerMemberID:         ownerID,
		PreviousOwnerMemberID: ptrext.Of(previousOwnerID),
		DueAt:                 time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
		Severity:              repo.SeverityCritical,
		Escalated:             true,
		Reason:                "critical_same_day",
		WorkloadScoreBefore:   12,
		WorkloadScoreAfter:    43,
	})

	if got.GetResponseId() != responseID.String() || got.GetOwnerMemberId() != ownerID.String() {
		t.Fatalf("assignment decision = %#v, want response/owner ids", got)
	}
	if got.GetPreviousOwnerMemberId() != previousOwnerID.String() || !got.GetEscalated() {
		t.Fatalf("assignment decision previous/escalated = %#v, want previous owner and escalation", got)
	}
	if got.GetDueAt() != "2026-07-31T09:00:00Z" || got.GetWorkloadScoreAfter() != 43 {
		t.Fatalf("assignment decision due/score = %#v, want due and workload score", got)
	}
}

func TestEscalationDecisionToProto(t *testing.T) {
	t.Parallel()

	responseID := uuid.New()
	previousDueAt := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	got := escalationDecisionToProto(svc.EscalationDecision{
		ResponseID:       responseID,
		PreviousSeverity: repo.SeverityHigh,
		Severity:         repo.SeverityCritical,
		PreviousDueAt:    ptrext.Of(previousDueAt),
		DueAt:            time.Date(2026, 7, 30, 17, 0, 0, 0, time.UTC),
		OwnerMissing:     true,
		DueAtChanged:     true,
		Reason:           repo.RecoveryBlockerOwner,
		ActionTaken:      "Escalated recovery: reason=owner_missing.",
	})

	if got.GetResponseId() != responseID.String() {
		t.Fatalf("ResponseId = %q, want %s", got.GetResponseId(), responseID)
	}
	if got.GetPreviousSeverity() != attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_HIGH ||
		got.GetSeverity() != attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_CRITICAL {
		t.Fatalf("severity = %v/%v, want high/critical", got.GetPreviousSeverity(), got.GetSeverity())
	}
	if got.GetPreviousDueAt() != "2026-07-30T09:00:00Z" || got.GetDueAt() != "2026-07-30T17:00:00Z" {
		t.Fatalf("due = %q/%q, want RFC3339 due values", got.GetPreviousDueAt(), got.GetDueAt())
	}
	if !got.GetOwnerMissing() || !got.GetDueAtChanged() || got.GetReason() != repo.RecoveryBlockerOwner {
		t.Fatalf("decision = %#v, want owner-missing due change", got)
	}
}
