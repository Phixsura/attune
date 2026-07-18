// SPDX-License-Identifier: Apache-2.0

// Package secretstore owns runtime secret encryption for attune.
//
// The implementation keeps the existing `secrets.tink_keyset` JSON shape and
// TINK-prefixed ciphertext layout for compatibility, but the actual AEAD work
// is handled directly with the standard library's AES-GCM primitives. That
// keeps runtime secrets self-contained while preserving existing operators'
// config and ciphertext data.
package secretstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	fingerprintBytes     = 16
	keyIDPrefixLen       = 5
	tinkPrefix           = 0x01
	aesGCMKeyTypeURL     = "type.googleapis.com/google.crypto.tink.AesGcmKey"
	aesGCMKeyMaterial    = "SYMMETRIC"
	aesGCMOutputPrefix   = "TINK"
	aesGCMEnabledStatus  = "ENABLED"
	aesGCMKeyVersion     = 0
	aesGCMKeySize        = 32
	aesGCMNonceSize      = 12
	aesGCMSerializedSize = 2 + 2 + aesGCMKeySize
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

type keysetDocument struct {
	PrimaryKeyID uint32      `json:"primaryKeyId"`
	Key          []keyRecord `json:"key"`
}

type keyRecord struct {
	KeyData          keyData `json:"keyData"`
	Status           string  `json:"status"`
	KeyID            uint32  `json:"keyId"`
	OutputPrefixType string  `json:"outputPrefixType"`
}

type keyData struct {
	TypeURL         string `json:"typeUrl"`
	Value           string `json:"value"`
	KeyMaterialType string `json:"keyMaterialType"`
}

type storedKey struct {
	id       uint32
	record   keyRecord
	keyBytes []byte
	aead     cipher.AEAD
	metadata KeyMetadata
}

// TinkStore encrypts/decrypts small runtime secrets with a shared AES-GCM
// keyset. It also satisfies inbound.SecretStore through Encrypt/Decrypt.
type TinkStore struct {
	primitive map[uint32]*storedKey
	primaryID uint32
	keys      []*storedKey
	legacy    *legacyAESGCMStore
}

// NewTinkStoreFromJSON reads a cleartext keyset JSON string. The config file
// containing it is the private runtime config; use `attune secrets
// generate-keyset` to create the initial value.
func NewTinkStoreFromJSON(raw string) (*TinkStore, error) {
	return NewTinkStoreFromJSONWithLegacy(raw, "")
}

// NewTinkStoreFromJSONWithLegacy reads a keyset and optionally enables a
// read-only fallback for the pre-keyring inbound AES-GCM envelope. New
// ciphertexts are always written with the current AES-GCM keyset; the fallback
// exists only so old inbound rows can boot and be migrated with `attune
// secrets reencrypt --apply`.
func NewTinkStoreFromJSONWithLegacy(raw string, legacyInboundMasterKey string) (*TinkStore, error) {
	doc, err := readKeysetJSON(raw)
	if err != nil {
		return nil, err
	}
	keys, err := buildKeys(doc)
	if err != nil {
		return nil, err
	}
	legacy, err := newLegacyAESGCMStore(legacyInboundMasterKey)
	if err != nil {
		return nil, err
	}
	store := ptrext.Of(TinkStore{
		primitive: make(map[uint32]*storedKey, len(keys)),
		keys:      keys,
		legacy:    legacy,
	})
	for _, key := range keys {
		if _, exists := store.primitive[key.id]; exists {
			return nil, fmt.Errorf("secretstore: duplicate key_id=%s", keyIDString(key.id))
		}
		store.primitive[key.id] = key
		key.metadata.Primary = key.id == doc.PrimaryKeyID
	}
	if _, ok := store.primitive[doc.PrimaryKeyID]; !ok {
		return nil, fmt.Errorf("secretstore: primary key_id=%s not found", keyIDString(doc.PrimaryKeyID))
	}
	store.primaryID = doc.PrimaryKeyID
	if err := validateKeyPrefixes(keys); err != nil {
		return nil, err
	}
	return store, nil
}

