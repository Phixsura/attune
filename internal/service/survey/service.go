// SPDX-License-Identifier: Apache-2.0

// Package survey coordinates post-resolution CSAT and CES campaigns, hosted
// links, public submission, analytics, and low-score review updates.
package survey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

var randomRead = rand.Read

var (
	ErrValidation       = errors.New("survey validation failed")
	ErrNotFound         = errors.New("survey not found")
	ErrConflict         = errors.New("survey conflict")
	ErrDisabled         = errors.New("survey disabled")
	ErrExpired          = errors.New("survey expired")
	ErrFingerprinting   = errors.New("survey public fingerprinting unavailable")
	ErrWebhookSignature = errors.New("survey provider webhook signature failed")
)

const (
	InsightSeverityInfo     = "info"
	InsightSeverityWarning  = "warning"
	InsightSeverityCritical = "critical"
)

type Service struct {
	repo              repository
	secrets           SecretStore
	deliveryTransport deliveryTransport
	feedbackWriter    npsFeedbackWriter
	publicBase        string
	now               func() time.Time
}

type SecretStore interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type pseudonymizer interface {
	Pseudonymize(purpose, raw string) (string, error)
}

// PublicResponseFingerprints are opaque, keyed values safe to persist with a
// public-survey response. They never contain raw request metadata.
type PublicResponseFingerprints struct {
	UserAgentHash string
	IPHash        string
	QualityFlags  []string
}

const (
	publicSurveyUserAgentFingerprintPurpose = "attune:survey:public-response:user-agent:v1"
	publicSurveyIPFingerprintPurpose        = "attune:survey:public-response:ip:v1"
	publicSurveyFingerprintTenantScope      = ":tenant:"
)

type repository interface {
	ListCampaigns(ctx context.Context, filter repo.CampaignFilter) ([]repo.Campaign, error)
	ListActiveCampaignsByTrigger(ctx context.Context, tenantID string, triggerEvent string) ([]repo.Campaign, error)
	GetCampaign(ctx context.Context, tenantID string, id uuid.UUID) (repo.Campaign, error)
	CreateCampaign(ctx context.Context, campaign repo.Campaign) (repo.Campaign, error)
	UpdateCampaign(ctx context.Context, campaign repo.Campaign) (repo.Campaign, error)
	ArchiveCampaign(ctx context.Context, tenantID string, id uuid.UUID, actorID string, archivedAt time.Time) (repo.Campaign, error)
	CreateInvitation(ctx context.Context, invitation repo.Invitation) (repo.Invitation, error)
	CreateInvitationWithContactCooldown(ctx context.Context, invitation repo.Invitation, cooldownSince *time.Time) (repo.Invitation, string, error)
	InvitationExistsByDedupeKey(ctx context.Context, tenantID string, campaignID uuid.UUID, dedupeKey string) (bool, error)
	FeedbackTriggerContext(ctx context.Context, tenantID string, feedbackID int64) (repo.TriggerContext, error)
	RequestRecipients(ctx context.Context, tenantID string, requestID uuid.UUID) ([]repo.RequestRecipient, error)
	CountCampaignInvitationsSince(ctx context.Context, tenantID string, campaignID uuid.UUID, since time.Time) (int, error)
	CountContactInvitationsSince(ctx context.Context, tenantID string, contactID uuid.UUID, since time.Time) (int, error)
	EmailSender(ctx context.Context, tenantID string, id uuid.UUID) (repo.EmailSender, error)
	ActiveEmailSender(ctx context.Context, tenantID string) (repo.EmailSender, error)
	TenantSlug(ctx context.Context, tenantID string) (string, error)
	CreateTenantUnsubscribeToken(ctx context.Context, tenantID string, contactID uuid.UUID, tokenHash string, expiresAt time.Time) error
	PersistInvitationUnsubscribeToken(ctx context.Context, tenantID string, invitationID uuid.UUID, expectedDeliverySecret []byte, deliverySecret []byte, contactID uuid.UUID, tokenHash string, expiresAt time.Time) (repo.Invitation, bool, error)
	ClaimPendingEmailInvitations(ctx context.Context, limit int, owner string) ([]repo.Invitation, error)
	PrepareInvitationDelivery(ctx context.Context, claimed repo.Invitation, owner string) (repo.Invitation, repo.RequestRecipient, bool, error)
	MarkInvitationDelivered(ctx context.Context, tenantID string, id uuid.UUID, owner string, provider string, providerMessageID string, httpStatus int) (repo.Invitation, error)
	MarkInvitationFailed(ctx context.Context, tenantID string, id uuid.UUID, owner string, errMsg string, failureKind string, httpStatus int, delay time.Duration, terminal bool) (repo.Invitation, error)
	RetryInvitationDelivery(ctx context.Context, tenantID string, id uuid.UUID) (repo.Invitation, error)
	RecordProviderEvent(ctx context.Context, input repo.ProviderEventInput) (repo.Invitation, error)
	SuppressInvitation(ctx context.Context, tenantID string, id uuid.UUID, reason string) (repo.Invitation, error)
	ExpireInvitation(ctx context.Context, tenantID string, id uuid.UUID, reason string) (repo.Invitation, error)
	ExpireStaleInvitations(ctx context.Context, limit int, now time.Time, reason string) (int, error)
	GetInvitationByTokenHash(ctx context.Context, tokenHash string) (repo.Invitation, error)
	MarkInvitationStarted(ctx context.Context, tenantID string, id uuid.UUID) (repo.Invitation, error)
	GetResponseByInvitation(ctx context.Context, tenantID string, invitationID uuid.UUID) (repo.Response, error)
	CreateResponse(ctx context.Context, response repo.Response, review *repo.LowScoreReviewSeed) (repo.Response, error)
	ListInvitations(ctx context.Context, filter repo.InvitationFilter) ([]repo.Invitation, error)
	ListResponses(ctx context.Context, filter repo.ResponseFilter) ([]repo.Response, error)
	GetLowScoreReview(ctx context.Context, tenantID string, responseID uuid.UUID) (repo.LowScoreReview, error)
	UpdateLowScoreReview(ctx context.Context, review repo.LowScoreReview) (repo.LowScoreReview, error)
	ClaimLowScoreReviewsForRecoveryAutomation(ctx context.Context, limit int, owner string) ([]repo.LowScoreReview, error)
	RecoveryNotificationContext(ctx context.Context, tenantID string, responseID uuid.UUID) (repo.RecoveryNotificationContext, error)
	GetRecoveryOwner(ctx context.Context, tenantID string, ownerMemberID uuid.UUID) (repo.RecoveryOwner, error)
	EnsureRecoveryNotification(ctx context.Context, input repo.RecoveryNotificationInput) (repo.RecoveryNotification, bool, error)
	ClaimPendingRecoveryNotifications(ctx context.Context, limit int, owner string) ([]repo.RecoveryNotification, error)
	MarkRecoveryNotificationDelivered(ctx context.Context, tenantID string, id uuid.UUID, owner string, provider string, providerMessageID string, httpStatus int) (repo.RecoveryNotification, error)
	MarkRecoveryNotificationFailed(ctx context.Context, tenantID string, id uuid.UUID, owner string, lastError string, failureKind string, httpStatus int, delay time.Duration, dead bool) (repo.RecoveryNotification, error)
	MarkRecoveryNotificationSuppressed(ctx context.Context, tenantID string, id uuid.UUID, owner string, reason string) (repo.RecoveryNotification, error)
	Analytics(ctx context.Context, filter repo.AnalyticsFilter) (repo.Analytics, error)
	AnalyticsTrend(ctx context.Context, filter repo.AnalyticsFilter) ([]repo.AnalyticsTrendBucket, error)
	AnalyticsSegments(ctx context.Context, filter repo.AnalyticsSegmentFilter) ([]repo.AnalyticsSegment, error)
}

