// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

const (
	CampaignHealthHealthy        = "healthy"
	CampaignHealthNeedsAttention = "needs_attention"
	CampaignHealthBlocked        = "blocked"

	CampaignHealthCheckPass = "pass"
	CampaignHealthCheckWarn = "warn"
	CampaignHealthCheckFail = "fail"
)

type CampaignHealth struct {
	CampaignID         uuid.UUID
	Status             string
	ReadinessScore     int
	Funnel             CampaignHealthFunnel
	Checks             []CampaignHealthCheck
	SuppressionReasons []repo.SuppressionReasonBucket
	GeneratedAt        time.Time
}

type CampaignHealthFunnel struct {
	InvitationCount            int
	PendingCount               int
	DelayedCount               int
	DeliveredCount             int
	OpenedCount                int
	StartedCount               int
	CompletedCount             int
	SuppressedCount            int
	ExpiredCount               int
	RejectedCount              int
	LowScoreCount              int
	OpenLowScoreReviewCount    int
	OverdueLowScoreReviewCount int
	DeliveryRate               float64
	OpenRate                   float64
	StartRate                  float64
	CompletionRate             float64
	ResponseRate               float64
	SuppressionRate            float64
	ExpiredRate                float64
	RecoveryOverdueRate        float64
}

type CampaignHealthCheck struct {
	ID                string
	Status            string
	Title             string
	Summary           string
	RecommendedAction string
	Evidence          string
}

func (s *Service) CampaignHealth(ctx context.Context, tenantID string, campaignID uuid.UUID) (CampaignHealth, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || campaignID == uuid.Nil {
		return CampaignHealth{}, ErrValidation
	}
	campaign, err := s.repo.GetCampaign(ctx, tenantID, campaignID)
	if err != nil {
		return CampaignHealth{}, mapRepoError(err)
	}
	analytics, err := s.repo.Analytics(ctx, repo.AnalyticsFilter{
		TenantID:   tenantID,
		CampaignID: ptrext.Of(campaignID),
	})
	if err != nil {
		return CampaignHealth{}, mapRepoError(err)
	}
	deliveryReady, deliveryBlocker, err := s.campaignDeliveryReadiness(ctx, campaign)
	if err != nil {
		return CampaignHealth{}, err
	}
	funnel := campaignHealthFunnel(analytics)
	checks := campaignHealthChecks(campaign, analytics, deliveryReady, deliveryBlocker)
	return CampaignHealth{
		CampaignID:         campaignID,
		Status:             campaignHealthStatus(checks),
		ReadinessScore:     campaignReadinessScore(checks),
		Funnel:             funnel,
		Checks:             checks,
		SuppressionReasons: analytics.SuppressionReasons,
		GeneratedAt:        s.now().UTC(),
	}, nil
}

func (s *Service) campaignDeliveryReadiness(ctx context.Context, campaign repo.Campaign) (bool, string, error) {
	if campaign.DistributionMode != repo.DistributionContactEmail {
		return true, "", nil
	}
	_, err := s.repo.ActiveEmailSender(ctx, campaign.TenantID)
	if errors.Is(mapRepoError(err), ErrNotFound) {
		return false, "email_sender_not_configured", nil
	}
	if err != nil {
		return false, "", mapRepoError(err)
	}
	if s.secrets == nil {
		return false, "delivery_secret_store_not_configured", nil
	}
	return true, "", nil
}

func campaignHealthChecks(
	campaign repo.Campaign,
	analytics repo.Analytics,
	deliveryReady bool,
	deliveryBlocker string,
) []CampaignHealthCheck {
	return []CampaignHealthCheck{
		campaignStatusHealthCheck(campaign),
		deliveryReadinessHealthCheck(campaign, deliveryReady, deliveryBlocker),
		deliveryFunnelHealthCheck(analytics),
		responseFunnelHealthCheck(analytics),
		suppressionHealthCheck(analytics),
		expiryHealthCheck(analytics),
		recoveryHealthCheck(analytics),
	}
}

