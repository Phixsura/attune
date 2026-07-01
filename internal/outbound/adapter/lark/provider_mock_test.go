// SPDX-License-Identifier: Apache-2.0

package lark

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
		Name: "lark-success",
		Responses: []outboundtest.ProviderResponse{{
			Status: http.StatusOK,
			Body:   `{"StatusCode":0,"StatusMessage":"success"}`,
		}},
		Check: func(req outboundtest.ProviderRequest) error {
			if err := outboundtest.CheckPostJSON(req); err != nil {
				return err
			}
			if req.Path != "/open-apis/bot/v2/hook/"+outboundtest.URLTokenMarker {
				return fmt.Errorf("path = %q, want Lark webhook path", req.Path)
			}
			msg := ptrext.Of(struct {
				MsgType   string         `json:"msg_type"`
				Card      map[string]any `json:"card"`
				Timestamp string         `json:"timestamp"`
				Sign      string         `json:"sign"`
			}{})
			if err := json.Unmarshal(req.Body, msg); err != nil {
				return fmt.Errorf("unmarshal Lark body: %w\nbody: %s", err, req.BodyString())
			}
			if msg.MsgType != "interactive" {
				return fmt.Errorf("msg_type = %q, want interactive", msg.MsgType)
			}
			if len(msg.Card) == 0 {
				return fmt.Errorf("lark card must be present")
			}
			if msg.Timestamp == "" || msg.Sign == "" {
				return fmt.Errorf("signed Lark webhook must include timestamp and sign")
			}
			return nil
		},
	})

	rendered, err := ptrext.Of(channel{}).RenderEvent(outboundtest.CanonicalEvent(), outbound.Target{
		ID:              "target-lark-provider",
		TenantID:        "tenant-conformance",
		URL:             provider.URL("/open-apis/bot/v2/hook/" + outboundtest.URLTokenMarker),
		Secret:          outboundtest.SecretMarker,
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
