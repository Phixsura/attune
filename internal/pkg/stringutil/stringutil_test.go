// SPDX-License-Identifier: Apache-2.0

package stringutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTruncate(t *testing.T) {
	t.Parallel()
	require.Equal(t, "hello", Truncate("hello", 10, "..."))
	require.Equal(t, "hel...", Truncate("hello world", 6, "..."))
	require.Equal(t, "...", Truncate("hello", 3, "..."))
}

func TestTruncateWords(t *testing.T) {
	t.Parallel()
	require.Equal(t, "hello world", TruncateWords("hello world", 20, "..."))
	require.Equal(t, "hello...", TruncateWords("hello world foo", 10, "..."))
}

func TestIsBlank(t *testing.T) {
	t.Parallel()
	require.True(t, IsBlank(""))
	require.True(t, IsBlank("   "))
	require.True(t, IsBlank("\t\n"))
	require.False(t, IsBlank("a"))
	require.False(t, IsBlank(" a "))
}

func TestFirstNonEmpty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "b", FirstNonEmpty("", "b", "c"))
	require.Equal(t, "a", FirstNonEmpty("a", "b"))
	require.Equal(t, "", FirstNonEmpty("", ""))
	require.Equal(t, "", FirstNonEmpty())
}

func TestToSnakeCase(t *testing.T) {
	t.Parallel()
	require.Equal(t, "hello_world", ToSnakeCase("HelloWorld"))
	require.Equal(t, "my_variable", ToSnakeCase("myVariable"))
	require.Equal(t, "already_snake", ToSnakeCase("already_snake"))
}

func TestToCamelCase(t *testing.T) {
	t.Parallel()
	require.Equal(t, "helloWorld", ToCamelCase("hello_world"))
	require.Equal(t, "myVariable", ToCamelCase("my_variable"))
	require.Equal(t, "already", ToCamelCase("already"))
}

func TestRemovePrefix(t *testing.T) {
	t.Parallel()
	require.Equal(t, "world", RemovePrefix("hello_world", "hello_"))
	require.Equal(t, "hello", RemovePrefix("hello", "world"))
}

func TestRemoveSuffix(t *testing.T) {
	t.Parallel()
	require.Equal(t, "hello", RemoveSuffix("hello_world", "_world"))
	require.Equal(t, "hello", RemoveSuffix("hello", "_world"))
}