// GenerateAES256GCMKeysetJSON returns an operator-pasteable cleartext keyset.
// It is intended for private config generation, not logs.
func GenerateAES256GCMKeysetJSON() (string, error) {
	keyBytes, err := randomKeyBytes()
	if err != nil {
		return "", fmt.Errorf("secretstore: generate keyset: %w", err)
	}
	keyID, err := randomKeyID(nil)
	if err != nil {
		return "", fmt.Errorf("secretstore: generate key id: %w", err)
	}
	doc := keysetDocument{
		PrimaryKeyID: keyID,
		Key:          []keyRecord{newAESGCMKeyRecord(keyID, keyBytes)},
	}
	return writeKeysetJSON(ptrext.Of(doc))
}

// AddAES256GCMKeyToKeysetJSON adds a fresh enabled AES-256-GCM key to an
// existing cleartext keyset and returns the updated JSON. The new key is
// non-primary unless makePrimary is true.
func AddAES256GCMKeyToKeysetJSON(raw string, makePrimary bool) (string, string, error) {
	doc, err := readKeysetJSON(raw)
	if err != nil {
		return "", "", err
	}
	existing := make(map[uint32]struct{}, len(doc.Key))
	for _, key := range doc.Key {
		existing[key.KeyID] = struct{}{}
	}
	keyBytes, err := randomKeyBytes()
	if err != nil {
		return "", "", fmt.Errorf("secretstore: add key: %w", err)
	}
	keyID, err := randomKeyID(existing)
	if err != nil {
		return "", "", fmt.Errorf("secretstore: add key id: %w", err)
	}
	doc.Key = append(doc.Key, newAESGCMKeyRecord(keyID, keyBytes))
	if makePrimary {
		doc.PrimaryKeyID = keyID
	}
	out, err := writeKeysetJSON(doc)
	if err != nil {
		return "", "", err
	}
	return out, keyIDString(keyID), nil
}

// SetPrimaryKeysetJSON returns a copy of raw with keyID set as primary.
func SetPrimaryKeysetJSON(raw, keyID string) (string, error) {
	id, err := parseKeyID(keyID)
	if err != nil {
		return "", err
	}
	doc, err := readKeysetJSON(raw)
	if err != nil {
		return "", err
	}
	if !containsKeyID(doc.Key, id) {
		return "", fmt.Errorf("secretstore: set primary: key %s not found", keyIDString(id))
	}
	doc.PrimaryKeyID = id
	return writeKeysetJSON(doc)
}

// DeleteKeyFromKeysetJSON returns a copy of raw with keyID removed. Primary key
// deletion is rejected.
func DeleteKeyFromKeysetJSON(raw, keyID string) (string, error) {
	id, err := parseKeyID(keyID)
	if err != nil {
		return "", err
	}
	doc, err := readKeysetJSON(raw)
	if err != nil {
		return "", err
	}
	if doc.PrimaryKeyID == id {
		return "", fmt.Errorf("secretstore: delete key: cannot delete primary key %s", keyIDString(id))
	}
	idx := -1
	for i, key := range doc.Key {
		if key.KeyID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", fmt.Errorf("secretstore: delete key: key %s not found", keyIDString(id))
	}
	doc.Key = append(doc.Key[:idx], doc.Key[idx+1:]...)
	if len(doc.Key) == 0 {
		return "", errors.New("secretstore: delete key: keyset must contain at least one key")
	}
	return writeKeysetJSON(doc)
}

// PrimaryKeyID is the key id used for new encryptions.
func (s *TinkStore) PrimaryKeyID() string {
	if s == nil {
		return ""
	}
	return keyIDString(s.primaryID)
}

// Keys returns metadata for every key in the local keyset.
func (s *TinkStore) Keys() []KeyMetadata {
	if s == nil {
		return nil
	}
	out := make([]KeyMetadata, 0, len(s.keys))
	for _, key := range s.keys {
		out = append(out, key.metadata)
	}
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
	key, ok := s.primitive[s.primaryID]
	if !ok {
		return EncryptedValue{}, fmt.Errorf("secretstore: primary key_id=%s not found", keyIDString(s.primaryID))
	}
	nonce := make([]byte, aesGCMNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return EncryptedValue{}, fmt.Errorf("secretstore: encrypt nonce: %w", err)
	}
	sealed := key.aead.Seal(nil, nonce, plaintext, aad)
	ciphertext := make([]byte, 0, keyIDPrefixLen+len(nonce)+len(sealed))
	ciphertext = append(ciphertext, tinkPrefix)
	ciphertext = appendUint32(ciphertext, key.id)
	ciphertext = append(ciphertext, nonce...)
	ciphertext = append(ciphertext, sealed...)
	return EncryptedValue{KeyID: keyIDString(key.id), Ciphertext: ciphertext}, nil
}

