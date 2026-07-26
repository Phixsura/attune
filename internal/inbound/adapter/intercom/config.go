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

	// SyncCursor resumes mid-window search pagination across ticks
	// (page-boundary granularity). Without it, a UTC day holding more
	// already-processed conversations than maxPagesPerTick can list
	// would be re-listed from the day floor every tick and the sync
	// could never reach the unprocessed tail. Empty = no continuation.
	SyncCursor string `json:"sync_cursor,omitempty"`

	// SyncWindowStart / SyncWindowEnd pin the exact search window the
	// cursor belongs to — a cursor is only valid against the query that
	// produced it.
	SyncWindowStart int64 `json:"sync_window_start,omitempty"`
	SyncWindowEnd   int64 `json:"sync_window_end,omitempty"`

	// SyncStats tracks backfill progress.
	SyncStats SyncStats `json:"sync_stats,omitempty"`
}

// SyncStats tracks operator-visible sync progress.
type SyncStats struct {
	ConversationsSynced int64 `json:"conversations_synced"`
	BackfillDone        bool  `json:"backfill_done"`
}

// setSyncCursor updates the mid-window continuation state as one unit.
func (c *Config) setSyncCursor(cursor string, windowStart, windowEnd int64) {
	c.SyncCursor = cursor
	c.SyncWindowStart = windowStart
	c.SyncWindowEnd = windowEnd
}

// applyPageStop records the continuation state for an early page stop.
func (c *Config) applyPageStop(stop pageStop, fetchCursor string, queryStart, queryEnd int64) {
	switch stop {
	case pageStopRetry:
		// Keep the cursor that fetched THIS page so the next tick
		// re-fetches it (processed head dedups for free).
		c.setSyncCursor(fetchCursor, queryStart, queryEnd)
	case pageStopDrained:
		// Everything below tickStart is covered; the remainder
		// (deferred items) belongs to the next tick's fresh window — a
		// cursor past deferred items would strand them.
		c.setSyncCursor("", 0, 0)
	default: // pageStopAbort
		c.setSyncCursor("", 0, 0)
	}
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
