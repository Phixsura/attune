// SPDX-License-Identifier: Apache-2.0

package intercom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/infra/intercomclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Public aliases keep the adapter's runtime helpers reusable from the
// Console handler without re-declaring Intercom payload shapes in a
// second package.
type (
	AccountInfo = intercomAccount
	ConnInputs  = intercomConnInputs
)

// ValidateConnConfig normalizes the Intercom connection payload.
func ValidateConnConfig(region, accessToken, startFrom string, filterStates, filterTags, filterExcludeTags []string, maxDetailFetches int) (ConnInputs, error) {
	region = strings.ToLower(strings.TrimSpace(region))
	if !intercomclient.ValidRegion(region) {
		return ConnInputs{}, errMissing("region must be 'us', 'eu', or 'au'")
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return ConnInputs{}, errMissing("access_token is required")
	}
	startFrom = strings.TrimSpace(startFrom)
	switch startFrom {
	case "", "now":
		startFrom = "now"
	case "full":
	default:
		return ConnInputs{}, errMissing("start_from must be 'now' or 'full'")
	}
	states, err := normalizeFilterStates(filterStates)
	if err != nil {
		return ConnInputs{}, err
	}
	if maxDetailFetches < 0 {
		return ConnInputs{}, errMissing("max_detail_fetches must not be negative")
	}
	return ConnInputs{
		Region:            region,
		AccessToken:       accessToken,
		StartFrom:         startFrom,
		FilterStates:      states,
		FilterTags:        normalizeTagList(filterTags),
		FilterExcludeTags: normalizeTagList(filterExcludeTags),
		MaxDetailFetches:  maxDetailFetches,
	}, nil
}

// normalizeTagList trims and dedups tag filters (case-preserving; the
// match itself is case-insensitive).
func normalizeTagList(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		key := strings.ToLower(tag)
		if tag == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag)
	}
	return out
}

// AuthTest checks an access token against Intercom's /me endpoint.
func AuthTest(ctx context.Context, region, accessToken string) (AccountInfo, error) {
	c := newAPIClient(region, accessToken)
	return c.AuthTest(ctx)
}

// IsPermanentError reports whether an Intercom API error should disable
// the source instead of being retried.
func IsPermanentError(err error) bool {
	return isPermanentIntercomError(err)
}

// APIErrorStatus extracts the HTTP status and Intercom error code from
// an API-level failure. ok=false for non-API errors (network, decode).
func APIErrorStatus(err error) (status int, code string, ok bool) {
	var ae apiError
	if errors.As(err, &ae) {
		return ae.Status, ae.Code, true
	}
	return 0, "", false
}

type missingFieldError struct{ msg string }

func (e missingFieldError) Error() string { return e.msg }

func errMissing(msg string) error { return missingFieldError{msg: msg} }

// SettingsUpdate carries the operator-editable connection settings for
// an in-place source update. Region is immutable (it selects the API
// host the stored watermark was minted against); AccessToken is
// replace-only — empty keeps the stored token. StartFrom and
// MaxDetailFetches are presence-aware (nil keeps the stored value);
// the filter lists always replace the stored set — callers prefill
// them from DecodeConnSummary so a partial edit doesn't wipe them.
type SettingsUpdate struct {
	AccessToken       string
	StartFrom         *string
	FilterStates      []string
	FilterTags        []string
	FilterExcludeTags []string
	MaxDetailFetches  *int
}

// ApplySettingsUpdate merges a settings update into a source's stored
// config blob and re-encrypts it. Sync state (watermark inputs, cursor,
// stats, workspace ID) is always preserved — that is the whole point of
// in-place updates over delete/recreate. When the update carries a new
// access token, the caller must have validated it (AuthTest) against
// the SAME workspace first: a token for a different workspace would
// corrupt idempotency keys and permalinks minted under the stored
// workspace ID.
func ApplySettingsUpdate(raw []byte, secrets inbound.SecretStore, upd SettingsUpdate) ([]byte, error) {
	cfg, token, err := parseConfig(raw, secrets)
	if err != nil {
		return nil, err
	}
	wipeBytes(token)

	// Absent optional scalars keep their stored values — "PATCH omits
	// it" must not degrade to "reset to default".
	startFrom := cfg.StartFrom
	if upd.StartFrom != nil {
		startFrom = ptrext.Indirect(upd.StartFrom)
	}
	maxDetail := cfg.MaxDetailFetches
	if upd.MaxDetailFetches != nil {
		maxDetail = ptrext.Indirect(upd.MaxDetailFetches)
	}

	inputs, err := ValidateConnConfig(
		cfg.Region,
		firstNonEmpty(strings.TrimSpace(upd.AccessToken), "keep"),
		startFrom,
		upd.FilterStates,
		upd.FilterTags,
		upd.FilterExcludeTags,
		maxDetail,
	)
	if err != nil {
		return nil, err
	}

	if tok := strings.TrimSpace(upd.AccessToken); tok != "" {
		enc, encErr := secrets.Encrypt([]byte(tok))
		if encErr != nil {
			return nil, fmt.Errorf("intercom: encrypt access_token: %w", encErr)
		}
		cfg.AccessTokenEncrypted = enc
	}
	cfg.StartFrom = inputs.StartFrom
	cfg.FilterStates = inputs.FilterStates
	cfg.FilterTags = inputs.FilterTags
	cfg.FilterExcludeTags = inputs.FilterExcludeTags
	cfg.MaxDetailFetches = inputs.MaxDetailFetches

	encoded, err := json.Marshal(cfg) // ptrext:allow json-marshal
	if err != nil {
		return nil, fmt.Errorf("intercom: marshal config: %w", err)
	}
	return secrets.Encrypt(encoded)
}

// ConnSummary is the decrypted, operator-visible slice of a stored
// config — everything except credentials and sync internals.
type ConnSummary struct {
	Region            string
	StartFrom         string
	FilterStates      []string
	FilterTags        []string
	FilterExcludeTags []string
	MaxDetailFetches  int
	WorkspaceID       string
}

// DecodeConnSummary exposes the editable settings for the Console edit
// form. Never returns the access token.
func DecodeConnSummary(raw []byte, secrets inbound.SecretStore) (ConnSummary, error) {
	cfg, token, err := parseConfig(raw, secrets)
	if err != nil {
		return ConnSummary{}, err
	}
	wipeBytes(token)
	return ConnSummary{
		Region:            cfg.Region,
		StartFrom:         cfg.StartFrom,
		FilterStates:      cfg.FilterStates,
		FilterTags:        cfg.FilterTags,
		FilterExcludeTags: cfg.FilterExcludeTags,
		MaxDetailFetches:  cfg.MaxDetailFetches,
		WorkspaceID:       cfg.WorkspaceID,
	}, nil
}

// StoredRegionAndToken decrypts the region + current access token for a
// pre-update AuthTest (workspace pinning). Caller must wipe the token.
func StoredRegionAndToken(raw []byte, secrets inbound.SecretStore) (string, []byte, error) {
	cfg, token, err := parseConfig(raw, secrets)
	if err != nil {
		return "", nil, err
	}
	return cfg.Region, token, nil
}

// WipeToken zeros credential bytes after use (public wrapper).
func WipeToken(b []byte) { wipeBytes(b) }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