func campaignStatusHealthCheck(campaign repo.Campaign) CampaignHealthCheck {
	switch campaign.Status {
	case repo.StatusActive:
		return healthCheck("campaign-status", CampaignHealthCheckPass,
			"Campaign is active",
			"The campaign can receive trigger events and create invitations.",
			"Keep campaign ownership and content current.",
			"status=active")
	case repo.StatusDraft:
		return healthCheck("campaign-status", CampaignHealthCheckWarn,
			"Campaign is still in draft",
			"Draft campaigns do not create live survey invitations.",
			"Activate the campaign after previewing recipients and sending a test email.",
			"status=draft")
	default:
		return healthCheck("campaign-status", CampaignHealthCheckFail,
			"Campaign is archived",
			"Archived campaigns cannot produce fresh customer signal.",
			"Create or reactivate an active campaign before judging survey health.",
			"status="+strings.TrimSpace(campaign.Status))
	}
}

func deliveryReadinessHealthCheck(
	campaign repo.Campaign,
	ready bool,
	blocker string,
) CampaignHealthCheck {
	if ready {
		return healthCheck("delivery-readiness", CampaignHealthCheckPass,
			"Delivery path is configured",
			"The campaign has the required delivery path for its distribution mode.",
			"Continue monitoring provider events and delivery failures.",
			"distribution_mode="+campaign.DistributionMode)
	}
	return healthCheck("delivery-readiness", CampaignHealthCheckFail,
		"Delivery path is blocked",
		"The campaign cannot safely deliver survey invitations.",
		"Resolve the delivery blocker before activating or evaluating this campaign.",
		"blocker="+strings.TrimSpace(blocker))
}

func deliveryFunnelHealthCheck(analytics repo.Analytics) CampaignHealthCheck {
	if analytics.InvitationCount == 0 {
		return healthCheck("delivery-funnel", CampaignHealthCheckWarn,
			"No invitations have been generated",
			"The campaign has no live delivery evidence yet.",
			"Preview recipients, confirm trigger filters, and run the source workflow that should create invitations.",
			"invitation_count=0")
	}
	blocked := analytics.DelayedDeliveryCount + analytics.RejectedDeliveryCount
	rate := ratio(blocked, analytics.InvitationCount)
	if analytics.RejectedDeliveryCount > 0 || rate >= 0.25 {
		return healthCheck("delivery-funnel", CampaignHealthCheckFail,
			"Delivery failures are blocking measurement",
			"Rejected or delayed invitations are preventing survey signal from reaching customers.",
			"Inspect provider errors and retry only after sender configuration is healthy.",
			fmt.Sprintf("delayed=%d rejected=%d invitations=%d", analytics.DelayedDeliveryCount, analytics.RejectedDeliveryCount, analytics.InvitationCount))
	}
	if analytics.PendingDeliveryCount > 0 || analytics.DelayedDeliveryCount > 0 {
		return healthCheck("delivery-funnel", CampaignHealthCheckWarn,
			"Delivery backlog needs attention",
			"Some survey invitations have not reached a final delivery state.",
			"Check the survey worker and provider event stream before increasing volume.",
			fmt.Sprintf("pending=%d delayed=%d invitations=%d", analytics.PendingDeliveryCount, analytics.DelayedDeliveryCount, analytics.InvitationCount))
	}
	return healthCheck("delivery-funnel", CampaignHealthCheckPass,
		"Delivery funnel is clear",
		"Generated invitations are not accumulating delivery backlog.",
		"Keep provider webhooks enabled so delivery outcomes remain current.",
		fmt.Sprintf("delivered=%d invitations=%d", analytics.DeliveredCount, analytics.InvitationCount))
}

