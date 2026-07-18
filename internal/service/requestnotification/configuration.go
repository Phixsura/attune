// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
)

func (s *Service) GetSettings(ctx context.Context, tenantID string) (repo.Settings, error) {
	settings, err := s.repo.GetSettings(ctx, strings.TrimSpace(tenantID))
	return settings, mapRepoError(err)
}

func (s *Service) UpdateSettings(ctx context.Context, in UpdateSettingsInput) (repo.Settings, error) {
	settings, err := s.GetSettings(ctx, in.TenantID)
	if err != nil {
		return repo.Settings{}, err
	}
	settingsPtr := ptrext.Of(settings)
	if err := applySettingsInput(settingsPtr, in); err != nil {
		return repo.Settings{}, err
	}
	settings = ptrext.Indirect(settingsPtr)
	settings.UpdatedBy = strings.TrimSpace(in.ActorID)
	out, err := s.repo.UpsertSettings(ctx, settings)
	return out, mapRepoError(err)
}

func applySettingsInput(settings *repo.Settings, in UpdateSettingsInput) error {
	if in.EmailEnabled != nil {
		settings.EmailEnabled = ptrext.Indirect(in.EmailEnabled)
	}
	if in.WebhookEnabled != nil {
		settings.WebhookEnabled = ptrext.Indirect(in.WebhookEnabled)
	}
	if in.EnabledEventTypes != nil {
		settings.EnabledEventTypes = in.EnabledEventTypes
	}
	if in.StatusPolicy != nil {
		settings.StatusPolicy = in.StatusPolicy
	}
	if in.RequirePublicUpdateForStatus != nil {
		settings.RequirePublicUpdateForStatus = ptrext.Indirect(in.RequirePublicUpdateForStatus)
	}
	if err := applyConsentMode(settings, in.DefaultConsentMode); err != nil {
		return err
	}
	return applySettingsLimits(settings, in)
}

func applyConsentMode(settings *repo.Settings, value *string) error {
	if value == nil {
		return nil
	}
	mode := strings.TrimSpace(ptrext.Indirect(value))
	if mode != "explicit_opt_in" && mode != "existing_app_consent" && mode != "disabled" {
		return ErrValidation
	}
	settings.DefaultConsentMode = mode
	return nil
}

func applySettingsLimits(settings *repo.Settings, in UpdateSettingsInput) error {
	value, ok, err := optionalNonNegativeInt(in.MaxRecipientsWithoutConfirm)
	if err != nil {
		return err
	}
	if ok {
		settings.MaxRecipientsWithoutConfirm = value
	}
	value, ok, err = optionalNonNegativeInt(in.TenantHourlySendLimit)
	if err != nil {
		return err
	}
	if ok {
		settings.TenantHourlySendLimit = value
	}
	value, ok, err = optionalNonNegativeInt(in.ContactDailySendLimit)
	if err != nil {
		return err
	}
	if ok {
		settings.ContactDailySendLimit = value
	}
	return nil
}

func optionalNonNegativeInt(value *int) (int, bool, error) {
	if value == nil {
		return 0, false, nil
	}
	out := ptrext.Indirect(value)
	if out < 0 {
		return 0, false, ErrValidation
	}
	return out, true, nil
}

func (s *Service) UpsertSender(ctx context.Context, in SenderInput) (repo.Sender, error) {
	fromEmail, err := normalizeEmail(in.FromEmail)
	if err != nil {
		return repo.Sender{}, err
	}
	replyTo := ""
	if strings.TrimSpace(in.ReplyTo) != "" {
		replyTo, err = normalizeEmail(in.ReplyTo)
		if err != nil {
			return repo.Sender{}, err
		}
	}
	if err := validateOutboundURL(in.ProviderURL); err != nil {
		return repo.Sender{}, err
	}
	configFields := map[string]string{"url": strings.TrimSpace(in.ProviderURL)}
	if secret := strings.TrimSpace(in.ProviderSecret); secret != "" {
		configFields["secret"] = secret
	}
	configPayload, err := s.encryptString(jsonStringObject(configFields))
	if err != nil {
		return repo.Sender{}, err
	}
	fromPayload, err := s.encryptString(fromEmail)
	if err != nil {
		return repo.Sender{}, err
	}
	var replyPayload []byte
	if replyTo != "" {
		replyPayload, err = s.encryptString(replyTo)
		if err != nil {
			return repo.Sender{}, err
		}
	}
	out, err := s.repo.UpsertSender(ctx, repo.Sender{
		TenantID:         strings.TrimSpace(in.TenantID),
		FromName:         strings.TrimSpace(in.FromName),
		FromEmailHash:    repo.EmailHash(fromEmail),
		FromEmailPayload: fromPayload,
		ReplyToHash:      repo.EmailHash(replyTo),
		ReplyToPayload:   replyPayload,
		Domain:           emailDomain(fromEmail),
		Provider:         strings.TrimSpace(in.Provider),
		ProviderConfig:   configPayload,
		CreatedBy:        strings.TrimSpace(in.ActorID),
	})
	return out, mapRepoError(err)
}

