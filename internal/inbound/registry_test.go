// SPDX-License-Identifier: Apache-2.0

package inbound_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type startErr struct {
	stubAdapter
	err error
}

func (s *startErr) Start(_ context.Context, _ inbound.Deps) error { return s.err }

func TestRegister_Duplicate_Panics(t *testing.T) {
	inbound.ResetForTest()
	inbound.Register("alpha", "Alpha", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "alpha"}) })
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate Register")
		}
	}()
	inbound.Register("alpha", "Alpha", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "alpha"}) })
}

// TestRegister_RejectsMalformed pins the registration-time invariants: channel
// is a frozen persisted+wire token and a nil factory would crash unattributed
// at StartAll, so a bad call site must panic at boot (sql.Register discipline).
func TestRegister_RejectsMalformed(t *testing.T) {
	okFactory := func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "x"}) }
	cases := []struct {
		name    string
		channel string
		display string
		factory inbound.Factory
	}{
		{"nil factory", "x", "X", nil},
		{"empty channel", "", "X", okFactory},
		{"empty display", "x", "", okFactory},
		{"whitespace display", "x", "   ", okFactory},
		// Non-lowercase channels are rejected: source is a frozen persisted+wire
		// token, and a case variant (e.g. "API") would silently shadow a
		// lowercase reserved core source ("api") because the collision check
		// keys on the literal string. Failing at Register is the single write
		// point that prevents the shadow class.
		{"uppercase channel", "API", "X", okFactory},
		{"mixed-case channel", "Webhook", "X", okFactory},
		// Channel character invariants — source is persisted as PG TEXT (rejects
		// NUL on INSERT) and emitted on a wire envelope an operator reads, so
		// invisible / control / whitespace / structural chars are storage and
		// identity-confusion vectors. Narrow allow-list at the single write point.
		{"channel with NUL byte", "api\x00x", "X", okFactory},
		{"channel with space", "x y", "X", okFactory},
		{"channel with tab", "x\ty", "X", okFactory},
		{"channel with newline", "x\ny", "X", okFactory},
		{"channel with slash", "x/y", "X", okFactory},
		{"channel with zero-width space", "x\u200by", "X", okFactory},
		{"channel with bidi RLO", "x\u202ey", "X", okFactory},
		{"channel with dot", "x.y", "X", okFactory},
		// Display invariants — newlines / NUL would corrupt the GitHub-issue
		// markdown table row (each row is one line) and PG TEXT rejects NUL.
		{"display with newline", "x", "Bad\nLabel", okFactory},
		{"display with CR", "x", "Bad\rLabel", okFactory},
		{"display with NUL", "x", "Bad\x00Label", okFactory},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inbound.ResetForTest()
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Register(%q, %q, factory=%v) did not panic", c.channel, c.display, c.factory != nil)
				}
			}()
			inbound.Register(c.channel, c.display, c.factory)
		})
	}
}

func TestFactories_SortedByChannel(t *testing.T) {
	inbound.ResetForTest()
	inbound.Register("zeta", "Zeta", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "zeta"}) })
	inbound.Register("alpha", "Alpha", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "alpha"}) })
	inbound.Register("mike", "Mike", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "mike"}) })

	got := inbound.Factories()
	want := []string{"alpha", "mike", "zeta"}
	wantDisplay := []string{"Alpha", "Mike", "Zeta"}
	if len(got) != 3 {
		t.Fatalf("got %d entries; want 3", len(got))
	}
	for i, e := range got {
		if e.Channel != want[i] {
			t.Errorf("entry[%d].Channel = %q; want %q", i, e.Channel, want[i])
		}
		if e.Display != wantDisplay[i] {
			t.Errorf("entry[%d].Display = %q; want %q", i, e.Display, wantDisplay[i])
		}
	}
}

func TestManager_StartAll_RollsBackOnFailure(t *testing.T) {
	inbound.ResetForTest()
	inbound.Register("one", "One", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "one"}) })
	inbound.Register("two", "Two", func() inbound.Adapter {
		return ptrext.Of(startErr{stubAdapter: stubAdapter{ch: "two"}, err: errors.New("boom")})
	})

	m := inbound.NewManager(inbound.Deps{})
	if err := m.StartAll(context.Background()); err == nil {
		t.Fatal("expected StartAll to return error")
	}
}

func TestManager_ShutdownAll_HonoursPerAdapterTimeout(t *testing.T) {
	inbound.ResetForTest()
	inbound.Register("fast", "Fast", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "fast"}) })
	inbound.Register("slow", "Slow", func() inbound.Adapter { return ptrext.Of(stubWithTimeout{stubAdapter: stubAdapter{ch: "slow"}}) })

	m := inbound.NewManager(inbound.Deps{})
	if err := m.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ShutdownAll(ctx); err != nil {
		t.Fatalf("ShutdownAll: %v", err)
	}
}
