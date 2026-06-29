// SPDX-License-Identifier: Apache-2.0

package discord

import (
	"encoding/json"
	"fmt"

	"github.com/Phixsura/attune/internal/inbound"
)

const configVersion = 1

// Config is the decrypted shape of inbound_sources.config for a Discord source.
type Config struct {
	Version            int    `json:"version"`
	PublicKeyEncrypted []byte `json:"public_key_encrypted"`
	publicKey          []byte
}

func parseConfig(ciphertext []byte, secrets inbound.SecretStore) (Config, error) {
	if len(ciphertext) == 0 {
		return Config{}, fmt.Errorf("discord config: empty ciphertext")
	}

	envelope, err := secrets.Decrypt(ciphertext)
	if err != nil {
		return Config{}, fmt.Errorf("discord config: decrypt envelope: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(envelope, &cfg); err != nil { // ptrext:allow unmarshal-out-param
		return Config{}, fmt.Errorf("discord config: unmarshal: %w", err)
	}

	if cfg.Version != configVersion {
		return Config{}, fmt.Errorf("discord config: unsupported version %d", cfg.Version)
	}

	if len(cfg.PublicKeyEncrypted) == 0 {
		return Config{}, fmt.Errorf("discord config: public_key_encrypted is required")
	}

	pk, err := secrets.Decrypt(cfg.PublicKeyEncrypted)
	if err != nil {
		return Config{}, fmt.Errorf("discord config: decrypt public key: %w", err)
	}
	cfg.publicKey = pk

	return cfg, nil
}
