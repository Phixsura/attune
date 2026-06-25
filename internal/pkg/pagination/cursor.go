// SPDX-License-Identifier: Apache-2.0

// Package pagination provides cursor-based pagination utilities.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidCursor is returned when a cursor cannot be decoded.
var ErrInvalidCursor = errors.New("invalid cursor")

// Cursor represents a pagination cursor.
type Cursor struct {
	// ID is the primary key of the last item.
	ID string `json:"id,omitempty"`

	// Timestamp is the sort timestamp of the last item.
	Timestamp time.Time `json:"ts,omitempty"`

	// Offset is a numeric offset (for offset-based fallback).
	Offset int64 `json:"off,omitempty"`

	// Direction indicates pagination direction (next/prev).
	Direction string `json:"dir,omitempty"`
}

// Encode encodes the cursor to a URL-safe string.
func (c Cursor) Encode() string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

// DecodeCursor decodes a cursor string.
func DecodeCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}

	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}

	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return Cursor{}, ErrInvalidCursor
	}

	return c, nil
}

// Page represents a page of results.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	PrevCursor string `json:"prev_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
	Total      int64  `json:"total,omitempty"`
}

// NewPage creates a new page from items.
func NewPage[T any](items []T, limit int, cursorFn func(T) Cursor) Page[T] {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		c := cursorFn(last)
		c.Direction = "next"
		nextCursor = c.Encode()
	}

	return Page[T]{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}
}

// Request represents a pagination request.
type Request struct {
	Cursor string
	Limit  int
}

// ParseRequest parses pagination parameters from query string.
func ParseRequest(cursor string, limitStr string, defaultLimit, maxLimit int) Request {
	limit := defaultLimit
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	return Request{
		Cursor: strings.TrimSpace(cursor),
		Limit:  limit,
	}
}

// SQLCondition returns SQL condition for cursor-based pagination.
// Returns empty string if no cursor.
func (c Cursor) SQLCondition(idCol, tsCol string) string {
	if c.ID == "" && c.Timestamp.IsZero() {
		return ""
	}

	if c.Direction == "prev" {
		if !c.Timestamp.IsZero() && c.ID != "" {
			return "(" + tsCol + " > $cursor_ts OR (" + tsCol + " = $cursor_ts AND " + idCol + " > $cursor_id))"
		}
		if c.ID != "" {
			return idCol + " > $cursor_id"
		}
	}

	// Default: next direction
	if !c.Timestamp.IsZero() && c.ID != "" {
		return "(" + tsCol + " < $cursor_ts OR (" + tsCol + " = $cursor_ts AND " + idCol + " < $cursor_id))"
	}
	if c.ID != "" {
		return idCol + " < $cursor_id"
	}

	return ""
}
