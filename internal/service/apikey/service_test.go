// SPDX-License-Identifier: Apache-2.0

package apikey

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
)

func TestAPIKeyPrefix(t *testing.T) {
	t.Parallel()
	require.Equal(t, "fbk_live_", domain.APIKeyPrefix)
}

func TestRawKeyLength(t *testing.T) {
	t.Parallel()
	require.Equal(t, 32, rawKeyHexLen)
}

func TestDisplayPrefixLength(t *testing.T) {
	t.Parallel()
	require.Equal(t, 12, displayPrefLen)
}

func TestTouchInterval(t *testing.T) {
	t.Parallel()
	require.Equal(t, 30, int(touchInterval.Seconds()))
}

func TestNilAPIKeysService(t *testing.T) {
	t.Parallel()
	var s *APIKeys
	require.Nil(t, s)
}