// DecryptValue decrypts ciphertext with caller-provided associated data.
func (s *TinkStore) DecryptValue(value EncryptedValue, aad []byte) ([]byte, error) {
	if s == nil || s.primitive == nil {
		return nil, errors.New("secretstore: nil Tink store")
	}
	if len(value.Ciphertext) >= keyIDPrefixLen && value.Ciphertext[0] == tinkPrefix {
		keyID := binary.BigEndian.Uint32(value.Ciphertext[1:keyIDPrefixLen])
		key, ok := s.primitive[keyID]
		if !ok {
			if aad == nil && s.legacy != nil && IsLegacyInboundEnvelope(value.Ciphertext) {
				return s.legacy.Decrypt(value.Ciphertext)
			}
			return nil, fmt.Errorf("secretstore: decrypt key_id=%s: key not found", keyIDString(keyID))
		}
		if len(value.Ciphertext) < keyIDPrefixLen+aesGCMNonceSize+key.aead.Overhead() {
			return nil, ErrCiphertextTooLow
		}
		nonce := value.Ciphertext[keyIDPrefixLen : keyIDPrefixLen+aesGCMNonceSize]
		sealed := value.Ciphertext[keyIDPrefixLen+aesGCMNonceSize:]
		plaintext, err := key.aead.Open(nil, nonce, sealed, aad)
		if err != nil {
			return nil, fmt.Errorf("secretstore: decrypt key_id=%s: %w", keyIDString(keyID), err)
		}
		return plaintext, nil
	}
	if aad == nil && s.legacy != nil && IsLegacyInboundEnvelope(value.Ciphertext) {
		return s.legacy.Decrypt(value.Ciphertext)
	}
	if len(value.Ciphertext) < keyIDPrefixLen {
		return nil, ErrCiphertextTooLow
	}
	return nil, fmt.Errorf("secretstore: unsupported Tink output prefix %#x", value.Ciphertext[0])
}

// KeyIDFromCiphertext extracts the TINK key id prefix from a TINK-prefixed
// AEAD ciphertext. It is best-effort metadata; DecryptValue remains the source
// of truth because the AEAD validates authenticity.
func KeyIDFromCiphertext(ciphertext []byte) (string, error) {
	if len(ciphertext) < keyIDPrefixLen {
		return "", ErrCiphertextTooLow
	}
	if ciphertext[0] != tinkPrefix {
		return "", fmt.Errorf("secretstore: unsupported Tink output prefix %#x", ciphertext[0])
	}
	return keyIDString(binary.BigEndian.Uint32(ciphertext[1:keyIDPrefixLen])), nil
}

