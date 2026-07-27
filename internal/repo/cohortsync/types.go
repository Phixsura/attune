// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSourceNotFound = errors.New("cohort source not found")
	ErrCohortNotFound = errors.New("cohort not found")
	ErrRunNotFound    = errors.New("cohort sync run not found")
	ErrConflict       = errors.New("cohort sync conflict")
)

// Source is one provider connection (Amplitude project, Mixpanel project).
type Source struct {
	ID                      uuid.UUID
	TenantID                string
	Provider                string
	Name                    string
	AuthType                string
	CredentialKeyID         string
	CredentialCiphertext    []byte
	BaseURL                 string
	ProviderConfig          []byte
	WebhookSecretKeyID      string
	WebhookSecretCiphertext []byte
	Enabled                 bool
	Status                  string
	LastSyncAt              *time.Time
	LastError               string
	CreatedBy               string
	UpdatedBy               string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// Cohort is one synced cohort definition.
type Cohort struct {
	ID               uuid.UUID
	TenantID         string
	CohortSourceID   uuid.UUID
	ExternalCohortID string
	Name             string
	Description      string
	StaleTTLDays     int
	MemberCount      int
	Enabled          bool
	LastSyncedAt     *time.Time
	LastError        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Membership is one user's membership in a cohort.
type Membership struct {
	ID             uuid.UUID
	TenantID       string
	CohortID       uuid.UUID
	ExternalUserID string
	Email          string
	DisplayName    string
	UserProperties []byte
	JoinedAt       time.Time
	LeftAt         *time.Time
	ExpiresAt      *time.Time
	LastSeenAt     time.Time
}

// MembershipUpsert is the input for bulk membership upsert.
type MembershipUpsert struct {
	ExternalUserID string
	Email          string
	DisplayName    string
	UserProperties []byte
}

// SyncRun is one sync execution record.
type SyncRun struct {
	ID             uuid.UUID
	TenantID       string
	CohortID       uuid.UUID
	Trigger        string
	Status         string
	MembersAdded   int
	MembersRemoved int
	MembersTotal   int
	ErrorMessage   string
	StartedAt      time.Time
	FinishedAt     *time.Time
	CreatedAt      time.Time
}
