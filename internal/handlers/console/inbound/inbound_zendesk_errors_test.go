// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"errors"
	"testing"
)

func TestFriendlyZendeskError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		subdomain    string
		wantContains string
	}{
		{"no help desk", errors.New("No help desk at acme.zendesk.com"), "acme", "not found"},
		{"couldn't authenticate", errors.New("Couldn't authenticate you"), "x", "authentication failed"},
		{"unauthorized", errors.New("unauthorized"), "x", "credentials were rejected"},
		{"forbidden", errors.New("request forbidden"), "x", "access denied"},
		{"rate_limited", errors.New("rate_limited"), "x", "rate limit"},
		{"timeout", errors.New("context deadline exceeded"), "x", "timed out"},
		{"dns failure", errors.New("no such host"), "bad", "Cannot reach"},
		{"validation passthrough", errors.New("can only contain lowercase"), "x", "can only contain"},
		{"required passthrough", errors.New("email is required"), "x", "is required"},
		{"unknown error", errors.New("something weird"), "x", "connection failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := friendlyZendeskError(tc.err, tc.subdomain)
			if !containsCI(got, tc.wantContains) {
				t.Errorf("friendlyZendeskError() = %q, want to contain %q", got, tc.wantContains)
			}
		})
	}
}

func containsCI(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(contains(s, substr) || contains(lower(s), lower(substr)))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func lower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestFriendlyZendeskError_429(t *testing.T) {
	err := errors.New("too many requests 429")
	got := friendlyZendeskError(err, "acme")
	if !containsCI(got, "rate limit") {
		t.Errorf("expected rate limit message, got %q", got)
	}
}
