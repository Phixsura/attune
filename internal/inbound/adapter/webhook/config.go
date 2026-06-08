// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// webhookConfig — the decrypted shape of inbound_sources.config for a
// "webhook" channel row. The two ciphertexts are envelope-encrypted via
// inbound.SecretStore (AES-GCM-256, version + key_id bytes — see
// internal/inbound/secrets.go for the layout). previous_expires_at is
// populated by rotate.RotateSecret; when nil OR in the past the
// previous secret is considered expired.
type webhookConfig struct {
	Version                 int        `json:"version"`
	SecretCurrentEncrypted  []byte     `json:"secret_current_encrypted"`
	SecretPreviousEncrypted []byte     `json:"secret_previous_encrypted,omitempty"`
	PreviousExpiresAt       *time.Time `json:"previous_expires_at,omitempty"`
	HMACAlgo                string     `json:"hmac_algo"`
}

// parseConfig returns the current secret, the previous secret (or
// nil/empty), and whether the previous secret has expired. raw is the
// AES-GCM envelope ciphertext stored in inbound_sources.config; the
// inner JSON has the webhookConfig shape.
func parseConfig(raw []byte, secrets inbound.SecretStore) (current, previous []byte, prevExpired bool, err error) {
	decoded, err := secrets.Decrypt(raw)
	if err != nil {
		return nil, nil, true, err
	}
	var cfg webhookConfig
	if e := json.Unmarshal(decoded, &cfg); e != nil {
		return nil, nil, true, e
	}
	if cfg.Version != 1 {
		return nil, nil, true, errors.New("webhook: unsupported config version")
	}
	if cfg.HMACAlgo != "" && cfg.HMACAlgo != "sha256" {
		return nil, nil, true, errors.New("webhook: unsupported hmac algo")
	}
	current = cfg.SecretCurrentEncrypted
	if len(cfg.SecretPreviousEncrypted) == 0 {
		return current, nil, true, nil
	}
	prevExpired = cfg.PreviousExpiresAt == nil || time.Now().After(ptrext.Indirect(cfg.PreviousExpiresAt))
	return current, cfg.SecretPreviousEncrypted, prevExpired, nil
}