type npsRepository interface {
	CreateNPSCampaign(ctx context.Context, campaign repo.Campaign, settings repo.NPSCampaignSettings) (repo.Campaign, error)
	UpdateNPSCampaign(ctx context.Context, campaign repo.Campaign, settings repo.NPSCampaignSettings) (repo.Campaign, error)
	GetNPSCampaignSettings(ctx context.Context, tenantID string, campaignID uuid.UUID) (repo.NPSCampaignSettings, error)
	FindNPSCampaignRunByRequestKey(ctx context.Context, tenantID string, campaignID uuid.UUID, clientRequestKey uuid.UUID) (repo.NPSCampaignRun, error)
	FindNPSCampaignRunByRecurrenceSource(ctx context.Context, tenantID string, campaignID uuid.UUID, sourceRunID uuid.UUID) (repo.NPSCampaignRun, error)
	GetNPSCampaignRun(ctx context.Context, tenantID string, campaignID uuid.UUID, runID uuid.UUID) (repo.NPSCampaignRun, error)
	ScheduleNPSCampaignRun(ctx context.Context, run repo.NPSCampaignRun) (repo.NPSCampaignRun, bool, error)
	CancelNPSCampaignRun(ctx context.Context, tenantID string, campaignID uuid.UUID, runID uuid.UUID, actor string, now time.Time) (repo.NPSCampaignRun, bool, error)
	ListNPSCampaignRuns(ctx context.Context, tenantID string, campaignID uuid.UUID, limit int) ([]repo.NPSCampaignRun, error)
	ListNPSCampaignRunPage(ctx context.Context, tenantID string, campaignID uuid.UUID, limit int, beforeSequence int) (repo.NPSCampaignRunPage, error)
	ClaimDueNPSCampaignRuns(ctx context.Context, limit int, owner string, now time.Time) ([]repo.NPSCampaignRun, error)
	ClaimNPSCampaignRunsForRecurrence(ctx context.Context, limit int, owner string, now time.Time) ([]repo.NPSCampaignRun, error)
	MarkNPSCampaignRunRecurrenceProcessed(ctx context.Context, tenantID string, runID uuid.UUID, owner string, now time.Time) error
	NPSRunAudience(ctx context.Context, run repo.NPSCampaignRun, now time.Time) (repo.NPSAudiencePreview, error)
	MaterializeNPSCampaignRun(ctx context.Context, run repo.NPSCampaignRun, preview repo.NPSAudiencePreview, invitations []repo.Invitation, owner string, now time.Time) (repo.NPSCampaignRun, error)
	MarkNPSCampaignRunFailed(ctx context.Context, tenantID string, runID uuid.UUID, owner string, reason string, audience repo.NPSAudiencePreview) error
	CloseExpiredNPSCampaignRuns(ctx context.Context, limit int, now time.Time) (int, error)
	WithTx(ctx context.Context, fn func(pgx.Tx) error) error
	CreateResponseTx(ctx context.Context, tx pgx.Tx, response repo.Response, review *repo.LowScoreReviewSeed) (repo.Response, error)
	NPSFeedbackSubjectTx(ctx context.Context, tx pgx.Tx, tenantID string, invitationID uuid.UUID) (repo.NPSAudienceCandidate, error)
	LinkResponseFeedbackTx(ctx context.Context, tx pgx.Tx, tenantID string, responseID uuid.UUID, feedbackID int64) error
	RecoveryNotificationContextTx(ctx context.Context, tx pgx.Tx, tenantID string, responseID uuid.UUID) (repo.RecoveryNotificationContext, error)
	EnsureRecoveryNotificationTx(ctx context.Context, tx pgx.Tx, input repo.RecoveryNotificationInput) (repo.RecoveryNotification, bool, error)
}

type npsFeedbackWriter interface {
	InsertIdempotentTx(
		ctx context.Context,
		tx pgx.Tx,
		tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
		in domain.IngestInput,
		idemHash []byte,
	) (int64, bool, error)
}

func New(r *repo.Repo, publicBase string) *Service {
	return ptrext.Of(Service{
		repo:       r,
		publicBase: strings.TrimRight(strings.TrimSpace(publicBase), "/"),
		now:        time.Now,
	})
}

func (s *Service) SetSecretStore(secrets SecretStore) {
	s.secrets = secrets
}

// FingerprintPublicResponse returns tenant-scoped, domain-separated pseudonyms
// for public request metadata. A configured keyed pseudonymizer is required so
// low-entropy values such as IP addresses cannot be reversed from database
// contents or correlated across tenants.
func (s *Service) FingerprintPublicResponse(ctx context.Context, token, userAgent, clientIP string) (PublicResponseFingerprints, error) {
	store, ok := s.secrets.(pseudonymizer)
	if !ok || store == nil {
		return PublicResponseFingerprints{}, ErrFingerprinting
	}
	invitation, _, err := s.resolvePublicInvitation(ctx, token)
	if err != nil {
		return PublicResponseFingerprints{}, err
	}
	tenantID := strings.TrimSpace(invitation.TenantID)
	if tenantID == "" {
		return PublicResponseFingerprints{}, ErrFingerprinting
	}
	userAgentHash, err := store.Pseudonymize(publicSurveyFingerprintPurpose(publicSurveyUserAgentFingerprintPurpose, tenantID), userAgent)
	if err != nil {
		return PublicResponseFingerprints{}, fmt.Errorf("%w: user-agent", ErrFingerprinting)
	}
	ipHash, err := store.Pseudonymize(publicSurveyFingerprintPurpose(publicSurveyIPFingerprintPurpose, tenantID), clientIP)
	if err != nil {
		return PublicResponseFingerprints{}, fmt.Errorf("%w: client address", ErrFingerprinting)
	}
	return PublicResponseFingerprints{
		UserAgentHash: userAgentHash,
		IPHash:        ipHash,
		QualityFlags:  publicResponseQualityFlags(invitation, userAgent, clientIP, s.now().UTC()),
	}, nil
}

func publicSurveyFingerprintPurpose(base, tenantID string) string {
	return base + publicSurveyFingerprintTenantScope + tenantID
}

// SetFeedbackWriter connects NPS comments to the durable feedback pipeline.
// It deliberately accepts only the transaction-scoped write operation.
func (s *Service) SetFeedbackWriter(writer npsFeedbackWriter) {
	s.feedbackWriter = writer
}

