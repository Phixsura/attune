// SPDX-License-Identifier: Apache-2.0

// Package secretstore owns runtime secret encryption for attune.
//
// The implementation is intentionally thin over Google Tink: attune owns the
// YAML/DB/key-registry lifecycle, while Tink owns AEAD keysets, primary-key
// selection, ciphertext prefixes, and old-key decryption during rotation.
package secretstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tinkaead "github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	tinkpb "github.com/tink-crypto/tink-go/v2/proto/tink_go_proto"
	"github.com/tink-crypto/tink-go/v2/tink"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	fingerprintBytes = 16
	keyIDPrefixLen   = 5
	tinkPrefix       = 0x01
)

var (
	ErrEmptyKeyset      = errors.New("secretstore: tink_keyset is required")
	ErrCiphertextTooLow = errors.New("secretstore: ciphertext too short for Tink key prefix")
)

// EncryptedValue is the DB-friendly representation returned by EncryptValue.
type EncryptedValue struct {
	KeyID      string
	Ciphertext []byte
}

// KeyMetadata is safe to persist in secret_key_registry. Fingerprint is a
// truncated SHA-256 over key material and key metadata; it is useful for drift
// detection and does not disclose the key.
type KeyMetadata struct {
	KeyID              string
	Primary            bool
	Status             string
	TypeURL            string
	OutputPrefixType   string
	KeyMaterialType    string
	FingerprintSHA256  string
	FingerprintVersion int
}

// TinkStore encrypts/decrypts small runtime secrets with a shared Tink AEAD
// keyset. It also satisfies inbound.SecretStore through Encrypt/Decrypt.
type TinkStore struct {
	primitive tink.AEAD
	primaryID string
	keys      []KeyMetadata
	legacy    *legacyAESGCMStore
}

// NewTinkStoreFromJSON reads a cleartext Tink keyset JSON string. The config
// file containing it is the private runtime config; use
// `attune secrets generate-keyset` to create the initial value.
func NewTinkStoreFromJSON(raw string) (*TinkStore, error) {
	return NewTinkStoreFromJSONWithLegacy(raw, "")
}

// NewTinkStoreFromJSONWithLegacy reads a Tink keyset and optionally enables a
// read-only fallback for the pre-keyring inbound AES-GCM envelope. New
// ciphertexts are always written with Tink; the fallback exists only so old
// inbound rows can boot and be migrated with `attune secrets reencrypt --apply`.
func NewTinkStoreFromJSONWithLegacy(raw string, legacyInboundMasterKey string) (*TinkStore, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, ErrEmptyKeyset
	}
	handle, err := insecurecleartextkeyset.Read(keyset.NewJSONReader(strings.NewReader(trimmed)))
	if err != nil {
		return nil, fmt.Errorf("secretstore: read Tink keyset: %w", err)
	}
	primitive, err := tinkaead.New(handle)
	if err != nil {
		return nil, fmt.Errorf("secretstore: build AEAD primitive: %w", err)
	}
	primary, err := handle.Primary()
	if err != nil {
		return nil, fmt.Errorf("secretstore: primary key: %w", err)
	}
	legacy, err := newLegacyAESGCMStore(legacyInboundMasterKey)
	if err != nil {
		return nil, err
	}
	keys := metadataFromKeyset(insecurecleartextkeyset.KeysetMaterial(handle))
	if err := validateKeyPrefixes(keys); err != nil {
		return nil, err
	}
	return ptrext.Of(TinkStore{
		primitive: primitive,
		primaryID: keyIDString(primary.KeyID()),
		keys:      keys,
		legacy:    legacy,
	}), nil
}

// GenerateAES256GCMKeysetJSON returns an operator-pasteable cleartext Tink
// keyset. It is intended for private config generation, not logs.
func GenerateAES256GCMKeysetJSON() (string, error) {
	handle, err := keyset.NewHandle(tinkaead.AES256GCMKeyTemplate())
	if err != nil {
		return "", fmt.Errorf("secretstore: generate keyset: %w", err)
	}
	return writeKeysetJSON(handle)
}

