// SPDX-License-Identifier: Apache-2.0

// Package requestnotification coordinates close-the-loop request notification
// settings, subscriptions, public updates, event resolution, and delivery.
package requestnotification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

var (
	ErrValidation = errors.New("request notification validation failed")
	ErrNotFound   = errors.New("request notification not found")
	ErrDisabled   = errors.New("request notification disabled")
)

type SecretStore interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type Service struct {
	repo       repository
	secrets    SecretStore
	transport  *notify.Transport
	publicBase string
}

type repository interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	GetSettings(ctx context.Context, tenantID string) (repo.Settings, error)
	UpsertSettings(ctx context.Context, settings repo.Settings) (repo.Settings, error)
	UpsertContact(ctx context.Context, contact repo.Contact) (repo.Contact, error)
	GetContact(ctx context.Context, tenantID string, contactID uuid.UUID) (repo.Contact, error)
	SuppressContact(ctx context.Context, tenantID string, contactID uuid.UUID, reason string) (repo.Subscriber, error)
	SuppressContactByEmailHash(ctx context.Context, tenantID string, emailHash string, reason string, kind string) (repo.Subscriber, error)
	UpsertRequestSubscription(ctx context.Context, sub repo.Subscription) (repo.Subscription, error)
	ListSubscribers(ctx context.Context, tenantID string, requestID uuid.UUID) ([]repo.Subscriber, error)
	EligibleRequestRecipients(ctx context.Context, tenantID string, requestID uuid.UUID) ([]repo.Subscriber, error)
	ResolvePublicRequest(ctx context.Context, tenantSlug string, publicSlug string) (repo.PublicRequestRef, error)
	ResolveTenantIDBySlug(ctx context.Context, tenantSlug string) (string, error)
	GetRequestSummary(ctx context.Context, tenantID string, requestID uuid.UUID) (repo.RequestSummary, error)
	GetEventContext(ctx context.Context, eventID uuid.UUID) (repo.EventContext, error)
	CreatePublicUpdateEventTx(ctx context.Context, tx pgx.Tx, in repo.PublicUpdateInput) (repo.Event, error)
	ClaimEvents(ctx context.Context, limit int, owner string) ([]repo.Event, error)
	MarkEventResolved(ctx context.Context, id uuid.UUID, owner string, snapshot map[string]any) error
	MarkEventFailed(ctx context.Context, id uuid.UUID, owner string, errMsg string, delay time.Duration) error
	InsertDelivery(ctx context.Context, delivery repo.DeliveryInput) (int64, error)
	CountTenantEmailDeliveriesSince(ctx context.Context, tenantID string, since time.Time) (int, error)
	CountContactEmailDeliveriesSince(ctx context.Context, tenantID string, contactID uuid.UUID, since time.Time) (int, error)
	ClaimDeliveries(ctx context.Context, limit int, owner string) ([]repo.Delivery, error)
	MarkDeliveryDelivered(ctx context.Context, id int64, owner string) (int64, error)
	MarkDeliveryFailed(ctx context.Context, id int64, owner string, errMsg string, failureKind string, httpStatus int, delay time.Duration) (int64, error)
	MarkDeliveryDead(ctx context.Context, id int64, owner string, reason string, failureKind string, httpStatus int) (int64, error)
	RetryDelivery(ctx context.Context, tenantID string, id int64, actorID string) (repo.Delivery, error)
	ListDeliveries(ctx context.Context, filter repo.ListDeliveryFilter) ([]repo.Delivery, error)
	ListWebhookTargets(ctx context.Context, tenantID string) ([]repo.WebhookTarget, error)
	ListActiveWebhookTargets(ctx context.Context, tenantID string) ([]repo.WebhookTarget, error)
	GetWebhookTarget(ctx context.Context, tenantID string, id uuid.UUID) (repo.WebhookTarget, error)
	CreateWebhookTarget(ctx context.Context, target repo.WebhookTarget) (repo.WebhookTarget, error)
	UpdateWebhookTarget(ctx context.Context, target repo.WebhookTarget) (repo.WebhookTarget, error)
	DeleteWebhookTarget(ctx context.Context, tenantID string, id uuid.UUID) error
	MarkWebhookTargetTested(ctx context.Context, tenantID string, id uuid.UUID, ok bool) (repo.WebhookTarget, error)
	UpsertSender(ctx context.Context, sender repo.Sender) (repo.Sender, error)
	VerifySender(ctx context.Context, tenantID string, id uuid.UUID) (repo.Sender, error)
	ActiveSender(ctx context.Context, tenantID string) (repo.Sender, error)
	CreateUnsubscribeToken(ctx context.Context, tenantID string, contactID uuid.UUID, requestID *uuid.UUID, scope string, tokenHash string, expiresAt time.Time) error
	UseUnsubscribeToken(ctx context.Context, tenantID string, tokenHash string, userAgent string) (repo.Subscription, error)
	ConfirmContactToken(ctx context.Context, tenantID string, tokenHash string, userAgent string) (repo.Contact, error)
}

