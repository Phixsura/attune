// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// SecretStore — envelope encryption. v1 implementation is AES-GCM-256.
// v2 may swap KMS / Vault behind the same interface; #94 tracks rotation.
type SecretStore interface {
	Encrypt(plaintext []byte) (ciphertext []byte, err error)
	Decrypt(ciphertext []byte) (plaintext []byte, err error)
}

// Envelope layout (see spec §Master key):
//
//	| 1 byte version | 1 byte key_id | 12 bytes nonce | ciphertext | 16 bytes auth tag |
//
// The key_id byte is reserved for #94 (master key rotation): a v0.3 reader
// can identify ciphertext from a future v0.3.x writer that introduces a
// second master key without a one-shot re-encrypt of the whole table.
const (
	envelopeVersion = 0x01
	masterKeyID     = 0x00
	nonceLen        = 12
	headerLen       = 2 // version + key_id
)

type aesGCMStore struct {
	aead cipher.AEAD
}

// NewAESGCMSecretStore — constructs a SecretStore backed by AES-GCM-256.
// Key MUST be exactly 32 bytes (AES-256).
func NewAESGCMSecretStore(key []byte) (SecretStore, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("inbound: master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("inbound: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("inbound: cipher.NewGCM: %w", err)
	}
	return ptrext.Of(aesGCMStore{aead: aead}), nil
}

// maxPlaintextLen caps the per-call Encrypt input. Inbound secrets are
// HMAC keys / IMAP credentials / small JSON config envelopes — none of
// these ever approach 1 MiB. Cap explicitly so the `headerLen+nonceLen+
// len(plaintext)+overhead` allocation cannot overflow int on 32-bit
// builds (CodeQL #29).
const maxPlaintextLen = 1 << 20

// Encrypt — writes version + key_id + nonce + ciphertext + tag.
func (s *aesGCMStore) Encrypt(plaintext []byte) ([]byte, error) {
	if len(plaintext) > maxPlaintextLen {
		return nil, fmt.Errorf("inbound: plaintext too large (%d > %d)",
			len(plaintext), maxPlaintextLen)
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("inbound: nonce read: %w", err)
	}
	out := make([]byte, headerLen+nonceLen, headerLen+nonceLen+len(plaintext)+s.aead.Overhead())
	out[0] = envelopeVersion
	out[1] = masterKeyID
	copy(out[headerLen:], nonce)
	return s.aead.Seal(out, nonce, plaintext, nil), nil
}

// Decrypt — verifies header bytes, splits nonce + sealed, opens.
func (s *aesGCMStore) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < headerLen+nonceLen+s.aead.Overhead() {
		return nil, errors.New("inbound: ciphertext too short")
	}
	if ciphertext[0] != envelopeVersion {
		return nil, fmt.Errorf("inbound: unknown envelope version %#x", ciphertext[0])
	}
	if ciphertext[1] != masterKeyID {
		// #94 will introduce multi-key lookup; for now reject.
		return nil, fmt.Errorf("inbound: unknown key_id %#x", ciphertext[1])
	}
	nonce := ciphertext[headerLen : headerLen+nonceLen]
	sealed := ciphertext[headerLen+nonceLen:]
	return s.aead.Open(nil, nonce, sealed, nil)
}
