// SPDX-License-Identifier: Apache-2.0

package inboundtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Phixsura/attune/internal/inbound"
)

// TestAdapterContract — every adapter calls this from its own _test.
// Minimum bar (see spec §Conformance test suite):
//
//  1. Channel() returns a non-empty string with no whitespace or '/'.
//  2. Start(ctx, fakeDeps) followed by immediate Shutdown does not panic.
//  3. ctx cancellation propagates: Shutdown returns within 5s, no goroutine leak.
//  4. Idempotent shutdown: calling Shutdown twice does not panic.
//  5. Duplicate Register on the same channel panics.
//  6. Adapter actually drives its IngestPort end-to-end — Trigger (when
//     supplied) MUST cause at least one FakeIngest.Calls entry. This
//     criterion is the one promised by spec §Conformance test suite #5;
//     adapters that don't naturally know how to fire an Ingest from the
//     conformance suite alone (no live HTTP server / no IMAP fixture)
//     pass nil and the sub-test is skipped — they're expected to cover
//     this in their own adapter-level tests.
//
// The adapter under test passes a factory (typically the same constructor
// the adapter's own init() registers).
func TestAdapterContract(t *testing.T, factory inbound.Factory, opts ...ContractOption) {
	t.Helper()
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	cfg := contractConfig{} // mutated below via the closures in opts.
	for _, o := range opts {
		o(&cfg) // ptrext:allow out-param config builder
	}

	t.Run("ChannelNonEmpty", func(t *testing.T) {
		a := factory()
		ch := a.Channel()
		if ch == "" {
			t.Error("Channel() returned empty string")
		}
		if strings.ContainsAny(ch, " \t\n/") {
			t.Errorf("Channel() = %q contains whitespace or '/'", ch)
		}
	})

	t.Run("StartShutdownOK", func(t *testing.T) {
		a := factory()
		deps := DepsFor(nil, nil, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := a.Start(ctx, deps); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if err := a.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	})

	t.Run("CtxCancelGraceful", func(t *testing.T) {
		a := factory()
		deps := DepsFor(nil, nil, nil)
		ctx, cancel := context.WithCancel(context.Background())
		if err := a.Start(ctx, deps); err != nil {
			t.Fatalf("Start: %v", err)
		}
		cancel()
		done := make(chan error, 1)
		go func() { done <- a.Shutdown(context.Background()) }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Shutdown returned %v after ctx cancel", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Shutdown did not return within 5s after ctx cancel")
		}
	})

	t.Run("IdempotentShutdown", func(t *testing.T) {
		a := factory()
		deps := DepsFor(nil, nil, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := a.Start(ctx, deps); err != nil {
			t.Fatalf("Start: %v", err)
		}
		_ = a.Shutdown(context.Background())
		_ = a.Shutdown(context.Background()) // second call MUST NOT panic
	})

	t.Run("DuplicateRegisterPanics", func(t *testing.T) {
		inbound.ResetForTest()
		ch := factory().Channel()
		inbound.Register(ch, factory)
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Register(%q, …) did not panic on duplicate", ch)
			}
		}()
		inbound.Register(ch, factory)
	})

	t.Run("IngestPortReceivesInput", func(t *testing.T) {
		if cfg.trigger == nil {
			t.Skip("no Trigger provided — adapter covers IngestPort end-to-end in its own test")
		}
		a := factory()
		ingest := &FakeIngest{} // ptrext:allow mutex-identity
		sources := NewFakeSources()
		mux := &FakeMux{} // ptrext:allow mutex-identity
		deps := DepsFor(ingest, sources, mux)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := a.Start(ctx, deps); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() { _ = a.Shutdown(context.Background()) }()

		if err := cfg.trigger(t, a, deps, sources, mux); err != nil {
			t.Fatalf("Trigger failed: %v", err)
		}
		if len(ingest.Calls) == 0 {
			t.Errorf("Trigger did not cause FakeIngest.Calls to grow; got 0 entries")
		}
	})
}

// ContractOption customises TestAdapterContract.
type ContractOption func(*contractConfig)

type contractConfig struct {
	trigger TriggerFunc
}

// TriggerFunc drives one end-to-end ingest through the adapter under
// test (e.g. send an HTTP request through the FakeMux's mounted route,
// or call adapter-specific seam to fire a poll round). Returning a
// non-nil error fails the sub-test.
type TriggerFunc func(t *testing.T, a inbound.Adapter, deps inbound.Deps, sources *FakeSources, mux *FakeMux) error

// WithTrigger supplies a TriggerFunc for the IngestPortReceivesInput
// sub-test. When omitted, the sub-test is skipped (the adapter is
// expected to cover this in its own adapter-level tests).
func WithTrigger(f TriggerFunc) ContractOption {
	return func(c *contractConfig) { c.trigger = f }
}