func New(r *repo.Repo, secrets SecretStore, transport *notify.Transport, publicBase string) *Service {
	return ptrext.Of(Service{
		repo:       r,
		secrets:    secrets,
		transport:  transport,
		publicBase: strings.TrimRight(strings.TrimSpace(publicBase), "/"),
	})
}

type UpdateSettingsInput struct {
	TenantID                     string
	EmailEnabled                 *bool
	WebhookEnabled               *bool
	EnabledEventTypes            map[string]any
	StatusPolicy                 map[string]any
	DefaultConsentMode           *string
	RequirePublicUpdateForStatus *bool
	MaxRecipientsWithoutConfirm  *int
	TenantHourlySendLimit        *int
	ContactDailySendLimit        *int
	ActorID                      string
}

type SubscribeInput struct {
	TenantSlug         string
	PublicSlug         string
	Email              string
	NotifyMe           bool
	ConsentTextVersion string
	DisplayName        string
	Organization       string
	Locale             string
	Timezone           string
	Source             string
	CreatedBy          string
}

type PublishInput struct {
	TenantID             string
	RequestID            uuid.UUID
	Title                string
	Body                 string
	Kind                 string
	Channels             []string
	ConfirmLargeAudience bool
	Actor                auditlogsvc.Actor
}

type WebhookTargetInput struct {
	TenantID                    string
	ID                          uuid.UUID
	Name                        string
	URL                         string
	Secret                      string
	SecretSet                   bool
	EventMask                   map[string]any
	IncludeRecipientIdentity    bool
	IncludeRecipientIdentitySet bool
	Status                      string
	ActorID                     string
}

type SenderInput struct {
	TenantID       string
	FromName       string
	FromEmail      string
	ReplyTo        string
	Provider       string
	ProviderURL    string
	ProviderSecret string
	ActorID        string
}

type ProviderSuppressionInput struct {
	TenantID          string
	Email             string
	EventType         string
	Reason            string
	Provider          string
	ProviderMessageID string
	ActorID           string
}

type ProviderConfig struct {
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"`
}

func mapRepoError(err error) error {
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, repo.ErrInvalidInput):
		return ErrValidation
	default:
		return err
	}
}

func normalizeEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(addr.Address) == "" {
		return "", ErrValidation
	}
	return strings.ToLower(strings.TrimSpace(addr.Address)), nil
}

func validateOutboundURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ErrValidation
	}
	loopbackHTTP := u.Scheme == "http" && nethardening.IsLoopbackHost(u.Hostname())
	if u.Scheme != "https" && !loopbackHTTP {
		return ErrValidation
	}
	if u.User != nil {
		return ErrValidation
	}
	return nil
}

func (s *Service) encryptString(value string) ([]byte, error) {
	if s.secrets == nil {
		return nil, errors.New("request notification secret store not configured")
	}
	return s.secrets.Encrypt([]byte(value))
}

func (s *Service) decryptString(value []byte) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	if s.secrets == nil {
		return "", errors.New("request notification secret store not configured")
	}
	plain, err := s.secrets.Decrypt(value)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func redactedEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at <= 1 {
		return email
	}
	return email[:1] + "***" + email[at:]
}

func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}
