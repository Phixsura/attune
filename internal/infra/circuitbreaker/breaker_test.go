// SPDX-License-Identifier: Apache-2.0

package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNew_DefaultsApplied(t *testing.T) {
	t.Parallel()
	b := New(Config{Name: "test"})
	require.Equal(t, StateClosed, b.State())
	require.Equal(t, 5, b.cfg.FailureThreshold)
	require.Equal(t, 2, b.cfg.SuccessThreshold)
	require.Equal(t, 30*time.Second, b.cfg.OpenDuration)
	require.Equal(t, 1, b.cfg.HalfOpenMaxConcurrent)
}

func TestNew_NegativeThresholds(t *testing.T) {
	t.Parallel()
	b := New(Config{Name: "test", FailureThreshold: -1, SuccessThreshold: -1, OpenDuration: -1, HalfOpenMaxConcurrent: -1})
	require.Equal(t, 5, b.cfg.FailureThreshold)
	require.Equal(t, 2, b.cfg.SuccessThreshold)
	require.Equal(t, 30*time.Second, b.cfg.OpenDuration)
	require.Equal(t, 1, b.cfg.HalfOpenMaxConcurrent)
}

func TestBreaker_ClosedState(t *testing.T) {
	t.Parallel()
	b := New(DefaultConfig("test"))

	// Should allow requests
	done, err := b.Allow()
	require.NoError(t, err)
	require.NotNil(t, done)
	done(true)

	require.Equal(t, StateClosed, b.State())
}

func TestBreaker_OpensAfterFailures(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("test")
	cfg.FailureThreshold = 3
	b := New(cfg)

	// Fail 3 times
	for i := 0; i < 3; i++ {
		done, err := b.Allow()
		require.NoError(t, err)
		done(false)
	}

	require.Equal(t, StateOpen, b.State())

	// Next request should be rejected
	_, err := b.Allow()
	require.ErrorIs(t, err, ErrCircuitOpen)
}

func TestBreaker_TransitionsToHalfOpen(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("test")
	cfg.FailureThreshold = 2
	cfg.OpenDuration = 10 * time.Millisecond
	b := New(cfg)

	// Open the circuit
	for i := 0; i < 2; i++ {
		done, _ := b.Allow()
		done(false)
	}
	require.Equal(t, StateOpen, b.State())

	// Wait for open duration
	time.Sleep(15 * time.Millisecond)

	// Should transition to half-open
	done, err := b.Allow()
	require.NoError(t, err)
	require.Equal(t, StateHalfOpen, b.State())
	done(true)
}

func TestBreaker_ClosesAfterSuccessInHalfOpen(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("test")
	cfg.FailureThreshold = 2
	cfg.SuccessThreshold = 2
	cfg.OpenDuration = 10 * time.Millisecond
	cfg.HalfOpenMaxConcurrent = 2
	b := New(cfg)

	// Open the circuit
	for i := 0; i < 2; i++ {
		done, _ := b.Allow()
		done(false)
	}

	time.Sleep(15 * time.Millisecond)

	// Success in half-open
	for i := 0; i < 2; i++ {
		done, err := b.Allow()
		require.NoError(t, err)
		done(true)
	}

	require.Equal(t, StateClosed, b.State())
}

func TestBreaker_ReopensOnHalfOpenFailure(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("test")
	cfg.FailureThreshold = 2
	cfg.OpenDuration = 10 * time.Millisecond
	b := New(cfg)

	// Open the circuit
	for i := 0; i < 2; i++ {
		done, _ := b.Allow()
		done(false)
	}

	time.Sleep(15 * time.Millisecond)

	// Fail in half-open
	done, err := b.Allow()
	require.NoError(t, err)
	done(false)

	require.Equal(t, StateOpen, b.State())
}

func TestBreaker_Execute(t *testing.T) {
	t.Parallel()
	b := New(DefaultConfig("test"))
	ctx := context.Background()

	// Success
	err := b.Execute(ctx, func(context.Context) error {
		return nil
	})
	require.NoError(t, err)

	// Failure
	testErr := errors.New("test error")
	err = b.Execute(ctx, func(context.Context) error {
		return testErr
	})
	require.ErrorIs(t, err, testErr)
}

func TestBreaker_Reset(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("test")
	cfg.FailureThreshold = 2
	b := New(cfg)

	// Open the circuit
	for i := 0; i < 2; i++ {
		done, _ := b.Allow()
		done(false)
	}
	require.Equal(t, StateOpen, b.State())

	// Reset
	b.Reset()
	require.Equal(t, StateClosed, b.State())

	// Should allow requests
	done, err := b.Allow()
	require.NoError(t, err)
	done(true)
}

func TestBreaker_HalfOpenConcurrencyLimit(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("test")
	cfg.FailureThreshold = 2
	cfg.OpenDuration = 10 * time.Millisecond
	cfg.HalfOpenMaxConcurrent = 1
	b := New(cfg)

	// Open the circuit
	for i := 0; i < 2; i++ {
		done, _ := b.Allow()
		done(false)
	}

	time.Sleep(15 * time.Millisecond)

	// First request in half-open allowed
	done1, err := b.Allow()
	require.NoError(t, err)

	// Second request rejected (concurrency limit)
	_, err = b.Allow()
	require.ErrorIs(t, err, ErrCircuitOpen)

	// Complete first request
	done1(true)

	// Now another request allowed
	done2, err := b.Allow()
	require.NoError(t, err)
	done2(true)
}
