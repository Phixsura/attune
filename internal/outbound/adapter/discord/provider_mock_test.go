// SPDX-License-Identifier: Apache-2.0

package discord

import (
	"encoding/json"
	"fmt"
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
		Check: func(req outboundtest.ProviderRequest) error {
			if err := outboundtest.CheckPostJSON(req); err != nil {
				return err
			}
			if req.Path != "/api/webhooks/123/"+outboundtest.URLTokenMarker {
				return fmt.Errorf("path = %q, want Discord webhook path", req.Path)
			}
			msg := ptrext.Of(struct {
				Embeds          []map[string]any `json:"embeds"`
				AllowedMentions struct {
					Parse []string `json:"parse"`
				} `json:"allowed_mentions"`
			}{})
			if err := json.Unmarshal(req.Body, msg); err != nil {
				return fmt.Errorf("unmarshal Discord body: %w\nbody: %s", err, req.BodyString())
			}
			if len(msg.Embeds) == 0 {
				return fmt.Errorf("discord embeds must be present")
			}
			if msg.AllowedMentions.Parse == nil || len(msg.AllowedMentions.Parse) != 0 {
				return fmt.Errorf("allowed_mentions.parse = %v, want empty list", msg.AllowedMentions.Parse)
			}
			return nil
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