func (s *Service) GetSender(ctx context.Context, tenantID string) (repo.Sender, error) {
	out, err := s.repo.LatestSender(ctx, strings.TrimSpace(tenantID))
	return out, mapRepoError(err)
}

func (s *Service) VerifySender(ctx context.Context, tenantID string, id uuid.UUID) (repo.Sender, error) {
	out, err := s.repo.VerifySender(ctx, strings.TrimSpace(tenantID), id)
	return out, mapRepoError(err)
}

func (s *Service) RedactedEmailPayload(payload []byte) string {
	email, err := s.decryptString(payload)
	if err != nil {
		return ""
	}
	return redactedEmail(email)
}

func (s *Service) WebhookTargetURL(target repo.WebhookTarget) string {
	value, err := s.decryptString(target.URLPayload)
	if err != nil {
		return ""
	}
	return value
}

func (s *Service) ListWebhookTargets(ctx context.Context, tenantID string) ([]repo.WebhookTarget, error) {
	items, err := s.repo.ListWebhookTargets(ctx, strings.TrimSpace(tenantID))
	return items, mapRepoError(err)
}

func (s *Service) CreateWebhookTarget(ctx context.Context, in WebhookTargetInput) (repo.WebhookTarget, error) {
	if err := validateOutboundURL(in.URL); err != nil {
		return repo.WebhookTarget{}, err
	}
	urlPayload, err := s.encryptString(strings.TrimSpace(in.URL))
	if err != nil {
		return repo.WebhookTarget{}, err
	}
	var secretPayload []byte
	if strings.TrimSpace(in.Secret) != "" {
		secretPayload, err = s.encryptString(strings.TrimSpace(in.Secret))
		if err != nil {
			return repo.WebhookTarget{}, err
		}
	}
	out, err := s.repo.CreateWebhookTarget(ctx, repo.WebhookTarget{
		TenantID:                 strings.TrimSpace(in.TenantID),
		Name:                     strings.TrimSpace(in.Name),
		URLPayload:               urlPayload,
		URLHost:                  repo.URLHost(in.URL),
		SecretPayload:            secretPayload,
		EventMask:                in.EventMask,
		IncludeRecipientIdentity: in.IncludeRecipientIdentity,
		CreatedBy:                strings.TrimSpace(in.ActorID),
	})
	return out, mapRepoError(err)
}

func (s *Service) UpdateWebhookTarget(ctx context.Context, in WebhookTargetInput) (repo.WebhookTarget, error) {
	current, err := s.repo.GetWebhookTarget(ctx, strings.TrimSpace(in.TenantID), in.ID)
	if err != nil {
		return repo.WebhookTarget{}, mapRepoError(err)
	}
	if strings.TrimSpace(in.Name) != "" {
		current.Name = strings.TrimSpace(in.Name)
	}
	if strings.TrimSpace(in.URL) != "" {
		if err := validateOutboundURL(in.URL); err != nil {
			return repo.WebhookTarget{}, err
		}
		current.URLPayload, err = s.encryptString(strings.TrimSpace(in.URL))
		if err != nil {
			return repo.WebhookTarget{}, err
		}
		current.URLHost = repo.URLHost(in.URL)
	}
	if in.SecretSet {
		current.SecretPayload = nil
		if strings.TrimSpace(in.Secret) != "" {
			current.SecretPayload, err = s.encryptString(strings.TrimSpace(in.Secret))
			if err != nil {
				return repo.WebhookTarget{}, err
			}
		}
	}
	if in.EventMask != nil {
		current.EventMask = in.EventMask
	}
	if in.IncludeRecipientIdentitySet {
		current.IncludeRecipientIdentity = in.IncludeRecipientIdentity
	}
	if strings.TrimSpace(in.Status) != "" {
		current.Status = strings.TrimSpace(in.Status)
	}
	out, err := s.repo.UpdateWebhookTarget(ctx, current)
	return out, mapRepoError(err)
}

func (s *Service) DeleteWebhookTarget(ctx context.Context, tenantID string, id uuid.UUID) error {
	return mapRepoError(s.repo.DeleteWebhookTarget(ctx, strings.TrimSpace(tenantID), id))
}

type WebhookTestResult struct {
	OK         bool
	StatusCode int
	LatencyMs  int64
	Message    string
}

