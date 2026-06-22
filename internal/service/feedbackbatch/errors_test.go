// SPDX-License-Identifier: Apache-2.0

package feedbackbatch

import (
	"errors"
	"testing"
)

func TestErrorsAreSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{"ErrRateLimited", ErrRateLimited},
		{"ErrIdempotencyConflict", ErrIdempotencyConflict},
		{"ErrRequestInProgress", ErrRequestInProgress},
		{"ErrCountMismatch", ErrCountMismatch},
		{"ErrExceedsMaxAffected", ErrExceedsMaxAffected},
		{"ErrConcurrentJobLimit", ErrConcurrentJobLimit},
		{"ErrVersionConflict", ErrVersionConflict},
		{"ErrNoOperation", ErrNoOperation},
		{"ErrNoSelection", ErrNoSelection},
		{"ErrMixedSelection", ErrMixedSelection},
		{"ErrJobNotFound", ErrJobNotFound},
		{"ErrJobNotCancellable", ErrJobNotCancellable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.err == nil {
				t.Errorf("%s is nil", tt.name)
			}
			if tt.err.Error() == "" {
				t.Errorf("%s has empty message", tt.name)
			}
		})
	}
}

func TestErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	allErrors := []error{
		ErrRateLimited,
		ErrIdempotencyConflict,
		ErrRequestInProgress,
		ErrCountMismatch,
		ErrExceedsMaxAffected,
		ErrConcurrentJobLimit,
		ErrVersionConflict,
		ErrNoOperation,
		ErrNoSelection,
		ErrMixedSelection,
		ErrJobNotFound,
		ErrJobNotCancellable,
	}

	for i, err1 := range allErrors {
		for j, err2 := range allErrors {
			if i != j && errors.Is(err1, err2) {
				t.Errorf("errors.Is(%v, %v) = true, want false", err1, err2)
			}
		}
	}
}

func TestErrorWrapping(t *testing.T) {
	t.Parallel()

	wrapped := errors.Join(ErrRateLimited, errors.New("extra context"))
	if !errors.Is(wrapped, ErrRateLimited) {
		t.Error("errors.Is(wrapped, ErrRateLimited) = false, want true")
	}
}
