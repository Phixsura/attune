// SPDX-License-Identifier: Apache-2.0

// Package survey owns post-resolution CSAT and CES campaign, invitation,
// response, and low-score review persistence.
package survey

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	TypeCSAT = "csat"
	TypeCES  = "ces"
	TypeNPS  = "nps"

	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusArchived = "archived"

	TriggerWorkflowTransition = "workflow_transition"
	TriggerReplySent          = "reply_sent"
	TriggerManualLink         = "manual_link"
	TriggerRequestResolved    = "request_resolved"
	TriggerScheduledRun       = "scheduled_run"

	DistributionContactEmail = "contact_email"
	DistributionSourceLink   = "source_link"

	DedupeOnePerSource     = "one_per_source"
	DedupeOnePerResolution = "one_per_resolution"
	DedupeOnePerTrigger    = "one_per_trigger"
	DedupeOnePerRun        = "one_per_run"

	NPSBucketDetractor = "detractor"
	NPSBucketPassive   = "passive"
	NPSBucketPromoter  = "promoter"

	NPSRunScheduled  = "scheduled"
	NPSRunEvaluating = "evaluating"
	NPSRunCollecting = "collecting"
	NPSRunClosed     = "closed"
	NPSRunFailed     = "failed"
	NPSRunCancelled  = "cancelled"

	NPSMeasurementPending     = "pending"
	NPSMeasurementPreliminary = "preliminary"
	NPSMeasurementDirectional = "directional"
	NPSMeasurementQualified   = "qualified"
	NPSMeasurementRedacted    = "redacted"
	NPSMeasurementUnavailable = "unavailable"

	DeliveryPending       = "pending"
	DeliveryAccepted      = "accepted"
	DeliveryDelivered     = "delivered"
	DeliveryRejected      = "rejected"
	DeliveryBounced       = "bounced"
	DeliveryComplained    = "complained"
	DeliveryDelayed       = "delayed"
	DeliveryNotApplicable = "not_applicable"

	ProviderEventAccepted           = "accepted"
	ProviderEventDelivered          = "delivered"
	ProviderEventBounced            = "bounced"
	ProviderEventComplained         = "complained"
	ProviderEventRejected           = "rejected"
	ProviderEventTemporarilyDelayed = "temporarily_delayed"
	ProviderEventOpened             = "opened"

	ResponseNotStarted = "not_started"
	ResponseOpened     = "opened"
	ResponseStarted    = "started"
	ResponseCompleted  = "completed"
	ResponseExpired    = "expired"

	SuppressionNotSuppressed = "not_suppressed"
	SuppressionSuppressed    = "suppressed"

	ReviewOpen      = "open"
	ReviewInReview  = "in_review"
	ReviewResolved  = "resolved"
	ReviewDismissed = "dismissed"

	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"

	RecoverySLAOnTrack = "on_track"
	RecoverySLADueSoon = "due_soon"
	RecoverySLAOverdue = "overdue"
	RecoverySLAClosed  = "closed"

	RecoveryBlockerNone      = "none"
	RecoveryBlockerOverdue   = "overdue_sla"
	RecoveryBlockerOwner     = "owner_missing"
	RecoveryBlockerDue       = "due_missing"
	RecoveryBlockerContact   = "customer_contact_missing"
	RecoveryBlockerRootCause = "root_cause_missing"
	RecoveryBlockerAction    = "action_missing"

	RecoveryNotificationEmail      = "email"
	RecoveryNotificationPending    = "pending"
	RecoveryNotificationDelivered  = "delivered"
	RecoveryNotificationFailed     = "failed"
	RecoveryNotificationDead       = "dead"
	RecoveryNotificationSuppressed = "suppressed"

	SegmentSourceType       = "source_type"
	SegmentCampaign         = "campaign"
	SegmentDistributionMode = "distribution_mode"
	SegmentTriggerEvent     = "trigger_event"
)

var (
	ErrNotFound                   = errors.New("survey not found")
	ErrInvalidInput               = errors.New("survey invalid input")
	ErrConflict                   = errors.New("survey conflict")
	ErrInvitationExpired          = errors.New("survey invitation expired")
	ErrCampaignNotActive          = errors.New("survey campaign not active")
	ErrNPSRunCohortUnavailable    = errors.New("NPS campaign run cohort unavailable")
	ErrNPSRunNoEligibleRecipients = errors.New("NPS campaign run has no eligible recipients")
	ErrNPSArtifactIntegrity       = errors.New("NPS evidence export artifact integrity failure")
	ErrNPSArtifactExpired         = errors.New("NPS evidence export artifact expired")
)

type Repo struct {
	pool pool
}

type pool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

