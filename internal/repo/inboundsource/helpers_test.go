// SPDX-License-Identifier: Apache-2.0

package inboundsource

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_nilIfEmpty(t *testing.T) {
	t.Parallel()

	t.Run("empty_returns_nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, nilIfEmpty(""))
	})

	t.Run("non_empty_returns_pointer", func(t *testing.T) {
		t.Parallel()
		got := nilIfEmpty("hello")
		require.NotNil(t, got)
		require.Equal(t, "hello", *got) // ptrext:allow test-assertion
	})

	t.Run("space_is_not_empty", func(t *testing.T) {
		t.Parallel()
		got := nilIfEmpty(" ")
		require.NotNil(t, got)
		require.Equal(t, " ", *got) // ptrext:allow test-assertion
	})
}
