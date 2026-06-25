// SPDX-License-Identifier: Apache-2.0

// Package logsanitize provides utilities for sanitizing data before logging.
package logsanitize

import (
	"regexp"
	"strings"
)

// Path sanitizes a URL path for logging by removing potentially injected
// newlines and control characters that could corrupt log parsing.
func Path(path string) string {
	return sanitizeString(path, 256)
}

// Header sanitizes an HTTP header value for logging.
func Header(value string) string {
	return sanitizeString(value, 128)
}

// UserInput sanitizes arbitrary user input for logging.
func UserInput(value string) string {
	return sanitizeString(value, 512)
}

// sanitizeString removes newlines, control characters, and truncates length.
func sanitizeString(s string, maxLen int) string {
	// Remove newlines and carriage returns (log injection prevention)
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")

	// Remove other control characters
	s = controlCharRegex.ReplaceAllString(s, "")

	// Truncate to max length
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}

	return s
}

// controlCharRegex matches ASCII control characters except tab
var controlCharRegex = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)

// Email sanitizes an email for logging (may contain user input).
func Email(email string) string {
	return sanitizeString(email, 256)
}

// Query sanitizes a database query for logging (removes excess whitespace).
func Query(query string) string {
	// Collapse multiple spaces/newlines to single space
	s := whitespaceRegex.ReplaceAllString(query, " ")
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		s = s[:500] + "..."
	}
	return s
}

var whitespaceRegex = regexp.MustCompile(`\s+`)

// TruncateBytes truncates a byte slice and returns a safe preview string.
func TruncateBytes(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return sanitizeString(string(b), maxLen)
	}
	return sanitizeString(string(b[:maxLen]), maxLen) + "..."
}
