// SPDX-License-Identifier: Apache-2.0

package generic

import (
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
		Name:      "raw-webhook-success",
		Responses: []outboundtest.ProviderResponse{{Status: http.StatusNoContent}},
		Check: func(req outboundtest.ProviderRequest) error {
			if err := outboundtest.CheckPostJSON(req); err != nil {
				return err
			}
			if req.Path != "/webhook/"+outboundtest.URLTokenMarker {
				return fmt.Errorf("path = %q, want token-bearing webhook path", req.Path)
			}
			if req.Header.Get("X-Attune-Signature") == "" {
				return fmt.Errorf("X-Attune-Signature must be set")
			}
			if req.Header.Get("X-Attune-Delivery-Id") != "delivery-conformance" {
				return fmt.Errorf("delivery id = %q, want delivery-conformance", req.Header.Get("X-Attune-Delivery-Id"))
			}
			if !strings.Contains(req.BodyString(), outboundtest.SensitiveFeedbackMarker) {
				return fmt.Errorf("raw webhook must preserve the feedback envelope body")
			}
			return nil
		},
	})

	rendered, err := ptrext.Of(channel{}).RenderEvent(outboundtest.CanonicalEvent(), outbound.Target{
		ID:               "target-generic-provider",
		TenantID:         "tenant-conformance",
		URL:              provider.URL("/webhook/" + outboundtest.URLTokenMarker),
		Secret:           outboundtest.SecretMarker,
		DestinationType:  channelID,
		SignatureVersion: outbound.SignatureVersionContentHash,
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
