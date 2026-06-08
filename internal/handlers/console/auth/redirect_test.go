// SPDX-License-Identifier: Apache-2.0

package auth

import "testing"

func TestRedirectIsSafe(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		postLogin string
		want      bool
	}{
		{"empty rejected", "https://attune.app", "", false},
		{"no leading slash", "https://attune.app", "console/", false},
		{"protocol-relative", "https://attune.app", "//evil.com/", false},
		{"backslash", "https://attune.app", "/\\evil.com", false},
		{"newline", "https://attune.app", "/c\non/", false},
		{"plain path", "https://attune.app", "/console/", true},
		{"with query", "https://attune.app", "/console/feedback?id=1", true},
		{"unicode rejected", "https://attune.app", "/conşole/", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redirectIsSafe(tt.baseURL, tt.postLogin); got != tt.want {
				t.Errorf("redirectIsSafe(%q, %q) = %v; want %v", tt.baseURL, tt.postLogin, got, tt.want)
			}
		})
	}
}