type CampaignInput struct {
	TenantID                      string
	ID                            uuid.UUID
	Name                          *string
	SurveyType                    string
	Status                        string
	TriggerEvent                  string
	DistributionMode              string
	DedupePolicy                  string
	TriggerFilter                 map[string]any
	TriggerFilterSet              bool
	Content                       map[string]any
	ContentSet                    bool
	Locale                        *string
	SamplingPercent               *float64
	MinDaysBetweenContact         *int
	ExpiresAfterDays              *int
	MaxDailyInvitations           *int
	LowScoreThreshold             *int
	RequireRecentCustomerActivity *bool
	RecentActivityDays            *int
	SuppressAutoResolved          *bool
	NPSSettings                   *NPSCampaignSettingsInput
	NPSSettingsSet                bool
	ActorID                       string
}

type NPSCampaignSettingsInput struct {
	CohortID                                  uuid.UUID
	DetractorOwnerMemberID                    uuid.UUID
	CollectionDays                            int
	MaximumRunRecipients                      int
	MinimumCompletedResponses                 int
	MinimumResponseRatePercent                int
	SamplePlanningConfidencePercent           int
	SamplePlanningMarginOfErrorPercent        int
	SamplePlanningExpectedResponseRatePercent int
	RecurrenceIntervalDays                    *int
	RecurrenceContactCooldownDays             *int
	RecurrenceSamplingPercent                 *int
}

type HostedLinkInput struct {
	TenantID   string
	CampaignID uuid.UUID
	SourceType string
	SourceID   string
	RequestID  *uuid.UUID
	Context    map[string]any
	ActorID    string
}

type WorkflowTransitionInput struct {
	TenantID          string
	FeedbackID        int64
	FromStateID       string
	FromStateName     string
	FromStateCategory string
	ToStateID         string
	ToStateName       string
	ToStateCategory   string
	ActorID           string
	AutoResolved      bool
	AutoResolvedSet   bool
}

type ReplySentInput struct {
	TenantID          string
	FeedbackID        int64
	DraftID           string
	AttemptID         string
	RevisionID        string
	ExternalMessageID string
	ActorID           string
}

type RequestResolvedInput struct {
	TenantID  string
	RequestID uuid.UUID
	OldStatus string
	NewStatus string
	Title     string
	ActorID   string
}

type PublicSubmitInput struct {
	Token           string
	Score           int
	Comment         string
	Locale          string
	FollowUpConsent *bool
	UserAgentHash   string
	IPHash          string
	QualityFlags    []string
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

type SignedProviderEventInput struct {
	TenantID  string
	SenderID  uuid.UUID
	Timestamp string
	Signature string
	RawBody   []byte
}

type ReviewInput struct {
	TenantID          string
	ResponseID        uuid.UUID
	Status            string
	Severity          string
	OwnerMemberID     *uuid.UUID
	OwnerMemberIDSet  bool
	RootCause         *string
	ActionTaken       *string
	CustomerContacted *bool
	DueAt             *time.Time
	DueAtSet          bool
	ActorID           string
}

type BatchReviewInput struct {
	TenantID          string
	ResponseIDs       []uuid.UUID
	Status            string
	Severity          string
	OwnerMemberID     *uuid.UUID
	OwnerMemberIDSet  bool
	RootCause         *string
	ActionTaken       *string
	CustomerContacted *bool
	DueAt             *time.Time
	DueAtSet          bool
	ActorID           string
}

type AssignmentInput struct {
	TenantID                string
	ResponseIDs             []uuid.UUID
	CandidateOwnerMemberIDs []uuid.UUID
	DueInHours              int
	ActorID                 string
}

type AssignmentResult struct {
	Reviews   []repo.LowScoreReview
	Decisions []AssignmentDecision
}

type AssignmentDecision struct {
	ResponseID            uuid.UUID
	OwnerMemberID         uuid.UUID
	PreviousOwnerMemberID *uuid.UUID
	DueAt                 time.Time
	Severity              string
	Escalated             bool
	Reason                string
	WorkloadScoreBefore   int
	WorkloadScoreAfter    int
}

type EscalationInput struct {
	TenantID    string
	ResponseIDs []uuid.UUID
	DueInHours  int
	Note        string
	ActorID     string
}

type EscalationResult struct {
	Reviews   []repo.LowScoreReview
	Decisions []EscalationDecision
}

type EscalationDecision struct {
	ResponseID       uuid.UUID
	PreviousSeverity string
	Severity         string
	PreviousDueAt    *time.Time
	DueAt            time.Time
	OwnerMissing     bool
	DueAtChanged     bool
	Reason           string
	ActionTaken      string
}

type RecoveryAutomationInput struct {
	Limit      int
	Owner      string
	DueInHours int
}

type RecoveryAutomationResult struct {
	Claimed               int
	Escalated             int
	Skipped               int
	NotificationsEnqueued int
	NotificationsSkipped  int
	Reviews               []repo.LowScoreReview
	Decisions             []EscalationDecision
}

type AnalyticsInsightFilter struct {
	TenantID   string
	CampaignID *uuid.UUID
	From       *time.Time
	To         *time.Time
	Limit      int
}

type AnalyticsInsight struct {
	ID                string
	Severity          string
	Title             string
	Summary           string
	Metric            string
	Value             float64
	Threshold         float64
	SegmentKey        string
	SegmentLabel      string
	SegmentDimension  string
	RecommendedAction string
	Rank              int
}

func (s *Service) ListCampaigns(ctx context.Context, tenantID string, status string, limit int) ([]repo.Campaign, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, ErrValidation
	}
	items, err := s.repo.ListCampaigns(ctx, repo.CampaignFilter{
		TenantID: tenantID,
		Status:   strings.TrimSpace(status),
		Limit:    limit,
	})
	return items, mapRepoError(err)
}

func (s *Service) CreateCampaign(ctx context.Context, in CampaignInput) (repo.Campaign, error) {
	normalized, err := s.normalizeNewCampaign(in)
	if err != nil {
		return repo.Campaign{}, err
	}
	if normalized.SurveyType == repo.TypeNPS {
		nps, err := s.npsRepo()
		if err != nil {
			return repo.Campaign{}, err
		}
		item, err := nps.CreateNPSCampaign(ctx, normalized, ptrext.Indirect(normalized.NPSSettings))
		return item, mapRepoError(err)
	}
	item, err := s.repo.CreateCampaign(ctx, normalized)
	return item, mapRepoError(err)
}

func (s *Service) UpdateCampaign(ctx context.Context, in CampaignInput) (repo.Campaign, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" || in.ID == uuid.Nil {
		return repo.Campaign{}, ErrValidation
	}
	current, err := s.repo.GetCampaign(ctx, tenantID, in.ID)
	if err != nil {
		return repo.Campaign{}, mapRepoError(err)
	}
	updated, err := s.applyCampaignUpdate(current, in)
	if err != nil {
		return repo.Campaign{}, err
	}
	if updated.SurveyType == repo.TypeNPS {
		nps, err := s.npsRepo()
		if err != nil {
			return repo.Campaign{}, err
		}
		settings := current.NPSSettings
		if settings == nil {
			stored, err := nps.GetNPSCampaignSettings(ctx, tenantID, in.ID)
			if err != nil {
				return repo.Campaign{}, mapRepoError(err)
			}
			settings = ptrext.Of(stored)
		}
		if in.NPSSettingsSet {
			updatedSettings, err := updatedNPSCampaignSettings(ptrext.Indirect(settings), in.NPSSettings)
			if err != nil {
				return repo.Campaign{}, err
			}
			settings = ptrext.Of(updatedSettings)
		}
		updated.NPSSettings = settings
		item, err := nps.UpdateNPSCampaign(ctx, updated, ptrext.Indirect(settings))
		return item, mapRepoError(err)
	}
	item, err := s.repo.UpdateCampaign(ctx, updated)
	return item, mapRepoError(err)
}

