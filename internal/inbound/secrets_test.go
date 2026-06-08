// SPDX-License-Identifier: Apache-2.0

package inbound_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
)

func TestAESGCMSecretStore_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	s, err := inbound.NewAESGCMSecretStore(key)
	if err != nil {
		t.Fatalf("NewAESGCMSecretStore: %v", err)
	}
	plaintext := []byte("hello inbound")
	ct, err := s.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// version(1) + key_id(1) + nonce(12) + plaintext(N) + tag(16)
	wantLen := 1 + 1 + 12 + len(plaintext) + 16
	if len(ct) != wantLen {
		t.Errorf("envelope length = %d; want %d", len(ct), wantLen)
	}
	if ct[0] != 0x01 {
		t.Errorf("version byte = %#x; want 0x01", ct[0])
	}
	if ct[1] != 0x00 {
		t.Errorf("key_id byte = %#x; want 0x00", ct[1])
	}
	out, err := s.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(out, plaintext) {
		t.Errorf("Decrypt round-trip mismatch")
	}
}

func TestAESGCMSecretStore_WrongKeyFails(t *testing.T) {
	a, _ := inbound.NewAESGCMSecretStore(bytes.Repeat([]byte{0x01}, 32))
	b, _ := inbound.NewAESGCMSecretStore(bytes.Repeat([]byte{0x02}, 32))
	ct, _ := a.Encrypt([]byte("hi"))
	if _, err := b.Decrypt(ct); err == nil {
		t.Error("expected Decrypt with wrong key to fail")
	}
}

func TestAESGCMSecretStore_RejectsShortKey(t *testing.T) {
	if _, err := inbound.NewAESGCMSecretStore(make([]byte, 16)); err == nil {
		t.Error("expected NewAESGCMSecretStore with 16-byte key to fail")
	}
}

func TestAESGCMSecretStore_RejectsCorruptVersion(t *testing.T) {
	s, _ := inbound.NewAESGCMSecretStore(bytes.Repeat([]byte{0x01}, 32))
	ct, _ := s.Encrypt([]byte("x"))
	ct[0] = 0xFF
	if _, err := s.Decrypt(ct); err == nil {
		t.Error("expected version-byte tampering to fail")
	}
}

func TestAESGCMSecretStore_RejectsTooShort(t *testing.T) {
	s, _ := inbound.NewAESGCMSecretStore(bytes.Repeat([]byte{0x01}, 32))
	if _, err := s.Decrypt([]byte{0x01, 0x00}); err == nil {
		t.Error("expected too-short ciphertext to fail")
	}
}
