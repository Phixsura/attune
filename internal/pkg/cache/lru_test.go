// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLRU_SetGet(t *testing.T) {
	t.Parallel()
	c := NewLRU[string, int](10, time.Hour)

	c.Set("a", 1)
	c.Set("b", 2)

	v, ok := c.Get("a")
	require.True(t, ok)
	require.Equal(t, 1, v)

	v, ok = c.Get("b")
	require.True(t, ok)
	require.Equal(t, 2, v)
}

func TestLRU_NotFound(t *testing.T) {
	t.Parallel()
	c := NewLRU[string, int](10, time.Hour)

	_, ok := c.Get("missing")
	require.False(t, ok)
}

func TestLRU_Eviction(t *testing.T) {
	t.Parallel()
	c := NewLRU[string, int](2, time.Hour)

	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3) // evicts "a"

	_, ok := c.Get("a")
	require.False(t, ok)

	v, ok := c.Get("b")
	require.True(t, ok)
	require.Equal(t, 2, v)

	v, ok = c.Get("c")
	require.True(t, ok)
	require.Equal(t, 3, v)
}

func TestLRU_LRUOrder(t *testing.T) {
	t.Parallel()
	c := NewLRU[string, int](2, time.Hour)

	c.Set("a", 1)
	c.Set("b", 2)
	c.Get("a")    // access "a", making "b" oldest
	c.Set("c", 3) // should evict "b"

	_, ok := c.Get("b")
	require.False(t, ok)

	v, ok := c.Get("a")
	require.True(t, ok)
	require.Equal(t, 1, v)
}

func TestLRU_TTLExpiration(t *testing.T) {
	t.Parallel()
	c := NewLRU[string, int](10, time.Millisecond)

	now := time.Now()
	c.nowFunc = func() time.Time { return now }

	c.Set("a", 1)

	v, ok := c.Get("a")
	require.True(t, ok)
	require.Equal(t, 1, v)

	// Advance time past TTL
	c.nowFunc = func() time.Time { return now.Add(2 * time.Millisecond) }

	_, ok = c.Get("a")
	require.False(t, ok)
}

func TestLRU_Update(t *testing.T) {
	t.Parallel()
	c := NewLRU[string, int](10, time.Hour)

	c.Set("a", 1)
	c.Set("a", 2)

	v, ok := c.Get("a")
	require.True(t, ok)
	require.Equal(t, 2, v)
	require.Equal(t, 1, c.Len())
}

func TestLRU_Delete(t *testing.T) {
	t.Parallel()
	c := NewLRU[string, int](10, time.Hour)

	c.Set("a", 1)
	c.Delete("a")

	_, ok := c.Get("a")
	require.False(t, ok)
	require.Equal(t, 0, c.Len())
}

func TestLRU_Clear(t *testing.T) {
	t.Parallel()
	c := NewLRU[string, int](10, time.Hour)

	c.Set("a", 1)
	c.Set("b", 2)
	c.Clear()

	require.Equal(t, 0, c.Len())
}

func TestLRU_Cleanup(t *testing.T) {
	t.Parallel()
	c := NewLRU[string, int](10, time.Millisecond)

	now := time.Now()
	c.nowFunc = func() time.Time { return now }

	c.Set("a", 1)
	c.Set("b", 2)

	c.nowFunc = func() time.Time { return now.Add(2 * time.Millisecond) }
	c.Cleanup()

	require.Equal(t, 0, c.Len())
}
