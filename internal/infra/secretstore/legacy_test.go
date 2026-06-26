// SPDX-License-Identifier: Apache-2.0

package secretstore

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeLegacyInboundMasterKey_Hex(t *testing.T) {
	t.Parallel()
	raw := strings.Repeat("ab", 32)
	key, err := DecodeLegacyInboundMasterKey(raw)
	require.NoError(t, err)
	require.Len(t, key, 32)
}

func TestDecodeLegacyInboundMasterKey_Base64(t *testing.T) {
	t.Parallel()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	encoded := base64.StdEncoding.EncodeToString(key)
	decoded, err := DecodeLegacyInboundMasterKey(encoded)
	require.NoError(t, err)
	require.Equal(t, key, decoded)
}

func TestDecodeLegacyInboundMasterKey_TrimWhitespace(t *testing.T) {
	t.Parallel()
	raw := "  " + strings.Repeat("ab", 32) + "  "
	key, err := DecodeLegacyInboundMasterKey(raw)
	require.NoError(t, err)
	require.Len(t, key, 32)
}

func TestDecodeLegacyInboundMasterKey_Invalid(t *testing.T) {
	t.Parallel()
	_, err := DecodeLegacyInboundMasterKey("too-short")
	require.Error(t, err)
	require.Contains(t, err.Error(), "32 bytes")
}

func TestLegacyAESGCMStore_RoundTrip(t *testing.T) {
	t.Parallel()
	keyHex := hex.EncodeToString(make([]byte, 32))
	store, err := newLegacyAESGCMStore(keyHex)
	require.NoError(t, err)
	require.NotNil(t, store)

	plaintext := []byte("hello world")
	ct, err := store.Encrypt(plaintext)
	require.NoError(t, err)

	pt, err := store.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, plaintext, pt)
}

func TestLegacyAESGCMStore_EmptyKey(t *testing.T) {
	t.Parallel()
	store, err := newLegacyAESGCMStore("")
	require.NoError(t, err)
	require.Nil(t, store)
}

func TestLegacyAESGCMStore_DecryptNilStore(t *testing.T) {
	t.Parallel()
	var store *legacyAESGCMStore
	_, err := store.Decrypt([]byte("some data"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestLegacyAESGCMStore_DecryptTooShort(t *testing.T) {
	t.Parallel()
	keyHex := hex.EncodeToString(make([]byte, 32))
	store, err := newLegacyAESGCMStore(keyHex)
	require.NoError(t, err)

	_, err = store.Decrypt([]byte{0x01, 0x00})
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

func TestLegacyAESGCMStore_DecryptBadVersion(t *testing.T) {
	t.Parallel()
	keyHex := hex.EncodeToString(make([]byte, 32))
	store, err := newLegacyAESGCMStore(keyHex)
	require.NoError(t, err)

	ct, err := store.Encrypt([]byte("test"))
	require.NoError(t, err)
	ct[0] = 0xFF
	_, err = store.Decrypt(ct)
	require.Error(t, err)
	require.Contains(t, err.Error(), "version")
}

func TestLegacyAESGCMStore_DecryptBadKeyID(t *testing.T) {
	t.Parallel()
	keyHex := hex.EncodeToString(make([]byte, 32))
	store, err := newLegacyAESGCMStore(keyHex)
	require.NoError(t, err)

	ct, err := store.Encrypt([]byte("test"))
	require.NoError(t, err)
	ct[1] = 0xFF
	_, err = store.Decrypt(ct)
	require.Error(t, err)
	require.Contains(t, err.Error(), "key_id")
}

func TestIsLegacyInboundEnvelope(t *testing.T) {
	t.Parallel()

	keyHex := hex.EncodeToString(make([]byte, 32))
	store, err := newLegacyAESGCMStore(keyHex)
	require.NoError(t, err)

	ct, err := store.Encrypt([]byte("test"))
	require.NoError(t, err)

	require.True(t, IsLegacyInboundEnvelope(ct))
	require.False(t, IsLegacyInboundEnvelope([]byte{0x01}))
	require.False(t, IsLegacyInboundEnvelope(nil))
}

func TestLegacyAESGCMStore_EncryptTooLarge(t *testing.T) {
	t.Parallel()
	keyHex := hex.EncodeToString(make([]byte, 32))
	store, err := newLegacyAESGCMStore(keyHex)
	require.NoError(t, err)

	huge := make([]byte, legacyMaxPlaintextLen+1)
	_, err = store.Encrypt(huge)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too large")
}
