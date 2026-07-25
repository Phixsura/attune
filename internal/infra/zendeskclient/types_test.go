// SPDX-License-Identifier: Apache-2.0

package zendeskclient_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/zendeskclient"
)

func TestAPIError_Permanent(t *testing.T) {
	tests := []struct {
		name string
		err  zendeskclient.APIError
		want bool
	}{
		{"unauthorized code", zendeskclient.APIError{Code: "unauthorized"}, true},
		{"forbidden code", zendeskclient.APIError{Code: "forbidden"}, true},
		{"401 status", zendeskclient.APIError{Status: http.StatusUnauthorized}, true},
		{"403 status", zendeskclient.APIError{Status: http.StatusForbidden}, true},
		{"500 status", zendeskclient.APIError{Status: 500}, false},
		{"rate_limited", zendeskclient.APIError{Code: "rate_limited"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Permanent(); got != tc.want {
				t.Errorf("Permanent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  zendeskclient.APIError
		want string
	}{
		{"empty code", zendeskclient.APIError{Method: "test"}, "zendesk test failed"},
		{"code+status", zendeskclient.APIError{Method: "test", Status: 401, Code: "unauthorized"}, "zendesk test: unauthorized status=401"},
		{"code only", zendeskclient.APIError{Method: "test", Code: "rate_limited"}, "zendesk test: rate_limited"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRateLimitError_Error(t *testing.T) {
	e := zendeskclient.RateLimitError{Method: "incremental", RetryAfter: 60 * time.Second}
	want := "zendesk incremental: rate limited (retry after 1m0s)"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty", "", 60 * time.Second},
		{"30 seconds", "30", 30 * time.Second},
		{"zero", "0", 60 * time.Second},
		{"negative", "-5", 60 * time.Second},
		{"non-numeric", "abc", 60 * time.Second},
		{"capped at 15min", "99999", 15 * time.Minute},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.value != "" {
				h.Set("Retry-After", tc.value)
			}
			got := zendeskclient.ParseRetryAfter(h)
			if got != tc.want {
				t.Errorf("ParseRetryAfter(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
