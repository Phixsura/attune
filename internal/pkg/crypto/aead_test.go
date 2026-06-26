// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"testing"
)

func TestAEAD_RoundTrip(t *testing.T) {
	aead, err := NewAEAD([]byte("test-key-for-encryption"))
	if err != nil {
		t.Fatalf("NewAEAD: %v", err)
	}

	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"empty", []byte{}},
		{"short", []byte("hello")},
		{"json", []byte(`{"state":"abc123","verifier":"xyz789"}`)},
		{"long", make([]byte, 1024)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := aead.Encrypt(tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}

			decrypted, err := aead.Decrypt(encrypted)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}

			if string(decrypted) != string(tt.plaintext) {
				t.Errorf("got %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestAEAD_UniqueNonce(t *testing.T) {
	aead, err := NewAEAD([]byte("test-key"))
	if err != nil {
		t.Fatalf("NewAEAD: %v", err)
	}

	plaintext := []byte("same plaintext")
	enc1, _ := aead.Encrypt(plaintext)
	enc2, _ := aead.Encrypt(plaintext)

	if enc1 == enc2 {
		t.Error("encrypting same plaintext should produce different ciphertext (unique nonce)")
	}
}

func TestAEAD_TamperedCiphertext(t *testing.T) {
	aead, err := NewAEAD([]byte("test-key"))
	if err != nil {
		t.Fatalf("NewAEAD: %v", err)
	}

	encrypted, _ := aead.Encrypt([]byte("secret"))

	// Tamper with the ciphertext
	tampered := encrypted[:len(encrypted)-2] + "XX"
	_, err = aead.Decrypt(tampered)
	if err == nil {
		t.Error("expected error for tampered ciphertext")
	}
}

func TestAEAD_InvalidBase64(t *testing.T) {
	aead, err := NewAEAD([]byte("test-key"))
	if err != nil {
		t.Fatalf("NewAEAD: %v", err)
	}

	_, err = aead.Decrypt("not-valid-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestAEAD_EmptyKey(t *testing.T) {
	_, err := NewAEAD([]byte{})
	if err == nil {
		t.Error("expected error for empty key")
	}
}

func TestAEAD_CiphertextTooShort(t *testing.T) {
	t.Parallel()
	aead, err := NewAEAD([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = aead.Decrypt("YQ") // "a" in base64url, way too short
	if err == nil {
		t.Fatal("expected error for short ciphertext")
	}
}

func TestAEAD_NilKey(t *testing.T) {
	t.Parallel()
	_, err := NewAEAD(nil)
	if err == nil {
		t.Fatal("expected error for nil key")
	}
}
