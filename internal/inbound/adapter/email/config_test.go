// SPDX-License-Identifier: Apache-2.0

package email

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound/inboundtest"
)

func TestParseEmailConfig_Valid(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}

	cfg := Config{
		Version:  ConfigVersion,
		Host:     "imap.example.com",
		Port:     993,
		TLS:      true,
		Username: "user@example.com",
		Folder:   "INBOX",
	}
	// Encrypt a password first, then embed it
	pw, err := secrets.Encrypt([]byte("secret-password"))
	require.NoError(t, err)
	cfg.PasswordEncrypted = pw

	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	// Encrypt the whole config
	enc, err := secrets.Encrypt(raw)
	require.NoError(t, err)

	got, gotPw, err := parseEmailConfig(enc, secrets)
	require.NoError(t, err)
	require.Equal(t, "imap.example.com", got.Host)
	require.Equal(t, 993, got.Port)
	require.True(t, got.TLS)
	require.Equal(t, "user@example.com", got.Username)
	require.Equal(t, "INBOX", got.Folder)
	require.Equal(t, "secret-password", string(gotPw))
}

func TestParseEmailConfig_DefaultFolder(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}

	cfg := Config{
		Version:  ConfigVersion,
		Host:     "imap.example.com",
		Port:     993,
		Username: "user@example.com",
		Folder:   "", // empty folder should default
	}
	pw, err := secrets.Encrypt([]byte("pw"))
	require.NoError(t, err)
	cfg.PasswordEncrypted = pw

	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	enc, err := secrets.Encrypt(raw)
	require.NoError(t, err)

	got, _, err := parseEmailConfig(enc, secrets)
	require.NoError(t, err)
	require.Equal(t, defaultFolder, got.Folder,
		"empty folder should default to INBOX")
}

func TestParseEmailConfig_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}

	cfg := Config{
		Version:  99,
		Host:     "imap.example.com",
		Port:     993,
		Username: "user@example.com",
	}
	pw, err := secrets.Encrypt([]byte("pw"))
	require.NoError(t, err)
	cfg.PasswordEncrypted = pw

	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	enc, err := secrets.Encrypt(raw)
	require.NoError(t, err)

	_, _, err = parseEmailConfig(enc, secrets)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported config version")
}

func TestParseEmailConfig_DecryptOuterFail(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}

	// Pass something that is not valid ciphertext (too short)
	_, _, err := parseEmailConfig([]byte{0x00}, secrets)
	require.Error(t, err, "should fail to decrypt outer envelope")
}

func TestParseEmailConfig_InvalidJSON(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}

	enc, err := secrets.Encrypt([]byte("not json"))
	require.NoError(t, err)

	_, _, err = parseEmailConfig(enc, secrets)
	require.Error(t, err, "should fail to unmarshal invalid JSON")
}

func TestParseEmailConfig_DecryptPasswordFail(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}

	cfg := Config{
		Version:           ConfigVersion,
		Host:              "imap.example.com",
		Port:              993,
		Username:          "user@example.com",
		PasswordEncrypted: []byte{0x00}, // too short for FakeSecrets.Decrypt
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	enc, err := secrets.Encrypt(raw)
	require.NoError(t, err)

	_, _, err = parseEmailConfig(enc, secrets)
	require.Error(t, err, "should fail to decrypt password")
}