func (s *Service) ArchiveCampaign(ctx context.Context, tenantID string, id uuid.UUID, actorID string) (repo.Campaign, error) {
	tenantID = strings.TrimSpace(tenantID)
	actorID = strings.TrimSpace(actorID)
	if tenantID == "" || id == uuid.Nil || actorID == "" {
		return repo.Campaign{}, ErrValidation
	}
	item, err := s.repo.ArchiveCampaign(ctx, tenantID, id, actorID, s.now().UTC())
	return item, mapRepoError(err)
}

func (s *Service) RetryInvitationDelivery(ctx context.Context, tenantID string, id uuid.UUID, actorID string) (repo.Invitation, error) {
	tenantID = strings.TrimSpace(tenantID)
	actorID = strings.TrimSpace(actorID)
	if tenantID == "" || id == uuid.Nil || actorID == "" {
		return repo.Invitation{}, ErrValidation
	}
	item, err := s.repo.RetryInvitationDelivery(ctx, tenantID, id)
	return item, mapRepoError(err)
}

func (s *Service) CreateHostedLink(ctx context.Context, in HostedLinkInput) (repo.Invitation, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	sourceType := strings.TrimSpace(in.SourceType)
	sourceID := strings.TrimSpace(in.SourceID)
	if tenantID == "" || in.CampaignID == uuid.Nil {
		return repo.Invitation{}, ErrValidation
	}
	if sourceType == "" {
		sourceType = "manual"
	}
	if sourceID == "" {
		sourceID = uuid.NewString()
	}
	campaign, err := s.repo.GetCampaign(ctx, tenantID, in.CampaignID)
	if err != nil {
		return repo.Invitation{}, mapRepoError(err)
	}
	if campaign.Status != repo.StatusActive || campaign.DistributionMode != repo.DistributionSourceLink {
		return repo.Invitation{}, ErrDisabled
	}
	token, err := newToken()
	if err != nil {
		return repo.Invitation{}, err
	}
	now := s.now().UTC()
	expiresAt := now.Add(time.Duration(campaign.ExpiresAfterDays) * 24 * time.Hour)
	invitation, err := s.repo.CreateInvitation(ctx, repo.Invitation{
		ID:                     uuid.New(),
		TenantID:               tenantID,
		CampaignID:             campaign.ID,
		CampaignContentVersion: campaign.ContentVersion,
		CampaignSnapshot:       campaignSnapshot(campaign),
		DedupeKey:              dedupeKeyAt(campaign, sourceType, sourceID, in.RequestID, now),
		SourceType:             sourceType,
		SourceID:               sourceID,
		RequestID:              in.RequestID,
		DistributionMode:       repo.DistributionSourceLink,
		TokenHash:              tokenHash(token),
		DeliveryStatus:         repo.DeliveryNotApplicable,
		ResponseStatus:         repo.ResponseNotStarted,
		SuppressionStatus:      repo.SuppressionNotSuppressed,
		RecipientSnapshot:      normalizeObject(in.Context),
		ExpiresAt:              ptrext.Of(expiresAt),
		CreatedBy:              strings.TrimSpace(in.ActorID),
	})
	if err != nil {
		return repo.Invitation{}, mapRepoError(err)
	}
	invitation.PublicURL = s.publicSurveyURL(token)
	return invitation, nil
}

func (s *Service) GetPublicSurvey(ctx context.Context, token string) (repo.PublicSurvey, error) {
	invitation, _, err := s.resolvePublicInvitation(ctx, token)
	if err != nil {
		return repo.PublicSurvey{}, err
	}
	response, responseErr := s.repo.GetResponseByInvitation(ctx, invitation.TenantID, invitation.ID)
	if responseErr != nil && !errors.Is(mapRepoError(responseErr), ErrNotFound) {
		return repo.PublicSurvey{}, mapRepoError(responseErr)
	}
	if errors.Is(mapRepoError(responseErr), ErrNotFound) {
		if _, err = s.repo.MarkInvitationStarted(ctx, invitation.TenantID, invitation.ID); err != nil {
			return repo.PublicSurvey{}, mapRepoError(err)
		}
		invitation, _, err = s.resolvePublicInvitation(ctx, token)
		if err != nil {
			return repo.PublicSurvey{}, mapRepoError(err)
		}
		response, responseErr, err = s.completedPublicResponse(ctx, invitation, response, responseErr)
		if err != nil {
			return repo.PublicSurvey{}, err
		}
	}
	unsubscribeURL := ""
	if invitation.ContactID != nil {
		unsubscribeURL, _, err = s.invitationUnsubscribeLinks(ctx, invitation)
		if err != nil {
			return repo.PublicSurvey{}, err
		}
	}
	invitation, campaign, err := s.resolvePublicInvitation(ctx, token)
	if err != nil {
		return repo.PublicSurvey{}, err
	}
	response, responseErr, err = s.completedPublicResponse(ctx, invitation, response, responseErr)
	if err != nil {
		return repo.PublicSurvey{}, err
	}
	out := repo.PublicSurvey{
		Campaign:       campaign,
		Invitation:     invitation,
		UnsubscribeURL: unsubscribeURL,
	}
	if responseErr == nil {
		out.Response = ptrext.Of(response)
	}
	return out, nil
}

func (s *Service) completedPublicResponse(
	ctx context.Context,
	invitation repo.Invitation,
	response repo.Response,
	responseErr error,
) (repo.Response, error, error) {
	if invitation.ResponseStatus != repo.ResponseCompleted || responseErr == nil {
		return response, responseErr, nil
	}
	response, err := s.repo.GetResponseByInvitation(ctx, invitation.TenantID, invitation.ID)
	if err != nil {
		return repo.Response{}, responseErr, mapRepoError(err)
	}
	return response, nil, nil
}

