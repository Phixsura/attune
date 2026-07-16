// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRenderNotificationBuildsProviderRequest(t *testing.T) {
	env := ptrext.Of(outbound.NotificationEnvelope{
		Version:            "1",
		EventID:            "event-1",
		EventType:          "request.shipped",
		TenantID:           "tenant-1",
		UnsubscribeURL:     "https://example.test/unsubscribe/request-token",
		ListUnsubscribeURL: "https://example.test/unsubscribe/tenant-token",
		Request: map[string]any{
			"title": "Dark mode",
			"state": "shipped",
		},
		Update: map[string]any{
			"body": "Dark mode is live.",
		},
		DeliveryID: "42",
	})
	rendered, err := ptrext.Of(channel{}).RenderNotification(env, outbound.Target{
		TenantID: "tenant-1",
		URL:      "https://mail.example.test/send",
		Secret:   "provider-secret",
		Config: map[string]any{
			"from_name":  "Attune",
			"from_email": "updates@example.test",
			"reply_to":   "support@example.test",
			"to_email":   "customer@example.test",
		},
	})
	if err != nil {
		t.Fatalf("RenderNotification() error = %v", err)
	}

	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", req.Method)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer provider-secret" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := req.Header.Get("X-Attune-Delivery-Id"); got != "42" {
		t.Fatalf("delivery header = %q", got)
	}

	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload emailPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Subject != "Shipped: Dark mode" {
		t.Fatalf("subject = %q", payload.Subject)
	}
	if payload.FromEmail != "updates@example.test" || payload.ToEmail != "customer@example.test" {
		t.Fatalf("from/to = %q/%q", payload.FromEmail, payload.ToEmail)
	}
	if payload.UnsubscribeURL != env.UnsubscribeURL {
		t.Fatalf("unsubscribe url = %q", payload.UnsubscribeURL)
	}
	if payload.ListUnsubscribeURL != env.ListUnsubscribeURL {
		t.Fatalf("list unsubscribe url = %q", payload.ListUnsubscribeURL)
	}
	assertOneClickHeaders(t, payload.Headers, env.ListUnsubscribeURL)
	if payload.TextBody == "" || payload.HTMLBody == "" {
		t.Fatalf("expected text and html bodies")
	}
}

func assertOneClickHeaders(t *testing.T, headers []emailHeader, unsubscribeURL string) {
	t.Helper()
	want := []emailHeader{
		{Name: "List-Unsubscribe", Value: "<" + unsubscribeURL + ">"},
		{Name: "List-Unsubscribe-Post", Value: "List-Unsubscribe=One-Click"},
	}
	if len(headers) != len(want) {
		t.Fatalf("headers = %+v, want %+v", headers, want)
	}
	for i := range want {
		if headers[i] != want[i] {
			t.Fatalf("headers = %+v, want %+v", headers, want)
		}
	}
}

func TestRenderNotificationRejectsEmptyProviderURL(t *testing.T) {
	rendered, err := ptrext.Of(channel{}).RenderNotification(ptrext.Of(outbound.NotificationEnvelope{}), outbound.Target{})
	if err != nil {
		t.Fatalf("RenderNotification() error = %v", err)
	}
	if _, err := rendered.Build(context.Background()); err == nil {
		t.Fatalf("Build() error = nil, want terminal error")
	}
}
