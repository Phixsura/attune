// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Phixsura/attune/internal/domain"
)

const (
	// maxContentLen is the upper limit for assembled ticket content,
	// leaving headroom under the 5000-char IngestInput limit.
	maxContentLen = 4500

	commentSeparator = "\n\n---\n\n"
)

// buildIngestInput assembles a domain.IngestInput from a ticket and its
// public comments with resolved user/org metadata.
func buildIngestInput(
	srcID, srcName, subdomain string,
	t ticket,
	comments []comment,
	users map[int64]zendeskUser,
	orgs map[int64]zendeskOrganization,
) domain.IngestInput {
	cr := buildContent(t, comments)
	requester := users[t.RequesterID]
	org := orgs[t.OrganizationID]
	sourceUser := resolveSourceUser(requester)
	pageURL := ticketAgentURL(subdomain, t.ID)

	return domain.IngestInput{
		Source:         channelName,
		Content:        cr.text,
		Type:           inferType(t),
		SourceUser:     sourceUser,
		PageURL:        pageURL,
		SourceMeta:     buildZendeskSourceMeta(srcID, srcName, subdomain, t, requester, org, cr),
		IdempotencyKey: zendeskIdempotencyKey(subdomain, t.ID, t.GeneratedTimestamp),
	}
}

// contentResult carries assembled content and message-role statistics.
type contentResult struct {
	text          string
	customerMsgs  int
	agentMsgs     int
	totalComments int
}

const (
	// keepFirstCustomer is the number of early customer messages to always keep.
	keepFirstCustomer = 3
	// keepLastCustomer is the number of recent customer messages to always keep.
	keepLastCustomer = 2
)

// buildContent concatenates the ticket subject, description, and a
// structurally-selected subset of public comments. Each comment is
// tagged [customer] or [agent]. When the full conversation exceeds
// maxContentLen, the builder keeps the first 3 + last 2 customer
// messages with an omission marker in between, preserving the most
// valuable signal for enrichment.
func buildContent(t ticket, comments []comment) contentResult {
	// First pass: classify and count.
	var entries []tagged
	var customerMsgs, agentMsgs int
	for i, c := range comments {
		if i == 0 {
			customerMsgs++
			continue
		}
		body := strings.TrimSpace(c.Body)
		if body == "" {
			continue
		}
		isCustomer := c.AuthorID == t.RequesterID
		tag := "[agent]"
		if isCustomer {
			tag = "[customer]"
			customerMsgs++
		} else {
			agentMsgs++
		}
		entries = append(entries, tagged{body: body, tag: tag, isCustomer: isCustomer})
	}

	// Build header (always kept).
	var header strings.Builder
	subject := strings.TrimSpace(t.Subject)
	if subject != "" {
		header.WriteString(subject)
		header.WriteString("\n\n")
	}
	desc := strings.TrimSpace(t.Description)
	if desc != "" {
		header.WriteString(desc)
	}

	// Assemble full content or structurally truncate.
	text := assembleComments(header.String(), entries)

	return contentResult{
		text:          text,
		customerMsgs:  customerMsgs,
		agentMsgs:     agentMsgs,
		totalComments: len(comments),
	}
}

// assembleComments builds the final content string. If the full
// conversation fits within maxContentLen, all comments are included.
// Otherwise, it keeps the head through the keepFirstCustomer-th customer
// message and the tail from the keepLastCustomer-th-from-last customer
// message onward — agent replies inside the kept ranges stay, so the
// transcript keeps its conversational shape — with an omission marker
// counting every dropped message in between.
func assembleComments(header string, entries []tagged) string {
	// Try full content first.
	full := buildFull(header, entries)
	if len(full) <= maxContentLen {
		return full
	}

	// Structural truncation: keep the first-N / last-M customer ranges.
	var custIdx []int
	for i, e := range entries {
		if e.isCustomer {
			custIdx = append(custIdx, i)
		}
	}
	if keepFirstCustomer+keepLastCustomer >= len(custIdx) {
		// Not enough customer messages to split — fall back to byte
		// truncation. Rune-safe: a mid-rune cut would produce invalid
		// UTF-8 that PostgreSQL rejects with an error outside the
		// deterministic-reject list, wedging the export cursor forever.
		return truncateBytesRuneSafe(full, maxContentLen) + " [truncated]"
	}
	headEnd := custIdx[keepFirstCustomer-1]
	tailStart := custIdx[len(custIdx)-keepLastCustomer]
	omitted := tailStart - headEnd - 1
	if omitted <= 0 {
		return truncateBytesRuneSafe(full, maxContentLen) + " [truncated]"
	}

	var b strings.Builder
	b.WriteString(header)
	for i := 0; i <= headEnd; i++ {
		b.WriteString(commentSeparator)
		b.WriteString(entries[i].tag)
		b.WriteString(" ")
		b.WriteString(entries[i].body)
	}
	b.WriteString(commentSeparator)
	fmt.Fprintf(&b, "[... %d messages omitted ...]", omitted) // ptrext:allow fmt-writer
	for i := tailStart; i < len(entries); i++ {
		b.WriteString(commentSeparator)
		b.WriteString(entries[i].tag)
		b.WriteString(" ")
		b.WriteString(entries[i].body)
	}
	result := b.String()
	if len(result) > maxContentLen {
		result = truncateBytesRuneSafe(result, maxContentLen) + " [truncated]"
	}
	return result
}