type Campaign struct {
	ID                            uuid.UUID
	TenantID                      string
	Name                          string
	SurveyType                    string
	Status                        string
	TriggerEvent                  string
	DistributionMode              string
	DedupePolicy                  string
	TriggerFilter                 map[string]any
	Content                       map[string]any
	Locale                        string
	ContentVersion                int
	SamplingPercent               float64
	MinDaysBetweenContact         int
	ExpiresAfterDays              int
	MaxDailyInvitations           int
	LowScoreThreshold             int
	RequireRecentCustomerActivity bool
	RecentActivityDays            int
	SuppressAutoResolved          bool
	CreatedBy                     string
	UpdatedBy                     string
	ArchivedAt                    *time.Time
	NPSSettings                   *NPSCampaignSettings
	CreatedAt                     time.Time
	UpdatedAt                     time.Time
}

type CampaignFilter struct {
	TenantID string
	Status   string
	Limit    int
}

type Invitation struct {
	ID                     uuid.UUID
	TenantID               string
	CampaignID             uuid.UUID
	RunID                  *uuid.UUID
	CampaignContentVersion int
	CampaignSnapshot       map[string]any
	DedupeKey              string
	SourceType             string
	SourceID               string
	RequestID              *uuid.UUID
	ContactID              *uuid.UUID
	DistributionMode       string
	TokenHash              string
	DeliveryStatus         string
	ResponseStatus         string
	SuppressionStatus      string
	SuppressionReason      string
	RecipientSnapshot      map[string]any
	DeliverySecret         []byte
	Provider               string
	ProviderMessageID      string
	Attempts               int
	FailureKind            string
	HTTPStatus             int
	LastError              string
	ClaimedAt              *time.Time
	ClaimedBy              string
	NextRetryAt            time.Time
	PublicURL              string
	DeliveredAt            *time.Time
	OpenedAt               *time.Time
	RespondedAt            *time.Time
	ExpiresAt              *time.Time
	CreatedBy              string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type InvitationFilter struct {
	TenantID           string
	CampaignID         *uuid.UUID
	ResponseStatus     string
	SuppressionStatus  string
	Limit              int
	IncludeTokenHashes bool
}

type ProviderEventInput struct {
	TenantID          string
	InvitationID      *uuid.UUID
	Provider          string
	ProviderEventType string
	ProviderMessageID string
	ProviderEventKey  string
	Payload           map[string]any
	OccurredAt        time.Time
}

type Response struct {
	ID              uuid.UUID
	TenantID        string
	CampaignID      uuid.UUID
	SurveyType      string
	InvitationID    uuid.UUID
	RequestID       *uuid.UUID
	ContactID       *uuid.UUID
	SourceType      string
	SourceID        string
	Score           int
	NPSBucket       string
	FollowUpConsent *bool
	FeedbackID      *int64
	Comment         string
	Locale          string
	Metadata        map[string]any
	UserAgentHash   string
	IPHash          string
	SubmittedAt     time.Time
	CreatedAt       time.Time
	Account         AccountContext
	Review          *LowScoreReview
}

type AccountContext struct {
	AccountKey     string
	AccountDisplay string
	Source         string
}

type ResponseFilter struct {
	TenantID              string
	CampaignID            *uuid.UUID
	LowScoreOnly          *bool
	SubmittedFrom         *time.Time
	SubmittedTo           *time.Time
	RecoverySLAStatus     string
	RecoveryBlockerReason string
	ReviewSeverity        string
	OwnerMemberID         *uuid.UUID
	AccountKey            string
	Limit                 int
}

type LowScoreReview struct {
	ResponseID                      uuid.UUID
	TenantID                        string
	CampaignID                      uuid.UUID
	Status                          string
	Severity                        string
	OwnerMemberID                   *uuid.UUID
	RootCause                       string
	ActionTaken                     string
	CustomerContacted               bool
	DueAt                           *time.Time
	InitialDueAt                    *time.Time
	CustomerContactedAt             *time.Time
	FirstTerminalAt                 *time.Time
	ReviewedAt                      *time.Time
	UpdatedBy                       string
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
	RecoveryNotificationStatus      string
	RecoveryNotificationReason      string
	RecoveryNotificationDeliveredAt *time.Time
	RecoveryNotificationLastError   string
}

type LowScoreReviewSeed struct {
	Severity      string
	OwnerMemberID *uuid.UUID
	DueAt         *time.Time
	UpdatedBy     string
}

type AnalyticsFilter struct {
	TenantID   string
	CampaignID *uuid.UUID
	RunID      *uuid.UUID
	From       *time.Time
	To         *time.Time
}

type AnalyticsSegmentFilter struct {
	TenantID   string
	CampaignID *uuid.UUID
	From       *time.Time
	To         *time.Time
	Dimension  string
	Limit      int
}

type Analytics struct {
	CampaignID                         *uuid.UUID
	InvitationCount                    int
	DeliveredCount                     int
	SuppressedCount                    int
	NotStartedCount                    int
	OpenedCount                        int
	StartedCount                       int
	ExpiredCount                       int
	PendingDeliveryCount               int
	DelayedDeliveryCount               int
	RejectedDeliveryCount              int
	CompletedCount                     int
	LowScoreCount                      int
	PositiveScoreCount                 int
	OpenLowScoreReviewCount            int
	OverdueLowScoreReviewCount         int
	UnassignedLowScoreReviewCount      int
	CriticalLowScoreReviewCount        int
	PendingCustomerContactReviewCount  int
	OldestOpenLowScoreReviewDueAt      *time.Time
	OverdueRecoveryQueueCount          int
	UnassignedRecoveryQueueCount       int
	PendingContactRecoveryQueueCount   int
	MissingRootCauseRecoveryQueueCount int
	MissingActionRecoveryQueueCount    int
	AverageScore                       float64
	ResponseRate                       float64
	StartRate                          float64
	CompletionRate                     float64
	PositiveScoreRate                  float64
	AverageResponseSeconds             float64
	ScoreDistribution                  []ScoreBucket
	SuppressionReasons                 []SuppressionReasonBucket
	OwnerRecoveryLoads                 []RecoveryOwnerLoad
	RecoveryOutcome                    RecoveryOutcome
	NPS                                float64
	NPSAvailable                       bool
	DetractorCount                     int
	PassiveCount                       int
	PromoterCount                      int
	RedactedResponseCount              int
	QualityFlaggedResponseCount        int
}

// RecoveryOutcome reports the explicitly recorded results of low-score
// recovery work in one analytics scope. It does not infer customer outcomes.
type RecoveryOutcome struct {
	ReviewCount                      int
	ResolvedCount                    int
	DismissedCount                   int
	CustomerContactedCount           int
	RootCauseRecordedCount           int
	ActionRecordedCount              int
	ContactedTimelinessEvidenceCount int
	ContactedOnTimeCount             int
	ContactedLateCount               int
	TerminalTimelinessEvidenceCount  int
	TerminalOnTimeCount              int
	TerminalLateCount                int
}

type RecoveryOwnerLoad struct {
	OwnerMemberID       uuid.UUID
	OpenCount           int
	OverdueCount        int
	DueSoonCount        int
	CriticalCount       int
	PendingContactCount int
	OldestOpenDueAt     *time.Time
	WorkloadScore       int
}

type AnalyticsTrendBucket struct {
	Date                        string
	InvitationCount             int
	DeliveredCount              int
	SuppressedCount             int
	CompletedCount              int
	LowScoreCount               int
	PositiveScoreCount          int
	NotStartedCount             int
	OpenedCount                 int
	StartedCount                int
	ExpiredCount                int
	AverageScore                float64
	ResponseRate                float64
	RunID                       *uuid.UUID
	NPS                         float64
	NPSAvailable                bool
	DetractorCount              int
	PassiveCount                int
	PromoterCount               int
	RedactedResponseCount       int
	QualityFlaggedResponseCount int
}

type NPSCampaignSettings struct {
	CampaignID                                uuid.UUID
	TenantID                                  string
	CohortID                                  uuid.UUID
	DetractorOwnerMemberID                    uuid.UUID
	CollectionDays                            int
	MaximumRunRecipients                      int
	MinimumCompletedResponses                 int
	MinimumResponseRatePercent                int
	RecurrenceIntervalDays                    int
	RecurrenceContactCooldownDays             int
	RecurrenceSamplingPercent                 int
	SamplePlanningConfidencePercent           int
	SamplePlanningMarginOfErrorPercent        int
	SamplePlanningExpectedResponseRatePercent int
	SampleSeed                                string
	CreatedAt                                 time.Time
	UpdatedAt                                 time.Time
}

type NPSCampaignRun struct {
	ID                                        uuid.UUID
	TenantID                                  string
	CampaignID                                uuid.UUID
	RecurrenceSourceRunID                     *uuid.UUID
	ExpectedCampaignUpdatedAt                 time.Time
	Sequence                                  int
	ClientRequestKey                          uuid.UUID
	RequestFingerprint                        string
	Status                                    string
	ScheduledAt                               time.Time
	OpenedAt                                  *time.Time
	ClosesAt                                  *time.Time
	DefinitionSnapshot                        map[string]any
	MeasurementKey                            string
	CollectionDays                            int
	MaximumRunRecipients                      int
	ContactCooldownDays                       int
	RecurrenceSamplingPercent                 int
	MinimumCompletedResponses                 int
	MinimumResponseRatePercent                int
	SamplePlanningPopulationCount             int
	SamplePlanningRequiredCompletedResponses  int
	SamplePlanningInvitationTarget            int
	SamplePlanningConfidencePercent           int
	SamplePlanningMarginOfErrorPercent        int
	SamplePlanningExpectedResponseRatePercent int
	InvitationCountBelowSamplePlanningTarget  bool
	EvaluatedCount                            int
	EligibleCount                             int
	InvitationCount                           int
	DeliveredCount                            int
	StartedCount                              int
	CompletedCount                            int
	ResponseRate                              float64
	HostedVisitRate                           float64
	CompletedResponseRate                     float64
	CompletionRate                            float64
	MeasurementReadiness                      string
	NPS                                       float64
	NPSAvailable                              bool
	DetractorCount                            int
	PassiveCount                              int
	PromoterCount                             int
	RedactedResponseCount                     int
	FailureReason                             string
	CancelledAt                               *time.Time
	CancelledBy                               string
	ClaimedAt                                 *time.Time
	ClaimedBy                                 string
	CreatedBy                                 string
	CreatedAt                                 time.Time
	UpdatedAt                                 time.Time
}

// NPSCampaignRunPage is ordered by descending sequence. NextBeforeSequence is
// zero when no older run exists.
type NPSCampaignRunPage struct {
	Runs               []NPSCampaignRun
	NextBeforeSequence int
}

type NPSAudienceCandidate struct {
	ContactID      uuid.UUID
	SubjectKey     string
	SubjectHash    string
	SubjectDisplay string
	DisplayName    string
}

type NPSAudiencePreview struct {
	EvaluatedCount   int
	EligibleCount    int
	ExcludedCount    int
	ExclusionReasons []SuppressionReasonBucket
	Candidates       []NPSAudienceCandidate
}

type AnalyticsSegment struct {
	Dimension              string
	Key                    string
	Label                  string
	CampaignID             *uuid.UUID
	InvitationCount        int
	DeliveredCount         int
	SuppressedCount        int
	CompletedCount         int
	LowScoreCount          int
	PositiveScoreCount     int
	ExpiredCount           int
	AverageScore           float64
	ResponseRate           float64
	LowScoreRate           float64
	PositiveScoreRate      float64
	SuppressionRate        float64
	AverageResponseSeconds float64
	AttentionScore         float64
}

type ScoreBucket struct {
	Score int
	Count int
}

type SuppressionReasonBucket struct {
	Reason string
	Count  int
}

type PublicSurvey struct {
	Campaign       Campaign
	Invitation     Invitation
	Response       *Response
	UnsubscribeURL string
}

type TriggerContext struct {
	TenantID       string
	FeedbackID     int64
	RequestID      *uuid.UUID
	RequestTitle   string
	RequestStatus  string
	Source         string
	SubjectKey     string
	SubjectHash    string
	SubjectDisplay string
	ContactID      *uuid.UUID
	ContactDisplay string
	ContactEmail   []byte
	CreatedAt      time.Time
	LastActivityAt time.Time
}

type RequestRecipient struct {
	ContactID      uuid.UUID
	DisplayName    string
	Organization   string
	ContactEmail   []byte
	ConsentState   string
	SubjectKey     string
	SubjectHash    string
	SubjectDisplay string
	LastActivityAt time.Time
}

type EmailSender struct {
	ID               uuid.UUID
	TenantID         string
	FromName         string
	FromEmailPayload []byte
	ReplyToPayload   []byte
	Provider         string
	ProviderConfig   []byte
}

type RecoveryOwner struct {
	ID          uuid.UUID
	TenantID    string
	DisplayName string
	Email       string
}

type RecoveryNotificationContext struct {
	TenantID        string
	ResponseID      uuid.UUID
	CampaignID      uuid.UUID
	CampaignName    string
	SurveyType      string
	RequestID       *uuid.UUID
	SourceType      string
	SourceID        string
	Score           int
	Comment         string
	FollowUpConsent *bool
	SubmittedAt     time.Time
	Owner           RecoveryOwner
	ReviewStatus    string
	Severity        string
	DueAt           *time.Time
}

type RecoveryNotificationInput struct {
	TenantID        string
	ResponseID      uuid.UUID
	OwnerMemberID   uuid.UUID
	Reason          string
	DestinationHash string
	Payload         map[string]any
}

type RecoveryNotification struct {
	ID                uuid.UUID
	TenantID          string
	ResponseID        uuid.UUID
	OwnerMemberID     *uuid.UUID
	Channel           string
	Status            string
	Reason            string
	DestinationHash   string
	Payload           map[string]any
	Provider          string
	ProviderMessageID string
	Attempts          int
	FailureKind       string
	HTTPStatus        int
	LastError         string
	ClaimedAt         *time.Time
	ClaimedBy         string
	NextRetryAt       time.Time
	DeliveredAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
