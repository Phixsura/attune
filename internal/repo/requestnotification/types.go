// SPDX-License-Identifier: Apache-2.0

// Package requestnotification owns close-the-loop request notification
// persistence: contacts, subscriptions, public updates, events, deliveries,
// webhook targets, and email senders.
package requestnotification

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	ChannelEmail   = "email"
	ChannelWebhook = "webhook"

	EventTypeStatusChanged = "request.status_changed"
	EventTypeShipped       = "request.shipped"
	EventTypeNeedInfo      = "request.need_info_direct"
	EventTypeModerator     = "request.moderator_response"
	EventTypeChangelog     = "changelog.post_published"

	EventStatusPending   = "pending"
	EventStatusResolving = "resolving"
	EventStatusResolved  = "resolved"
	EventStatusFailed    = "failed"
	EventStatusDead      = "dead"

	DeliveryStatusPending    = "pending"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusFailed     = "failed"
	DeliveryStatusDead       = "dead"
	DeliveryStatusSuppressed = "suppressed"

	ConsentUnknown    = "unknown"
	ConsentOptedIn    = "opted_in"
	ConsentOptedOut   = "opted_out"
	ConsentSuppressed = "suppressed"

	SubscriptionScopeRequest = "request"
	SubscriptionStatusActive = "active"

	SourceSubmitter = "submitter"
	SourceVoter     = "voter"
	SourceCommenter = "commenter"
	SourceFollower  = "follower"
	SourceManual    = "manual"
)

var (
	ErrNotFound     = errors.New("request notification not found")
	ErrInvalidInput = errors.New("request notification invalid input")
	ErrConflict     = errors.New("request notification conflict")
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

func (r *Repo) Begin(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

type Settings struct {
	TenantID                     string
	EmailEnabled                 bool
	WebhookEnabled               bool
	EnabledEventTypes            map[string]any
	StatusPolicy                 map[string]any
	DefaultConsentMode           string
	RequirePublicUpdateForStatus bool
	MaxRecipientsWithoutConfirm  int
	TenantHourlySendLimit        int
	ContactDailySendLimit        int
	UpdatedBy                    string
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}

type Contact struct {
	ID                 uuid.UUID
	TenantID           string
	SubjectKey         string
	SubjectHash        string
	DisplayName        string
	Organization       string
	EmailHash          string
	EmailPayload       []byte
	EmailVerifiedAt    *time.Time
	ConsentState       string
	ConsentSource      string
	ConsentTextVersion string
	LegalBasis         string
	ConsentedAt        *time.Time
	Locale             string
	Timezone           string
	BouncedAt          *time.Time
	ComplainedAt       *time.Time
	SuppressedAt       *time.Time
	SuppressionReason  string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Subscription struct {
	ID             uuid.UUID
	TenantID       string
	RequestID      uuid.UUID
	ContactID      uuid.UUID
	Scope          string
	Source         string
	Status         string
	CreatedBy      string
	UnsubscribedAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Subscriber struct {
	ContactID          uuid.UUID
	DisplayName        string
	Organization       string
	EmailPayload       []byte
	ConsentState       string
	SubscriptionStatus string
	Sources            []string
	CreatedAt          *time.Time
	UnsubscribedAt     *time.Time
}

type PublicRequestRef struct {
	TenantID    string
	RequestID   uuid.UUID
	PublicSlug  string
	PublicTitle string
	PublicState string
}

type RequestSummary struct {
	ID          uuid.UUID
	DisplayID   string
	Title       string
	Description string
	Status      string
}

type EventContext struct {
	TenantSlug  string
	Request     RequestSummary
	UpdateID    uuid.UUID
	UpdateTitle string
	UpdateBody  string
	UpdateKind  string
}

type WebhookTarget struct {
	ID                       uuid.UUID
	TenantID                 string
	Name                     string
	URLPayload               []byte
	URLHost                  string
	SecretPayload            []byte
	SignatureVersion         string
	EventMask                map[string]any
	IncludeRecipientIdentity bool
	Status                   string
	VerifiedAt               *time.Time
	LastTestedAt             *time.Time
	CreatedBy                string
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type Sender struct {
	ID               uuid.UUID
	TenantID         string
	FromName         string
	FromEmailHash    string
	FromEmailPayload []byte
	ReplyToHash      string
	ReplyToPayload   []byte
	Domain           string
	DKIMStatus       string
	SPFStatus        string
	DMARCStatus      string
	Provider         string
	ProviderConfig   []byte
	Status           string
	VerifiedAt       *time.Time
	CreatedBy        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type PublicUpdateInput struct {
	TenantID  string
	RequestID uuid.UUID
	Title     string
	Body      string
	Kind      string
	OldStatus string
	NewStatus string
	EventType string
	DedupeKey string
	Channels  []string
	ActorType string
	ActorID   string
	Notify    bool
}

type Event struct {
	ID                uuid.UUID
	TenantID          string
	PrimaryRequestID  *uuid.UUID
	UpdateID          *uuid.UUID
	DirectFollowupID  *uuid.UUID
	EventType         string
	AudienceScope     string
	DedupeKey         string
	OldStatus         string
	NewStatus         string
	ActorType         string
	ActorID           string
	Status            string
	Attempts          int
	RecipientSnapshot map[string]any
	CreatedAt         time.Time
}

type Delivery struct {
	ID                int64
	TenantID          string
	EventID           uuid.UUID
	SubscriptionID    *uuid.UUID
	ContactID         *uuid.UUID
	WebhookTargetID   *uuid.UUID
	Channel           string
	DestinationHash   string
	Payload           map[string]any
	SensitivePayload  []byte
	Status            string
	Attempts          int
	FailureKind       string
	HTTPStatus        int
	LastError         string
	DeadReason        string
	TraceID           string
	CreatedAt         time.Time
	DeliveredAt       *time.Time
	NextRetryAt       *time.Time
	LastManualRetryAt *time.Time
	RetriedBy         string
	ManualRetryCount  int
}

type DeliveryInput struct {
	TenantID         string
	EventID          uuid.UUID
	SubscriptionID   *uuid.UUID
	ContactID        *uuid.UUID
	WebhookTargetID  *uuid.UUID
	Channel          string
	DestinationHash  string
	Payload          map[string]any
	SensitivePayload []byte
	TraceID          string
}

type ListDeliveryFilter struct {
	TenantID  string
	Statuses  []string
	Limit     int
	BeforeID  int64
	RequestID *uuid.UUID
	Channel   string
}
