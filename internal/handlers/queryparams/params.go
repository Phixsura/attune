// SPDX-License-Identifier: Apache-2.0

// Package queryparams provides safe URL query parameter parsing.
package queryparams

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Bool parses a boolean query parameter. Returns defaultVal if the
// parameter is missing or invalid.
func Bool(r *http.Request, key string, defaultVal bool) bool {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return defaultVal
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return defaultVal
	}
}

// Int parses an integer query parameter with bounds checking.
// Returns defaultVal if the parameter is missing, invalid, or out of bounds.
func Int(r *http.Request, key string, defaultVal, minVal, maxVal int) int {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	if n < minVal {
		return minVal
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

// Int64 parses an int64 query parameter with bounds checking.
func Int64(r *http.Request, key string, defaultVal, minVal, maxVal int64) int64 {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return defaultVal
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return defaultVal
	}
	if n < minVal {
		return minVal
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

// String parses a string query parameter with length limit.
// Returns defaultVal if the parameter is missing or exceeds maxLen.
func String(r *http.Request, key string, defaultVal string, maxLen int) string {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return defaultVal
	}
	if len(v) > maxLen {
		return defaultVal
	}
	return v
}

// Enum parses a string query parameter that must be one of allowed values.
// Returns defaultVal if the parameter is missing or not in allowed list.
func Enum(r *http.Request, key string, defaultVal string, allowed []string) string {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return defaultVal
	}
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return defaultVal
}

// Duration parses a duration query parameter with bounds checking.
// Returns defaultVal if the parameter is missing, invalid, or out of bounds.
func Duration(r *http.Request, key string, defaultVal, minVal, maxVal time.Duration) time.Duration {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return defaultVal
	}
	if d < minVal {
		return minVal
	}
	if d > maxVal {
		return maxVal
	}
	return d
}

// Time parses a time query parameter in RFC3339 format.
// Returns defaultVal if the parameter is missing or invalid.
func Time(r *http.Request, key string, defaultVal time.Time) time.Time {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return defaultVal
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return defaultVal
	}
	return t
}
