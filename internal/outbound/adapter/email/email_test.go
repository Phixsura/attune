// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
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

func TestRenderSurveyInvitationNotification(t *testing.T) {
	env := ptrext.Of(outbound.NotificationEnvelope{
		Version:            "1",
		EventID:            "survey-1",
		EventType:          "survey.invitation",
		TenantID:           "tenant-1",
		UnsubscribeURL:     "https://example.test/v1/portal/acme/unsubscribe?token=request-token",
		ListUnsubscribeURL: "https://example.test/v1/portal/acme/unsubscribe?token=tenant-token",
		Survey: map[string]any{
			"title":         "Resolution feedback",
			"intro":         "Your feedback helps us improve.",
			"question":      "How satisfied are you with the resolution?",
			"request_title": "Dark mode",
			"public_url":    "https://example.test/surveys/token",
			"score_min":     1,
			"score_max":     5,
			"expires_at":    "2026-08-06T12:00:00Z",
		},
		Recipient: map[string]any{
			"contact_id": "contact-1",
			"email":      "c***@example.test",
		},
	})
	rendered, err := ptrext.Of(channel{}).RenderNotification(env, outbound.Target{
		TenantID: "tenant-1",
		URL:      "https://mail.example.test/send",
		Config: map[string]any{
			"from_name":  "Attune",
			"from_email": "updates@example.test",
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
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload emailPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Subject != "Resolution feedback" {
		t.Fatalf("subject = %q", payload.Subject)
	}
	if payload.UnsubscribeURL != env.UnsubscribeURL || payload.ListUnsubscribeURL != env.ListUnsubscribeURL {
		t.Fatalf("unsubscribe urls = %q/%q", payload.UnsubscribeURL, payload.ListUnsubscribeURL)
	}
	assertOneClickHeaders(t, payload.Headers, env.ListUnsubscribeURL)
	if !strings.Contains(payload.TextBody, "Share feedback: https://example.test/surveys/token") {
		t.Fatalf("text body missing survey url: %q", payload.TextBody)
	}
	for _, want := range []string{
		"1: https://example.test/surveys/token?score=1",
		"5: https://example.test/surveys/token?score=5",
		"Unsubscribe: https://example.test/v1/portal/acme/unsubscribe?token=request-token",
	} {
		if !strings.Contains(payload.TextBody, want) {
			t.Fatalf("text body missing score link %q: %q", want, payload.TextBody)
		}
	}
	for _, want := range []string{
		`href="https://example.test/surveys/token?score=1"`,
		`href="https://example.test/surveys/token?score=5"`,
		`href="https://example.test/v1/portal/acme/unsubscribe?token=request-token"`,
		`>Share feedback</a>`,
	} {
		if !strings.Contains(payload.HTMLBody, want) {
			t.Fatalf("html body missing %q: %q", want, payload.HTMLBody)
		}
	}
	if _, ok := payload.Metadata["survey"]; !ok {
		t.Fatalf("metadata missing survey: %#v", payload.Metadata)
	}
	if _, ok := payload.Metadata["request"]; ok {
		t.Fatalf("metadata unexpectedly includes empty request: %#v", payload.Metadata)
	}
}

func TestRenderSurveyRecoveryNotification(t *testing.T) {
	env := ptrext.Of(outbound.NotificationEnvelope{
		Version:   "1",
		EventID:   "response-1",
		EventType: "survey.recovery_escalation",
		TenantID:  "tenant-1",
		Survey: map[string]any{
			"campaign_name": "Post-resolution CSAT",
			"score":         1,
			"severity":      "critical",
			"reason":        "overdue_sla",
			"due_at":        "2026-07-30T12:00:00Z",
			"source_type":   "reply_sent",
			"source_id":     "attempt-1",
			"comment":       "The answer did not solve my problem.",
			"console_url":   "https://example.test/integrations/surveys",
		},
		Recipient: map[string]any{
			"owner_member_id": "owner-1",
			"email":           "o***@example.test",
		},
	})
	rendered, err := ptrext.Of(channel{}).RenderNotification(env, outbound.Target{
		TenantID: "tenant-1",
		URL:      "https://mail.example.test/send",
		Config: map[string]any{
			"from_name":  "Attune",
			"from_email": "updates@example.test",
			"to_email":   "ops@example.test",
		},
	})
	if err != nil {
		t.Fatalf("RenderNotification() error = %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload emailPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Subject != "Low-score recovery: Post-resolution CSAT" {
		t.Fatalf("subject = %q", payload.Subject)
	}
	for _, want := range []string{
		"Low-score recovery needs attention",
		"Score: 1",
		"Reason: overdue_sla",
		"The answer did not solve my problem.",
		"Open Attune: https://example.test/integrations/surveys",
	} {
		if !strings.Contains(payload.TextBody, want) {
			t.Fatalf("text body missing %q: %q", want, payload.TextBody)
		}
	}
	if payload.UnsubscribeURL != "" || len(payload.Headers) != 0 {
		t.Fatalf("internal recovery email should not include unsubscribe fields: %#v", payload)
	}
	if _, ok := payload.Metadata["survey"]; !ok {
		t.Fatalf("metadata missing survey: %#v", payload.Metadata)
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