// AddAES256GCMKeyToKeysetJSON adds a fresh enabled AES-256-GCM key to an
// existing cleartext Tink keyset and returns the updated JSON. The new key is
// non-primary unless makePrimary is true.
func AddAES256GCMKeyToKeysetJSON(raw string, makePrimary bool) (string, string, error) {
	handle, err := readKeysetJSON(raw)
	if err != nil {
		return "", "", err
	}
	manager := keyset.NewManagerFromHandle(handle)
	keyID, err := manager.Add(tinkaead.AES256GCMKeyTemplate())
	if err != nil {
		return "", "", fmt.Errorf("secretstore: add key: %w", err)
	}
	if makePrimary {
		if err := manager.SetPrimary(keyID); err != nil {
			return "", "", fmt.Errorf("secretstore: set new primary: %w", err)
		}
	}
	updated, err := manager.Handle()
	if err != nil {
		return "", "", fmt.Errorf("secretstore: build updated keyset: %w", err)
	}
	out, err := writeKeysetJSON(updated)
	if err != nil {
		return "", "", err
	}
	return out, keyIDString(keyID), nil
}

// SetPrimaryKeysetJSON returns a copy of raw with keyID set as Tink primary.
func SetPrimaryKeysetJSON(raw, keyID string) (string, error) {
	id, err := parseKeyID(keyID)
	if err != nil {
		return "", err
	}
	handle, err := readKeysetJSON(raw)
	if err != nil {
		return "", err
	}
	manager := keyset.NewManagerFromHandle(handle)
	if err := manager.SetPrimary(id); err != nil {
		return "", fmt.Errorf("secretstore: set primary: %w", err)
	}
	updated, err := manager.Handle()
	if err != nil {
		return "", fmt.Errorf("secretstore: build updated keyset: %w", err)
	}
	return writeKeysetJSON(updated)
}

// DeleteKeyFromKeysetJSON returns a copy of raw with keyID removed. Tink rejects
// deletion of the primary key.
func DeleteKeyFromKeysetJSON(raw, keyID string) (string, error) {
	id, err := parseKeyID(keyID)
	if err != nil {
		return "", err
	}
	handle, err := readKeysetJSON(raw)
	if err != nil {
		return "", err
	}
	manager := keyset.NewManagerFromHandle(handle)
	if err := manager.Delete(id); err != nil {
		return "", fmt.Errorf("secretstore: delete key: %w", err)
	}
	updated, err := manager.Handle()
	if err != nil {
		return "", fmt.Errorf("secretstore: build updated keyset: %w", err)
	}
	return writeKeysetJSON(updated)
}

// PrimaryKeyID is the key id used for new encryptions.
func (s *TinkStore) PrimaryKeyID() string {
	if s == nil {
		return ""
	}
	return s.primaryID
}

// Keys returns metadata for every key in the local keyset.
func (s *TinkStore) Keys() []KeyMetadata {
	if s == nil {
		return nil
	}
	out := make([]KeyMetadata, len(s.keys))
	copy(out, s.keys)
	return out
}

// Encrypt satisfies inbound.SecretStore with empty associated data.
func (s *TinkStore) Encrypt(plaintext []byte) ([]byte, error) {
	value, err := s.EncryptValue(plaintext, nil)
	if err != nil {
		return nil, err
	}
	return value.Ciphertext, nil
}

// Decrypt satisfies inbound.SecretStore with empty associated data.
func (s *TinkStore) Decrypt(ciphertext []byte) ([]byte, error) {
	return s.DecryptValue(EncryptedValue{Ciphertext: ciphertext}, nil)
}

// EncryptValue encrypts plaintext with caller-provided associated data. The
// associated data should bind the ciphertext to its table/row/use.
func (s *TinkStore) EncryptValue(plaintext, aad []byte) (EncryptedValue, error) {
	if s == nil || s.primitive == nil {
		return EncryptedValue{}, errors.New("secretstore: nil Tink store")
	}
	ciphertext, err := s.primitive.Encrypt(plaintext, aad)
	if err != nil {
		return EncryptedValue{}, fmt.Errorf("secretstore: encrypt: %w", err)
	}
	return EncryptedValue{KeyID: s.primaryID, Ciphertext: ciphertext}, nil
}

// DecryptValue decrypts ciphertext with caller-provided associated data.
func (s *TinkStore) DecryptValue(value EncryptedValue, aad []byte) ([]byte, error) {
	if s == nil || s.primitive == nil {
		return nil, errors.New("secretstore: nil Tink store")
	}
	plaintext, err := s.primitive.Decrypt(value.Ciphertext, aad)
	if err != nil {
		if aad == nil && s.legacy != nil && IsLegacyInboundEnvelope(value.Ciphertext) {
			return s.legacy.Decrypt(value.Ciphertext)
		}
		return nil, fmt.Errorf("secretstore: decrypt key_id=%s: %w", value.KeyID, err)
	}
	return plaintext, nil
}

