// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"strings"
	"testing"
)

func TestBuildContent(t *testing.T) {
	tests := []struct {
		name     string
		ticket   ticket
		comments []comment
		want     string
	}{
		{
			name:   "subject and description only",
			ticket: ticket{Subject: "Printer on fire", Description: "The smoke is colorful."},
			want:   "Printer on fire\n\nThe smoke is colorful.",
		},
		{
			name:   "empty subject",
			ticket: ticket{Description: "Just a description."},
			want:   "Just a description.",
		},
		{
			name:   "empty description",
			ticket: ticket{Subject: "Just a subject"},
			want:   "Just a subject\n\n",
		},
		{
			name:   "subject + description + comments with role tags",
			ticket: ticket{Subject: "Help", Description: "I need help.", RequesterID: 10},
			comments: []comment{
				{ID: 1, Body: "I need help.", Public: true, AuthorID: 10},      // first = description, skipped
				{ID: 2, Body: "Me too!", Public: true, AuthorID: 10},           // [customer]
				{ID: 3, Body: "Have you tried X?", Public: true, AuthorID: 99}, // [agent]
			},
			want: "Help\n\nI need help.\n\n---\n\n[customer] Me too!\n\n---\n\n[agent] Have you tried X?",
		},
		{
			name:   "first comment skipped even if body differs",
			ticket: ticket{Subject: "Bug", Description: "Original desc", RequesterID: 10},
			comments: []comment{
				{ID: 1, Body: "Different body from desc", Public: true, AuthorID: 10},
				{ID: 2, Body: "Follow-up", Public: true, AuthorID: 10},
			},
			want: "Bug\n\nOriginal desc\n\n---\n\n[customer] Follow-up",
		},
		{
			name:     "empty comments slice",
			ticket:   ticket{Subject: "Hello", Description: "World"},
			comments: []comment{},
			want:     "Hello\n\nWorld",
		},
		{
			name:   "empty comment body skipped",
			ticket: ticket{Subject: "S", Description: "D", RequesterID: 1},
			comments: []comment{
				{ID: 1, Body: "D", Public: true, AuthorID: 1},
				{ID: 2, Body: "", Public: true, AuthorID: 1},
				{ID: 3, Body: "  ", Public: true, AuthorID: 99},
				{ID: 4, Body: "Real comment", Public: true, AuthorID: 1},
			},
			want: "S\n\nD\n\n---\n\n[customer] Real comment",
		},
		{
			name:     "nil comments",
			ticket:   ticket{Subject: "S", Description: "D"},
			comments: nil,
			want:     "S\n\nD",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildContent(tc.ticket, tc.comments)
			if got.text != tc.want {
				t.Errorf("buildContent().text =\n%q\nwant:\n%q", got.text, tc.want)
			}
		})
	}
}

func TestBuildContent_Truncation(t *testing.T) {
	longDesc := strings.Repeat("a", 5000)
	tk := ticket{Subject: "S", Description: longDesc}
	got := buildContent(tk, nil)
	if len(got.text) > maxContentLen+len(" [truncated]")+10 {
		t.Errorf("expected truncated output, got length %d", len(got.text))
	}
	if !strings.HasSuffix(got.text, " [truncated]") {
		t.Errorf("expected [truncated] suffix, got: %s", got.text[len(got.text)-20:])
	}
}

