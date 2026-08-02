// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

type deliveryTransport interface {
	Send(ctx context.Context, label string, build notify.RequestBuilder, check notify.ResponseChecker) error
}

type TestEmailInput struct {
	TenantID   string
	CampaignID uuid.UUID
	ToEmail    string
	ActorID    string
}

type TestEmailResult struct {
	OK       bool
	Provider string
	SentAt   time.Time
}

func (s *Service) SetDeliveryTransport(transport deliveryTransport) {
	s.deliveryTransport = transport
}

func (s *Service) SendTestEmail(ctx context.Context, in TestEmailInput) (TestEmailResult, error) {
	if strings.TrimSpace(in.TenantID) == "" ||
		in.CampaignID == uuid.Nil ||
		strings.TrimSpace(in.ActorID) == "" {
		return TestEmailResult{}, ErrValidation
	}
	toEmail, err := normalizeTestEmail(in.ToEmail)
	if err != nil {
		return TestEmailResult{}, err
	}
	if s.deliveryTransport == nil {
		return TestEmailResult{}, fmt.Errorf("%w: survey delivery transport not configured", notify.ErrTerminal)
	}
	campaign, err := s.repo.GetCampaign(ctx, in.TenantID, in.CampaignID)
	if err != nil {
		return TestEmailResult{}, mapRepoError(err)
	}
	if campaign.Status == repo.StatusArchived {
		return TestEmailResult{}, ErrDisabled
	}
	sender, err := s.repo.ActiveEmailSender(ctx, in.TenantID)
	if err != nil {
		return TestEmailResult{}, mapRepoError(err)
	}
	target, env, sentAt, err := s.testEmailRenderInput(campaign, sender, toEmail, in.ActorID)
	if err != nil {
		return TestEmailResult{}, err
	}
	channel := outbound.LookupNotification("email")
	if channel == nil {
		return TestEmailResult{}, fmt.Errorf("%w: survey email channel unavailable", notify.ErrTerminal)
	}
	rendered, err := channel.RenderNotification(ptrext.Of(env), target)
	if err != nil {
		return TestEmailResult{}, err
	}
	label := fmt.Sprintf("survey-test-email-%s", campaign.ID)
	if err := s.deliveryTransport.Send(ctx, label, rendered.Build, wrapSurveyNotificationCheck(rendered.Check)); err != nil {
		return TestEmailResult{}, err
	}
	return TestEmailResult{OK: true, Provider: strings.TrimSpace(sender.Provider), SentAt: sentAt}, nil
}

func (s *Service) testEmailRenderInput(
	campaign repo.Campaign,
	sender repo.EmailSender,
	toEmail string,
	actorID string,
) (outbound.Target, outbound.NotificationEnvelope, time.Time, error) {
	config, fromEmail, replyTo, err := s.emailSenderSecrets(sender)
	if err != nil {
		return outbound.Target{}, outbound.NotificationEnvelope{}, time.Time{}, err
	}
	now := s.now().UTC()
	invitationID := uuid.New()
	invitation := repo.Invitation{
		ID:                     invitationID,
		TenantID:               campaign.TenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       campaignSnapshot(campaign),
		DedupeKey:              "test-email:" + invitationID.String(),
		SourceType:             "test",
		SourceID:               "console-test",
		DistributionMode:       repo.DistributionContactEmail,
		DeliveryStatus:         repo.DeliveryNotApplicable,
		ResponseStatus:         repo.ResponseNotStarted,
		SuppressionStatus:      repo.SuppressionNotSuppressed,
		RecipientSnapshot: map[string]any{
			"display_name": "Test recipient",
			"test":         true,
		},
		ExpiresAt: ptrext.Of(now.Add(time.Duration(campaign.ExpiresAfterDays) * 24 * time.Hour)),
		CreatedBy: strings.TrimSpace(actorID),
	}
	contactID := uuid.New()
	contact := repo.RequestRecipient{
		ContactID:      contactID,
		DisplayName:    "Test recipient",
		SubjectDisplay: "Survey test email",
	}
	target := outbound.Target{
		ID:              invitationID.String(),
		TenantID:        campaign.TenantID,
		URL:             config.URL,
		Secret:          config.Secret,
		DestinationType: "email",
		Config: map[string]any{
			"from_name":  sender.FromName,
			"from_email": fromEmail,
			"reply_to":   replyTo,
			"to_email":   toEmail,
		},
	}
	env := s.surveyEnvelope(invitation, contact, s.publicSurveyURL("test-preview"), toEmail, "", "")
	markSurveyTestContent(env.Survey, actorID)
	env.DeliveryID = invitationID.String()
	return target, env, now, nil
}

func markSurveyTestContent(survey map[string]any, actorID string) {
	survey["is_test"] = true
	survey["test_actor_id"] = strings.TrimSpace(actorID)
	if title, ok := survey["title"].(string); ok && strings.TrimSpace(title) != "" {
		survey["title"] = "Test: " + strings.TrimSpace(title)
	} else {
		survey["title"] = "Test survey email"
	}
	if intro, ok := survey["intro"].(string); ok && strings.TrimSpace(intro) != "" {
		survey["intro"] = "This is a test survey email from Attune Console. " + strings.TrimSpace(intro)
	} else {
		survey["intro"] = "This is a test survey email from Attune Console."
	}
}

func normalizeTestEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(addr.Address) == "" {
		return "", ErrValidation
	}
	return strings.ToLower(strings.TrimSpace(addr.Address)), nil
}
