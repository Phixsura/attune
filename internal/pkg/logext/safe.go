package logext

import (
	"net/url"
	"strings"
)

// SafeURLForLog keeps only scheme + host from a URL before it enters logs.
func SafeURLForLog(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "invalid"
	}
	return u.Scheme + "://" + u.Host
}
