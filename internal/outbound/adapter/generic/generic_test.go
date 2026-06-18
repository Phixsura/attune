// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRenderEvent_SetsDeliveryIDHeader(t *testing.T) {
	c := channel{}
	env := ptrext.Of(outbound.Envelope{
		Version:    "2",
		EventType:  "feedback.enriched",
		TenantID:   "t1",
		Feedback:   map[string]any{"id": float64(7)},
		DeliveryID: "42",
	})
	rendered, err := c.RenderEvent(env, outbound.Target{URL: "https://example.com/hook", Secret: "shhh"})
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := req.Header.Get("X-Attune-Delivery-Id"); got != "42" {
		t.Fatalf("X-Attune-Delivery-Id = %q, want %q", got, "42")
	}
	if req.Header.Get("X-Attune-Signature") == "" {
		t.Fatal("expected X-Attune-Signature to still be set")
	}
}

func TestRenderEvent_OmitsDeliveryIDHeaderWhenUnset(t *testing.T) {
	c := channel{}
	env := ptrext.Of(outbound.Envelope{Version: "2", TenantID: "t1", Feedback: map[string]any{}})
	rendered, err := c.RenderEvent(env, outbound.Target{URL: "https://example.com/hook", Secret: "shhh"})
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := req.Header["X-Attune-Delivery-Id"]; ok {
		t.Fatal("X-Attune-Delivery-Id should be absent when DeliveryID is empty")
	}
}
