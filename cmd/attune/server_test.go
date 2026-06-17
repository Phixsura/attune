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

func TestRetryDatabasePingWithoutTimeoutCallsPingOnce(t *testing.T) {
	t.Parallel()
	called := 0

	err := retryDatabasePing(
		context.Background(),
		0,
		time.Millisecond,
		func(context.Context) error {
			called++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("retryDatabasePing() err = %v; want nil", err)
	}
	if called != 1 {
		t.Fatalf("ping calls = %d; want 1", called)
	}
}

func TestRetryDatabasePingDefaultsZeroInterval(t *testing.T) {
	t.Parallel()
	called := 0

	err := retryDatabasePing(
		context.Background(),
		10*time.Millisecond,
		0,
		func(context.Context) error {
			called++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("retryDatabasePing() err = %v; want nil", err)
	}
	if called != 1 {
		t.Fatalf("ping calls = %d; want 1", called)
	}
}

func TestSleepBeforeShutdownReturnsImmediatelyForNonPositiveDelay(t *testing.T) {
	t.Parallel()
	start := time.Now()
	sleepBeforeShutdown(0)
	if time.Since(start) > 20*time.Millisecond {
		t.Fatalf("sleepBeforeShutdown(0) took too long: %s", time.Since(start))
	}
}

func TestSleepBeforeShutdownWaitsForDelay(t *testing.T) {
	t.Parallel()
	start := time.Now()
	sleepBeforeShutdown(5 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 4*time.Millisecond {
		t.Fatalf("sleepBeforeShutdown() elapsed = %s; want at least 4ms", elapsed)
	}
}

func TestShutdownTracingInvokesShutdownWithDeadline(t *testing.T) {
	t.Parallel()
	called := false

	shutdownTracing(func(ctx context.Context) error {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected shutdown context deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > 5*time.Second {
			t.Fatalf("shutdown deadline remaining = %s; want within 0..5s", remaining)
		}
		return errors.New("flush failed")
	})

	if !called {
		t.Fatal("expected shutdown function to be called")
	}
}
