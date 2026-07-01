// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"encoding/json"
	"fmt"
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
		Check: func(req outboundtest.ProviderRequest) error {
			if err := outboundtest.CheckPostJSON(req); err != nil {
				return err
			}
			if req.Path != "/services/T000/B000/"+outboundtest.URLTokenMarker {
				return fmt.Errorf("path = %q, want Slack webhook path", req.Path)
			}
			if strings.Contains(req.BodyString(), outboundtest.URLTokenMarker) {
				return fmt.Errorf("Slack URL token leaked into request body")
			}
			msg := ptrext.Of(struct {
				Blocks []map[string]any `json:"blocks"`
			}{})
			if err := json.Unmarshal(req.Body, msg); err != nil {
				return fmt.Errorf("unmarshal Slack body: %w\nbody: %s", err, req.BodyString())
			}
			if len(msg.Blocks) < 3 {
				return fmt.Errorf("blocks = %d, want at least 3", len(msg.Blocks))
			}
			return nil
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