// KeyIDFromCiphertext extracts the Tink key id prefix from a TINK-prefixed
// AEAD ciphertext. It is best-effort metadata; DecryptValue remains the source
// of truth because Tink validates authenticity.
func KeyIDFromCiphertext(ciphertext []byte) (string, error) {
	if len(ciphertext) < keyIDPrefixLen {
		return "", ErrCiphertextTooLow
	}
	if ciphertext[0] != tinkPrefix {
		return "", fmt.Errorf("secretstore: unsupported Tink output prefix %#x", ciphertext[0])
	}
	return keyIDString(binary.BigEndian.Uint32(ciphertext[1:keyIDPrefixLen])), nil
}

func metadataFromKeyset(ks *tinkpb.Keyset) []KeyMetadata {
	if ks == nil {
		return nil
	}
	out := make([]KeyMetadata, 0, len(ks.GetKey()))
	primaryID := keyIDString(ks.GetPrimaryKeyId())
	for _, k := range ks.GetKey() {
		keyID := keyIDString(k.GetKeyId())
		data := k.GetKeyData()
		out = append(out, KeyMetadata{
			KeyID:              keyID,
			Primary:            keyID == primaryID,
			Status:             strings.TrimPrefix(k.GetStatus().String(), "KEY_STATUS_TYPE_"),
			TypeURL:            data.GetTypeUrl(),
			OutputPrefixType:   strings.TrimPrefix(k.GetOutputPrefixType().String(), "OUTPUT_PREFIX_TYPE_"),
			KeyMaterialType:    strings.TrimPrefix(data.GetKeyMaterialType().String(), "KEY_MATERIAL_TYPE_"),
			FingerprintSHA256:  fingerprintKey(k),
			FingerprintVersion: 1,
		})
	}
	return out
}

func validateKeyPrefixes(keys []KeyMetadata) error {
	for _, key := range keys {
		if key.OutputPrefixType != "TINK" {
			return fmt.Errorf("secretstore: key_id=%s output prefix %q is unsupported; use TINK-prefixed AEAD keys",
				key.KeyID, key.OutputPrefixType)
		}
	}
	return nil
}

func fingerprintKey(k *tinkpb.Keyset_Key) string {
	h := sha256.New()
	writeUint32(h, k.GetKeyId())
	writeUint32(h, uint32(k.GetStatus()))
	writeUint32(h, uint32(k.GetOutputPrefixType()))
	data := k.GetKeyData()
	_, _ = h.Write([]byte(data.GetTypeUrl()))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(data.GetKeyMaterialType().String()))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(data.GetValue())
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:fingerprintBytes])
}

func writeUint32(h interface{ Write([]byte) (int, error) }, n uint32) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, n)
	_, _ = h.Write(buf)
}

func keyIDString(id uint32) string {
	return fmt.Sprintf("%d", id)
}

func parseKeyID(keyID string) (uint32, error) {
	trimmed := strings.TrimSpace(keyID)
	if trimmed == "" {
		return 0, errors.New("secretstore: key id is required")
	}
	parsed, err := strconv.ParseUint(trimmed, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("secretstore: invalid key id %q: %w", keyID, err)
	}
	return uint32(parsed), nil
}

func readKeysetJSON(raw string) (*keyset.Handle, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, ErrEmptyKeyset
	}
	handle, err := insecurecleartextkeyset.Read(keyset.NewJSONReader(strings.NewReader(trimmed)))
	if err != nil {
		return nil, fmt.Errorf("secretstore: read Tink keyset: %w", err)
	}
	return handle, nil
}

func writeKeysetJSON(handle *keyset.Handle) (string, error) {
	buf := ptrext.Of(bytes.Buffer{})
	if err := insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf)); err != nil {
		return "", fmt.Errorf("secretstore: write keyset: %w", err)
	}
	return buf.String(), nil
}

// AssociatedData joins caller-owned dimensions into a stable binary AAD value.
// The NUL separator avoids accidental ambiguity between adjacent components.
func AssociatedData(parts ...string) []byte {
	joined := strings.Join(parts, "\x00")
	if joined == "" {
		return nil
	}
	return []byte(joined)
}

// RedactKeysetForLog returns a stable short hash of the config value for boot
// diagnostics without exposing any key material.
func RedactKeysetForLog(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return "sha256:" + base64.RawStdEncoding.EncodeToString(sum[:8])
}
