// SPDX-License-Identifier: Apache-2.0

// Package extraction implements support ticket feedback extraction.
// It parses structured fields from support tickets (subject, body,
// customer info, priority) and maps them to feedback ingest inputs.
package extraction

import "strings"

// Ticket represents a parsed support ticket.
type Ticket struct {
	Subject     string
	Body        string
	Customer    string
	Priority    string
	TicketID    string
	Tags        []string
	Attachments int
}

// ExtractFeedbackContent builds a single content string from a support
// ticket for ingestion into the feedback pipeline.
func ExtractFeedbackContent(t Ticket) string {
	var b strings.Builder
	if t.Subject != "" {
		b.WriteString(t.Subject)
		b.WriteString("\n\n")
	}
	b.WriteString(t.Body)
	return b.String()
}

// MapPriority converts common support ticket priority strings to
// attune severity levels.
func MapPriority(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "urgent", "critical", "p0", "p1":
		return "high"
	case "high", "p2":
		return "medium"
	case "normal", "medium", "p3":
		return "low"
	case "low", "p4", "p5":
		return "low"
	default:
		return "medium"
	}
}

// BuildSourceMeta creates metadata from a ticket for inclusion in the
// feedback's SourceMeta field.
func BuildSourceMeta(t Ticket) map[string]any {
	meta := map[string]any{
		"ticket_id": t.TicketID,
	}
	if t.Customer != "" {
		meta["customer"] = t.Customer
	}
	if t.Priority != "" {
		meta["priority"] = t.Priority
	}
	if len(t.Tags) > 0 {
		meta["tags"] = t.Tags
	}
	if t.Attachments > 0 {
		meta["attachment_count"] = t.Attachments
	}
	return meta
}
