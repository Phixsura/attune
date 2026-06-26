// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"encoding/json"
	"testing"

	"github.com/Phixsura/attune/internal/infra/secretstore"
)

func newTestStore(t *testing.T) *secretstore.TinkStore {
	t.Helper()
	raw, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatalf("generate keyset: %v", err)
	}
	store, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

const testChannelID = "11111111-1111-1111-1111-111111111111"

func TestDecryptLLMChannel_OK(t *testing.T) {
	store := newTestStore(t)
	val, err := store.EncryptValue([]byte("sk-test-key"),
		secretstore.AssociatedData("llm_channel", testChannelID, "api_key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := decryptLLMChannel(store, testChannelID, val.KeyID, val.Ciphertext); err != nil {
		t.Fatalf("expected decrypt to succeed, got %v", err)
	}
}

func TestDecryptLLMChannel_WrongKeysetFailsLoudly(t *testing.T) {
	writer := newTestStore(t)
	reader := newTestStore(t) // a different keyset cannot decrypt the writer's ciphertext
	val, err := writer.EncryptValue([]byte("sk-test-key"),
		secretstore.AssociatedData("llm_channel", testChannelID, "api_key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := decryptLLMChannel(reader, testChannelID, val.KeyID, val.Ciphertext); err == nil {
		t.Fatal("expected decrypt to fail with a mismatched keyset, got nil")
	}
}

func makeWebhookEnvelope(t *testing.T, store *secretstore.TinkStore, withPrevious bool) []byte {
	t.Helper()
	cur, err := store.Encrypt([]byte("current-webhook-signing-secret-32b"))
	if err != nil {
		t.Fatal(err)
	}
	env := inboundWebhookSecrets{SecretCurrentEncrypted: cur}
	if withPrevious {
		prev, err := store.Encrypt([]byte("previous-webhook-signing-secret-32"))
		if err != nil {
			t.Fatal(err)
		}
		env.SecretPreviousEncrypted = prev
	}
	plain, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := store.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	return outer
}

func TestDecryptInboundWebhook_OK(t *testing.T) {
	store := newTestStore(t)
	if err := decryptInboundWebhook(store, makeWebhookEnvelope(t, store, false)); err != nil {
		t.Fatalf("expected webhook decrypt to succeed, got %v", err)
	}
}

func TestDecryptInboundWebhook_WithPrevious(t *testing.T) {
	store := newTestStore(t)
	if err := decryptInboundWebhook(store, makeWebhookEnvelope(t, store, true)); err != nil {
		t.Fatalf("expected webhook decrypt (with previous secret) to succeed, got %v", err)
	}
}

func TestDecryptInboundWebhook_WrongKeysetFailsLoudly(t *testing.T) {
	writer := newTestStore(t)
	reader := newTestStore(t)
	if err := decryptInboundWebhook(reader, makeWebhookEnvelope(t, writer, false)); err == nil {
		t.Fatal("expected webhook decrypt to fail with a mismatched keyset, got nil")
	}
}

func TestDecryptInboundEmail_OK(t *testing.T) {
	store := newTestStore(t)
	pw, err := store.Encrypt([]byte("imap-password"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(inboundEmailSecrets{PasswordEncrypted: pw})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := store.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := decryptInboundEmail(store, outer); err != nil {
		t.Fatalf("expected email decrypt to succeed, got %v", err)
	}
}

func TestDecryptInboundEmail_WrongKeyset(t *testing.T) {
	t.Parallel()
	writer := newTestStore(t)
	reader := newTestStore(t)
	pw, err := writer.Encrypt([]byte("imap-password"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(inboundEmailSecrets{PasswordEncrypted: pw})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := writer.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := decryptInboundEmail(reader, outer); err == nil {
		t.Fatal("expected email decrypt to fail with mismatched keyset")
	}
}

func TestDecryptInboundWebhook_NoCurrentSecret(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	// Create an envelope with empty current secret
	env := inboundWebhookSecrets{SecretCurrentEncrypted: nil}
	plain, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := store.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	err = decryptInboundWebhook(store, outer)
	if err == nil {
		t.Fatal("expected error for empty current secret")
	}
}

func TestDecryptInboundEmail_NoPassword(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	// Create an envelope with empty password
	env := inboundEmailSecrets{PasswordEncrypted: nil}
	plain, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := store.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	err = decryptInboundEmail(store, outer)
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestDecryptInboundWebhook_BadInnerJSON(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	// Encrypt something that is NOT valid JSON for the inner envelope
	outer, err := store.Encrypt([]byte("not-valid-json"))
	if err != nil {
		t.Fatal(err)
	}
	err = decryptInboundWebhook(store, outer)
	if err == nil {
		t.Fatal("expected error for bad inner JSON")
	}
}

func TestDecryptInboundEmail_BadInnerJSON(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	outer, err := store.Encrypt([]byte("not-valid-json"))
	if err != nil {
		t.Fatal(err)
	}
	err = decryptInboundEmail(store, outer)
	if err == nil {
		t.Fatal("expected error for bad inner JSON")
	}
}

func TestDecryptInboundWebhook_BadCurrentSecret(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	// Create envelope with a current_secret that is not valid ciphertext
	env := inboundWebhookSecrets{
		SecretCurrentEncrypted: []byte("not-valid-ciphertext"),
	}
	plain, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := store.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	err = decryptInboundWebhook(store, outer)
	if err == nil {
		t.Fatal("expected error for bad current secret ciphertext")
	}
}

func TestDecryptInboundWebhook_BadPreviousSecret(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	cur, err := store.Encrypt([]byte("current-webhook-signing-secret-32b"))
	if err != nil {
		t.Fatal(err)
	}
	env := inboundWebhookSecrets{
		SecretCurrentEncrypted:  cur,
		SecretPreviousEncrypted: []byte("not-valid-ciphertext"),
	}
	plain, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := store.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	err = decryptInboundWebhook(store, outer)
	if err == nil {
		t.Fatal("expected error for bad previous secret ciphertext")
	}
}

func TestDecryptInboundEmail_BadPasswordCiphertext(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	env := inboundEmailSecrets{
		PasswordEncrypted: []byte("not-valid-ciphertext"),
	}
	plain, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := store.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	err = decryptInboundEmail(store, outer)
	if err == nil {
		t.Fatal("expected error for bad password ciphertext")
	}
}
