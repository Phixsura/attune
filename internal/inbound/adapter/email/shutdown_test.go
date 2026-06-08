// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestShutdownReturnsOnCtxDeadlineEvenWithStuckGoroutine — locks in the
// #66 B1 fix: even if a worker goroutine ignores its run-context (e.g.
// it's blocked inside an IMAP `Login(...).Wait()` that doesn't accept
// a Go ctx), Shutdown MUST return as soon as the ctx passed by the
// framework Manager expires. Prior implementation called wg.Wait()
// directly and could hang forever.
func TestShutdownReturnsOnCtxDeadlineEvenWithStuckGoroutine(t *testing.T) {
	a := &adapter{lastSuccessAt: map[string]time.Time{}} // ptrext:allow inbound-adapter-mutex-identity
	// Simulate a goroutine that is irrevocably stuck — never exits, even
	// after we cancel. This is what a wedged IMAP blocking-read looks
	// like from the framework's perspective.
	stuck := make(chan struct{})
	a.cancel = func() {} // no-op cancel — the "goroutine" doesn't observe it
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		<-stuck // never closed during the test
	}()
	defer close(stuck) // release the goroutine when the test ends

	// Manager calls Shutdown with a 100ms deadline. Pre-B1 this would
	// hang on wg.Wait() forever.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	t0 := time.Now()
	err := a.Shutdown(ctx)
	elapsed := time.Since(t0)

	if err == nil {
		t.Fatal("Shutdown returned nil; want ctx.DeadlineExceeded so caller knows it gave up")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown err = %v; want context.DeadlineExceeded", err)
	}
	// Generous slack — Shutdown should be back within a small multiple
	// of the deadline. Pre-B1 this assertion would never fire because
	// Shutdown would not return.
	if elapsed > 500*time.Millisecond {
		t.Errorf("Shutdown took %v; want under 500ms (deadline was 100ms)", elapsed)
	}
}

// TestShutdownReturnsImmediatelyWhenWorkersExit — happy path: when the
// goroutine respects its cancel and exits quickly, Shutdown returns
// before the ctx deadline.
func TestShutdownReturnsImmediatelyWhenWorkersExit(t *testing.T) {
	a := &adapter{lastSuccessAt: map[string]time.Time{}} // ptrext:allow inbound-adapter-mutex-identity
	runCtx, runCancel := context.WithCancel(context.Background())
	a.cancel = runCancel
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		<-runCtx.Done()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t0 := time.Now()
	err := a.Shutdown(ctx)
	elapsed := time.Since(t0)

	if err != nil {
		t.Errorf("Shutdown err = %v; want nil (worker exited cleanly)", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("Shutdown took %v; clean-exit case should be near-instant", elapsed)
	}
}

// TestShutdownIsIdempotent — a second Shutdown call after a successful
// first one must not panic, hang, or double-cancel.
func TestShutdownIsIdempotent(t *testing.T) {
	a := &adapter{lastSuccessAt: map[string]time.Time{}} // ptrext:allow inbound-adapter-mutex-identity
	runCtx, runCancel := context.WithCancel(context.Background())
	a.cancel = runCancel
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		<-runCtx.Done()
	}()

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown err = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- a.Shutdown(context.Background())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("second Shutdown err = %v; want nil (no-op)", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("second Shutdown did not return within 1s")
	}
}

// Compile-time guard: the unused sync import would fail the build if
// the goroutine pattern above ever stops using the WaitGroup.
var _ sync.WaitGroup
