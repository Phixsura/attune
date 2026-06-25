// SPDX-License-Identifier: Apache-2.0

// Package stringutil provides string manipulation utilities.
package stringutil

import (
	"strings"
	"unicode"
)

// Truncate truncates a string to maxLen characters, adding suffix if truncated.
func Truncate(s string, maxLen int, suffix string) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= len(suffix) {
		return suffix[:maxLen]
	}
	return s[:maxLen-len(suffix)] + suffix
}

// TruncateWords truncates a string to approximately maxLen characters,
// breaking at word boundaries when possible.
func TruncateWords(s string, maxLen int, suffix string) string {
	if len(s) <= maxLen {
		return s
	}
	cutoff := maxLen - len(suffix)
	if cutoff <= 0 {
		return suffix[:maxLen]
	}
	// Find last space before cutoff
	lastSpace := strings.LastIndex(s[:cutoff], " ")
	if lastSpace > 0 {
		return s[:lastSpace] + suffix
	}
	return s[:cutoff] + suffix
}

// IsBlank reports whether s is empty or contains only whitespace.
func IsBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

// FirstNonEmpty returns the first non-empty string from the arguments.
func FirstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// ToSnakeCase converts a string to snake_case.
func ToSnakeCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 5)
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ToCamelCase converts a snake_case string to camelCase.
func ToCamelCase(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// RemovePrefix removes a prefix if present, otherwise returns the original.
func RemovePrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}

// RemoveSuffix removes a suffix if present, otherwise returns the original.
func RemoveSuffix(s, suffix string) string {
	return strings.TrimSuffix(s, suffix)
}
