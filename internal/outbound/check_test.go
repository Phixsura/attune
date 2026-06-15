// SPDX-License-Identifier: Apache-2.0

package outbound

import (
	"context"
	"errors"
	"testing"
)

func TestCheckWebhook(t *testing.T) {
	check := CheckWebhook("test")
	ctx := context.Background()

	tests := []struct {
		name     string
		status   int
		wantNil  bool
		wantTerm bool
	}{
		{"200 ok", 200, true, false},
		{"201 ok", 201, true, false},
		{"204 ok", 204, true, false},
		{"299 ok", 299, true, false},
		{"400 terminal", 400, false, true},
		{"401 terminal", 401, false, true},
		{"403 terminal", 403, false, true},
		{"404 terminal", 404, false, true},
		{"408 retryable", 408, false, false},
		{"429 retryable", 429, false, false},
		{"500 retryable", 500, false, false},
		{"502 retryable", 502, false, false},
		{"503 retryable", 503, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := check(ctx, tt.status, nil)
			if tt.wantNil && err != nil {
				t.Errorf("status %d: got error %v, want nil", tt.status, err)
			}
			if !tt.wantNil && err == nil {
				t.Errorf("status %d: got nil, want error", tt.status)
			}
			if tt.wantTerm && !errors.Is(err, ErrTerminal) {
				t.Errorf("status %d: got %v, want ErrTerminal", tt.status, err)
			}
			if !tt.wantTerm && errors.Is(err, ErrTerminal) {
				t.Errorf("status %d: got ErrTerminal, want retryable", tt.status)
			}
		})
	}
}

func TestCheckGitHub(t *testing.T) {
	check := CheckGitHub("github")
	ctx := context.Background()

	tests := []struct {
		name     string
		status   int
		wantNil  bool
		wantTerm bool
	}{
		{"200 ok", 200, true, false},
		{"201 ok (created)", 201, true, false},
		{"400 terminal", 400, false, true},
		{"401 terminal", 401, false, true},
		{"403 retryable (secondary rate limit)", 403, false, false},
		{"404 terminal", 404, false, true},
		{"408 retryable", 408, false, false},
		{"429 retryable", 429, false, false},
		{"500 retryable", 500, false, false},
		{"502 retryable", 502, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := check(ctx, tt.status, nil)
			if tt.wantNil && err != nil {
				t.Errorf("status %d: got error %v, want nil", tt.status, err)
			}
			if !tt.wantNil && err == nil {
				t.Errorf("status %d: got nil, want error", tt.status)
			}
			if tt.wantTerm && !errors.Is(err, ErrTerminal) {
				t.Errorf("status %d: got %v, want ErrTerminal", tt.status, err)
			}
			if !tt.wantTerm && errors.Is(err, ErrTerminal) {
				t.Errorf("status %d: got ErrTerminal, want retryable", tt.status)
			}
		})
	}
}
