// SPDX-License-Identifier: Apache-2.0

package email

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/outboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestProviderMockNotificationDelivery(t *testing.T) {
	provider := outboundtest.NewProvider(t, outboundtest.ProviderScenario{
		Name:      "email-provider-success",
		Responses: []outboundtest.ProviderResponse{{Status: http.StatusOK}},
		Check: func(req outboundtest.ProviderRequest) error {
			if err := outboundtest.CheckPostJSON(req); err != nil {
				return err
			}
			if req.Path != "/send" {
				return fmt.Errorf("path = %q, want /send", req.Path)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer "+outboundtest.SecretMarker {
				return fmt.Errorf("Authorization = %q, want bearer token", got)
			}
			if got := req.Header.Get("X-Attune-Delivery-Id"); got != "delivery-conformance" {
				return fmt.Errorf("delivery id = %q, want delivery-conformance", got)
			}
			payload := ptrext.Of(emailPayload{})
			if err := json.Unmarshal(req.Body, payload); err != nil { // ptrext:allow unmarshal-out-param
				return fmt.Errorf("unmarshal email request: %w\nbody: %s", err, req.BodyString())
			}
			if payload.ToEmail != "customer@example.test" || payload.Subject == "" {
				return fmt.Errorf("email payload missing recipient or subject: %+v", payload)
			}
			return nil
		},
	})

	target := conformanceTarget()
	target.URL = provider.URL("/send")
	rendered, err := ptrext.Of(channel{}).RenderNotification(outboundtest.CanonicalNotification(), target)
	if err != nil {
		t.Fatalf("RenderNotification: %v", err)
	}
	result := outboundtest.SendRendered(t, rendered)
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.Status)
	}
	if provider.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.CallCount())
	}
}

var _ outbound.NotificationChannel = ptrext.Of(channel{})
