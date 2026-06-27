// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/secretstore"
)

func TestZero_Empty(t *testing.T) {
	t.Parallel()
	zero([]byte{})
}

func TestZero_Nil(t *testing.T) {
	t.Parallel()
	zero(nil)
}

func TestLLMChannelAAD_DifferentChannels(t *testing.T) {
	t.Parallel()

	a := llmChannelAAD("aaaa")
	b := llmChannelAAD("bbbb")
	require.NotEqual(t, a, b, "different channel IDs must produce different AAD")
}

func TestLiveKeyIDs_NilStore(t *testing.T) {
	t.Parallel()

	keys := liveKeyIDs(nil)
	require.Empty(t, keys)
}

func TestDecryptLLMChannel_EmptyPlaintext(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	val, err := store.EncryptValue([]byte(""),
		secretstore.AssociatedData("llm_channel", testChannelID, "api_key"))
	require.NoError(t, err)

	err = decryptLLMChannel(store, testChannelID, val.KeyID, val.Ciphertext)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}
