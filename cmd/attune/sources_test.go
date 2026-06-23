package main

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
)

// TestAssembleSourceSet_RejectsReservedCollision: an adapter channel that
// collides with a reserved core source is an error (the silent-shadow guard),
// now exercisable directly because assembly returns an error instead of
// os.Exit-ing inside the helper.
func TestAssembleSourceSet_RejectsReservedCollision(t *testing.T) {
	t.Parallel()
	core := map[string]string{"api": "API client", "web": "Web Widget"}
	_, err := assembleSourceSet(core, []inbound.Entry{{Channel: "api", Display: "Pwned"}})
	if err == nil {
		t.Fatal("expected an error when an adapter channel collides with a reserved core source")
	}
	if !strings.Contains(err.Error(), "api") || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("error %q should name the colliding reserved source", err)
	}
}

// TestAssembleSourceSet_RejectsDuplicateChannel: assembly is the dedup
// authority for the overlay — two entries with the same channel error.
func TestAssembleSourceSet_RejectsDuplicateChannel(t *testing.T) {
	t.Parallel()
	core := map[string]string{"api": "API client"}
	_, err := assembleSourceSet(core, []inbound.Entry{
		{Channel: "slack", Display: "Slack"},
		{Channel: "slack", Display: "Dup"},
	})
	if err == nil {
		t.Fatal("expected an error on a duplicate adapter channel")
	}
}

// TestAssembleSourceSet_OK: a clean union resolves core + adapter channels.
func TestAssembleSourceSet_OK(t *testing.T) {
	t.Parallel()
	core := map[string]string{"api": "API client"}
	set, err := assembleSourceSet(core, []inbound.Entry{{Channel: "slack", Display: "Slack"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !set.Has("api") || !set.Has("slack") {
		t.Error("assembled set is missing an expected source")
	}
	if got := set.Display("slack"); got != "Slack" {
		t.Errorf("Display(slack) = %q; want Slack", got)
	}
}

// TestBuildSourceSet_Conformance is the composition-root conformance gate. It
// runs in package main, where main.go's adapter blank-imports have populated the
// registry, so it sees the real CoreSources ∪ Factories() union.
func TestBuildSourceSet_Conformance(t *testing.T) {
	set, err := buildSourceSet()
	if err != nil {
		t.Fatalf("buildSourceSet: %v", err)
	}

	for _, e := range inbound.Factories() {
		if domain.IsReservedSource(e.Channel) {
			t.Errorf("adapter channel %q collides with a reserved core source", e.Channel)
		}
		if e.Display == "" {
			t.Errorf("adapter channel %q registered with an empty Display label", e.Channel)
		}
		if !set.Has(e.Channel) {
			t.Errorf("assembled set is missing registered channel %q", e.Channel)
		}
	}
	for k := range domain.CoreSources() {
		if !set.Has(k) {
			t.Errorf("assembled set is missing core source %q", k)
		}
	}
	for _, s := range set.All() {
		if set.Display(s) == "" {
			t.Errorf("source %q has an empty display in the assembled set", s)
		}
	}
	for _, ch := range []string{"webhook", "email"} {
		if !set.Has(ch) {
			t.Errorf("assembled set is missing built-in adapter channel %q", ch)
		}
	}

	// Drift guard: the assembled set must agree with DefaultSourceSet (the pure
	// read-path fallback) on every default member's label. Catches the
	// hand-maintained webhook/email overlay in DefaultSourceSet drifting from the
	// adapters' real Register displays.
	def := domain.DefaultSourceSet()
	for _, s := range def.All() {
		if !set.Has(s) {
			t.Errorf("DefaultSourceSet member %q missing from the assembled set", s)
		}
		if set.Display(s) != def.Display(s) {
			t.Errorf("label drift for %q: assembled %q vs default %q", s, set.Display(s), def.Display(s))
		}
	}
}
