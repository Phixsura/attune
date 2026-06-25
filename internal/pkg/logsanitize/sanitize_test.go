// SPDX-License-Identifier: Apache-2.0

package logsanitize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPath_RemovesNewlines(t *testing.T) {
	t.Parallel()
	result := Path("/api/v1\n/admin")
	require.NotContains(t, result, "\n")
	require.Contains(t, result, "\\n")
}

func TestPath_TruncatesLong(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 300)
	result := Path(long)
	require.LessOrEqual(t, len(result), 260)
	require.Contains(t, result, "...")
}

func TestHeader_RemovesControlChars(t *testing.T) {
	t.Parallel()
	result := Header("value\x00\x01\x02")
	require.NotContains(t, result, "\x00")
	require.NotContains(t, result, "\x01")
}

func TestUserInput_Sanitizes(t *testing.T) {
	t.Parallel()
	input := "hello\r\nworld\x00"
	result := UserInput(input)
	require.Contains(t, result, "\\r")
	require.Contains(t, result, "\\n")
	require.NotContains(t, result, "\x00")
}

func TestQuery_CollapsesWhitespace(t *testing.T) {
	t.Parallel()
	query := "SELECT *\n  FROM\n    users"
	result := Query(query)
	require.Equal(t, "SELECT * FROM users", result)
}

func TestQuery_TruncatesLong(t *testing.T) {
	t.Parallel()
	long := "SELECT " + strings.Repeat("x", 600)
	result := Query(long)
	require.LessOrEqual(t, len(result), 505)
	require.Contains(t, result, "...")
}

func TestTruncateBytes_Short(t *testing.T) {
	t.Parallel()
	result := TruncateBytes([]byte("hello"), 10)
	require.Equal(t, "hello", result)
}

func TestTruncateBytes_Long(t *testing.T) {
	t.Parallel()
	long := []byte(strings.Repeat("x", 100))
	result := TruncateBytes(long, 10)
	require.Contains(t, result, "...")
}

func TestEmail_Basic(t *testing.T) {
	t.Parallel()
	result := Email("user@example.com")
	require.Equal(t, "user@example.com", result)
}
