package webhooksub

import "github.com/Phixsura/attune/internal/domain"

// Static samples — the performList fallback for tenants with no matching
// events yet. MUST stay schema-identical to the live envelopes
// (enrich.buildOutboxEnvelopeTyped / customerrequest.BuildRequestEnvelope);
// the handler test cross-checks the top-level shape and CI's golden tests
// pin the builders.
func staticSample(eventType string) []byte {
	switch eventType {
	case domain.EventFeedbackCreated, domain.EventFeedbackUrgent:
		return feedbackSample(eventType)
	case domain.EventRequestCreated:
		return []byte(`{
  "version": "2",
  "event_type": "request.created",
  "delivered_at": "2026-07-01T10:00:00Z",
  "trace_id": "sample-trace-id",
  "request": {
    "id": "11111111-2222-3333-4444-555555555555",
    "display_id": "REQ-42",
    "title": "Add dark mode",
    "description": "Several customers asked for a dark theme.",
    "status": "open",
    "priority": "high",
    "created_at": "2026-07-01T10:00:00Z",
    "updated_at": "2026-07-01T10:00:00Z"
  }
}`)
	case domain.EventRequestStatusChanged:
		return []byte(`{
  "version": "2",
  "event_type": "request.status_changed",
  "delivered_at": "2026-07-02T09:30:00Z",
  "trace_id": "sample-trace-id",
  "request": {
    "id": "11111111-2222-3333-4444-555555555555",
    "display_id": "REQ-42",
    "title": "Add dark mode",
    "description": "Several customers asked for a dark theme.",
    "status": "in_progress",
    "previous_status": "planned",
    "priority": "high",
    "created_at": "2026-07-01T10:00:00Z",
    "updated_at": "2026-07-02T09:30:00Z"
  }
}`)
	default:
		return []byte(`{}`)
	}
}

func feedbackSample(eventType string) []byte {
	urgent := "false"
	if eventType == domain.EventFeedbackUrgent {
		urgent = "true"
	}
	return []byte(`{
  "version": "2",
  "event_type": "` + eventType + `",
  "delivered_at": "2026-07-01T08:09:15Z",
  "trace_id": "sample-trace-id",
  "feedback": {
    "id": 12345,
    "tenant_id": "sample-tenant",
    "content": "The export button does nothing when I click it.",
    "source": "api",
    "source_display": "API client",
    "user_id": "user-789",
    "language": "en",
    "submitted_at": "2026-07-01T08:09:10Z",
    "enriched": {
      "title": "Export button unresponsive",
      "attrs": {"type": "bug", "severity": "P1"},
      "is_urgent": ` + urgent + `,
      "rationale": "User reports a broken core workflow.",
      "enriched_at": "2026-07-01T08:09:15Z"
    }
  }
}`)
}
