// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package auditlog

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func Test_nullableJSON(t *testing.T) {
	t.Parallel()

	t.Run("nil input returns nil", func(t *testing.T) {
		t.Parallel()
		got := nullableJSON(nil)
		require.Nil(t, got)
		require.Equal(t, 0, len(got))
	})

	t.Run("empty byte slice returns nil", func(t *testing.T) {
		t.Parallel()
		got := nullableJSON(json.RawMessage{})
		require.Nil(t, got)
	})

	t.Run("valid JSON returns same bytes", func(t *testing.T) {
		t.Parallel()
		input := json.RawMessage(`{"a":1}`)
		got := nullableJSON(input)
		require.Equal(t, []byte(`{"a":1}`), got)
	})
}

func Test_boundedLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter ListFilter
		want   int
	}{
		{
			name:   "unbounded with zero limit returns zero",
			filter: ListFilter{Unbounded: true, Limit: 0},
			want:   0,
		},
		{
			name:   "unbounded with high limit returns as-is",
			filter: ListFilter{Unbounded: true, Limit: 1000},
			want:   1000,
		},
		{
			name:   "bounded with zero limit defaults to 100",
			filter: ListFilter{Unbounded: false, Limit: 0},
			want:   100,
		},
		{
			name:   "bounded with negative limit defaults to 100",
			filter: ListFilter{Unbounded: false, Limit: -1},
			want:   100,
		},
		{
			name:   "bounded with normal limit returns as-is",
			filter: ListFilter{Unbounded: false, Limit: 50},
			want:   50,
		},
		{
			name:   "bounded at max returns 500",
			filter: ListFilter{Unbounded: false, Limit: 500},
			want:   500,
		},
		{
			name:   "bounded above max clamps to 500",
			filter: ListFilter{Unbounded: false, Limit: 501},
			want:   500,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := boundedLimit(tc.filter)
			require.Equal(t, tc.want, got)
		})
	}
}

func Test_appendActionFilter(t *testing.T) {
	t.Parallel()

	t.Run("empty actions returns unchanged", func(t *testing.T) {
		t.Parallel()
		q, args := appendActionFilter("SELECT 1", []any{"existing"}, nil)
		require.Equal(t, "SELECT 1", q)
		require.Equal(t, []any{"existing"}, args)
	})

	t.Run("single action appends IN clause", func(t *testing.T) {
		t.Parallel()
		q, args := appendActionFilter("SELECT 1", []any{"tenant1"}, []string{"create"})
		require.Contains(t, q, "AND action IN ($2)")
		require.Equal(t, []any{"tenant1", "create"}, args)
	})

	t.Run("multiple actions appends IN clause with all placeholders", func(t *testing.T) {
		t.Parallel()
		q, args := appendActionFilter("SELECT 1", []any{"tenant1"}, []string{"create", "delete"})
		require.Contains(t, q, "AND action IN ($2, $3)")
		require.Equal(t, []any{"tenant1", "create", "delete"}, args)
	})
}

func Test_appendExactFilter(t *testing.T) {
	t.Parallel()

	t.Run("empty value returns unchanged", func(t *testing.T) {
		t.Parallel()
		q, args := appendExactFilter("SELECT 1", []any{"a"}, "col", "")
		require.Equal(t, "SELECT 1", q)
		require.Equal(t, []any{"a"}, args)
	})

	t.Run("whitespace-only value returns unchanged", func(t *testing.T) {
		t.Parallel()
		q, args := appendExactFilter("SELECT 1", []any{"a"}, "col", "   ")
		require.Equal(t, "SELECT 1", q)
		require.Equal(t, []any{"a"}, args)
	})

	t.Run("padded value is trimmed and filter added", func(t *testing.T) {
		t.Parallel()
		q, args := appendExactFilter("SELECT 1", []any{"a"}, "actor_type", " trimmed ")
		require.Contains(t, q, "AND actor_type = $2")
		require.Equal(t, []any{"a", "trimmed"}, args)
	})

	t.Run("normal value adds filter", func(t *testing.T) {
		t.Parallel()
		q, args := appendExactFilter("SELECT 1", []any{"a"}, "target_id", "abc")
		require.Contains(t, q, "AND target_id = $2")
		require.Equal(t, []any{"a", "abc"}, args)
	})
}

func Test_appendTimeFilter(t *testing.T) {
	t.Parallel()

	t.Run("nil value returns unchanged", func(t *testing.T) {
		t.Parallel()
		q, args := appendTimeFilter("SELECT 1", []any{"a"}, "created_at >= ", nil)
		require.Equal(t, "SELECT 1", q)
		require.Equal(t, []any{"a"}, args)
	})

	t.Run("non-nil value adds clause", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
		q, args := appendTimeFilter("SELECT 1", []any{"a"}, "created_at >= ", ptrext.Of(ts))
		require.Contains(t, q, "AND created_at >= $2")
		require.Len(t, args, 2)
		require.Equal(t, ts, args[1])
	})
}