func responseFunnelHealthCheck(analytics repo.Analytics) CampaignHealthCheck {
	if analytics.InvitationCount < 5 {
		return healthCheck("response-funnel", CampaignHealthCheckWarn,
			"Response sample is not yet meaningful",
			"The campaign needs more invitations before response rate is reliable.",
			"Use hosted-link or test-send validation, then wait for a larger live sample.",
			fmt.Sprintf("invitations=%d completed=%d", analytics.InvitationCount, analytics.CompletedCount))
	}
	if analytics.ResponseRate < 0.1 {
		return healthCheck("response-funnel", CampaignHealthCheckWarn,
			"Response rate is weak",
			"Customers are not converting from invitation to completed survey often enough.",
			"Review timing, copy, placement, and whether the survey is sent after a meaningful resolution.",
			fmt.Sprintf("response_rate=%.2f completed=%d invitations=%d", analytics.ResponseRate, analytics.CompletedCount, analytics.InvitationCount))
	}
	return healthCheck("response-funnel", CampaignHealthCheckPass,
		"Response funnel has usable signal",
		"Completed responses are clearing the minimum reliability threshold.",
		"Segment response quality before changing global campaign settings.",
		fmt.Sprintf("response_rate=%.2f completed=%d invitations=%d", analytics.ResponseRate, analytics.CompletedCount, analytics.InvitationCount))
}

func suppressionHealthCheck(analytics repo.Analytics) CampaignHealthCheck {
	if analytics.InvitationCount == 0 || analytics.SuppressedCount == 0 {
		return healthCheck("suppression-rate", CampaignHealthCheckPass,
			"Suppression is not limiting reach",
			"Eligibility rules are not currently hiding a large share of survey opportunities.",
			"Keep suppression reasons visible when campaign volume grows.",
			fmt.Sprintf("suppressed=%d invitations=%d", analytics.SuppressedCount, analytics.InvitationCount))
	}
	rate := ratio(analytics.SuppressedCount, analytics.InvitationCount)
	status := CampaignHealthCheckPass
	title := "Suppression is within range"
	summary := "Eligibility and consent controls are allowing enough survey opportunities through."
	action := "Monitor the top suppression reasons as the campaign scales."
	if rate >= 0.5 {
		status = CampaignHealthCheckFail
		title = "Suppression rate is blocking reach"
		summary = "Most survey opportunities are being suppressed before customers can respond."
		action = "Inspect suppression reasons and tune cooldown, daily limit, or recent-activity rules."
	} else if rate >= 0.25 {
		status = CampaignHealthCheckWarn
		title = "Suppression rate is elevated"
		summary = "A meaningful share of survey opportunities is being suppressed."
		action = "Review suppression reason distribution before increasing campaign volume."
	}
	return healthCheck("suppression-rate", status, title, summary, action,
		fmt.Sprintf("suppression_rate=%.2f suppressed=%d invitations=%d", rate, analytics.SuppressedCount, analytics.InvitationCount))
}

func expiryHealthCheck(analytics repo.Analytics) CampaignHealthCheck {
	if analytics.InvitationCount < 5 || ratio(analytics.ExpiredCount, analytics.InvitationCount) < 0.2 {
		return healthCheck("expiry-rate", CampaignHealthCheckPass,
			"Expiry rate is controlled",
			"Invitations are not expiring at a rate that threatens measurement quality.",
			"Keep expiry windows aligned with the resolution moment.",
			fmt.Sprintf("expired=%d invitations=%d", analytics.ExpiredCount, analytics.InvitationCount))
	}
	return healthCheck("expiry-rate", CampaignHealthCheckWarn,
		"Invitations are expiring too often",
		"Customers are leaving too many survey invitations untouched until expiry.",
		"Adjust reminder timing, expiry length, or the point where the survey is offered.",
		fmt.Sprintf("expired_rate=%.2f expired=%d invitations=%d", ratio(analytics.ExpiredCount, analytics.InvitationCount), analytics.ExpiredCount, analytics.InvitationCount))
}

