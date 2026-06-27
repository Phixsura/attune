// SPDX-License-Identifier: Apache-2.0

package secretstore

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func TestTinkStoreRoundTrip(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	if _, err := NewTinkStoreFromJSON(" "); err == nil {
		t.Fatal("expected empty keyset error")
	}
}

func TestTinkStoreNilStoreEncrypt(t *testing.T) {
	t.Parallel()

	var store *TinkStore
	_, err := store.EncryptValue([]byte("hello"), nil)
	if err == nil {
		t.Fatal("expected error from nil store encrypt")
	}
}

func TestTinkStoreNilStoreDecrypt(t *testing.T) {
	t.Parallel()

	var store *TinkStore
	_, err := store.DecryptValue(EncryptedValue{Ciphertext: []byte("ct")}, nil)
	if err == nil {
		t.Fatal("expected error from nil store decrypt")
	}
}

func TestTinkStoreNilPrimaryKeyID(t *testing.T) {
	t.Parallel()

	var store *TinkStore
	if id := store.PrimaryKeyID(); id != "" {
		t.Fatalf("expected empty primary key id from nil store, got %q", id)
	}
}

func TestTinkStoreNilKeys(t *testing.T) {
	t.Parallel()

	var store *TinkStore
	if keys := store.Keys(); keys != nil {
		t.Fatalf("expected nil keys from nil store, got %v", keys)
	}
}

