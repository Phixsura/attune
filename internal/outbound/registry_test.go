// SPDX-License-Identifier: Apache-2.0

package outbound

import (
	"context"
	"net/http"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type stubEventChannel struct {
	id string
}

func (s *stubEventChannel) ID() string { return s.id }
func (s *stubEventChannel) RenderEvent(env *Envelope, dst Target) (Rendered, error) {
	return Rendered{
		Build: func(ctx context.Context) (*http.Request, error) { return nil, nil },
		Check: CheckWebhook(s.id),
	}, nil
}

type stubDigestChannel struct {
	id string
}

func (s *stubDigestChannel) ID() string { return s.id }
func (s *stubDigestChannel) RenderDigest(view any, dst Target) (Rendered, error) {
	return Rendered{
		Build: func(ctx context.Context) (*http.Request, error) { return nil, nil },
		Check: CheckWebhook(s.id),
	}, nil
}

type stubBothChannel struct {
	id string
}

func (s *stubBothChannel) ID() string { return s.id }
func (s *stubBothChannel) RenderEvent(env *Envelope, dst Target) (Rendered, error) {
	return Rendered{}, nil
}

func (s *stubBothChannel) RenderDigest(view any, dst Target) (Rendered, error) {
	return Rendered{}, nil
}

func TestRegister_DuplicatePanics(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	Register(ptrext.Of(stubEventChannel{id: "test-dup"}))

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	Register(ptrext.Of(stubEventChannel{id: "test-dup"}))
}

func TestRegister_InvalidTypePanics(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on invalid type")
		}
	}()
	Register("not a channel")
}

func TestLookupEvent(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	Register(ptrext.Of(stubEventChannel{id: "event-only"}))
	Register(ptrext.Of(stubDigestChannel{id: "digest-only"}))
	Register(ptrext.Of(stubBothChannel{id: "both"}))

	tests := []struct {
		name     string
		destType string
		wantNil  bool
	}{
		{"event channel found", "event-only", false},
		{"digest-only channel returns nil for event", "digest-only", true},
		{"both channel found for event", "both", false},
		{"unknown channel returns nil", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := LookupEvent(tt.destType)
			if (ch == nil) != tt.wantNil {
				t.Errorf("LookupEvent(%q) nil=%v, want nil=%v", tt.destType, ch == nil, tt.wantNil)
			}
		})
	}
}

func TestLookupDigest(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	Register(ptrext.Of(stubEventChannel{id: "event-only"}))
	Register(ptrext.Of(stubDigestChannel{id: "digest-only"}))
	Register(ptrext.Of(stubBothChannel{id: "both"}))

	tests := []struct {
		name     string
		destType string
		wantNil  bool
	}{
		{"digest channel found", "digest-only", false},
		{"event-only channel returns nil for digest", "event-only", true},
		{"both channel found for digest", "both", false},
		{"unknown channel returns nil", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := LookupDigest(tt.destType)
			if (ch == nil) != tt.wantNil {
				t.Errorf("LookupDigest(%q) nil=%v, want nil=%v", tt.destType, ch == nil, tt.wantNil)
			}
		})
	}
}

func TestChannels_Sorted(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	Register(ptrext.Of(stubEventChannel{id: "zebra"}))
	Register(ptrext.Of(stubDigestChannel{id: "alpha"}))
	Register(ptrext.Of(stubBothChannel{id: "middle"}))

	entries := Channels()
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	want := []string{"alpha", "middle", "zebra"}
	for i, e := range entries {
		if e.ID != want[i] {
			t.Errorf("entry[%d].ID = %q, want %q", i, e.ID, want[i])
		}
	}
}

func TestChannels_Capabilities(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	Register(ptrext.Of(stubEventChannel{id: "event-only"}))
	Register(ptrext.Of(stubDigestChannel{id: "digest-only"}))
	Register(ptrext.Of(stubBothChannel{id: "both"}))

	entries := Channels()
	byID := make(map[string]Entry)
	for _, e := range entries {
		byID[e.ID] = e
	}

	tests := []struct {
		id         string
		wantEvent  bool
		wantDigest bool
	}{
		{"event-only", true, false},
		{"digest-only", false, true},
		{"both", true, true},
	}

	for _, tt := range tests {
		e := byID[tt.id]
		if e.SupportsEvent != tt.wantEvent {
			t.Errorf("%s.SupportsEvent = %v, want %v", tt.id, e.SupportsEvent, tt.wantEvent)
		}
		if e.SupportsDigest != tt.wantDigest {
			t.Errorf("%s.SupportsDigest = %v, want %v", tt.id, e.SupportsDigest, tt.wantDigest)
		}
	}
}

func TestUnregisterForTest(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	Register(ptrext.Of(stubEventChannel{id: "to-remove"}))
	Register(ptrext.Of(stubEventChannel{id: "to-keep"}))

	if ch := LookupEvent("to-remove"); ch == nil {
		t.Fatal("expected to-remove to be registered")
	}
	if ch := LookupEvent("to-keep"); ch == nil {
		t.Fatal("expected to-keep to be registered")
	}

	UnregisterForTest("to-remove")

	if ch := LookupEvent("to-remove"); ch != nil {
		t.Error("expected to-remove to be unregistered, but it's still present")
	}
	if ch := LookupEvent("to-keep"); ch == nil {
		t.Error("expected to-keep to remain registered")
	}
}

func TestUnregisterForTest_NonExistent(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	UnregisterForTest("does-not-exist")
}
