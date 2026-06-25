// SPDX-License-Identifier: Apache-2.0

package pagination

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCursor_EncodeDecode(t *testing.T) {
	t.Parallel()
	original := Cursor{
		ID:        "abc123",
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Direction: "next",
	}

	encoded := original.Encode()
	require.NotEmpty(t, encoded)

	decoded, err := DecodeCursor(encoded)
	require.NoError(t, err)
	require.Equal(t, original.ID, decoded.ID)
	require.Equal(t, original.Direction, decoded.Direction)
	require.True(t, original.Timestamp.Equal(decoded.Timestamp))
}

func TestDecodeCursor_Empty(t *testing.T) {
	t.Parallel()
	c, err := DecodeCursor("")
	require.NoError(t, err)
	require.Empty(t, c.ID)
}

func TestDecodeCursor_Invalid(t *testing.T) {
	t.Parallel()
	_, err := DecodeCursor("not-valid-base64!!!")
	require.ErrorIs(t, err, ErrInvalidCursor)
}

func TestDecodeCursor_InvalidJSON(t *testing.T) {
	t.Parallel()
	// Valid base64 but invalid JSON
	_, err := DecodeCursor("bm90LWpzb24")
	require.ErrorIs(t, err, ErrInvalidCursor)
}

type testItem struct {
	ID   string
	Name string
}

func TestNewPage_NoMore(t *testing.T) {
	t.Parallel()
	items := []testItem{
		{ID: "1", Name: "a"},
		{ID: "2", Name: "b"},
	}

	page := NewPage(items, 10, func(item testItem) Cursor {
		return Cursor{ID: item.ID}
	})

	require.Len(t, page.Items, 2)
	require.False(t, page.HasMore)
	require.Empty(t, page.NextCursor)
}

func TestNewPage_HasMore(t *testing.T) {
	t.Parallel()
	items := []testItem{
		{ID: "1", Name: "a"},
		{ID: "2", Name: "b"},
		{ID: "3", Name: "c"},
	}

	page := NewPage(items, 2, func(item testItem) Cursor {
		return Cursor{ID: item.ID}
	})

	require.Len(t, page.Items, 2)
	require.True(t, page.HasMore)
	require.NotEmpty(t, page.NextCursor)

	// Verify cursor points to last item
	cursor, err := DecodeCursor(page.NextCursor)
	require.NoError(t, err)
	require.Equal(t, "2", cursor.ID)
	require.Equal(t, "next", cursor.Direction)
}

func TestParseRequest_Defaults(t *testing.T) {
	t.Parallel()
	req := ParseRequest("", "", 20, 100)
	require.Equal(t, 20, req.Limit)
	require.Empty(t, req.Cursor)
}

func TestParseRequest_CustomLimit(t *testing.T) {
	t.Parallel()
	req := ParseRequest("cursor123", "50", 20, 100)
	require.Equal(t, 50, req.Limit)
	require.Equal(t, "cursor123", req.Cursor)
}

func TestParseRequest_MaxLimit(t *testing.T) {
	t.Parallel()
	req := ParseRequest("", "500", 20, 100)
	require.Equal(t, 100, req.Limit) // Capped at max
}

func TestParseRequest_InvalidLimit(t *testing.T) {
	t.Parallel()
	req := ParseRequest("", "invalid", 20, 100)
	require.Equal(t, 20, req.Limit) // Falls back to default
}

func TestCursor_SQLCondition_Empty(t *testing.T) {
	t.Parallel()
	c := Cursor{}
	require.Empty(t, c.SQLCondition("id", "created_at"))
}

func TestCursor_SQLCondition_IDOnly(t *testing.T) {
	t.Parallel()
	c := Cursor{ID: "abc", Direction: "next"}
	cond := c.SQLCondition("id", "created_at")
	require.Contains(t, cond, "id < $cursor_id")
}

func TestCursor_SQLCondition_Prev(t *testing.T) {
	t.Parallel()
	c := Cursor{ID: "abc", Direction: "prev"}
	cond := c.SQLCondition("id", "created_at")
	require.Contains(t, cond, "id > $cursor_id")
}
