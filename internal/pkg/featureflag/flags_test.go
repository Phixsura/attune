// SPDX-License-Identifier: Apache-2.0

package featureflag

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStore_Register(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Register("feature_a", "Test feature A", true)
	s.Register("feature_b", "Test feature B", false)

	require.True(t, s.IsEnabled("feature_a"))
	require.False(t, s.IsEnabled("feature_b"))
}

func TestStore_IsEnabled_NotFound(t *testing.T) {
	t.Parallel()
	s := NewStore()
	require.False(t, s.IsEnabled("nonexistent"))
}

func TestStore_SetEnabled(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Register("feature", "Test", false)

	require.False(t, s.IsEnabled("feature"))

	ok := s.SetEnabled("feature", true)
	require.True(t, ok)
	require.True(t, s.IsEnabled("feature"))

	ok = s.SetEnabled("feature", false)
	require.True(t, ok)
	require.False(t, s.IsEnabled("feature"))
}

func TestStore_SetEnabled_NotFound(t *testing.T) {
	t.Parallel()
	s := NewStore()
	ok := s.SetEnabled("nonexistent", true)
	require.False(t, ok)
}

func TestStore_All(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Register("a", "A", true)
	s.Register("b", "B", false)

	all := s.All()
	require.Len(t, all, 2)
}

func TestContext(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Register("feature", "Test", true)

	ctx := WithStore(context.Background(), s)
	retrieved := FromContext(ctx)
	require.NotNil(t, retrieved)
	require.True(t, retrieved.IsEnabled("feature"))
}

func TestFromContext_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	require.Nil(t, FromContext(ctx))
}

func TestEnabled_WithContext(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Register("feature", "Test", true)

	ctx := WithStore(context.Background(), s)
	require.True(t, Enabled(ctx, "feature"))
	require.False(t, Enabled(ctx, "nonexistent"))
}

func TestEnabled_NoContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	require.False(t, Enabled(ctx, "feature"))
}

func TestGlobal(t *testing.T) {
	// Not parallel - modifies global state
	Global.Register("global_test", "Test", true)
	require.True(t, Global.IsEnabled("global_test"))
}