func (s *Service) SubmitPublicResponse(ctx context.Context, in PublicSubmitInput) (repo.Response, bool, string, error) {
	invitation, campaign, err := s.resolvePublicInvitation(ctx, in.Token)
	if err != nil {
		return repo.Response{}, false, "", err
	}
	if invitation.ResponseStatus == repo.ResponseCompleted {
		return s.idempotentPublicResponse(ctx, invitation, campaign, nil)
	}
	comment, err := normalizedPublicSurveyComment(in.Comment)
	if err != nil {
		return repo.Response{}, false, "", err
	}
	in.Comment = comment
	if campaign.SurveyType == repo.TypeNPS {
		return s.submitNPSPublicResponse(ctx, invitation, campaign, in)
	}
	if in.FollowUpConsent != nil {
		return repo.Response{}, false, "", ErrValidation
	}
	if err := validateScore(campaign.SurveyType, in.Score); err != nil {
		return repo.Response{}, false, "", err
	}
	lowScore := in.Score <= campaign.LowScoreThreshold
	now := s.now().UTC()
	review := lowScoreReviewSeed(campaign, in.Score, now)
	response, err := s.repo.CreateResponse(ctx, repo.Response{
		ID:            uuid.New(),
		TenantID:      invitation.TenantID,
		CampaignID:    invitation.CampaignID,
		SurveyType:    campaign.SurveyType,
		InvitationID:  invitation.ID,
		RequestID:     invitation.RequestID,
		ContactID:     invitation.ContactID,
		SourceType:    invitation.SourceType,
		SourceID:      invitation.SourceID,
		Score:         in.Score,
		Comment:       in.Comment,
		Locale:        normalizedLocale(in.Locale, campaign.Locale),
		Metadata:      publicResponseQualityMetadata(in.QualityFlags),
		UserAgentHash: strings.TrimSpace(in.UserAgentHash),
		IPHash:        strings.TrimSpace(in.IPHash),
		SubmittedAt:   now,
	}, review)
	if err != nil {
		mapped := mapRepoError(err)
		if errors.Is(mapped, ErrConflict) {
			return s.idempotentPublicResponse(ctx, invitation, campaign, mapped)
		}
		return repo.Response{}, false, "", mapped
	}
	return response, lowScore, publicText(campaign.Content, "thank_you"), nil
}

const publicResponseTooFastThreshold = 3 * time.Second

func publicResponseQualityFlags(invitation repo.Invitation, userAgent, clientIP string, now time.Time) []string {
	flags := make([]string, 0, 4)
	if isAutomatedSurveyUserAgent(userAgent) {
		flags = append(flags, "automated_client")
	}
	if strings.TrimSpace(userAgent) == "" {
		flags = append(flags, "missing_user_agent")
	}
	if strings.TrimSpace(clientIP) == "" {
		flags = append(flags, "missing_client_address")
	}
	if invitation.ResponseStatus == repo.ResponseNotStarted {
		flags = append(flags, "submitted_without_page_visit")
	}
	if invitation.OpenedAt != nil {
		elapsed := now.Sub(ptrext.Indirect(invitation.OpenedAt))
		if elapsed >= 0 && elapsed < publicResponseTooFastThreshold {
			flags = append(flags, "submitted_too_quickly")
		}
	}
	return normalizePublicResponseQualityFlags(flags)
}

func isAutomatedSurveyUserAgent(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, marker := range []string{
		"bot", "crawler", "headless", "phantom", "playwright", "puppeteer", "selenium", "spider",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func publicResponseQualityMetadata(flags []string) map[string]any {
	flags = normalizePublicResponseQualityFlags(flags)
	status := repo.ResponseQualityStatusObserved
	if len(flags) > 0 {
		status = repo.ResponseQualityStatusFlagged
	}
	return responseQualityMetadata(status, flags)
}

func normalizePublicResponseQualityFlags(flags []string) []string {
	allowed := map[string]struct{}{
		"automated_client":             {},
		"missing_user_agent":           {},
		"missing_client_address":       {},
		"submitted_without_page_visit": {},
		"submitted_too_quickly":        {},
	}
	seen := make(map[string]struct{}, len(flags))
	out := make([]string, 0, len(flags))
	for _, flag := range flags {
		flag = strings.TrimSpace(flag)
		if _, ok := allowed[flag]; !ok {
			continue
		}
		if _, ok := seen[flag]; ok {
			continue
		}
		seen[flag] = struct{}{}
		out = append(out, flag)
	}
	return out
}

func responseQualityMetadata(status string, flags []string) map[string]any {
	metadata := map[string]any{
		"response_quality_version": repo.ResponseQualityMetadataVersion,
		"response_quality_status":  status,
		"response_quality_reasons": append([]string(nil), flags...),
	}
	return metadata
}

func normalizedPublicSurveyComment(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > domain.MaxContentLen {
		return "", ErrValidation
	}
	return value, nil
}

func (s *Service) idempotentPublicResponse(
	ctx context.Context,
	invitation repo.Invitation,
	campaign repo.Campaign,
	fallback error,
) (repo.Response, bool, string, error) {
	response, err := s.repo.GetResponseByInvitation(ctx, invitation.TenantID, invitation.ID)
	if err != nil {
		if fallback != nil {
			return repo.Response{}, false, "", fallback
		}
		return repo.Response{}, false, "", mapRepoError(err)
	}
	lowScore := response.Review != nil || response.Score <= campaign.LowScoreThreshold
	return response, lowScore, publicText(campaign.Content, "thank_you"), nil
}

func (s *Service) ListInvitations(ctx context.Context, filter repo.InvitationFilter) ([]repo.Invitation, error) {
	if strings.TrimSpace(filter.TenantID) == "" {
		return nil, ErrValidation
	}
	items, err := s.repo.ListInvitations(ctx, filter)
	return items, mapRepoError(err)
}

func (s *Service) ListResponses(ctx context.Context, filter repo.ResponseFilter) ([]repo.Response, error) {
	if strings.TrimSpace(filter.TenantID) == "" {
		return nil, ErrValidation
	}
	items, err := s.repo.ListResponses(ctx, filter)
	return items, mapRepoError(err)
}

func (s *Service) Analytics(ctx context.Context, filter repo.AnalyticsFilter) (repo.Analytics, error) {
	if strings.TrimSpace(filter.TenantID) == "" {
		return repo.Analytics{}, ErrValidation
	}
	item, err := s.repo.Analytics(ctx, filter)
	return item, mapRepoError(err)
}

func (s *Service) AnalyticsTrend(ctx context.Context, filter repo.AnalyticsFilter) ([]repo.AnalyticsTrendBucket, error) {
	if strings.TrimSpace(filter.TenantID) == "" {
		return nil, ErrValidation
	}
	normalized, err := normalizeAnalyticsWindow(filter, s.now())
	if err != nil {
		return nil, err
	}
	items, err := s.repo.AnalyticsTrend(ctx, normalized)
	return items, mapRepoError(err)
}

func (s *Service) AnalyticsSegments(ctx context.Context, filter repo.AnalyticsSegmentFilter) ([]repo.AnalyticsSegment, error) {
	if strings.TrimSpace(filter.TenantID) == "" {
		return nil, ErrValidation
	}
	normalized, err := normalizeAnalyticsSegmentFilter(filter, s.now())
	if err != nil {
		return nil, err
	}
	items, err := s.repo.AnalyticsSegments(ctx, normalized)
	return items, mapRepoError(err)
}

func (s *Service) AnalyticsInsights(ctx context.Context, filter AnalyticsInsightFilter) ([]AnalyticsInsight, error) {
	if strings.TrimSpace(filter.TenantID) == "" {
		return nil, ErrValidation
	}
	normalized, limit, err := normalizeAnalyticsInsightFilter(filter, s.now())
	if err != nil {
		return nil, err
	}
	analytics, err := s.repo.Analytics(ctx, normalized)
	if err != nil {
		return nil, mapRepoError(err)
	}
	segments, err := s.repo.AnalyticsSegments(ctx, insightSegmentFilter(normalized))
	if err != nil {
		return nil, mapRepoError(err)
	}
	items := rankInsights(buildAnalyticsInsights(analytics, segments))
	return limitInsights(items, limit), nil
}

func normalizeAnalyticsInsightFilter(
	filter AnalyticsInsightFilter,
	now time.Time,
) (repo.AnalyticsFilter, int, error) {
	normalized, err := normalizeAnalyticsWindow(repo.AnalyticsFilter{
		TenantID:   filter.TenantID,
		CampaignID: filter.CampaignID,
		From:       filter.From,
		To:         filter.To,
	}, now)
	if err != nil {
		return repo.AnalyticsFilter{}, 0, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 6
	}
	if limit > 20 {
		return repo.AnalyticsFilter{}, 0, ErrValidation
	}
	return normalized, limit, nil
}

func insightSegmentFilter(filter repo.AnalyticsFilter) repo.AnalyticsSegmentFilter {
	return repo.AnalyticsSegmentFilter{
		TenantID:   filter.TenantID,
		CampaignID: filter.CampaignID,
		From:       filter.From,
		To:         filter.To,
		Dimension:  repo.SegmentSourceType,
		Limit:      5,
	}
}

func buildAnalyticsInsights(analytics repo.Analytics, segments []repo.AnalyticsSegment) []AnalyticsInsight {
	var out []AnalyticsInsight
	out = appendOverdueReviewInsight(out, analytics)
	out = appendCriticalReviewInsight(out, analytics)
	out = appendUnassignedReviewInsight(out, analytics)
	out = appendPendingCustomerContactInsight(out, analytics)
	out = appendMissingRootCauseInsight(out, analytics)
	out = appendMissingActionInsight(out, analytics)
	out = appendLowScoreRateInsight(out, analytics)
	out = appendResponseRateInsight(out, analytics)
	out = appendSuppressionRateInsight(out, analytics)
	out = appendExpiredRateInsight(out, analytics)
	out = appendSegmentAttentionInsight(out, segments)
	if len(out) == 0 {
		out = append(out, AnalyticsInsight{
			ID:                "survey-health-stable",
			Severity:          InsightSeverityInfo,
			Title:             "Survey loop is stable",
			Summary:           "No operational survey risk crossed the active thresholds in this window.",
			Metric:            "overall_health",
			Value:             1,
			Threshold:         1,
			RecommendedAction: "Keep monitoring response quality and low-score follow-up queues.",
		})
	}
	return out
}

func appendOverdueReviewInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.OverdueLowScoreReviewCount == 0 {
		return out
	}
	severity := InsightSeverityWarning
	if analytics.OverdueLowScoreReviewCount >= 3 {
		severity = InsightSeverityCritical
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-overdue-low-score-reviews",
		Severity:          severity,
		Title:             "Low-score reviews are overdue",
		Summary:           "Customer recovery is blocked by overdue low-score follow-up work.",
		Metric:            "overdue_low_score_review_count",
		Value:             float64(analytics.OverdueLowScoreReviewCount),
		Threshold:         1,
		RecommendedAction: "Assign owners and resolve overdue low-score reviews before sending more survey volume.",
	})
}

func appendCriticalReviewInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.CriticalLowScoreReviewCount == 0 {
		return out
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-critical-low-score-reviews",
		Severity:          InsightSeverityCritical,
		Title:             "Critical low-score reviews are open",
		Summary:           "High-severity customer recovery work is still active.",
		Metric:            "critical_low_score_review_count",
		Value:             float64(analytics.CriticalLowScoreReviewCount),
		Threshold:         1,
		RecommendedAction: "Escalate critical low-score reviews to an owner with a same-day recovery plan.",
	})
}

func appendUnassignedReviewInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.UnassignedLowScoreReviewCount == 0 {
		return out
	}
	severity := InsightSeverityWarning
	if analytics.UnassignedLowScoreReviewCount >= 3 {
		severity = InsightSeverityCritical
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-unassigned-low-score-reviews",
		Severity:          severity,
		Title:             "Low-score reviews need owners",
		Summary:           "Some active recovery work has not been assigned to a team member.",
		Metric:            "unassigned_low_score_review_count",
		Value:             float64(analytics.UnassignedLowScoreReviewCount),
		Threshold:         1,
		RecommendedAction: "Assign owners before reviewing campaign performance or sending more invitations.",
	})
}

func appendPendingCustomerContactInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.PendingCustomerContactReviewCount == 0 {
		return out
	}
	severity := InsightSeverityWarning
	if analytics.PendingCustomerContactReviewCount >= 3 {
		severity = InsightSeverityCritical
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-pending-customer-contact",
		Severity:          severity,
		Title:             "Customers still need recovery contact",
		Summary:           "Active low-score reviews have not recorded customer contact yet.",
		Metric:            "pending_customer_contact_review_count",
		Value:             float64(analytics.PendingCustomerContactReviewCount),
		Threshold:         1,
		RecommendedAction: "Contact affected customers and record the recovery action on each review.",
	})
}

func appendMissingRootCauseInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.MissingRootCauseRecoveryQueueCount == 0 {
		return out
	}
	severity := InsightSeverityWarning
	if analytics.MissingRootCauseRecoveryQueueCount >= 5 {
		severity = InsightSeverityCritical
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-missing-root-cause-reviews",
		Severity:          severity,
		Title:             "Low-score reviews need root causes",
		Summary:           "Customer recovery work is moving without a captured reason the team can learn from.",
		Metric:            "missing_root_cause_recovery_queue_count",
		Value:             float64(analytics.MissingRootCauseRecoveryQueueCount),
		Threshold:         1,
		RecommendedAction: "Capture root causes on active recovery reviews before treating the queue as healthy.",
	})
}

func appendMissingActionInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.MissingActionRecoveryQueueCount == 0 {
		return out
	}
	severity := InsightSeverityWarning
	if analytics.MissingActionRecoveryQueueCount >= 5 {
		severity = InsightSeverityCritical
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-missing-action-reviews",
		Severity:          severity,
		Title:             "Low-score reviews need recovery actions",
		Summary:           "Root causes are captured, but the customer recovery action is still missing.",
		Metric:            "missing_action_recovery_queue_count",
		Value:             float64(analytics.MissingActionRecoveryQueueCount),
		Threshold:         1,
		RecommendedAction: "Record the action taken for each low-score recovery review before closing the loop.",
	})
}

func appendLowScoreRateInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.CompletedCount < 3 {
		return out
	}
	rate := float64(analytics.LowScoreCount) / float64(analytics.CompletedCount)
	if rate < 0.3 {
		return out
	}
	severity := InsightSeverityWarning
	if rate >= 0.5 {
		severity = InsightSeverityCritical
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-low-score-rate",
		Severity:          severity,
		Title:             "Low-score rate is elevated",
		Summary:           "A high share of completed survey responses needs recovery attention.",
		Metric:            "low_score_rate",
		Value:             rate,
		Threshold:         0.3,
		RecommendedAction: "Review low-score comments and cluster root causes before adjusting the campaign.",
	})
}

func appendResponseRateInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.InvitationCount < 5 || analytics.ResponseRate >= 0.2 {
		return out
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-response-rate",
		Severity:          InsightSeverityWarning,
		Title:             "Response rate is below target",
		Summary:           "Survey invitations are not converting into enough customer signal.",
		Metric:            "response_rate",
		Value:             analytics.ResponseRate,
		Threshold:         0.2,
		RecommendedAction: "Check survey placement, delivery timing, and whether the request already feels resolved to customers.",
	})
}

func appendSuppressionRateInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.InvitationCount < 5 {
		return out
	}
	rate := float64(analytics.SuppressedCount) / float64(analytics.InvitationCount)
	if rate < 0.25 {
		return out
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-suppression-rate",
		Severity:          InsightSeverityWarning,
		Title:             "Suppression rate is high",
		Summary:           "Eligibility rules are suppressing a large share of survey opportunities.",
		Metric:            "suppression_rate",
		Value:             rate,
		Threshold:         0.25,
		RecommendedAction: "Inspect suppression reasons and tune cooldown, daily limits, or recent-activity requirements.",
	})
}

func appendExpiredRateInsight(out []AnalyticsInsight, analytics repo.Analytics) []AnalyticsInsight {
	if analytics.InvitationCount < 5 {
		return out
	}
	rate := float64(analytics.ExpiredCount) / float64(analytics.InvitationCount)
	if rate < 0.2 {
		return out
	}
	return append(out, AnalyticsInsight{
		ID:                "survey-expired-rate",
		Severity:          InsightSeverityWarning,
		Title:             "Invitations are expiring",
		Summary:           "Customers are leaving survey invitations untouched until expiry.",
		Metric:            "expired_rate",
		Value:             rate,
		Threshold:         0.2,
		RecommendedAction: "Review expiry length, reminder timing, and whether the survey entry point is visible enough.",
	})
}

func appendSegmentAttentionInsight(out []AnalyticsInsight, segments []repo.AnalyticsSegment) []AnalyticsInsight {
	for _, segment := range segments {
		if segment.InvitationCount < 2 || segment.AttentionScore < 3 {
			continue
		}
		severity := InsightSeverityWarning
		if segment.LowScoreRate >= 0.5 || segment.SuppressionRate >= 0.5 {
			severity = InsightSeverityCritical
		}
		return append(out, AnalyticsInsight{
			ID:                "survey-segment-attention-" + segment.Key,
			Severity:          severity,
			Title:             "A survey segment needs attention",
			Summary:           "One segment is concentrating low scores, expiries, or suppressions.",
			Metric:            "segment_attention_score",
			Value:             segment.AttentionScore,
			Threshold:         3,
			SegmentKey:        segment.Key,
			SegmentLabel:      segment.Label,
			SegmentDimension:  segment.Dimension,
			RecommendedAction: "Drill into this segment before changing global survey settings.",
		})
	}
	return out
}

func rankInsights(items []AnalyticsInsight) []AnalyticsInsight {
	sort.SliceStable(items, func(i, j int) bool {
		left := insightPriority(items[i])
		right := insightPriority(items[j])
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left > right
	})
	for idx := range items {
		items[idx].Rank = idx + 1
	}
	return items
}

func insightPriority(item AnalyticsInsight) float64 {
	return float64(insightSeverityWeight(item.Severity)*1000+insightActionWeight(item.Metric)) +
		insightThresholdGap(item)
}

func insightSeverityWeight(severity string) int {
	switch severity {
	case InsightSeverityCritical:
		return 3
	case InsightSeverityWarning:
		return 2
	case InsightSeverityInfo:
		return 1
	default:
		return 0
	}
}

func insightActionWeight(metric string) int {
	switch metric {
	case "overdue_low_score_review_count":
		return 30
	case "critical_low_score_review_count":
		return 28
	case "unassigned_low_score_review_count":
		return 26
	case "pending_customer_contact_review_count":
		return 24
	case "missing_root_cause_recovery_queue_count":
		return 22
	case "missing_action_recovery_queue_count":
		return 21
	case "low_score_rate":
		return 20
	case "segment_attention_score":
		return 15
	case "response_rate":
		return 10
	case "suppression_rate", "expired_rate":
		return 5
	default:
		return 0
	}
}

func insightThresholdGap(item AnalyticsInsight) float64 {
	switch item.Metric {
	case "response_rate":
		return (item.Threshold - item.Value) * 100
	case "overdue_low_score_review_count", "critical_low_score_review_count",
		"unassigned_low_score_review_count", "pending_customer_contact_review_count":
		return item.Value * 10
	case "overall_health":
		return 0
	default:
		if strings.HasSuffix(item.Metric, "_rate") {
			return (item.Value - item.Threshold) * 100
		}
		return item.Value - item.Threshold
	}
}

