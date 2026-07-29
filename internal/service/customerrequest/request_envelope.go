package customerrequest

import (
	"encoding/json"
	"time"

	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
)

// BuildRequestEnvelope marshals the v2 envelope JSON for customer-request
// automation events (#234): request.created and request.status_changed.
// Shape mirrors the feedback envelope (version/event_type/delivered_at/
// trace_id + one entity object); previousStatus is included only when
// non-empty (status_changed).
//
// Like the feedback envelope, the payload is frozen at enqueue time — the
// outbox worker POSTs it verbatim, so later edits to the request don't
// retroactively change what subscribers receive.
func BuildRequestEnvelope(s repo.Summary, eventType, previousStatus, traceID string) ([]byte, error) {
	type requestOut struct {
		ID             string `json:"id"`
		DisplayID      string `json:"display_id"`
		Title          string `json:"title"`
		Description    string `json:"description"`
		Status         string `json:"status"`
		PreviousStatus string `json:"previous_status,omitempty"`
		Priority       string `json:"priority"`
		CreatedAt      string `json:"created_at"`
		UpdatedAt      string `json:"updated_at"`
	}
	type envelopeOut struct {
		Version     string     `json:"version"`
		EventType   string     `json:"event_type"`
		DeliveredAt string     `json:"delivered_at"`
		TraceID     string     `json:"trace_id,omitempty"`
		Request     requestOut `json:"request"`
	}
	env := envelopeOut{
		Version:     "2",
		EventType:   eventType,
		DeliveredAt: s.UpdatedAt.UTC().Format(time.RFC3339),
		TraceID:     traceID,
		Request: requestOut{
			ID:             s.ID.String(),
			DisplayID:      s.DisplayID,
			Title:          s.Title,
			Description:    s.Description,
			Status:         string(s.Status),
			PreviousStatus: previousStatus,
			Priority:       string(s.Priority),
			CreatedAt:      s.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:      s.UpdatedAt.UTC().Format(time.RFC3339),
		},
	}
	return json.Marshal(env)
}
