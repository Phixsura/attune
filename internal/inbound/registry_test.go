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
	inbound.Register("alpha", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "alpha"}) })
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate Register")
		}
	}()
	inbound.Register("alpha", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "alpha"}) })
}

func TestFactories_SortedByChannel(t *testing.T) {
	inbound.ResetForTest()
	inbound.Register("zeta", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "zeta"}) })
	inbound.Register("alpha", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "alpha"}) })
	inbound.Register("mike", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "mike"}) })

	got := inbound.Factories()
	want := []string{"alpha", "mike", "zeta"}
	if len(got) != 3 {
		t.Fatalf("got %d entries; want 3", len(got))
	}
	for i, e := range got {
		if e.Channel != want[i] {
			t.Errorf("entry[%d].Channel = %q; want %q", i, e.Channel, want[i])
		}
	}
}

func TestManager_StartAll_RollsBackOnFailure(t *testing.T) {
	inbound.ResetForTest()
	inbound.Register("one", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "one"}) })
	inbound.Register("two", func() inbound.Adapter {
		return ptrext.Of(startErr{stubAdapter: stubAdapter{ch: "two"}, err: errors.New("boom")})
	})

	m := inbound.NewManager(inbound.Deps{})
	if err := m.StartAll(context.Background()); err == nil {
		t.Fatal("expected StartAll to return error")
	}
}

func TestManager_ShutdownAll_HonoursPerAdapterTimeout(t *testing.T) {
	inbound.ResetForTest()
	inbound.Register("fast", func() inbound.Adapter { return ptrext.Of(stubAdapter{ch: "fast"}) })
	inbound.Register("slow", func() inbound.Adapter { return ptrext.Of(stubWithTimeout{stubAdapter: stubAdapter{ch: "slow"}}) })

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
