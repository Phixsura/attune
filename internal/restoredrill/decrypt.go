// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Phixsura/attune/internal/infra/secretstore"
)

// SECURITY: functions in this file decrypt real managed secrets to prove
// recoverability. They MUST NOT log, print, return, or embed decrypted
// plaintext in error messages, and they zero the plaintext as soon as the
// presence check is done. Only booleans/errors/counts may escape.

// zero overwrites a plaintext buffer so it does not linger in memory.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// inboundWebhookSecrets and inboundEmailSecrets mirror the persisted,
// Tink-AEAD-wrapped JSON envelope the inbound webhook/email adapters write into
// inbound_sources.config. The json tags are a frozen storage format; the drill
// decodes only the nested ciphertext fields it must prove decryptable and
// ignores the rest of the envelope.
type inboundWebhookSecrets struct {
	SecretCurrentEncrypted  []byte `json:"secret_current_encrypted"`
	SecretPreviousEncrypted []byte `json:"secret_previous_encrypted"`
}

type inboundEmailSecrets struct {
	PasswordEncrypted []byte `json:"password_encrypted"`
}

// llmChannelAAD reconstructs the associated data bound to an llm_channels API
// key at encryption time (internal/service/llmconfig). It MUST match the writer
// or AEAD authentication fails.
func llmChannelAAD(channelID string) []byte {
	return secretstore.AssociatedData("llm_channel", channelID, "api_key")
}

// decryptLLMChannel proves a managed LLM credential ciphertext is decryptable
// with the live keyset. It never returns or logs the plaintext.
func decryptLLMChannel(store *secretstore.TinkStore, channelID, keyID string, ciphertext []byte) error {
	pt, err := store.DecryptValue(
		secretstore.EncryptedValue{KeyID: keyID, Ciphertext: ciphertext},
		llmChannelAAD(channelID),
	)
	if err != nil {
		return fmt.Errorf("llm_channel %s: %w", channelID, err)
	}
	defer zero(pt)
	if len(pt) == 0 {
		return fmt.Errorf("llm_channel %s: decrypted credential is empty", channelID)
	}
	return nil
}

// decryptInboundWebhook proves a webhook source's two-level config envelope is
// decryptable: the outer Tink envelope and the nested current (and previous)
// signing secrets.
func decryptInboundWebhook(store *secretstore.TinkStore, config []byte) error {
	outer, err := store.Decrypt(config)
	if err != nil {
		return fmt.Errorf("webhook config envelope: %w", err)
	}
	defer zero(outer)
	var s inboundWebhookSecrets
	if err := json.Unmarshal(outer, &s); err != nil {
		// Static error: a wrapped json error would echo decrypted envelope bytes
		// into the report/audit ledger.
		return errors.New("webhook config envelope is not valid JSON")
	}
	if len(s.SecretCurrentEncrypted) == 0 {
		return errors.New("webhook config: no current secret in envelope")
	}
	cur, err := store.Decrypt(s.SecretCurrentEncrypted)
	if err != nil {
		return fmt.Errorf("webhook current secret: %w", err)
	}
	defer zero(cur)
	if len(cur) == 0 {
		return errors.New("webhook current secret decrypted empty")
	}
	if len(s.SecretPreviousEncrypted) > 0 {
		prev, err := store.Decrypt(s.SecretPreviousEncrypted)
		if err != nil {
			return fmt.Errorf("webhook previous secret: %w", err)
		}
		zero(prev)
	}
	return nil
}

// decryptInboundEmail proves an email source's two-level config envelope is
// decryptable: the outer Tink envelope and the nested IMAP password.
func decryptInboundEmail(store *secretstore.TinkStore, config []byte) error {
	outer, err := store.Decrypt(config)
	if err != nil {
		return fmt.Errorf("email config envelope: %w", err)
	}
	defer zero(outer)
	var s inboundEmailSecrets
	if err := json.Unmarshal(outer, &s); err != nil {
		// Static error: a wrapped json error would echo decrypted envelope bytes.
		return errors.New("email config envelope is not valid JSON")
	}
	if len(s.PasswordEncrypted) == 0 {
		return errors.New("email config: no password in envelope")
	}
	pw, err := store.Decrypt(s.PasswordEncrypted)
	if err != nil {
		return fmt.Errorf("email password: %w", err)
	}
	defer zero(pw)
	if len(pw) == 0 {
		return errors.New("email password decrypted empty")
	}
	return nil
}
