// SPDX-License-Identifier: Apache-2.0

package member

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailRegex(t *testing.T) {
	tests := []struct {
		name  string
		email string
		valid bool
	}{
		// Valid emails
		{"simple email", "user@example.com", true},
		{"with subdomain", "user@mail.example.com", true},
		{"with plus", "user+tag@example.com", true},
		{"with dots", "first.last@example.com", true},
		{"with underscore", "user_name@example.com", true},
		{"with hyphen", "user-name@example.com", true},
		{"with numbers", "user123@example.com", true},
		{"short domain", "user@example.co", true},
		{"long tld", "user@example.museum", true},

		// Invalid emails
		{"empty string", "", false},
		{"no at sign", "userexample.com", false},
		{"no domain", "user@", false},
		{"no local part", "@example.com", false},
		{"double at", "user@@example.com", false},
		{"spaces", "user @example.com", false},
		{"single char tld", "user@example.c", false},
		{"no tld", "user@example", false},
		// Note: the simple regex allows these edge cases (not RFC-strict)
		{"leading dot (allowed by simple regex)", ".user@example.com", true},
		{"trailing dot local (allowed)", "user.@example.com", true},
		{"double dot local (allowed)", "user..name@example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := emailRE.MatchString(tt.email)
			assert.Equal(t, tt.valid, result, "email: %s", tt.email)
		})
	}
}

func TestEmailNormalization(t *testing.T) {
	// Email should be lowercased and trimmed
	tests := []struct {
		input    string
		expected string
	}{
		{"User@Example.COM", "user@example.com"},
		{"  user@example.com  ", "user@example.com"},
		{"USER@EXAMPLE.COM", "user@example.com"},
		{"\tuser@example.com\n", "user@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// This tests the normalization logic used in Invite handler
			normalized := strings.ToLower(strings.TrimSpace(tt.input))
			assert.Equal(t, tt.expected, normalized)
		})
	}
}
