// SPDX-License-Identifier: Apache-2.0

package domain

import "testing"

// TestValidSources_WebhookAndEmail — these two source enums back the first
// two inbound adapters introduced in #66. They must round-trip ValidSources.
func TestValidSources_WebhookAndEmail(t *testing.T) {
	for _, src := range []string{"webhook", "email"} {
		if !ValidSources[src] {
			t.Errorf("ValidSources[%q] = false; want true", src)
		}
	}
}

// TestSourceDisplayName_WebhookAndEmail — the human-facing strings must
// match the values stamped onto notify-card envelopes for the new channels.
func TestSourceDisplayName_WebhookAndEmail(t *testing.T) {
	want := map[string]string{
		"webhook": "Webhook",
		"email":   "Email",
	}
	for src, w := range want {
		if got := SourceDisplayName(src); got != w {
			t.Errorf("SourceDisplayName(%q) = %q; want %q", src, got, w)
		}
	}
}