func Test_buildListQuery(t *testing.T) {
	t.Parallel()

	t.Run("minimal filter produces base query", func(t *testing.T) {
		t.Parallel()
		q, args, err := buildListQuery(ListFilter{TenantID: "t1"}, 100)
		require.NoError(t, err)
		require.True(t, strings.Contains(q, "tenant_id = $1"))
		require.True(t, strings.Contains(q, "ORDER BY created_at DESC"))
		require.True(t, strings.Contains(q, "LIMIT"))
		require.Equal(t, "t1", args[0])
	})

	t.Run("with actions includes IN clause", func(t *testing.T) {
		t.Parallel()
		q, args, err := buildListQuery(ListFilter{
			TenantID: "t1",
			Actions:  []string{"create", "update"},
		}, 50)
		require.NoError(t, err)
		require.True(t, strings.Contains(q, "action IN ($2, $3)"))
		require.Equal(t, "create", args[1])
		require.Equal(t, "update", args[2])
	})

	t.Run("with all filters includes all clauses", func(t *testing.T) {
		t.Parallel()
		from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
		q, args, err := buildListQuery(ListFilter{
			TenantID:   "t1",
			Actions:    []string{"create"},
			ActorType:  "user",
			ActorID:    "u1",
			TargetType: "tenant",
			TargetID:   "tid",
			From:       ptrext.Of(from),
			To:         ptrext.Of(to),
		}, 20)
		require.NoError(t, err)
		require.True(t, strings.Contains(q, "tenant_id = $1"))
		require.True(t, strings.Contains(q, "action IN ($2)"))
		require.True(t, strings.Contains(q, "actor_type = $3"))
		require.True(t, strings.Contains(q, "actor_id = $4"))
		require.True(t, strings.Contains(q, "target_type = $5"))
		require.True(t, strings.Contains(q, "target_id = $6"))
		require.True(t, strings.Contains(q, "created_at >= $7"))
		require.True(t, strings.Contains(q, "created_at <= $8"))
		require.Len(t, args, 9) // 8 filter values + LIMIT arg
	})

	t.Run("arg numbering is sequential", func(t *testing.T) {
		t.Parallel()
		q, args, err := buildListQuery(ListFilter{
			TenantID:  "t1",
			ActorType: "api_key",
			TargetID:  "xyz",
		}, 100)
		require.NoError(t, err)
		require.True(t, strings.Contains(q, "tenant_id = $1"))
		require.True(t, strings.Contains(q, "actor_type = $2"))
		require.True(t, strings.Contains(q, "target_id = $3"))
		require.Equal(t, "t1", args[0])
		require.Equal(t, "api_key", args[1])
		require.Equal(t, "xyz", args[2])
	})
}

func Test_ParseCursor(t *testing.T) {
	t.Parallel()

	t.Run("empty string returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := ParseCursor("")
		require.Error(t, err)
	})

	t.Run("whitespace-only returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := ParseCursor("   ")
		require.Error(t, err)
	})

	t.Run("no colon returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := ParseCursor("invalid")
		require.Error(t, err)
	})

	t.Run("bad timestamp returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := ParseCursor("abc:123")
		require.Error(t, err)
		require.ErrorContains(t, err, "timestamp")
	})

	t.Run("bad id returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := ParseCursor("123:abc")
		require.Error(t, err)
		require.ErrorContains(t, err, "id")
	})

	t.Run("valid cursor parses correctly", func(t *testing.T) {
		t.Parallel()
		ts, id, err := ParseCursor("1719500000000000000:42")
		require.NoError(t, err)
		require.Equal(t, int64(42), id)
		require.Equal(t, time.Unix(0, 1719500000000000000).UTC(), ts)
	})
}

func Test_formatCursor(t *testing.T) {
	t.Parallel()

	t.Run("formats entry as unixnanos:id", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2025, 6, 27, 12, 0, 0, 0, time.UTC)
		entry := Entry{
			ID:        99,
			CreatedAt: ts,
		}
		got := formatCursor(entry)
		require.Equal(t, "1751025600000000000:99", got)
	})
}

func Test_paginateListResult(t *testing.T) {
	t.Parallel()

	makeEntries := func(n int) []Entry {
		entries := make([]Entry, n)
		for i := range entries {
			entries[i] = Entry{
				ID:        int64(i + 1),
				CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour),
			}
		}
		return entries
	}

	t.Run("unbounded returns all items with no cursor", func(t *testing.T) {
		t.Parallel()
		items := makeEntries(5)
		result := paginateListResult(items, 3, true)
		require.Len(t, result.Items, 5)
		require.Empty(t, result.NextCursor)
	})

	t.Run("items at limit returns all with no cursor", func(t *testing.T) {
		t.Parallel()
		items := makeEntries(3)
		result := paginateListResult(items, 3, false)
		require.Len(t, result.Items, 3)
		require.Empty(t, result.NextCursor)
	})

	t.Run("items below limit returns all with no cursor", func(t *testing.T) {
		t.Parallel()
		items := makeEntries(2)
		result := paginateListResult(items, 5, false)
		require.Len(t, result.Items, 2)
		require.Empty(t, result.NextCursor)
	})

	t.Run("items above limit truncates and sets cursor", func(t *testing.T) {
		t.Parallel()
		items := makeEntries(4)
		result := paginateListResult(items, 3, false)
		require.Len(t, result.Items, 3)
		require.NotEmpty(t, result.NextCursor)
		// cursor should come from items[limit-1] = items[2]
		expected := formatCursor(items[2])
		require.Equal(t, expected, result.NextCursor)
	})
}
