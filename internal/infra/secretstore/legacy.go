// SPDX-License-Identifier: Apache-2.0

package secretstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	LegacyInboundKeyID     = "legacy-inbound-master-key"
	legacyEnvelopeVersion  = 0x01
	legacyMasterKeyID      = 0x00
	legacyNonceLen         = 12
	legacyHeaderLen        = 2
	legacyTagLen           = 16
	legacyMaxPlaintextLen  = 1 << 20
	legacyMinCiphertextLen = legacyHeaderLen + legacyNonceLen + legacyTagLen
)

type legacyAESGCMStore struct {
	aead cipher.AEAD
}

func newLegacyAESGCMStore(raw string) (*legacyAESGCMStore, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	key, err := DecodeLegacyInboundMasterKey(trimmed)
	if err != nil {
		return nil, err
	}
	return newLegacyAESGCMStoreFromKey(key)
}

// DecodeLegacyInboundMasterKey accepts the old inbound master key encoding:
// either 32 raw bytes as hex or 32 raw bytes as standard base64.
func DecodeLegacyInboundMasterKey(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if key, err := hex.DecodeString(trimmed); err == nil && len(key) == 32 {
		return key, nil
	}
	if key, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(key) == 32 {
		return key, nil
	}
	return nil, fmt.Errorf(
		"secretstore: legacy_inbound_master_key must decode to exactly 32 bytes as hex or base64; got %d-byte input",
		len(trimmed),
	)
}

func newLegacyAESGCMStoreFromKey(key []byte) (*legacyAESGCMStore, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secretstore: legacy inbound master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretstore: legacy aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretstore: legacy cipher.NewGCM: %w", err)
	}
	return ptrext.Of(legacyAESGCMStore{aead: aead}), nil
}

func (s *legacyAESGCMStore) Encrypt(plaintext []byte) ([]byte, error) {
	if len(plaintext) > legacyMaxPlaintextLen {
		return nil, fmt.Errorf("secretstore: legacy plaintext too large (%d > %d)",
			len(plaintext), legacyMaxPlaintextLen)
	}
	nonce := make([]byte, legacyNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secretstore: legacy nonce read: %w", err)
	}
	out := make([]byte, legacyHeaderLen+legacyNonceLen, legacyHeaderLen+legacyNonceLen+len(plaintext)+s.aead.Overhead())
	out[0] = legacyEnvelopeVersion
	out[1] = legacyMasterKeyID
	copy(out[legacyHeaderLen:], nonce)
	return s.aead.Seal(out, nonce, plaintext, nil), nil
}

func (s *legacyAESGCMStore) Decrypt(ciphertext []byte) ([]byte, error) {
	if s == nil || s.aead == nil {
		return nil, errors.New("secretstore: nil legacy inbound store")
	}
	if len(ciphertext) < legacyHeaderLen+legacyNonceLen+s.aead.Overhead() {
		return nil, errors.New("secretstore: legacy ciphertext too short")
	}
	if ciphertext[0] != legacyEnvelopeVersion {
		return nil, fmt.Errorf("secretstore: legacy unknown envelope version %#x", ciphertext[0])
	}
	if ciphertext[1] != legacyMasterKeyID {
		return nil, fmt.Errorf("secretstore: legacy unknown key_id %#x", ciphertext[1])
	}
	nonce := ciphertext[legacyHeaderLen : legacyHeaderLen+legacyNonceLen]
	sealed := ciphertext[legacyHeaderLen+legacyNonceLen:]
	return s.aead.Open(nil, nonce, sealed, nil)
}

// IsLegacyInboundEnvelope reports whether ciphertext has the old inbound
// envelope header. Tink decryption is still attempted first; this predicate is
// used only for fallback/migration paths.
func IsLegacyInboundEnvelope(ciphertext []byte) bool {
	return len(ciphertext) >= legacyMinCiphertextLen &&
		ciphertext[0] == legacyEnvelopeVersion &&
		ciphertext[1] == legacyMasterKeyID
}
