// SPDX-License-Identifier: Apache-2.0

package email

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
)

// emailConfig is the decrypted shape of inbound_sources.config for an
// "email" channel row.
type emailConfig struct {
	Version             int    `json:"version"`
	Host                string `json:"host"`
	Port                int    `json:"port"`
	TLS                 bool   `json:"tls"`
	Username            string `json:"username"`
	PasswordEncrypted   []byte `json:"password_encrypted"`
	Folder              string `json:"folder"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	StartFrom           string `json:"start_from"`   // "now" | "all_unseen"
	AfterIngest         string `json:"after_ingest"` // "mark_seen" | "keep_unseen" | "move_to:<folder>"
}

const (
	defaultPollInterval = 60 * time.Second
	defaultFolder       = "INBOX"
)

// parseEmailConfig — decrypts the inbound_sources.config envelope and
// returns the typed view + the resolved IMAP password (also decrypted).
// Caller is responsible for clearing the password from memory ASAP.
func parseEmailConfig(raw []byte, secrets inbound.SecretStore) (emailConfig, string, error) {
	decoded, err := secrets.Decrypt(raw)
	if err != nil {
		return emailConfig{}, "", err
	}
	var cfg emailConfig
	if e := json.Unmarshal(decoded, &cfg); e != nil {
		return emailConfig{}, "", e
	}
	if cfg.Version != 1 {
		return emailConfig{}, "", errors.New("email: unsupported config version")
	}
	if cfg.Folder == "" {
		cfg.Folder = defaultFolder
	}
	if cfg.PollIntervalSeconds <= 0 {
		cfg.PollIntervalSeconds = int(defaultPollInterval / time.Second)
	}
	pw, err := secrets.Decrypt(cfg.PasswordEncrypted)
	if err != nil {
		return emailConfig{}, "", err
	}
	return cfg, string(pw), nil
}
