package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryDatabasePingSucceedsAfterTransientFailures(t *testing.T) {
	t.Parallel()
	transient := errors.New("dns not ready")
	attempts := 0

	err := retryDatabasePing(
		context.Background(),
		200*time.Millisecond,
		time.Millisecond,
		func(context.Context) error {
			attempts++
			if attempts < 3 {
				return transient
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("retryDatabasePing() err = %v; want nil", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d; want 3", attempts)
	}
}

func TestRetryDatabasePingReturnsLastErrorOnTimeout(t *testing.T) {
	t.Parallel()
	want := errors.New("still unavailable")

	err := retryDatabasePing(
		context.Background(),
		5*time.Millisecond,
		time.Millisecond,
		func(context.Context) error {
			return want
		},
	)

	if !errors.Is(err, want) {
		t.Fatalf("retryDatabasePing() err = %v; want %v", err, want)
	}
}