func TestKeyIDFromCiphertextTooShort(t *testing.T) {
	t.Parallel()

	_, err := KeyIDFromCiphertext([]byte{0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for too-short ciphertext")
	}
	if !errors.Is(err, ErrCiphertextTooLow) {
		t.Fatalf("expected ErrCiphertextTooLow, got %v", err)
	}
}

func TestKeyIDFromCiphertextWrongPrefix(t *testing.T) {
	t.Parallel()

	_, err := KeyIDFromCiphertext([]byte{0x00, 0x01, 0x02, 0x03, 0x04})
	if err == nil {
		t.Fatal("expected error for wrong prefix")
	}
}

func TestKeyIDFromCiphertextValid(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	store := mustNewTinkStore(t, raw)
	value, err := store.EncryptValue([]byte("test"), nil)
	if err != nil {
		t.Fatal(err)
	}
	keyID, err := KeyIDFromCiphertext(value.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if keyID != store.PrimaryKeyID() {
		t.Fatalf("key id = %s, want %s", keyID, store.PrimaryKeyID())
	}
}

func TestAssociatedDataReturnsNilForEmpty(t *testing.T) {
	t.Parallel()

	if got := AssociatedData(); got != nil {
		t.Fatalf("expected nil for empty AssociatedData, got %v", got)
	}
}

func TestAssociatedDataJoinsParts(t *testing.T) {
	t.Parallel()

	got := AssociatedData("a", "b", "c")
	want := "a\x00b\x00c"
	if string(got) != want {
		t.Fatalf("AssociatedData = %q, want %q", got, want)
	}
}

func TestAssociatedDataSinglePart(t *testing.T) {
	t.Parallel()

	got := AssociatedData("only")
	if string(got) != "only" {
		t.Fatalf("AssociatedData = %q, want %q", got, "only")
	}
}

func TestRedactKeysetForLog(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	redacted := RedactKeysetForLog(raw)
	if redacted == "" {
		t.Fatal("expected non-empty redacted keyset")
	}
	if len(redacted) < 10 {
		t.Fatalf("redacted keyset too short: %q", redacted)
	}
	// Same input produces same hash.
	redacted2 := RedactKeysetForLog(raw)
	if redacted != redacted2 {
		t.Fatalf("RedactKeysetForLog not deterministic: %q != %q", redacted, redacted2)
	}
	// Whitespace-padded input produces same hash.
	redacted3 := RedactKeysetForLog("  " + raw + "  ")
	if redacted != redacted3 {
		t.Fatalf("RedactKeysetForLog should trim whitespace: %q != %q", redacted, redacted3)
	}
	// Different input produces different hash.
	raw2 := mustGeneratedKeyset(t)
	redacted4 := RedactKeysetForLog(raw2)
	if redacted == redacted4 {
		t.Fatal("RedactKeysetForLog should differ for different inputs")
	}
}

func TestDecryptValueTinkFailureWithLegacyFallback(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	legacyKey := bytes.Repeat([]byte{0x42}, 32)
	store, err := NewTinkStoreFromJSONWithLegacy(raw, hex.EncodeToString(legacyKey))
	if err != nil {
		t.Fatal(err)
	}

	// Encrypt with Tink, then try to decrypt with wrong AAD.
	// Since AAD is non-nil, legacy fallback is not attempted.
	value, err := store.EncryptValue([]byte("test"), AssociatedData("row", "1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DecryptValue(value, AssociatedData("row", "999"))
	if err == nil {
		t.Fatal("expected decrypt failure with wrong AAD")
	}
}

func TestDecryptValueLegacyFallbackNotTriggeredWithAAD(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	legacyKey := bytes.Repeat([]byte{0x42}, 32)
	legacy, err := newLegacyAESGCMStoreFromKey(legacyKey)
	if err != nil {
		t.Fatal(err)
	}
	legacyCt, err := legacy.Encrypt([]byte("legacy-data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTinkStoreFromJSONWithLegacy(raw, hex.EncodeToString(legacyKey))
	if err != nil {
		t.Fatal(err)
	}

	// With non-nil AAD, legacy fallback is NOT attempted even for legacy envelope.
	_, err = store.DecryptValue(EncryptedValue{Ciphertext: legacyCt}, AssociatedData("some", "aad"))
	if err == nil {
		t.Fatal("expected error: legacy fallback should not trigger with AAD")
	}
}

func TestEncryptDecryptViaSimpleInterface(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	store := mustNewTinkStore(t, raw)
	ct, err := store.Encrypt([]byte("simple-interface-test"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := store.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "simple-interface-test" {
		t.Fatalf("plaintext = %q", pt)
	}
}

func TestAddAES256GCMKeyToKeysetJSONMakePrimary(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	initial := mustNewTinkStore(t, raw)
	oldPrimary := initial.PrimaryKeyID()

	expanded, newID, err := AddAES256GCMKeyToKeysetJSON(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if newID == "" || newID == oldPrimary {
		t.Fatalf("new key id = %q, old primary = %q", newID, oldPrimary)
	}
	expandedStore := mustNewTinkStore(t, expanded)
	if expandedStore.PrimaryKeyID() != newID {
		t.Fatalf("primary after add with makePrimary: got %s, want %s", expandedStore.PrimaryKeyID(), newID)
	}
	if len(expandedStore.Keys()) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(expandedStore.Keys()))
	}
}

func TestAddAES256GCMKeyToKeysetJSONEmptyInput(t *testing.T) {
	t.Parallel()

	_, _, err := AddAES256GCMKeyToKeysetJSON("", false)
	if err == nil {
		t.Fatal("expected error for empty keyset input")
	}
}

func TestSetPrimaryKeysetJSONEmptyKeyID(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	_, err := SetPrimaryKeysetJSON(raw, "")
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
}

func TestSetPrimaryKeysetJSONInvalidKeyID(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	_, err := SetPrimaryKeysetJSON(raw, "not-a-number")
	if err == nil {
		t.Fatal("expected error for invalid key ID")
	}
}

func TestSetPrimaryKeysetJSONEmptyKeyset(t *testing.T) {
	t.Parallel()

	_, err := SetPrimaryKeysetJSON("", "12345")
	if err == nil {
		t.Fatal("expected error for empty keyset")
	}
}

func TestDeleteKeyFromKeysetJSONEmptyKeyID(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	_, err := DeleteKeyFromKeysetJSON(raw, "")
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
}

func TestDeleteKeyFromKeysetJSONEmptyKeyset(t *testing.T) {
	t.Parallel()

	_, err := DeleteKeyFromKeysetJSON("", "12345")
	if err == nil {
		t.Fatal("expected error for empty keyset")
	}
}

func TestKeysReturnsCopy(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	store := mustNewTinkStore(t, raw)
	keys1 := store.Keys()
	keys2 := store.Keys()

	if len(keys1) != len(keys2) {
		t.Fatal("Keys() returned different lengths")
	}
	// Mutating returned slice should not affect store.
	keys1[0].KeyID = "mutated"
	keys3 := store.Keys()
	if keys3[0].KeyID == "mutated" {
		t.Fatal("Keys() did not return a copy")
	}
}

func TestMetadataFromKeysetNil(t *testing.T) {
	t.Parallel()

	if got := metadataFromKeyset(nil); got != nil {
		t.Fatalf("expected nil for nil keyset, got %v", got)
	}
}

func TestTinkStoreRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := NewTinkStoreFromJSON("{not valid json}")
	if err == nil {
		t.Fatal("expected error for invalid JSON keyset")
	}
}

func TestNewTinkStoreFromJSONWithLegacyInvalidLegacyKey(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	_, err := NewTinkStoreFromJSONWithLegacy(raw, "not-a-valid-hex-or-base64-key-at-all")
	if err == nil {
		t.Fatal("expected error for invalid legacy key")
	}
}

func TestEncryptValueKeyIDMatchesPrimary(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	store := mustNewTinkStore(t, raw)
	value, err := store.EncryptValue([]byte("data"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if value.KeyID != store.PrimaryKeyID() {
		t.Fatalf("encrypted value key ID = %s, want primary %s", value.KeyID, store.PrimaryKeyID())
	}
}

func TestRotatedKeysetDecryptsOldCiphertext(t *testing.T) {
	t.Parallel()

	raw := mustGeneratedKeyset(t)
	store1 := mustNewTinkStore(t, raw)
	ct, err := store1.EncryptValue([]byte("before-rotation"), nil)
	if err != nil {
		t.Fatal(err)
	}

	// Add new key and make it primary.
	rotated, _, err := AddAES256GCMKeyToKeysetJSON(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	store2 := mustNewTinkStore(t, rotated)

	// Old ciphertext should still decrypt with new keyset.
	pt, err := store2.DecryptValue(ct, nil)
	if err != nil {
		t.Fatalf("decrypt old ciphertext after rotation err = %v", err)
	}
	if string(pt) != "before-rotation" {
		t.Fatalf("plaintext = %q", pt)
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
