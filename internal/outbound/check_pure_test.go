// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package outbound

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCheckWebhook_ErrorMessages(t *testing.T) {
	t.Parallel()

	check := CheckWebhook("my-webhook")
	ctx := context.Background()

	t.Run("retryable error contains label", func(t *testing.T) {
		t.Parallel()
		err := check(ctx, 429, []byte("rate limited"))
		if err == nil {
			t.Fatal("want error for 429")
		}
		if !strings.Contains(err.Error(), "my-webhook") {
			t.Errorf("error = %q, want label in message", err.Error())
		}
		if !strings.Contains(err.Error(), "429") {
			t.Errorf("error = %q, want status in message", err.Error())
		}
	})

	t.Run("terminal error contains label and status", func(t *testing.T) {
		t.Parallel()
		err := check(ctx, 403, nil)
		if err == nil {
			t.Fatal("want error for 403")
		}
		if !errors.Is(err, ErrTerminal) {
			t.Errorf("want ErrTerminal, got %v", err)
		}
		if !strings.Contains(err.Error(), "my-webhook") {
			t.Errorf("error = %q, want label in message", err.Error())
		}
	})

	t.Run("5xx error is not terminal", func(t *testing.T) {
		t.Parallel()
		err := check(ctx, 503, nil)
		if err == nil {
			t.Fatal("want error for 503")
		}
		if errors.Is(err, ErrTerminal) {
			t.Error("5xx should not be ErrTerminal")
		}
	})
}

func TestCheckGitHub_ErrorMessages(t *testing.T) {
	t.Parallel()

	check := CheckGitHub("gh-issues")
	ctx := context.Background()

	t.Run("403 secondary rate limit is retryable", func(t *testing.T) {
		t.Parallel()
		err := check(ctx, 403, nil)
		if err == nil {
			t.Fatal("want error for 403")
		}
		if errors.Is(err, ErrTerminal) {
			t.Error("403 should not be ErrTerminal for GitHub (secondary rate limit)")
		}
		if !strings.Contains(err.Error(), "rate limit") {
			t.Errorf("error = %q, want rate limit mention", err.Error())
		}
	})

	t.Run("202 is non-success (not 200/201)", func(t *testing.T) {
		t.Parallel()
		err := check(ctx, 202, nil)
		if err == nil {
			t.Fatal("want error for 202 (GitHub only accepts 200/201)")
		}
		if errors.Is(err, ErrTerminal) {
			t.Error("202 should not be terminal")
		}
	})

	t.Run("5xx contains label", func(t *testing.T) {
		t.Parallel()
		err := check(ctx, 500, nil)
		if err == nil {
			t.Fatal("want error for 500")
		}
		if !strings.Contains(err.Error(), "gh-issues") {
			t.Errorf("error = %q, want label in message", err.Error())
		}
	})
}

func TestErrTerminal_IsSentinel(t *testing.T) {
	t.Parallel()

	if ErrTerminal == nil {
		t.Fatal("ErrTerminal should not be nil")
	}
	if ErrTerminal.Error() == "" {
		t.Fatal("ErrTerminal should have a message")
	}
}
