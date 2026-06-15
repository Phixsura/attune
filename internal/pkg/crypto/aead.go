// SPDX-License-Identifier: Apache-2.0

// Package crypto provides cryptographic utilities.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// AEAD provides authenticated encryption using AES-256-GCM.
// It is safe for concurrent use.
type AEAD struct {
	aead cipher.AEAD
}

// NewAEAD creates an AEAD encryptor from a key of any length.
// The key is hashed with SHA-256 to produce a 32-byte AES key.
func NewAEAD(key []byte) (*AEAD, error) {
	if len(key) == 0 {
		return nil, errors.New("key cannot be empty")
	}

	// Derive 32-byte key using SHA-256
	hash := sha256.Sum256(key)

	block, err := aes.NewCipher(hash[:])
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	return ptrext.Of(AEAD{aead: aead}), nil
}

// Encrypt encrypts plaintext and returns base64url-encoded ciphertext.
// The nonce is prepended to the ciphertext.
func (a *AEAD) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	// Seal appends the ciphertext to the nonce
	ciphertext := a.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64url-encoded ciphertext.
func (a *AEAD) Decrypt(encoded string) ([]byte, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	nonceSize := a.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := a.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}
