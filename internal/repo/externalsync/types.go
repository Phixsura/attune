// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrConnectionNotFound   = errors.New("external sync connection not found")
	ErrMappingNotFound      = errors.New("external sync mapping not found")
	ErrRunNotFound          = errors.New("external sync run not found")
	ErrLocalObjectNotFound  = errors.New("external sync local object not found")
	ErrFailureNotFound      = errors.New("external sync failure not found")
	ErrConflictNotFound     = errors.New("external sync conflict not found")
	ErrEventNotFound        = errors.New("external sync event not found")
	ErrInstallationNotFound = errors.New("external provider installation not found")
	ErrResourceNotFound     = errors.New("external provider installation resource not found")
	ErrConflict             = errors.New("external sync conflict")
	ErrInvalidInput         = errors.New("external sync invalid input")
)

const (
	ConnectionStatusActive      = "active"
	ConnectionStatusDisabled    = "disabled"
	ConnectionStatusQuarantined = "quarantined"
	ConnectionStatusDeleted     = "deleted"

	TestStatusUntested = "untested"
	TestStatusOK       = "ok"
	TestStatusWarning  = "warning"
	TestStatusFailed   = "failed"

	DirectionPull          = "pull"
	DirectionPush          = "push"
	DirectionBidirectional = "bidirectional"

	TriggerManual   = "manual"
	TriggerRetry    = "retry"
	TriggerSystem   = "system"
	TriggerWebhook  = "webhook"
	TriggerBackfill = "backfill"

	RunStatusQueued    = "queued"
	RunStatusRunning   = "running"
	RunStatusSucceeded = "succeeded"
	RunStatusPartial   = "partial"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"
	RunStatusDead      = "dead"

	StreamDefault = "default"

	SyncStatePending  = "pending"
	SyncStateSynced   = "synced"
	SyncStateFailed   = "failed"
	SyncStateConflict = "conflict"
	SyncStateDeleted  = "deleted"

	ChildTypeComment = "comment"

	EventSignatureVerified    = "verified"
	EventSignatureFailed      = "failed"
	EventSignatureNotRequired = "not_required"

	EventStatusReceived = "received"
	EventStatusReplayed = "replayed"
	EventStatusIgnored  = "ignored"
	EventStatusFailed   = "failed"

	InstallationKindGitHubApp = "github_app"
	InstallationKindOAuthApp  = "oauth_app"
	InstallationKindToken     = "token"
	InstallationKindManual    = "manual"

	InstallationStatusPending   = "pending"
	InstallationStatusActive    = "active"
	InstallationStatusLimited   = "limited"
	InstallationStatusDrifted   = "drifted"
	InstallationStatusSuspended = "suspended"
	InstallationStatusDeleted   = "deleted"

	ResourceSelectionAll      = "all"
	ResourceSelectionSelected = "selected"
	ResourceSelectionNone     = "none"

	ResourceTypeRepository   = "repository"
	ResourceTypeProject      = "project"
	ResourceTypeWorkspace    = "workspace"
	ResourceTypeOrganization = "organization"

	ResourceStatusActive  = "active"
	ResourceStatusRemoved = "removed"
	ResourceStatusUnknown = "unknown"
)

