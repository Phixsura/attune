// SPDX-License-Identifier: Apache-2.0

package semanticsearch

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// NormalizeQuery trims and validates operator search text before it reaches
// embedding generation or PostgreSQL full-text search.
func NormalizeQuery(query string) (string, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", ErrEmptyQuery
	}
	if utf8.RuneCountInString(trimmed) > MaxQueryRunes {
		return "", ErrQueryTooLong
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return "", ErrInvalidQuery
		}
	}
	return trimmed, nil
}
