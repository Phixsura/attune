// SPDX-License-Identifier: Apache-2.0

package secretstore

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestTinkStoreRoundTrip(t *testing.T) {
	raw, err := GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	aad := AssociatedData("llm_channel", "channel-1", "api_key")
	value, err := store.EncryptValue([]byte("secret-api-key"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if value.KeyID == "" {
		t.Fatal("expected key id on encrypted value")
	}
	if !bytes.HasPrefix(value.Ciphertext, []byte{tinkPrefix}) {
		t.Fatalf("ciphertext should carry a Tink key prefix, got %#x", value.Ciphertext[:1])
	}
	extracted, err := KeyIDFromCiphertext(value.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if extracted != value.KeyID {
		t.Fatalf("ciphertext key id = %s; want %s", extracted, value.KeyID)
	}
	plaintext, err := store.DecryptValue(value, aad)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "secret-api-key" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestTinkStoreAssociatedDataMismatchFails(t *testing.T) {
	raw, err := GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.EncryptValue([]byte("secret-api-key"), AssociatedData("row", "1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecryptValue(value, AssociatedData("row", "2")); err == nil {
		t.Fatal("expected associated data mismatch to fail")
	}
}

func TestTinkStoreInboundCompatibleMethods(t *testing.T) {
	raw, err := GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := store.Encrypt([]byte("inbound-secret"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := store.Decrypt(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "inbound-secret" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestTinkStoreDecryptsLegacyInboundEnvelope(t *testing.T) {
	raw, err := GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	legacyKey := bytes.Repeat([]byte{0x42}, 32)
	legacy, err := newLegacyAESGCMStoreFromKey(legacyKey)
	if err != nil {
		t.Fatal(err)
	}
	legacyCiphertext, err := legacy.Encrypt([]byte("legacy-inbound-secret"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTinkStoreFromJSONWithLegacy(raw, hex.EncodeToString(legacyKey))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := store.Decrypt(legacyCiphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "legacy-inbound-secret" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestTinkStoreRejectsLegacyInboundEnvelopeWithoutLegacyKey(t *testing.T) {
	raw, err := GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := newLegacyAESGCMStoreFromKey(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	legacyCiphertext, err := legacy.Encrypt([]byte("legacy-inbound-secret"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Decrypt(legacyCiphertext); err == nil {
		t.Fatal("expected legacy ciphertext to fail without legacy key")
	}
}

func TestTinkStoreMetadata(t *testing.T) {
	raw, err := GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	keys := store.Keys()
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d; want 1", len(keys))
	}
	if keys[0].KeyID != store.PrimaryKeyID() || !keys[0].Primary {
		t.Fatalf("primary metadata mismatch: %+v primary=%s", keys[0], store.PrimaryKeyID())
	}
	if keys[0].FingerprintSHA256 == "" {
		t.Fatal("expected fingerprint")
	}
}

func TestKeysetJSONAddSetPrimaryAndDelete(t *testing.T) {
	raw := mustGeneratedKeyset(t)
	initial := mustNewTinkStore(t, raw)
	oldPrimary := initial.PrimaryKeyID()
	expanded, newID, err := AddAES256GCMKeyToKeysetJSON(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	if newID == "" || newID == oldPrimary {
		t.Fatalf("new key id = %q old primary=%q", newID, oldPrimary)
	}
	assertKeysetState(t, expanded, oldPrimary, 2)

	promoted, err := SetPrimaryKeysetJSON(expanded, newID)
	if err != nil {
		t.Fatal(err)
	}
	assertKeysetState(t, promoted, newID, 2)

	trimmed, err := DeleteKeyFromKeysetJSON(promoted, oldPrimary)
	if err != nil {
		t.Fatal(err)
	}
	assertKeysetState(t, trimmed, newID, 1)
}

func TestDeleteKeysetJSONRejectsPrimary(t *testing.T) {
	raw, err := GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteKeyFromKeysetJSON(raw, store.PrimaryKeyID()); err == nil {
		t.Fatal("expected primary key delete to fail")
	}
}

func TestTinkStoreRejectsEmptyKeyset(t *testing.T) {
	if _, err := NewTinkStoreFromJSON(" "); err == nil {
		t.Fatal("expected empty keyset error")
	}
}

func mustGeneratedKeyset(t *testing.T) string {
	t.Helper()
	raw, err := GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func mustNewTinkStore(t *testing.T, raw string) *TinkStore {
	t.Helper()
	store, err := NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func assertKeysetState(t *testing.T, raw, wantPrimary string, wantKeys int) {
	t.Helper()
	store := mustNewTinkStore(t, raw)
	if store.PrimaryKeyID() != wantPrimary {
		t.Fatalf("primary = %s; want %s", store.PrimaryKeyID(), wantPrimary)
	}
	if got := len(store.Keys()); got != wantKeys {
		t.Fatalf("len(keys) = %d; want %d", got, wantKeys)
	}
}
