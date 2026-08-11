package domain

// Automation event vocabulary (#234) — the event_type values a webhook
// subscription can select. Like the source vocabulary, these are frozen
// storage + wire tokens: persisted inside notify_outbox payloads and
// emitted verbatim on the envelope. The set is APPEND-ONLY — never rename
// a token in place, never repurpose its meaning; retire by dropping it
// from write-path validation only after no queued envelope carries it.
const (
	// EventFeedbackEnriched is the legacy envelope event_type emitted to
	// tenant_notify_targets rows. Not subscribable via webhook_subscriptions.
	EventFeedbackEnriched = "feedback.enriched"

	// EventFeedbackCreated fires for every enriched feedback row.
	EventFeedbackCreated = "feedback.created"
	// EventFeedbackUrgent additionally fires when the enriched row is
	// urgent (Snapshot.IsUrgent — the radar-audience predicate).
	EventFeedbackUrgent = "feedback.urgent"
	// EventRequestCreated fires when a customer request is created.
	EventRequestCreated = "request.created"
	// EventRequestStatusChanged fires when a customer request's status
	// transitions.
	EventRequestStatusChanged = "request.status_changed"
)

// AutomationEvents lists the subscribable event types, in display order.
var AutomationEvents = []string{
	EventFeedbackCreated,
	EventFeedbackUrgent,
	EventRequestCreated,
	EventRequestStatusChanged,
}

var automationEventSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(AutomationEvents))
	for _, e := range AutomationEvents {
		m[e] = struct{}{}
	}
	return m
}()

// IsAutomationEvent reports whether s is a subscribable automation event
// type. Write-path validation only — delivery treats stored event types
// as opaque.
func IsAutomationEvent(s string) bool {
	_, ok := automationEventSet[s]
	return ok
}