func recoveryHealthCheck(analytics repo.Analytics) CampaignHealthCheck {
	switch {
	case analytics.OverdueLowScoreReviewCount > 0:
		return healthCheck("recovery-queue", CampaignHealthCheckFail,
			"Customer recovery is overdue",
			"Low-score responses have active follow-up work past its due date.",
			"Assign owners and resolve overdue low-score reviews before treating the campaign as healthy.",
			fmt.Sprintf("overdue_reviews=%d open_reviews=%d", analytics.OverdueLowScoreReviewCount, analytics.OpenLowScoreReviewCount))
	case analytics.CriticalLowScoreReviewCount > 0 || analytics.UnassignedLowScoreReviewCount > 0:
		return healthCheck("recovery-queue", CampaignHealthCheckWarn,
			"Customer recovery needs ownership",
			"Critical or unassigned low-score reviews need active operator attention.",
			"Use low-score assignment and escalation before sending more volume.",
			fmt.Sprintf("critical_reviews=%d unassigned_reviews=%d", analytics.CriticalLowScoreReviewCount, analytics.UnassignedLowScoreReviewCount))
	default:
		return healthCheck("recovery-queue", CampaignHealthCheckPass,
			"Recovery queue is controlled",
			"Low-score follow-up work is not overdue or missing ownership.",
			"Keep root causes and recovery actions complete for future reviews.",
			fmt.Sprintf("open_reviews=%d overdue_reviews=%d", analytics.OpenLowScoreReviewCount, analytics.OverdueLowScoreReviewCount))
	}
}

func campaignHealthFunnel(analytics repo.Analytics) CampaignHealthFunnel {
	return CampaignHealthFunnel{
		InvitationCount:            analytics.InvitationCount,
		PendingCount:               analytics.PendingDeliveryCount,
		DelayedCount:               analytics.DelayedDeliveryCount,
		DeliveredCount:             analytics.DeliveredCount,
		OpenedCount:                analytics.OpenedCount,
		StartedCount:               analytics.StartedCount,
		CompletedCount:             analytics.CompletedCount,
		SuppressedCount:            analytics.SuppressedCount,
		ExpiredCount:               analytics.ExpiredCount,
		RejectedCount:              analytics.RejectedDeliveryCount,
		LowScoreCount:              analytics.LowScoreCount,
		OpenLowScoreReviewCount:    analytics.OpenLowScoreReviewCount,
		OverdueLowScoreReviewCount: analytics.OverdueLowScoreReviewCount,
		DeliveryRate:               ratio(analytics.DeliveredCount, analytics.InvitationCount),
		OpenRate:                   ratio(analytics.OpenedCount, analytics.DeliveredCount),
		StartRate:                  analytics.StartRate,
		CompletionRate:             analytics.CompletionRate,
		ResponseRate:               analytics.ResponseRate,
		SuppressionRate:            ratio(analytics.SuppressedCount, analytics.InvitationCount),
		ExpiredRate:                ratio(analytics.ExpiredCount, analytics.InvitationCount),
		RecoveryOverdueRate:        ratio(analytics.OverdueLowScoreReviewCount, analytics.OpenLowScoreReviewCount),
	}
}

func campaignHealthStatus(checks []CampaignHealthCheck) string {
	status := CampaignHealthHealthy
	for _, check := range checks {
		if check.Status == CampaignHealthCheckFail {
			return CampaignHealthBlocked
		}
		if check.Status == CampaignHealthCheckWarn {
			status = CampaignHealthNeedsAttention
		}
	}
	return status
}

func campaignReadinessScore(checks []CampaignHealthCheck) int {
	score := 100
	for _, check := range checks {
		switch check.Status {
		case CampaignHealthCheckFail:
			score -= 25
		case CampaignHealthCheckWarn:
			score -= 10
		}
	}
	if score < 0 {
		return 0
	}
	return score
}

func healthCheck(id string, status string, title string, summary string, action string, evidence string) CampaignHealthCheck {
	return CampaignHealthCheck{
		ID:                id,
		Status:            status,
		Title:             title,
		Summary:           summary,
		RecommendedAction: action,
		Evidence:          evidence,
	}
}

func ratio(numerator int, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
