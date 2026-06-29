// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"encoding/json"
	"fmt"

	"github.com/Phixsura/attune/internal/inbound"
)

const configVersion = 1

// Config is the decrypted shape of inbound_sources.config for a Slack source.
type Config struct {
	Version              int    `json:"version"`
	SigningSecretEncrypt []byte `json:"signing_secret_encrypted"`
	BotTokenEncrypted    []byte `json:"bot_token_encrypted"`
	signingSecret        []byte
}

func parseConfig(ciphertext []byte, secrets inbound.SecretStore) (Config, error) {
	if len(ciphertext) == 0 {
		return Config{}, fmt.Errorf("slack config: empty ciphertext")
	}

	envelope, err := secrets.Decrypt(ciphertext)
	if err != nil {
		return Config{}, fmt.Errorf("slack config: decrypt envelope: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(envelope, &cfg); err != nil { // ptrext:allow unmarshal-out-param
		return Config{}, fmt.Errorf("slack config: unmarshal: %w", err)
	}

	if cfg.Version != configVersion {
		return Config{}, fmt.Errorf("slack config: unsupported version %d", cfg.Version)
	}

	if len(cfg.SigningSecretEncrypt) == 0 {
		return Config{}, fmt.Errorf("slack config: signing_secret_encrypted is required")
	}

	secret, err := secrets.Decrypt(cfg.SigningSecretEncrypt)
	if err != nil {
		return Config{}, fmt.Errorf("slack config: decrypt signing secret: %w", err)
	}
	cfg.signingSecret = secret

	return cfg, nil
}
