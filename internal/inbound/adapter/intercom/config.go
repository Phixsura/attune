// SPDX-License-Identifier: Apache-2.0

package intercom

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/infra/intercomclient"
)

// Config is the decrypted shape of inbound_sources.config for an
// Intercom source. The updated_at watermark lives in Source.State.LastUID
// (native int64), not here — Intercom needs no opaque cursor storage.
type Config struct {
	Version int `json:"version"`

	// Region selects the API host: "us", "eu", or "au".
	Region string `json:"region"`

	// AccessTokenEncrypted is the double-encrypted private-app token.
	AccessTokenEncrypted []byte `json:"access_token_encrypted"`

	// WorkspaceID is the app id_code captured at create time (used in
	// permalinks and idempotency keys).
	WorkspaceID string `json:"workspace_id,omitempty"`

	// StartFrom controls first-sync behavior: "now" or "full".
	StartFrom string `json:"start_from"`

	// FilterStates restricts ingested conversations by state
	// (open/closed/snoozed). Empty = all states.
	FilterStates []string `json:"filter_states,omitempty"`

	// FilterTags requires conversations to carry ALL listed tags.
	FilterTags []string `json:"filter_tags,omitempty"`

	// FilterExcludeTags skips conversations carrying ANY listed tag.
	FilterExcludeTags []string `json:"filter_exclude_tags,omitempty"`

	// MaxDetailFetches caps per-tick conversation detail API calls.
	MaxDetailFetches int `json:"max_detail_fetches,omitempty"`

	// SyncStats tracks backfill progress.
	SyncStats SyncStats `json:"sync_stats,omitempty"`
}

// SyncStats tracks operator-visible sync progress.
type SyncStats struct {
	ConversationsSynced int64 `json:"conversations_synced"`
	BackfillDone        bool  `json:"backfill_done"`
}

// ConfigVersion is the only supported on-disk schema version.
const ConfigVersion = 1

// intercomConnInputs captures the validated Console create / test payload.
type intercomConnInputs struct {
	Region            string
	AccessToken       string
	StartFrom         string
	FilterStates      []string
	FilterTags        []string
	FilterExcludeTags []string
	MaxDetailFetches  int
}

// parseConfig decrypts the outer config envelope, unmarshals the Config,
// then decrypts the inner access token.
func parseConfig(raw []byte, secrets inbound.SecretStore) (Config, []byte, error) {
	decoded, err := secrets.Decrypt(raw)
	if err != nil {
		return Config{}, nil, err
	}
	var cfg Config
	if err := json.Unmarshal(decoded, &cfg); err != nil { // ptrext:allow json-unmarshal
		return Config{}, nil, err
	}
	if cfg.Version != ConfigVersion {
		return Config{}, nil, errors.New("intercom: unsupported config version")
	}
	if !intercomclient.ValidRegion(cfg.Region) {
		return Config{}, nil, fmt.Errorf("intercom: invalid region %q", cfg.Region)
	}
	if len(cfg.AccessTokenEncrypted) == 0 {
		return Config{}, nil, errors.New("intercom: access_token missing")
	}
	token, err := secrets.Decrypt(cfg.AccessTokenEncrypted)
	if err != nil {
		return Config{}, nil, fmt.Errorf("intercom: decrypt access_token: %w", err)
	}
	return cfg, token, nil
}

// validStates is the closed set of Intercom conversation states.
var validStates = map[string]bool{"open": true, "closed": true, "snoozed": true}

// normalizeFilterStates lowercases, dedups, and validates state filters.
func normalizeFilterStates(states []string) ([]string, error) {
	if len(states) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range states {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		if !validStates[s] {
			return nil, fmt.Errorf("invalid conversation state %q (valid: open, closed, snoozed)", s)
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

const (
	inboundSourceIDKey   = "inbound_source_id"
	inboundSourceNameKey = "inbound_source_name"
)