func limitInsights(items []AnalyticsInsight, limit int) []AnalyticsInsight {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func normalizeAnalyticsWindow(filter repo.AnalyticsFilter, now time.Time) (repo.AnalyticsFilter, error) {
	const maxTrendWindow = 370 * 24 * time.Hour
	out := filter
	if out.To == nil {
		end := utcDayStart(now).AddDate(0, 0, 1)
		out.To = ptrext.Of(end)
	}
	if out.From == nil {
		start := ptrext.Indirect(out.To).AddDate(0, 0, -30)
		out.From = ptrext.Of(start)
	}
	from := ptrext.Indirect(out.From)
	to := ptrext.Indirect(out.To)
	if !from.Before(to) || to.Sub(from) > maxTrendWindow {
		return repo.AnalyticsFilter{}, ErrValidation
	}
	return out, nil
}

func normalizeAnalyticsSegmentFilter(filter repo.AnalyticsSegmentFilter, now time.Time) (repo.AnalyticsSegmentFilter, error) {
	out := filter
	out.Dimension = normalizeAnalyticsSegmentDimension(out.Dimension)
	if out.Dimension == "" {
		return repo.AnalyticsSegmentFilter{}, ErrValidation
	}
	if out.Limit <= 0 {
		out.Limit = 8
	}
	if out.Limit > 100 {
		return repo.AnalyticsSegmentFilter{}, ErrValidation
	}
	normalized, err := normalizeAnalyticsWindow(repo.AnalyticsFilter{
		TenantID:   out.TenantID,
		CampaignID: out.CampaignID,
		From:       out.From,
		To:         out.To,
	}, now)
	if err != nil {
		return repo.AnalyticsSegmentFilter{}, err
	}
	out.TenantID = normalized.TenantID
	out.CampaignID = normalized.CampaignID
	out.From = normalized.From
	out.To = normalized.To
	return out, nil
}

func normalizeAnalyticsSegmentDimension(value string) string {
	switch strings.TrimSpace(value) {
	case "", repo.SegmentSourceType:
		return repo.SegmentSourceType
	case repo.SegmentCampaign:
		return repo.SegmentCampaign
	case repo.SegmentDistributionMode:
		return repo.SegmentDistributionMode
	case repo.SegmentTriggerEvent:
		return repo.SegmentTriggerEvent
	default:
		return ""
	}
}

func utcDayStart(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) UpdateLowScoreReview(ctx context.Context, in ReviewInput) (repo.LowScoreReview, error) {
	if strings.TrimSpace(in.TenantID) == "" || in.ResponseID == uuid.Nil || strings.TrimSpace(in.ActorID) == "" {
		return repo.LowScoreReview{}, ErrValidation
	}
	current, err := s.repo.GetLowScoreReview(ctx, strings.TrimSpace(in.TenantID), in.ResponseID)
	if err != nil {
		return repo.LowScoreReview{}, mapRepoError(err)
	}
	next, err := applyReviewUpdate(current, in)
	if err != nil {
		return repo.LowScoreReview{}, err
	}
	item, err := s.repo.UpdateLowScoreReview(ctx, next)
	return item, mapRepoError(err)
}

func (s *Service) BatchUpdateLowScoreReviews(ctx context.Context, in BatchReviewInput) ([]repo.LowScoreReview, error) {
	responseIDs, err := validateBatchReviewInput(in)
	if err != nil {
		return nil, err
	}
	out := make([]repo.LowScoreReview, 0, len(responseIDs))
	for _, responseID := range responseIDs {
		item, err := s.UpdateLowScoreReview(ctx, batchReviewItemInput(in, responseID))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

const maxBatchLowScoreReviews = 50

func validateBatchReviewInput(in BatchReviewInput) ([]uuid.UUID, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ActorID) == "" {
		return nil, ErrValidation
	}
	if len(in.ResponseIDs) == 0 || len(in.ResponseIDs) > maxBatchLowScoreReviews || !batchReviewPatchPresent(in) {
		return nil, ErrValidation
	}
	seen := make(map[uuid.UUID]struct{}, len(in.ResponseIDs))
	out := make([]uuid.UUID, 0, len(in.ResponseIDs))
	for _, id := range in.ResponseIDs {
		if id == uuid.Nil {
			return nil, ErrValidation
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func batchReviewPatchPresent(in BatchReviewInput) bool {
	return in.Status != "" ||
		in.Severity != "" ||
		in.OwnerMemberIDSet ||
		in.RootCause != nil ||
		in.ActionTaken != nil ||
		in.CustomerContacted != nil ||
		in.DueAtSet
}

func batchReviewItemInput(in BatchReviewInput, responseID uuid.UUID) ReviewInput {
	return ReviewInput{
		TenantID:          in.TenantID,
		ResponseID:        responseID,
		Status:            in.Status,
		Severity:          in.Severity,
		OwnerMemberID:     in.OwnerMemberID,
		OwnerMemberIDSet:  in.OwnerMemberIDSet,
		RootCause:         in.RootCause,
		ActionTaken:       in.ActionTaken,
		CustomerContacted: in.CustomerContacted,
		DueAt:             in.DueAt,
		DueAtSet:          in.DueAtSet,
		ActorID:           in.ActorID,
	}
}

func (s *Service) resolvePublicInvitation(ctx context.Context, token string) (repo.Invitation, repo.Campaign, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return repo.Invitation{}, repo.Campaign{}, ErrValidation
	}
	invitation, err := s.repo.GetInvitationByTokenHash(ctx, tokenHash(token))
	if err != nil {
		return repo.Invitation{}, repo.Campaign{}, mapRepoError(err)
	}
	campaign, err := s.repo.GetCampaign(ctx, invitation.TenantID, invitation.CampaignID)
	if err != nil {
		return repo.Invitation{}, repo.Campaign{}, mapRepoError(err)
	}
	publicCampaign, err := campaignFromInvitationSnapshot(campaign, invitation)
	if err != nil {
		return repo.Invitation{}, repo.Campaign{}, err
	}
	if invitation.ResponseStatus == repo.ResponseCompleted {
		return invitation, publicCampaign, nil
	}
	if invitation.SuppressionStatus == repo.SuppressionSuppressed {
		return repo.Invitation{}, repo.Campaign{}, ErrDisabled
	}
	if invitation.ExpiresAt != nil && !s.now().Before(ptrext.Indirect(invitation.ExpiresAt)) {
		if _, err := s.repo.ExpireInvitation(ctx, invitation.TenantID, invitation.ID, "expired"); err != nil {
			mapped := mapRepoError(err)
			if !errors.Is(mapped, ErrNotFound) {
				return repo.Invitation{}, repo.Campaign{}, mapped
			}
			completed, readErr := s.repo.GetInvitationByTokenHash(ctx, tokenHash(token))
			if readErr != nil {
				return repo.Invitation{}, repo.Campaign{}, mapRepoError(readErr)
			}
			if completed.ResponseStatus == repo.ResponseCompleted {
				publicCampaign, snapshotErr := campaignFromInvitationSnapshot(campaign, completed)
				if snapshotErr != nil {
					return repo.Invitation{}, repo.Campaign{}, snapshotErr
				}
				return completed, publicCampaign, nil
			}
			return repo.Invitation{}, repo.Campaign{}, mapped
		}
		return repo.Invitation{}, repo.Campaign{}, ErrExpired
	}
	if campaign.Status != repo.StatusActive {
		return repo.Invitation{}, repo.Campaign{}, ErrDisabled
	}
	return invitation, publicCampaign, nil
}

func mapRepoError(err error) error {
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, repo.ErrInvalidInput):
		return ErrValidation
	case errors.Is(err, repo.ErrInvitationExpired):
		return ErrExpired
	case errors.Is(err, repo.ErrNPSArtifactExpired):
		return ErrExpired
	case errors.Is(err, repo.ErrCampaignNotActive):
		return ErrDisabled
	case errors.Is(err, repo.ErrConflict):
		return ErrConflict
	default:
		return err
	}
}

func newToken() (string, error) {
	var b [32]byte
	if _, err := randomRead(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func HashValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Service) publicSurveyURL(token string) string {
	if s.publicBase == "" {
		return "/surveys/" + token
	}
	return s.publicBase + "/surveys/" + token
}

func (s *Service) unsubscribeURL(tenantSlug string, token string) string {
	path := fmt.Sprintf(
		"/v1/portal/%s/unsubscribe?token=%s",
		url.PathEscape(strings.TrimSpace(tenantSlug)),
		url.QueryEscape(strings.TrimSpace(token)),
	)
	if s.publicBase == "" {
		return path
	}
	return s.publicBase + path
}

func dedupeKeyAt(campaign repo.Campaign, sourceType string, sourceID string, requestID *uuid.UUID, at time.Time) string {
	switch campaign.DedupePolicy {
	case repo.DedupeOnePerResolution:
		if requestID != nil {
			return fmt.Sprintf("request:%s", ptrext.Indirect(requestID))
		}
	case repo.DedupeOnePerTrigger:
		return fmt.Sprintf("trigger:%s:%s:%s:%d", campaign.TriggerEvent, sourceType, sourceID, at.UnixNano())
	}
	return fmt.Sprintf("source:%s:%s", sourceType, sourceID)
}
