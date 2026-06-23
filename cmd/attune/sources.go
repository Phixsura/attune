package main

import (
	"fmt"
	"maps"
	"sort"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
)

// buildSourceSet assembles the injected SourceSet from the reserved core sources
// unioned with every self-registered inbound adapter channel. It runs after
// main.go's adapter blank-imports have populated the registry in init(), so
// inbound.Factories() is complete. This is the only place that reads the
// registry; the assembled set is injected down into the validators and the
// enricher as a plain domain.SourceSet value.
//
// It returns an error (rather than exiting) so the policy is unit-testable and
// the caller — setupRuntimeServices, which already returns error — owns the
// terminal exit via main.go's logext.Errorf + os.Exit (cmd/attune convention;
// never log.Fatalf, CLAUDE.md §7).
func buildSourceSet() (domain.SourceSet, error) {
	return assembleSourceSet(domain.CoreSources(), inbound.Factories())
}

// assembleSourceSet is the pure union+validation: a total function of its
// inputs (no globals), so the two safety-critical branches are directly
// testable. It is the single dedup authority for the overlay.
//
// CoreSources keys are RESERVED. An adapter channel that collides with one — or
// that duplicates another adapter — is an error, never a silent last-wins
// shadow: `source` is a persisted + wire identity an operator reads in
// dispatched envelopes, so a shadowed core source is identity confusion, not a
// cosmetic label swap.
func assembleSourceSet(core map[string]string, entries []inbound.Entry) (domain.SourceSet, error) {
	m := maps.Clone(core)
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if _, isCore := core[e.Channel]; isCore {
			return nil, fmt.Errorf("inbound adapter channel %q collides with a reserved core source (reserved: %v); rename the adapter channel", e.Channel, sortedKeys(core))
		}
		if _, dup := seen[e.Channel]; dup {
			return nil, fmt.Errorf("inbound adapter channel %q registered twice", e.Channel)
		}
		seen[e.Channel] = struct{}{}
		m[e.Channel] = e.Display
	}
	return domain.NewSourceSet(m), nil
}

// sortedKeys returns the map keys in deterministic order for stable error text.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
