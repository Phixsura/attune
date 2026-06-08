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
// Minimum bar (see spec §Conformance test suite — 5 lifecycle gates):
//
//  1. Channel() returns a non-empty string with no whitespace or '/'.
//  2. Start(ctx, fakeDeps) followed by immediate Shutdown does not panic.
//  3. ctx cancellation propagates: Shutdown returns within 5s, no goroutine leak.
//  4. Idempotent shutdown: calling Shutdown twice does not panic.
//  5. Duplicate Register on the same channel panics.
//
// End-to-end IngestPort coverage is delegated to each adapter's own
// adapter-level test where the fixture (HTTP request for webhook, IMAP
// fake for email) is naturally available; the conformance suite stays
// focused on lifecycle.
//
// The adapter under test passes a factory (typically the same constructor
// the adapter's own init() registers).
func TestAdapterContract(t *testing.T, factory inbound.Factory) {
	t.Helper()
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

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
}