func (s *Service) TestWebhookTarget(ctx context.Context, tenantID string, id uuid.UUID) (WebhookTestResult, error) {
	target, err := s.repo.GetWebhookTarget(ctx, strings.TrimSpace(tenantID), id)
	if err != nil {
		return WebhookTestResult{}, mapRepoError(err)
	}
	urlValue, err := s.decryptString(target.URLPayload)
	if err != nil {
		return WebhookTestResult{}, err
	}
	secretValue, err := s.decryptString(target.SecretPayload)
	if err != nil {
		return WebhookTestResult{}, err
	}
	channel := outbound.LookupNotification("raw-webhook")
	if channel == nil {
		return WebhookTestResult{}, fmt.Errorf("%w: raw-webhook notification channel unavailable", ErrValidation)
	}
	env := ptrext.Of(outbound.NotificationEnvelope{
		Version:   "1",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		EventID:   uuid.NewString(),
		EventType: repo.EventTypeStatusChanged,
		TenantID:  strings.TrimSpace(tenantID),
		Request: map[string]any{
			"id":    "test",
			"title": "Test notification",
			"state": "open",
		},
		Update: map[string]any{
			"title": "Test notification",
			"body":  "Connectivity test from Attune.",
			"kind":  "status_change",
		},
	})
	rendered, err := channel.RenderNotification(env, outbound.Target{
		ID:               target.ID.String(),
		TenantID:         target.TenantID,
		URL:              urlValue,
		Secret:           secretValue,
		SignatureVersion: outbound.SignatureVersionBytes,
		DestinationType:  "raw-webhook",
	})
	if err != nil {
		return WebhookTestResult{}, err
	}
	start := time.Now()
	err = s.transport.Send(ctx, "request-notification-webhook-test", rendered.Build, wrapNotificationCheck(rendered.Check))
	latency := time.Since(start).Milliseconds()
	if err != nil {
		_, _ = s.repo.MarkWebhookTargetTested(ctx, tenantID, id, false)
		return WebhookTestResult{OK: false, LatencyMs: latency, Message: err.Error()}, nil
	}
	_, _ = s.repo.MarkWebhookTargetTested(ctx, tenantID, id, true)
	return WebhookTestResult{OK: true, StatusCode: http.StatusOK, LatencyMs: latency, Message: "ok"}, nil
}

func (s *Service) SubscribePublicRequest(ctx context.Context, in SubscribeInput) (repo.Subscription, error) {
	if !in.NotifyMe {
		return repo.Subscription{}, ErrDisabled
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return repo.Subscription{}, err
	}
	ref, err := s.repo.ResolvePublicRequest(ctx, strings.TrimSpace(in.TenantSlug), strings.TrimSpace(in.PublicSlug))
	if err != nil {
		return repo.Subscription{}, mapRepoError(err)
	}
	payload, err := s.encryptString(email)
	if err != nil {
		return repo.Subscription{}, err
	}
	contact, err := s.repo.UpsertContact(ctx, repo.Contact{
		TenantID:           ref.TenantID,
		DisplayName:        strings.TrimSpace(in.DisplayName),
		Organization:       strings.TrimSpace(in.Organization),
		EmailHash:          repo.EmailHash(email),
		EmailPayload:       payload,
		ConsentState:       repo.ConsentOptedIn,
		ConsentSource:      "portal",
		ConsentTextVersion: strings.TrimSpace(in.ConsentTextVersion),
		LegalBasis:         "consent",
		Locale:             strings.TrimSpace(in.Locale),
		Timezone:           strings.TrimSpace(in.Timezone),
	})
	if err != nil {
		return repo.Subscription{}, mapRepoError(err)
	}
	sub, err := s.repo.UpsertRequestSubscription(ctx, repo.Subscription{
		TenantID:  ref.TenantID,
		RequestID: ref.RequestID,
		ContactID: contact.ID,
		Source:    defaultSource(in.Source),
		CreatedBy: defaultActor(in.CreatedBy),
	})
	return sub, mapRepoError(err)
}

func (s *Service) Unsubscribe(ctx context.Context, tenantSlug string, token string, userAgent string) (repo.Subscription, error) {
	tenantID, err := s.repo.ResolveTenantIDBySlug(ctx, strings.TrimSpace(tenantSlug))
	if err != nil {
		return repo.Subscription{}, mapRepoError(err)
	}
	sub, err := s.repo.UseUnsubscribeToken(ctx, tenantID, tokenHash(strings.TrimSpace(token)), strings.TrimSpace(userAgent))
	return sub, mapRepoError(err)
}

func (s *Service) ConfirmContact(ctx context.Context, tenantSlug string, token string, userAgent string) (repo.Contact, error) {
	tenantID, err := s.repo.ResolveTenantIDBySlug(ctx, strings.TrimSpace(tenantSlug))
	if err != nil {
		return repo.Contact{}, mapRepoError(err)
	}
	contact, err := s.repo.ConfirmContactToken(ctx, tenantID, tokenHash(strings.TrimSpace(token)), strings.TrimSpace(userAgent))
	return contact, mapRepoError(err)
}

func defaultSource(source string) string {
	switch strings.TrimSpace(source) {
	case repo.SourceSubmitter, repo.SourceVoter, repo.SourceCommenter, repo.SourceFollower, repo.SourceManual:
		return strings.TrimSpace(source)
	default:
		return repo.SourceFollower
	}
}

func defaultActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "portal"
	}
	return actor
}
