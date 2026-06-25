// SPDX-License-Identifier: Apache-2.0

package sliceutil

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMap(t *testing.T) {
	t.Parallel()
	nums := []int{1, 2, 3}
	result := Map(nums, func(n int) string { return strconv.Itoa(n * 2) })
	require.Equal(t, []string{"2", "4", "6"}, result)
}

func TestMap_Nil(t *testing.T) {
	t.Parallel()
	var s []int
	result := Map(s, func(n int) int { return n })
	require.Nil(t, result)
}

func TestFilter(t *testing.T) {
	t.Parallel()
	nums := []int{1, 2, 3, 4, 5}
	result := Filter(nums, func(n int) bool { return n%2 == 0 })
	require.Equal(t, []int{2, 4}, result)
}

func TestFilter_Nil(t *testing.T) {
	t.Parallel()
	var s []int
	result := Filter(s, func(n int) bool { return true })
	require.Nil(t, result)
}

func TestContains(t *testing.T) {
	t.Parallel()
	require.True(t, Contains([]int{1, 2, 3}, 2))
	require.False(t, Contains([]int{1, 2, 3}, 4))
	require.False(t, Contains([]int{}, 1))
}

func TestUnique(t *testing.T) {
	t.Parallel()
	nums := []int{1, 2, 2, 3, 1, 4}
	result := Unique(nums)
	require.Equal(t, []int{1, 2, 3, 4}, result)
}

func TestUnique_Nil(t *testing.T) {
	t.Parallel()
	var s []int
	result := Unique(s)
	require.Nil(t, result)
}

func TestChunk(t *testing.T) {
	t.Parallel()
	nums := []int{1, 2, 3, 4, 5}
	result := Chunk(nums, 2)
	require.Equal(t, [][]int{{1, 2}, {3, 4}, {5}}, result)
}

func TestChunk_ZeroSize(t *testing.T) {
	t.Parallel()
	result := Chunk([]int{1, 2, 3}, 0)
	require.Nil(t, result)
}

func TestChunk_Empty(t *testing.T) {
	t.Parallel()
	result := Chunk([]int{}, 2)
	require.Nil(t, result)
}

func TestFirst(t *testing.T) {
	t.Parallel()
	require.Equal(t, 1, First([]int{1, 2, 3}))
	require.Equal(t, 0, First([]int{}))
}

func TestLast(t *testing.T) {
	t.Parallel()
	require.Equal(t, 3, Last([]int{1, 2, 3}))
	require.Equal(t, 0, Last([]int{}))
}