type Connection struct {
	ID                      uuid.UUID
	TenantID                string
	Provider                string
	ProviderInstallationID  *uuid.UUID
	Name                    string
	Enabled                 bool
	Status                  string
	AuthType                string
	BaseURL                 string
	ProviderConfig          []byte
	Scopes                  []string
	CredentialKeyID         string
	CredentialCiphertext    []byte
	WebhookSecretKeyID      string
	WebhookSecretCiphertext []byte
	WebhookSecretSetAt      *time.Time
	LastTestedAt            *time.Time
	LastTestStatus          string
	LastError               string
	CreatedBy               string
	UpdatedBy               string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type Mapping struct {
	ID                 uuid.UUID
	TenantID           string
	ConnectionID       uuid.UUID
	LocalObjectType    string
	ExternalObjectType string
	Direction          string
	FieldMapping       []byte
	StatusMapping      []byte
	ConflictPolicy     string
	TombstonePolicy    string
	Enabled            bool
	MappingVersion     int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type SyncRun struct {
	ID               uuid.UUID
	TenantID         string
	ConnectionID     uuid.UUID
	MappingID        *uuid.UUID
	Direction        string
	Trigger          string
	Status           string
	ClaimedAt        *time.Time
	ClaimedBy        string
	Attempts         int
	NextRetryAt      time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	CursorBefore     []byte
	CursorAfter      []byte
	InputMetadata    []byte
	RecordsSeen      int
	RecordsChanged   int
	RecordsFailed    int
	ConflictsCreated int
	ErrorKind        string
	ErrorMessage     string
	ActorID          string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ListRunsFilter struct {
	TenantID     string
	ConnectionID *uuid.UUID
	MappingID    *uuid.UUID
	Status       string
	BeforeID     *uuid.UUID
	Limit        int
}

type ListRunsResult struct {
	Runs         []SyncRun
	NextBeforeID string
}

type SyncEvent struct {
	ID                uuid.UUID
	TenantID          string
	ConnectionID      uuid.UUID
	MappingID         *uuid.UUID
	Provider          string
	EventType         string
	ExternalEventID   string
	DedupeKey         string
	SignatureStatus   string
	Status            string
	PayloadDigest     string
	NormalizedPayload []byte
	ReceivedAt        time.Time
	ReplayedAt        *time.Time
	ReplayedBy        string
	RunID             *uuid.UUID
	FailureReason     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ListEventsFilter struct {
	TenantID     string
	ConnectionID *uuid.UUID
	Status       string
	BeforeID     *uuid.UUID
	Limit        int
}

type ListEventsResult struct {
	Events       []SyncEvent
	NextBeforeID string
}

type ProviderInstallation struct {
	ID                     uuid.UUID
	TenantID               string
	Provider               string
	DisplayName            string
	InstallationKind       string
	Status                 string
	ExternalInstallationID string
	AccountLogin           string
	AccountID              string
	AccountURL             string
	BaseURL                string
	Permissions            []byte
	CapabilityProfile      []byte
	ResourceSelection      string
	QualificationStatus    string
	LastQualifiedAt        *time.Time
	LastError              string
	CreatedBy              string
	UpdatedBy              string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type ProviderInstallationResource struct {
	ID                 uuid.UUID
	TenantID           string
	InstallationID     uuid.UUID
	Provider           string
	ResourceType       string
	ExternalResourceID string
	ResourceKey        string
	DisplayName        string
	HTMLURL            string
	Selected           bool
	Status             string
	Permissions        []byte
	LastSeenAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ProviderInstallationWithResources struct {
	Installation ProviderInstallation
	Resources    []ProviderInstallationResource
}

type SyncAttempt struct {
	ID                int64
	RunID             uuid.UUID
	AttemptNumber     int
	StartedAt         time.Time
	FinishedAt        *time.Time
	Result            string
	HTTPStatus        int
	ProviderRequestID string
	RetryAfter        *time.Time
	ErrorKind         string
	ErrorMessage      string
}

type RecordFailure struct {
	ID                uuid.UUID
	TenantID          string
	RunID             uuid.UUID
	MappingID         uuid.UUID
	Operation         string
	LocalObjectID     string
	ExternalKey       string
	FailureKind       string
	Message           string
	PayloadDigest     string
	RetryMode         string
	NormalizedPayload []byte
	Retryable         bool
	ResolvedAt        *time.Time
	ResolvedBy        string
	CreatedAt         time.Time
}

type ConflictRow struct {
	ID               uuid.UUID
	TenantID         string
	MappingID        uuid.UUID
	LocalObjectID    string
	ExternalKey      string
	ConflictKind     string
	Status           string
	LocalSnapshot    []byte
	ExternalSnapshot []byte
	Resolution       string
	ResolvedAt       *time.Time
	ResolvedBy       string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Health struct {
	EnabledConnections      int
	FailingConnections      int
	StaleConnections        int
	ActiveRuns              int
	RetryableRuns           int
	DeadRuns                int
	OpenConflicts           int
	NewestSuccessfulRunAt   *time.Time
	DisabledConnections     int
	ThrottledRuns           int
	UnauthorizedRuns        int
	ProviderUnavailableRuns int
	DelayedRetryRuns        int
	NewestRetryAfter        *time.Time
	DegradedConnections     int
	QuarantinedConnections  int
}

type ResetCursorResult struct {
	Mapping Mapping
	Run     SyncRun
}

type BackfillResult struct {
	Mapping Mapping
	Run     SyncRun
}

type CustomerRequestIssueCreateRunInput struct {
	TenantID     string
	RequestID    uuid.UUID
	ConnectionID *uuid.UUID
	MappingID    *uuid.UUID
	ActorID      string
}

type CustomerRequestIssuePullRunInput struct {
	TenantID     string
	RequestID    uuid.UUID
	ConnectionID uuid.UUID
	MappingID    uuid.UUID
	ExternalKey  string
	ActorID      string
}

type CustomerRequestIssueCreateRunResult struct {
	Mapping Mapping
	Run     SyncRun
}

type CustomerRequestIssuePullRunResult struct {
	Mapping Mapping
	Run     SyncRun
}

type BatchResolveConflictsResult struct {
	Conflicts []ConflictRow
}

type MetricSnapshot struct {
	Points []MetricPoint
}

type MetricPoint struct {
	Provider           string
	ExternalObjectType string
	DeadRuns           int
	LagSeconds         float64
}

type RunDetail struct {
	Run       SyncRun
	Attempts  []SyncAttempt
	Failures  []RecordFailure
	Conflicts []ConflictRow
}

type RecordTimelineFilter struct {
	TenantID      string
	MappingID     uuid.UUID
	LocalObjectID string
	ExternalKey   string
	Limit         int
}

type RecordTimelineEntry struct {
	Kind          string
	OccurredAt    time.Time
	RunID         *uuid.UUID
	Status        string
	Operation     string
	LocalObjectID string
	ExternalKey   string
	Summary       string
	Detail        []byte
}

type PullRecord struct {
	LocalObjectID     string
	ExternalKey       string
	ExternalURL       string
	ExternalVersion   string
	ExternalUpdatedAt *time.Time
	Deleted           bool
	Payload           []byte
}

type PullChildRecord struct {
	ParentExternalKey string
	Type              string
	ExternalKey       string
	ExternalURL       string
	ExternalVersion   string
	ExternalUpdatedAt *time.Time
	Deleted           bool
	Payload           []byte
}

type ApplyPullInput struct {
	TenantID      string
	RunID         uuid.UUID
	ConnectionID  uuid.UUID
	MappingID     uuid.UUID
	Provider      string
	StreamKey     string
	CursorBefore  []byte
	CursorAfter   []byte
	InputMetadata []byte
	Records       []PullRecord
	Children      []PullChildRecord
}

type PushRecord struct {
	LocalObjectID   string
	ExternalKey     string
	ExternalVersion string
	LocalVersion    string
	LocalUpdatedAt  time.Time
	Payload         []byte
}

type PushResult struct {
	LocalObjectID   string
	ExternalKey     string
	ExternalURL     string
	ExternalVersion string
	ErrorKind       string
	ErrorMessage    string
	Retryable       bool
}

type ApplyPushInput struct {
	TenantID     string
	RunID        uuid.UUID
	ConnectionID uuid.UUID
	MappingID    uuid.UUID
	Provider     string
	Records      []PushRecord
	Results      []PushResult
}

type ApplyStats struct {
	RecordsSeen      int
	RecordsChanged   int
	RecordsFailed    int
	ConflictsCreated int
}

type AttemptInput struct {
	RunID             uuid.UUID
	AttemptNumber     int
	StartedAt         time.Time
	Result            string
	HTTPStatus        int
	ProviderRequestID string
	RetryAfter        *time.Time
	ErrorKind         string
	ErrorMessage      string
}
