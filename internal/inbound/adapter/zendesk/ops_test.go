// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"errors"
	"net/http"
	"testing"
)

func TestIsPermanentZendeskError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "apiError unauthorized",
			err:  apiError{Method: "test", Status: http.StatusUnauthorized, Code: "unauthorized"},
			want: true,
		},
		{
			name: "apiError 401 without code",
			err:  apiError{Method: "test", Status: http.StatusUnauthorized},
			want: true,
		},
		{
			name: "apiError rate_limited is transient",
			err:  apiError{Method: "test", Status: http.StatusTooManyRequests, Code: "rate_limited"},
			want: false,
		},
		{
			name: "apiError 403 forbidden is permanent",
			err:  apiError{Method: "test", Status: http.StatusForbidden, Code: "forbidden"},
			want: true,
		},
		{
			name: "string error with forbidden",
			err:  errors.New("request forbidden by Zendesk"),
			want: true,
		},
		{
			name: "apiError 500 is transient",
			err:  apiError{Method: "test", Status: http.StatusInternalServerError},
			want: false,
		},
		{
			name: "string error with unauthorized",
			err:  errors.New("Couldn't authenticate you"),
			want: true,
		},
		{
			name: "string error with invalid credentials",
			err:  errors.New("invalid credentials for user"),
			want: true,
		},
		{
			name: "string error with invalid_credentials",
			err:  errors.New("error: invalid_credentials"),
			want: true,
		},
		{
			name: "generic transient error",
			err:  errors.New("connection timeout"),
			want: false,
		},
		{
			name: "network error",
			err:  errors.New("dial tcp: connection refused"),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isPermanentZendeskError(tc.err)
			if got != tc.want {
				t.Errorf("isPermanentZendeskError() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsDuplicateError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "idempotency key conflict",
			err:  errors.New("idempotency key used with different request"),
			want: true,
		},
		{
			name: "case insensitive",
			err:  errors.New("Idempotency Key Used With Different Request"),
			want: true,
		},
		{
			name: "unrelated error",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "empty error",
			err:  errors.New(""),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isDuplicateError(tc.err)
			if got != tc.want {
				t.Errorf("isDuplicateError() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApiError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  apiError
		want string
	}{
		{
			name: "empty code",
			err:  apiError{Method: "test"},
			want: "zendesk test failed",
		},
		{
			name: "code with status",
			err:  apiError{Method: "test", Status: 401, Code: "unauthorized"},
			want: "zendesk test: unauthorized status=401",
		},
		{
			name: "code without status",
			err:  apiError{Method: "test", Code: "rate_limited"},
			want: "zendesk test: rate_limited",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			if got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApiError_Permanent(t *testing.T) {
	tests := []struct {
		name string
		err  apiError
		want bool
	}{
		{
			name: "code unauthorized",
			err:  apiError{Code: "unauthorized"},
			want: true,
		},
		{
			name: "status 401 without code",
			err:  apiError{Status: http.StatusUnauthorized},
			want: true,
		},
		{
			name: "code forbidden",
			err:  apiError{Code: "forbidden"},
			want: true,
		},
		{
			name: "status 403 without code",
			err:  apiError{Status: http.StatusForbidden},
			want: true,
		},
		{
			name: "rate_limited",
			err:  apiError{Code: "rate_limited"},
			want: false,
		},
		{
			name: "server error",
			err:  apiError{Status: 500},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Permanent()
			if got != tc.want {
				t.Errorf("Permanent() = %v, want %v", got, tc.want)
			}
		})
	}
}