func TestResolveSourceUser(t *testing.T) {
	tests := []struct {
		name string
		user zendeskUser
		want string
	}{
		{name: "email preferred", user: zendeskUser{ID: 1, Name: "Alice", Email: "alice@ex.com"}, want: "alice@ex.com"},
		{name: "name fallback", user: zendeskUser{ID: 2, Name: "Bob"}, want: "Bob"},
		{name: "id fallback", user: zendeskUser{ID: 42}, want: "user:42"},
		{name: "zero value", user: zendeskUser{}, want: ""},
		{name: "whitespace email", user: zendeskUser{ID: 3, Email: "  "}, want: "user:3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSourceUser(tc.user)
			if got != tc.want {
				t.Errorf("resolveSourceUser() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildZendeskSourceMeta_Keys(t *testing.T) {
	tk := ticket{
		ID:                 123,
		Status:             "open",
		Priority:           "high",
		Type:               "incident",
		Tags:               []string{"billing", "urgent"},
		RequesterID:        10,
		OrganizationID:     20,
		CreatedAt:          "2026-07-01T00:00:00Z",
		UpdatedAt:          "2026-07-02T00:00:00Z",
		Via:                ticketVia{Channel: "email"},
		SatisfactionRating: satisfactionRating{Score: "good"},
	}
	user := zendeskUser{ID: 10, Name: "Alice", Email: "alice@ex.com"}
	org := zendeskOrganization{ID: 20, Name: "Acme"}
	cr := contentResult{text: "test", customerMsgs: 3, agentMsgs: 2, totalComments: 5}
	m := buildZendeskSourceMeta("src-id", "src-name", "myco", tk, user, org, cr)

	required := []string{
		inboundSourceIDKey, inboundSourceNameKey,
		"zendesk_subdomain", "zendesk_ticket_id", "zendesk_ticket_url",
		"zendesk_status", "zendesk_priority", "zendesk_type", "zendesk_tags",
		"zendesk_requester_id", "zendesk_requester_name", "zendesk_requester_email",
		"zendesk_organization_id", "zendesk_organization_name",
		"zendesk_satisfaction_score", "zendesk_via_channel",
		"zendesk_comment_count", "zendesk_customer_message_count", "zendesk_agent_message_count",
		"zendesk_custom_fields", "zendesk_created_at", "zendesk_updated_at",
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	if m[inboundSourceIDKey] != "src-id" {
		t.Errorf("inbound_source_id = %v, want src-id", m[inboundSourceIDKey])
	}
	if m["zendesk_ticket_id"] != int64(123) {
		t.Errorf("zendesk_ticket_id = %v, want 123", m["zendesk_ticket_id"])
	}
	if m["zendesk_comment_count"] != 5 {
		t.Errorf("zendesk_comment_count = %v, want 5", m["zendesk_comment_count"])
	}
	if m["zendesk_requester_name"] != "Alice" {
		t.Errorf("zendesk_requester_name = %v, want Alice", m["zendesk_requester_name"])
	}
}

func TestZendeskIdempotencyKey(t *testing.T) {
	got := zendeskIdempotencyKey("mycompany", 42, 1753279800)
	want := "zendesk_mycompany_42_1753279800"
	if got != want {
		t.Errorf("zendeskIdempotencyKey() = %q, want %q", got, want)
	}
}

func TestSanitizeKeyPart(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"Hello-World", "Hello-World"},
		{"my.company", "my_company"},
		{"  spaces  ", "spaces"},
		{"---", "---"},
		{"", "unknown"},
		{"abc@def", "abc_def"},
		{"a_b-c3", "a_b-c3"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeKeyPart(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeKeyPart(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestTicketAgentURL(t *testing.T) {
	got := ticketAgentURL("acme", 12345)
	want := "https://acme.zendesk.com/agent/tickets/12345"
	if got != want {
		t.Errorf("ticketAgentURL() = %q, want %q", got, want)
	}
}

func TestInferType(t *testing.T) {
	tests := []struct {
		name string
		t    ticket
		want string
	}{
		{"urgent incident → bug_report", ticket{Priority: "urgent", Type: "incident"}, "bug_report"},
		{"high problem → bug_report", ticket{Priority: "high", Type: "problem"}, "bug_report"},
		{"bad satisfaction → complaint", ticket{SatisfactionRating: satisfactionRating{Score: "bad"}}, "complaint"},
		{"question → feature_request", ticket{Type: "question"}, "feature_request"},
		{"task → task", ticket{Type: "task"}, "task"},
		{"normal incident → empty", ticket{Priority: "normal", Type: "incident"}, ""},
		{"no fields → empty", ticket{}, ""},
		{"question + bad CSAT → feature_request (type wins)", ticket{Type: "question", SatisfactionRating: satisfactionRating{Score: "bad"}}, "feature_request"},
		{"bad CSAT alone → complaint", ticket{SatisfactionRating: satisfactionRating{Score: "bad"}}, "complaint"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := inferType(tc.t)
			if got != tc.want {
				t.Errorf("inferType() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMatchesFilter(t *testing.T) {
	tests := []struct {
		name   string
		ticket ticket
		filter TicketFilter
		want   bool
	}{
		{"empty filter matches all", ticket{Status: "open", Tags: []string{"a"}}, TicketFilter{}, true},
		{"status match", ticket{Status: "open"}, TicketFilter{Statuses: []string{"open", "pending"}}, true},
		{"status miss", ticket{Status: "closed"}, TicketFilter{Statuses: []string{"open"}}, false},
		{"tag required present", ticket{Tags: []string{"bug", "urgent"}}, TicketFilter{Tags: []string{"bug"}}, true},
		{"tag required missing", ticket{Tags: []string{"bug"}}, TicketFilter{Tags: []string{"feature"}}, false},
		{"all tags required", ticket{Tags: []string{"a", "b"}}, TicketFilter{Tags: []string{"a", "b"}}, true},
		{"not all tags present", ticket{Tags: []string{"a"}}, TicketFilter{Tags: []string{"a", "b"}}, false},
		{"exclude tag match → false", ticket{Tags: []string{"spam", "test"}}, TicketFilter{ExcludeTags: []string{"spam"}}, false},
		{"exclude tag miss → true", ticket{Tags: []string{"legit"}}, TicketFilter{ExcludeTags: []string{"spam"}}, true},
		{"combo: status + tags", ticket{Status: "open", Tags: []string{"bug"}}, TicketFilter{Statuses: []string{"open"}, Tags: []string{"bug"}}, true},
		{"case insensitive status", ticket{Status: "Open"}, TicketFilter{Statuses: []string{"open"}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesFilter(tc.ticket, tc.filter)
			if got != tc.want {
				t.Errorf("matchesFilter() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildContent_CustomerAgentCounts(t *testing.T) {
	tk := ticket{Subject: "S", Description: "D", RequesterID: 10}
	comments := []comment{
		{ID: 1, Body: "D", Public: true, AuthorID: 10},       // skipped (description) but counts as customer
		{ID: 2, Body: "Reply 1", Public: true, AuthorID: 99}, // agent
		{ID: 3, Body: "Reply 2", Public: true, AuthorID: 10}, // customer
		{ID: 4, Body: "Reply 3", Public: true, AuthorID: 99}, // agent
		{ID: 5, Body: "Reply 4", Public: true, AuthorID: 10}, // customer
	}
	cr := buildContent(tk, comments)
	if cr.customerMsgs != 3 {
		t.Errorf("customerMsgs = %d, want 3", cr.customerMsgs)
	}
	if cr.agentMsgs != 2 {
		t.Errorf("agentMsgs = %d, want 2", cr.agentMsgs)
	}
	if cr.totalComments != 5 {
		t.Errorf("totalComments = %d, want 5", cr.totalComments)
	}
}

func TestBuildContent_StructuralTruncation(t *testing.T) {
	// Build a ticket with many customer comments that exceeds maxContentLen.
	tk := ticket{Subject: "Long ticket", Description: "Initial description.", RequesterID: 1}
	var comments []comment
	// First comment = description (skipped in content).
	comments = append(comments, comment{ID: 1, Body: "Initial description.", Public: true, AuthorID: 1})
	// 20 customer messages with long bodies.
	for i := 2; i <= 21; i++ {
		comments = append(comments, comment{
			ID:       int64(i),
			Body:     strings.Repeat("Customer message content. ", 50),
			Public:   true,
			AuthorID: 1,
		})
	}

	cr := buildContent(tk, comments)

	// Should contain omission marker.
	if !strings.Contains(cr.text, "messages omitted") {
		t.Error("expected omission marker in truncated content")
	}
	// Should contain first customer message.
	if !strings.Contains(cr.text, "[customer] Customer message content.") {
		t.Error("expected first customer message to be preserved")
	}
	// Total should be within limit.
	if len(cr.text) > maxContentLen+len(" [truncated]")+10 {
		t.Errorf("content too long: %d chars", len(cr.text))
	}
}