func buildKeys(doc *keysetDocument) ([]*storedKey, error) {
	if doc == nil {
		return nil, ErrEmptyKeyset
	}
	if len(doc.Key) == 0 {
		return nil, ErrEmptyKeyset
	}
	keys := make([]*storedKey, 0, len(doc.Key))
	for _, rec := range doc.Key {
		key, err := buildKey(rec)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func buildKey(rec keyRecord) (*storedKey, error) {
	if rec.OutputPrefixType != aesGCMOutputPrefix {
		return nil, fmt.Errorf("secretstore: key_id=%s output prefix %q is unsupported; use TINK-prefixed AEAD keys",
			keyIDString(rec.KeyID), rec.OutputPrefixType)
	}
	if rec.Status != aesGCMEnabledStatus {
		return nil, fmt.Errorf("secretstore: key_id=%s status %q is unsupported; use an enabled AEAD key",
			keyIDString(rec.KeyID), rec.Status)
	}
	if rec.KeyData.TypeURL != aesGCMKeyTypeURL {
		return nil, fmt.Errorf("secretstore: key_id=%s type_url %q is unsupported", keyIDString(rec.KeyID), rec.KeyData.TypeURL)
	}
	if rec.KeyData.KeyMaterialType != aesGCMKeyMaterial {
		return nil, fmt.Errorf("secretstore: key_id=%s key_material_type %q is unsupported", keyIDString(rec.KeyID), rec.KeyData.KeyMaterialType)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.KeyData.Value))
	if err != nil {
		return nil, fmt.Errorf("secretstore: key_id=%s decode key data: %w", keyIDString(rec.KeyID), err)
	}
	keyBytes, err := decodeAesGCMKey(raw)
	if err != nil {
		return nil, fmt.Errorf("secretstore: key_id=%s parse key data: %w", keyIDString(rec.KeyID), err)
	}
	aead, err := newAESGCM(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("secretstore: key_id=%s build AEAD: %w", keyIDString(rec.KeyID), err)
	}
	metadata := KeyMetadata{
		KeyID:              keyIDString(rec.KeyID),
		Primary:            false,
		Status:             rec.Status,
		TypeURL:            rec.KeyData.TypeURL,
		OutputPrefixType:   rec.OutputPrefixType,
		KeyMaterialType:    rec.KeyData.KeyMaterialType,
		FingerprintSHA256:  fingerprintKey(rec, raw),
		FingerprintVersion: 1,
	}
	return ptrext.Of(storedKey{
		id:       rec.KeyID,
		record:   rec,
		keyBytes: keyBytes,
		aead:     aead,
		metadata: metadata,
	}), nil
}

func metadataFromKeyset(doc *keysetDocument) []KeyMetadata {
	if doc == nil {
		return nil
	}
	out := make([]KeyMetadata, 0, len(doc.Key))
	primaryID := keyIDString(doc.PrimaryKeyID)
	for _, rec := range doc.Key {
		metadata := KeyMetadata{
			KeyID:              keyIDString(rec.KeyID),
			Primary:            keyIDString(rec.KeyID) == primaryID,
			Status:             rec.Status,
			TypeURL:            rec.KeyData.TypeURL,
			OutputPrefixType:   rec.OutputPrefixType,
			KeyMaterialType:    rec.KeyData.KeyMaterialType,
			FingerprintSHA256:  fingerprintKey(rec, decodeKeyValueMust(rec.KeyData.Value)),
			FingerprintVersion: 1,
		}
		out = append(out, metadata)
	}
	return out
}

func validateKeyPrefixes(keys []*storedKey) error {
	for _, key := range keys {
		if key.metadata.OutputPrefixType != aesGCMOutputPrefix {
			return fmt.Errorf("secretstore: key_id=%s output prefix %q is unsupported; use TINK-prefixed AEAD keys",
				key.metadata.KeyID, key.metadata.OutputPrefixType)
		}
	}
	return nil
}

func fingerprintKey(rec keyRecord, raw []byte) string {
	h := sha256.New()
	writeUint32(h, rec.KeyID)
	writeUint32(h, statusToNumeric(rec.Status))
	writeUint32(h, outputPrefixToNumeric(rec.OutputPrefixType))
	data := rec.KeyData
	_, _ = h.Write([]byte(data.TypeURL))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(data.KeyMaterialType))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(raw)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:fingerprintBytes])
}

func statusToNumeric(status string) uint32 {
	switch status {
	case aesGCMEnabledStatus:
		return 1
	default:
		return 0
	}
}

func outputPrefixToNumeric(prefix string) uint32 {
	switch prefix {
	case aesGCMOutputPrefix:
		return 1
	default:
		return 0
	}
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

func readKeysetJSON(raw string) (*keysetDocument, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, ErrEmptyKeyset
	}
	doc := ptrext.Of(keysetDocument{})
	if err := json.Unmarshal([]byte(trimmed), doc); err != nil {
		return nil, fmt.Errorf("secretstore: read keyset JSON: %w", err)
	}
	if len(doc.Key) == 0 {
		return nil, ErrEmptyKeyset
	}
	return doc, nil
}

func writeKeysetJSON(doc *keysetDocument) (string, error) {
	if doc == nil {
		return "", ErrEmptyKeyset
	}
	payload, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("secretstore: write keyset: %w", err)
	}
	return string(payload), nil
}

func newAESGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != aesGCMKeySize {
		return nil, fmt.Errorf("secretstore: aes-gcm key must be %d bytes, got %d", aesGCMKeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretstore: aes.NewCipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func randomKeyBytes() ([]byte, error) {
	key := make([]byte, aesGCMKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func randomKeyID(existing map[uint32]struct{}) (uint32, error) {
	for tries := 0; tries < 1024; tries++ {
		var buf [4]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, err
		}
		id := binary.BigEndian.Uint32(buf[:]) | 0x01000000
		if existing == nil {
			return id, nil
		}
		if _, ok := existing[id]; !ok {
			return id, nil
		}
	}
	return 0, errors.New("secretstore: unable to allocate unique key id")
}

func appendUint32(buf []byte, n uint32) []byte {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], n)
	return append(buf, tmp[:]...)
}

func newAESGCMKeyRecord(id uint32, keyBytes []byte) keyRecord {
	return keyRecord{
		KeyData: keyData{
			TypeURL:         aesGCMKeyTypeURL,
			Value:           base64.StdEncoding.EncodeToString(encodeAesGCMKey(keyBytes)),
			KeyMaterialType: aesGCMKeyMaterial,
		},
		Status:           aesGCMEnabledStatus,
		KeyID:            id,
		OutputPrefixType: aesGCMOutputPrefix,
	}
}

func encodeAesGCMKey(keyBytes []byte) []byte {
	buf := make([]byte, 0, aesGCMSerializedSize)
	buf = append(buf, 0x08) // field 1, wire type 0
	buf = append(buf, 0x00) // version = 0
	buf = append(buf, 0x12) // field 2, wire type 2
	buf = appendUint64(buf, uint64(len(keyBytes)))
	buf = append(buf, keyBytes...)
	return buf
}

func appendUint64(buf []byte, n uint64) []byte {
	var tmp [10]byte
	m := binary.PutUvarint(tmp[:], n)
	return append(buf, tmp[:m]...)
}

func decodeAesGCMKey(serialized []byte) ([]byte, error) {
	var (
		i           int
		versionSeen bool
		keyBytes    []byte
	)
	for i < len(serialized) {
		tag, n := binary.Uvarint(serialized[i:])
		if n <= 0 {
			return nil, errors.New("secretstore: malformed key proto tag")
		}
		i += n
		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x7)

		switch fieldNum {
		case 1:
			if wireType != 0 {
				return nil, errors.New("secretstore: malformed key proto version field")
			}
			version, n := binary.Uvarint(serialized[i:])
			if n <= 0 {
				return nil, errors.New("secretstore: malformed key proto version")
			}
			i += n
			versionSeen = true
			if version != aesGCMKeyVersion {
				return nil, fmt.Errorf("secretstore: unsupported AES-GCM key version %d", version)
			}
		case 2:
			if wireType != 2 {
				return nil, errors.New("secretstore: malformed key proto value field")
			}
			l, n := binary.Uvarint(serialized[i:])
			if n <= 0 {
				return nil, errors.New("secretstore: malformed key proto value length")
			}
			i += n
			if l > uint64(len(serialized)-i) {
				return nil, errors.New("secretstore: malformed key proto value")
			}
			keyBytes = append([]byte(nil), serialized[i:i+int(l)]...)
			i += int(l)
		default:
			var err error
			i, err = skipProtoField(serialized, i, wireType)
			if err != nil {
				return nil, err
			}
		}
	}
	if !versionSeen {
		return nil, errors.New("secretstore: missing AES-GCM key version")
	}
	if len(keyBytes) != aesGCMKeySize {
		return nil, fmt.Errorf("secretstore: AES-GCM key must be %d bytes, got %d", aesGCMKeySize, len(keyBytes))
	}
	return keyBytes, nil
}

func decodeKeyValueMust(raw string) []byte {
	decoded, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	return decoded
}

func skipProtoField(serialized []byte, idx int, wireType int) (int, error) {
	switch wireType {
	case 0:
		_, n := binary.Uvarint(serialized[idx:])
		if n <= 0 {
			return idx, errors.New("secretstore: malformed proto varint")
		}
		idx += n
		return idx, nil
	case 1:
		if len(serialized)-idx < 8 {
			return idx, errors.New("secretstore: malformed proto fixed64")
		}
		idx += 8
		return idx, nil
	case 2:
		l, n := binary.Uvarint(serialized[idx:])
		if n <= 0 {
			return idx, errors.New("secretstore: malformed proto length")
		}
		idx += n
		if l > uint64(len(serialized)-idx) {
			return idx, errors.New("secretstore: malformed proto bytes")
		}
		idx += int(l)
		return idx, nil
	case 5:
		if len(serialized)-idx < 4 {
			return idx, errors.New("secretstore: malformed proto fixed32")
		}
		idx += 4
		return idx, nil
	default:
		return idx, fmt.Errorf("secretstore: unsupported proto wire type %d", wireType)
	}
}

func containsKeyID(keys []keyRecord, id uint32) bool {
	for _, key := range keys {
		if key.KeyID == id {
			return true
		}
	}
	return false
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
