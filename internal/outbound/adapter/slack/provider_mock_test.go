// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/outboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestProviderMockEventDelivery(t *testing.T) {
	provider := outboundtest.NewProvider(t, outboundtest.ProviderScenario{
		Name:      "slack-success",
		Responses: []outboundtest.ProviderResponse{{Status: http.StatusOK, Body: "ok"}},
		Assert: func(t *testing.T, req outboundtest.ProviderRequest) {
			t.Helper()
			outboundtest.AssertPostJSON(t, req)
			if req.Path != "/services/T000/B000/"+outboundtest.URLTokenMarker {
				t.Fatalf("path = %q, want Slack webhook path", req.Path)
			}
			if strings.Contains(req.BodyString(), outboundtest.URLTokenMarker) {
				t.Fatal("Slack URL token leaked into request body")
			}
			msg := ptrext.Of(struct {
				Blocks []map[string]any `json:"blocks"`
			}{})
			if err := json.Unmarshal(req.Body, msg); err != nil {
				t.Fatalf("unmarshal Slack body: %v\nbody: %s", err, req.BodyString())
			}
			if len(msg.Blocks) < 3 {
				t.Fatalf("blocks = %d, want at least 3", len(msg.Blocks))
			}
		},
	})

	rendered, err := ptrext.Of(channel{}).RenderEvent(outboundtest.CanonicalEvent(), outbound.Target{
		ID:              "target-slack-provider",
		TenantID:        "tenant-conformance",
		URL:             provider.URL("/services/T000/B000/" + outboundtest.URLTokenMarker),
		DestinationType: channelID,
	})
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	result := outboundtest.SendRendered(t, rendered)
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.Status)
	}
	if provider.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.CallCount())
	}
}
