// SPDX-License-Identifier: Apache-2.0

package email

import (
	"encoding/json"
	"errors"

	"github.com/Phixsura/attune/internal/inbound"
)

// Config is the decrypted shape of inbound_sources.config for an
// "email" channel row.
//
// Exported so internal/handlers/console/inbound can build the envelope
// at create time without duplicating the field list (H1, #66 review).
// ConfigVersion is the only supported value of Version.
type Config struct {
	Version           int    `json:"version"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	TLS               bool   `json:"tls"`
	Username          string `json:"username"`
	PasswordEncrypted []byte `json:"password_encrypted"`
	Folder            string `json:"folder"`
	StartFrom         string `json:"start_from"`   // "now" | "all_unseen"
	AfterIngest       string `json:"after_ingest"` // "mark_seen" | "keep_unseen" | "move_to:<folder>"
}

// ConfigVersion is the on-disk schema version stored in Config.Version.
const ConfigVersion = 1

const defaultFolder = "INBOX"

// parseEmailConfig — decrypts the inbound_sources.config envelope and
// returns the typed view + the resolved IMAP password (also decrypted).
// Password is returned as `[]byte` so the caller can wipe it after
// LOGIN (Go strings are immutable + GC-only, so a `string` return
// would pin the plaintext until garbage collection). The caller MUST
// zero the slice ASAP after handing it to the IMAP client (#66 review
// M-4).
func parseEmailConfig(raw []byte, secrets inbound.SecretStore) (Config, []byte, error) {
	decoded, err := secrets.Decrypt(raw)
	if err != nil {
		return Config{}, nil, err
	}
	var cfg Config
	if e := json.Unmarshal(decoded, &cfg); e != nil {
		return Config{}, nil, e
	}
	if cfg.Version != ConfigVersion {
		return Config{}, nil, errors.New("email: unsupported config version")
	}
	if cfg.Folder == "" {
		cfg.Folder = defaultFolder
	}
	pw, err := secrets.Decrypt(cfg.PasswordEncrypted)
	if err != nil {
		return Config{}, nil, err
	}
	return cfg, pw, nil
}
