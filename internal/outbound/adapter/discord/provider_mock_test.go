// SPDX-License-Identifier: Apache-2.0

package discord

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/outboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestProviderMockEventDelivery(t *testing.T) {
	provider := outboundtest.NewProvider(t, outboundtest.ProviderScenario{
		Name:      "discord-success",
		Responses: []outboundtest.ProviderResponse{{Status: http.StatusNoContent}},
		Assert: func(t *testing.T, req outboundtest.ProviderRequest) {
			t.Helper()
			outboundtest.AssertPostJSON(t, req)
			if req.Path != "/api/webhooks/123/"+outboundtest.URLTokenMarker {
				t.Fatalf("path = %q, want Discord webhook path", req.Path)
			}
			msg := ptrext.Of(struct {
				Embeds          []map[string]any `json:"embeds"`
				AllowedMentions struct {
					Parse []string `json:"parse"`
				} `json:"allowed_mentions"`
			}{})
			if err := json.Unmarshal(req.Body, msg); err != nil {
				t.Fatalf("unmarshal Discord body: %v\nbody: %s", err, req.BodyString())
			}
			if len(msg.Embeds) == 0 {
				t.Fatal("discord embeds must be present")
			}
			if msg.AllowedMentions.Parse == nil || len(msg.AllowedMentions.Parse) != 0 {
				t.Fatalf("allowed_mentions.parse = %v, want empty list", msg.AllowedMentions.Parse)
			}
		},
	})

	rendered, err := ptrext.Of(channel{}).RenderEvent(outboundtest.CanonicalEvent(), outbound.Target{
		ID:              "target-discord-provider",
		TenantID:        "tenant-conformance",
		URL:             provider.URL("/api/webhooks/123/" + outboundtest.URLTokenMarker),
		DestinationType: channelID,
	})
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	result := outboundtest.SendRendered(t, rendered)
	if result.Status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", result.Status)
	}
	if provider.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.CallCount())
	}
}
