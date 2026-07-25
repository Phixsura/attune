// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/infra/zendeskclient"
)

// Config is the decrypted shape of inbound_sources.config for a Zendesk source.
type Config struct {
	Version int `json:"version"`
	// AuthMode selects the credential type: "api_token" or "oauth".
	AuthMode  string `json:"auth_mode"`
	Subdomain string `json:"subdomain"`

	// api_token auth fields.
	Email             string `json:"email,omitempty"`
	APITokenEncrypted []byte `json:"api_token_encrypted,omitempty"`

	// oauth auth fields.
	OAuthTokenEncrypted []byte `json:"oauth_token_encrypted,omitempty"` // JSON: zendeskclient.OAuthToken

	// SyncCursor is the opaque Zendesk incremental-export cursor for resume.
	SyncCursor string `json:"sync_cursor,omitempty"`

	// StartFrom controls first-sync behavior: "now" or "full".
	StartFrom string `json:"start_from"`

	// MaxCommentFetches caps per-ticket comment API calls per poll tick.
	MaxCommentFetches int `json:"max_comment_fetches,omitempty"`

	// Filter restricts which tickets are ingested.
	Filter TicketFilter `json:"filter,omitempty"`

	// SyncStats tracks backfill progress.
	SyncStats SyncStats `json:"sync_stats,omitempty"`
}

// TicketFilter restricts which tickets are ingested.
type TicketFilter struct {
	Tags        []string `json:"tags,omitempty"`
	ExcludeTags []string `json:"exclude_tags,omitempty"`
	Statuses    []string `json:"statuses,omitempty"`
}

// SyncStats tracks operator-visible sync progress.
type SyncStats struct {
	TicketsSynced int64 `json:"tickets_synced"`
	LastTicketID  int64 `json:"last_ticket_id"`
	BackfillDone  bool  `json:"backfill_done"`
}

// ConfigVersion is the only supported on-disk schema version.
const ConfigVersion = 1

// zendeskConnInputs captures the validated Console create / test payload.
type zendeskConnInputs struct {
	Subdomain string
	AuthMode  string
	Email     string
	APIToken  string
	// OAuth paste-mode fields.
	OAuthAccessToken  string
	OAuthRefreshToken string
	OAuthClientID     string
	OAuthClientSecret string
}

// subdomainRE validates Zendesk subdomain format.
var subdomainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// parseConfig decrypts the outer config envelope, unmarshals the Config,
// then decrypts the inner credential.
func parseConfig(raw []byte, secrets inbound.SecretStore) (Config, credential, error) {
	decoded, err := secrets.Decrypt(raw)
	if err != nil {
		return Config{}, credential{}, err
	}
	var cfg Config
	if err := json.Unmarshal(decoded, &cfg); err != nil { // ptrext:allow json-unmarshal
		return Config{}, credential{}, err
	}
	if cfg.Version != ConfigVersion {
		return Config{}, credential{}, errors.New("zendesk: unsupported config version")
	}
	if cfg.Subdomain == "" {
		return Config{}, credential{}, errors.New("zendesk: subdomain missing")
	}

	switch cfg.AuthMode {
	case AuthModeAPIToken:
		if len(cfg.APITokenEncrypted) == 0 {
			return Config{}, credential{}, errors.New("zendesk: api_token missing")
		}
		if cfg.Email == "" {
			return Config{}, credential{}, errors.New("zendesk: email missing for api_token auth")
		}
		token, err := secrets.Decrypt(cfg.APITokenEncrypted)
		if err != nil {
			return Config{}, credential{}, fmt.Errorf("zendesk: decrypt api_token: %w", err)
		}
		return cfg, credential{
			Mode:     AuthModeAPIToken,
			Email:    cfg.Email,
			APIToken: token,
		}, nil

	case AuthModeOAuth:
		if len(cfg.OAuthTokenEncrypted) == 0 {
			return Config{}, credential{}, errors.New("zendesk: oauth_token missing")
		}
		decrypted, err := secrets.Decrypt(cfg.OAuthTokenEncrypted)
		if err != nil {
			return Config{}, credential{}, fmt.Errorf("zendesk: decrypt oauth_token: %w", err)
		}
		var tok zendeskclient.OAuthToken
		if err := json.Unmarshal(decrypted, &tok); err != nil { // ptrext:allow json-unmarshal
			return Config{}, credential{}, fmt.Errorf("zendesk: parse oauth_token: %w", err)
		}
		return cfg, credential{
			Mode:         AuthModeOAuth,
			AccessToken:  tok.AccessToken,
			RefreshToken: tok.RefreshToken,
			ClientID:     tok.ClientID,
			ClientSecret: tok.ClientSecret,
		}, nil

	default:
		return Config{}, credential{}, fmt.Errorf("zendesk: unsupported auth_mode %q", cfg.AuthMode)
	}
}

// validateSubdomain checks the subdomain format.
func validateSubdomain(subdomain string) error {
	subdomain = strings.TrimSpace(strings.ToLower(subdomain))
	if subdomain == "" {
		return errors.New("subdomain must not be empty")
	}
	if !subdomainRE.MatchString(subdomain) {
		return fmt.Errorf("subdomain %q can only contain lowercase letters, numbers, and hyphens", subdomain)
	}
	return nil
}

const (
	inboundSourceIDKey   = "inbound_source_id"
	inboundSourceNameKey = "inbound_source_name"
)
