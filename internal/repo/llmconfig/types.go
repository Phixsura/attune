// SPDX-License-Identifier: Apache-2.0

// Package llmconfig owns DB access for managed LLM channels and routes.
package llmconfig

import (
	"time"

	"github.com/google/uuid"
)

const (
	ProtocolOpenAICompat    = "openai-compat"
	ProtocolOpenAIResponses = "openai-responses"
	ProtocolAnthropic       = "anthropic"
	ProtocolGemini          = "gemini"

	AuthModeBearer = "bearer"
	AuthModeNone   = "none"

	ChannelStatusEnabled  = "enabled"
	ChannelStatusDisabled = "disabled"
	ChannelStatusDraining = "draining"
)

type KeyRegistryRow struct {
	KeyID              string
	Primary            bool
	Status             string
	TypeURL            string
	OutputPrefixType   string
	KeyMaterialType    string
	FingerprintSHA256  string
	FingerprintVersion int
}

type SecretKeyReference struct {
	Location string
	RowID    string
	KeyID    string
}

type InboundConfigRef struct {
	ID      uuid.UUID
	Channel string
	Config  []byte
}

type Channel struct {
	ID                   uuid.UUID
	Name                 string
	Protocol             string
	BaseURL              string
	AuthMode             string
	CredentialKeyID      string
	CredentialCiphertext []byte
	Status               string
	Priority             int
	Weight               int
	TimeoutSeconds       int
	CreatedAt            time.Time
	UpdatedAt            time.Time
	LastTestedAt         *time.Time
	LastTestStatus       string
	LastError            string
}

type Ability struct {
	ID            uuid.UUID
	ChannelID     uuid.UUID
	LogicalModel  string
	ProviderModel string
	Enabled       bool
	Priority      int
	Weight        int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Route struct {
	ID           uuid.UUID
	TenantID     string
	Purpose      string
	LogicalModel string
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Candidate struct {
	Route   Route
	Channel Channel
	Ability Ability
}
