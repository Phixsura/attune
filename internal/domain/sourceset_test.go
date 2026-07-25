// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"reflect"
	"testing"
)

// TestSourceSet_HasDisplayAll covers the three SourceSet operations and the
// never-empty Display fallback contract.
func TestSourceSet_HasDisplayAll(t *testing.T) {
	t.Parallel()
	set := NewSourceSet(map[string]string{
		"api":     "API client",
		"webhook": "Webhook",
		"blank":   "", // empty label must fall back to the raw key
	})

	if !set.Has("api") || !set.Has("webhook") {
		t.Error("Has() = false for a registered source")
	}
	if set.Has("nope") {
		t.Error("Has(\"nope\") = true; want false")
	}
	if got := set.Display("api"); got != "API client" {
		t.Errorf("Display(\"api\") = %q; want %q", got, "API client")
	}
	if got := set.Display("blank"); got != "blank" {
		t.Errorf("Display(\"blank\") = %q; want raw-key fallback %q", got, "blank")
	}
	if got := set.Display("unknown"); got != "unknown" {
		t.Errorf("Display(\"unknown\") = %q; want raw-key fallback %q", got, "unknown")
	}
	if got := set.All(); !reflect.DeepEqual(got, []string{"api", "blank", "webhook"}) {
		t.Errorf("All() = %v; want sorted [api blank webhook]", got)
	}

	// All() on an empty set must be a non-nil empty slice, never nil (callers
	// join it into error text).
	if got := NewSourceSet(map[string]string{}).All(); got == nil || len(got) != 0 {
		t.Errorf("All() on empty set = %v; want non-nil empty slice", got)
	}
}

// TestIsReservedSourceCompleteness cross-checks the predicate against the actual
// core map, so a future core-source addition that updates one but not the other
// is caught even though each is otherwise tested against its own golden.
func TestIsReservedSourceCompleteness(t *testing.T) {
	t.Parallel()
	for k := range CoreSources() {
		if !IsReservedSource(k) {
			t.Errorf("IsReservedSource(%q) = false, but %q is in CoreSources()", k, k)
		}
	}
}

// TestNewSourceSet_DefensiveCopy pins the constructor's whole job: the set
// holds its own copy, so mutating the input map after construction can never
// alias or corrupt the frozen source vocabulary. Guards against a future
// "simplification" of NewSourceSet's copy loop into a reference assignment.
func TestNewSourceSet_DefensiveCopy(t *testing.T) {
	t.Parallel()
	in := map[string]string{"api": "API client"}
	set := NewSourceSet(in)
	in["api"] = "HACKED" // mutate the input AFTER construction
	in["extra"] = "x"
	if got := set.Display("api"); got != "API client" {
		t.Errorf("post-construction input mutation leaked into the set: Display(api) = %q", got)
	}
	if set.Has("extra") {
		t.Error("a key added to the input map after construction appeared in the set")
	}
}

// TestIsReservedSource pins the reserved-name tier the boot-time collision
// guard relies on: an inbound adapter channel may never claim a core source.
func TestIsReservedSource(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"api", "web", "mcp", "other", "portal"} {
		if !IsReservedSource(src) {
			t.Errorf("IsReservedSource(%q) = false; want true (core source)", src)
		}
	}
	for _, src := range []string{"webhook", "email", "slack", "rss", "bogus"} {
		if IsReservedSource(src) {
			t.Errorf("IsReservedSource(%q) = true; want false (not a core source)", src)
		}
	}
}

// TestSourceVocabulary_AppendOnly is the frozen-golden conformance gate.
//
// The shipped source vocabulary is an append-only storage + wire contract:
// `source` is persisted in user_feedback.source and emitted on the outbound
// envelope. Editing the golden slice below to REMOVE or RENAME an entry is a
// breaking change requiring a soft-deprecation plan — never an in-place edit.
//
// It also closes the historical comment-vs-map drift (the package comment once
// omitted "mcp") by deriving the truth from DefaultSourceSet alone and pinning
// SourceDisplayName to agree with it.
func TestSourceVocabulary_AppendOnly(t *testing.T) {
	t.Parallel()
	golden := map[string]string{
		"api":     "API client",
		"webhook": "Webhook",
		"email":   "Email",
		"slack":   "Slack",
		"zendesk": "Zendesk",
		"web":     "Web Widget",
		"mcp":     "MCP",
		"other":   "Other",
		"portal":  "Portal",
	}
	set := DefaultSourceSet()

	wantAll := []string{"api", "email", "mcp", "other", "portal", "slack", "web", "webhook", "zendesk"} // sorted
	if got := set.All(); !reflect.DeepEqual(got, wantAll) {
		t.Fatalf("DefaultSourceSet().All() = %v; want %v (a removal is a breaking change)", got, wantAll)
	}
	for k, want := range golden {
		if got := set.Display(k); got != want {
			t.Errorf("Display(%q) = %q; want %q", k, got, want)
		}
		// SourceDisplayName must agree with the injected set (closes drift).
		if got := SourceDisplayName(k); got != want {
			t.Errorf("SourceDisplayName(%q) = %q; want %q", k, got, want)
		}
		if set.Display(k) == "" {
			t.Errorf("Display(%q) is empty; every member must have a label", k)
		}
	}
}
