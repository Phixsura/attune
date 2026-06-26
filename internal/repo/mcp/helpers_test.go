// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_hashCode(t *testing.T) {
	t.Parallel()

	t.Run("empty_string", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", hashCode(""))
	})

	t.Run("known_value", func(t *testing.T) {
		t.Parallel()
		// SHA-256("test") is a well-known constant.
		require.Equal(t, "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", hashCode("test"))
	})

	t.Run("deterministic", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, hashCode("hello"), hashCode("hello"))
	})

	t.Run("different_inputs_differ", func(t *testing.T) {
		t.Parallel()
		require.NotEqual(t, hashCode("abc"), hashCode("def"))
	})
}

func Test_hashToken(t *testing.T) {
	t.Parallel()

	t.Run("empty_string", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", hashToken(""))
	})

	t.Run("consistent_output", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, hashToken("token123"), hashToken("token123"))
	})

	t.Run("same_algorithm_as_hashCode", func(t *testing.T) {
		t.Parallel()
		input := "arbitrary-value"
		require.Equal(t, hashCode(input), hashToken(input))
	})
}

func Test_generateRefreshToken(t *testing.T) {
	t.Parallel()

	t.Run("no_error", func(t *testing.T) {
		t.Parallel()
		token, err := generateRefreshToken()
		require.NoError(t, err)
		require.NotEmpty(t, token)
	})

	t.Run("64_hex_chars", func(t *testing.T) {
		t.Parallel()
		token, err := generateRefreshToken()
		require.NoError(t, err)
		require.Len(t, token, 64)
	})

	t.Run("valid_hex", func(t *testing.T) {
		t.Parallel()
		token, err := generateRefreshToken()
		require.NoError(t, err)
		_, decErr := hex.DecodeString(token)
		require.NoError(t, decErr)
	})

	t.Run("unique_across_calls", func(t *testing.T) {
		t.Parallel()
		t1, err1 := generateRefreshToken()
		require.NoError(t, err1)
		t2, err2 := generateRefreshToken()
		require.NoError(t, err2)
		require.NotEqual(t, t1, t2)
	})
}
