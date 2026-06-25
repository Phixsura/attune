// SPDX-License-Identifier: Apache-2.0

package maputil

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeys(t *testing.T) {
	t.Parallel()
	m := map[string]int{"a": 1, "b": 2}
	keys := Keys(m)
	sort.Strings(keys)
	require.Equal(t, []string{"a", "b"}, keys)
}

func TestKeys_Nil(t *testing.T) {
	t.Parallel()
	var m map[string]int
	require.Nil(t, Keys(m))
}

func TestValues(t *testing.T) {
	t.Parallel()
	m := map[string]int{"a": 1, "b": 2}
	vals := Values(m)
	sort.Ints(vals)
	require.Equal(t, []int{1, 2}, vals)
}

func TestValues_Nil(t *testing.T) {
	t.Parallel()
	var m map[string]int
	require.Nil(t, Values(m))
}

func TestMerge(t *testing.T) {
	t.Parallel()
	m1 := map[string]int{"a": 1}
	m2 := map[string]int{"b": 2, "a": 3}
	result := Merge(m1, m2)
	require.Equal(t, map[string]int{"a": 3, "b": 2}, result)
}

func TestMerge_Empty(t *testing.T) {
	t.Parallel()
	result := Merge[string, int]()
	require.Empty(t, result)
}

func TestFilterKeys(t *testing.T) {
	t.Parallel()
	m := map[string]int{"apple": 1, "banana": 2, "avocado": 3}
	result := FilterKeys(m, func(k string) bool { return k[0] == 'a' })
	require.Equal(t, map[string]int{"apple": 1, "avocado": 3}, result)
}

func TestFilterKeys_Nil(t *testing.T) {
	t.Parallel()
	var m map[string]int
	require.Nil(t, FilterKeys(m, func(k string) bool { return true }))
}

func TestFilterValues(t *testing.T) {
	t.Parallel()
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	result := FilterValues(m, func(v int) bool { return v > 1 })
	require.Equal(t, map[string]int{"b": 2, "c": 3}, result)
}

func TestGetOr(t *testing.T) {
	t.Parallel()
	m := map[string]int{"a": 1}
	require.Equal(t, 1, GetOr(m, "a", 0))
	require.Equal(t, 99, GetOr(m, "b", 99))
}