// truncateBytesRuneSafe cuts s to at most maxBytes without splitting a
// UTF-8 rune (same helper as the Intercom adapter).
func truncateBytesRuneSafe(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func buildFull(header string, entries []tagged) string {
	var b strings.Builder
	b.WriteString(header)
	for _, e := range entries {
		b.WriteString(commentSeparator)
		b.WriteString(e.tag)
		b.WriteString(" ")
		b.WriteString(e.body)
	}
	return b.String()
}

type tagged struct {
	body       string
	tag        string
	isCustomer bool
}

// inferType maps Zendesk ticket metadata to an IngestInput.Type hint
// that gives the enrichment LLM a better classification starting point.
func inferType(t ticket) string {
	// Urgent/high priority incidents and problems → bug report.
	if (t.Priority == "urgent" || t.Priority == "high") &&
		(t.Type == "incident" || t.Type == "problem") {
		return "bug_report"
	}
	// Questions → feature request (type takes precedence over satisfaction).
	if t.Type == "question" {
		return "feature_request"
	}
	// Tasks → task.
	if t.Type == "task" {
		return "task"
	}
	// Bad satisfaction (when type doesn't provide a stronger signal) → complaint.
	if t.SatisfactionRating.Score == "bad" {
		return "complaint"
	}
	return "" // let the LLM decide
}

// resolveSourceUser returns the best human-readable identifier for the
// ticket requester.
func resolveSourceUser(u zendeskUser) string {
	if email := strings.TrimSpace(u.Email); email != "" {
		return email
	}
	if name := strings.TrimSpace(u.Name); name != "" {
		return name
	}
	if u.ID > 0 {
		return "user:" + strconv.FormatInt(u.ID, 10)
	}
	return ""
}

// ticketAgentURL builds the Zendesk agent-facing ticket URL.
func ticketAgentURL(subdomain string, ticketID int64) string {
	return fmt.Sprintf("https://%s.zendesk.com/agent/tickets/%d", subdomain, ticketID)
}

// buildZendeskSourceMeta assembles the source metadata map for a ticket.
func buildZendeskSourceMeta(
	srcID, srcName, subdomain string,
	t ticket,
	requester zendeskUser,
	org zendeskOrganization,
	cr contentResult,
) map[string]any {
	tagsJSON, _ := json.Marshal(t.Tags)                 // ptrext:allow json-marshal
	customFieldsJSON, _ := json.Marshal(t.CustomFields) // ptrext:allow json-marshal

	m := map[string]any{
		inboundSourceIDKey:               srcID,
		inboundSourceNameKey:             srcName,
		"zendesk_subdomain":              subdomain,
		"zendesk_ticket_id":              t.ID,
		"zendesk_ticket_url":             ticketAgentURL(subdomain, t.ID),
		"zendesk_status":                 t.Status,
		"zendesk_priority":               t.Priority,
		"zendesk_type":                   t.Type,
		"zendesk_tags":                   string(tagsJSON),
		"zendesk_requester_id":           t.RequesterID,
		"zendesk_requester_name":         strings.TrimSpace(requester.Name),
		"zendesk_requester_email":        strings.TrimSpace(requester.Email),
		"zendesk_organization_id":        t.OrganizationID,
		"zendesk_organization_name":      strings.TrimSpace(org.Name),
		"zendesk_satisfaction_score":     t.SatisfactionRating.Score,
		"zendesk_via_channel":            t.Via.Channel,
		"zendesk_comment_count":          cr.totalComments,
		"zendesk_customer_message_count": cr.customerMsgs,
		"zendesk_agent_message_count":    cr.agentMsgs,
		"zendesk_custom_fields":          string(customFieldsJSON),
		"zendesk_created_at":             t.CreatedAt,
		"zendesk_updated_at":             t.UpdatedAt,
	}
	return m
}

// matchesFilter returns true if the ticket passes the configured filter.
// An empty filter matches everything.
func matchesFilter(t ticket, f TicketFilter) bool {
	// Status filter: if set, ticket status must be in the list.
	if len(f.Statuses) > 0 {
		found := false
		for _, s := range f.Statuses {
			if strings.EqualFold(t.Status, s) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	// Exclude tags: if the ticket has ANY excluded tag, skip.
	if len(f.ExcludeTags) > 0 {
		for _, et := range f.ExcludeTags {
			for _, tt := range t.Tags {
				if strings.EqualFold(tt, et) {
					return false
				}
			}
		}
	}
	// Required tags: ticket must have ALL required tags.
	if len(f.Tags) > 0 {
		for _, rt := range f.Tags {
			found := false
			for _, tt := range t.Tags {
				if strings.EqualFold(tt, rt) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// zendeskIdempotencyKey builds the replay-safe deduplication key.
// Includes generatedTimestamp so ticket updates produce new feedback rows.
func zendeskIdempotencyKey(subdomain string, ticketID, generatedTimestamp int64) string {
	return "zendesk_" + sanitizeKeyPart(subdomain) + "_" +
		strconv.FormatInt(ticketID, 10) + "_" +
		strconv.FormatInt(generatedTimestamp, 10)
}

// sanitizeKeyPart strips characters not allowed in idempotency keys.
func sanitizeKeyPart(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}
