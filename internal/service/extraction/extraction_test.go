// SPDX-License-Identifier: Apache-2.0

package extraction

import (
	"strings"
	"testing"
)

func TestExtractFeedbackContent(t *testing.T) {
	ticket := Ticket{Subject: "Login broken", Body: "Cannot log in since yesterday."}
	got := ExtractFeedbackContent(ticket)
	if !strings.Contains(got, "Login broken") {
		t.Error("should include subject")
	}
	if !strings.Contains(got, "Cannot log in") {
		t.Error("should include body")
	}
}

func TestExtractFeedbackContent_NoSubject(t *testing.T) {
	ticket := Ticket{Body: "Just a body"}
	got := ExtractFeedbackContent(ticket)
	if got != "Just a body" {
		t.Errorf("got %q, want just the body", got)
	}
}

func TestMapPriority(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"urgent", "high"},
		{"Critical", "high"},
		{"P1", "high"},
		{"high", "medium"},
		{"normal", "low"},
		{"low", "low"},
		{"", "medium"},
		{"  P0  ", "high"},
	}
	for _, tt := range tests {
		got := MapPriority(tt.input)
		if got != tt.want {
			t.Errorf("MapPriority(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildSourceMeta(t *testing.T) {
	ticket := Ticket{
		TicketID:    "TICKET-123",
		Customer:    "Acme Corp",
		Priority:    "high",
		Tags:        []string{"billing", "urgent"},
		Attachments: 2,
	}
	meta := BuildSourceMeta(ticket)
	if meta["ticket_id"] != "TICKET-123" {
		t.Error("missing ticket_id")
	}
	if meta["customer"] != "Acme Corp" {
		t.Error("missing customer")
	}
	if meta["attachment_count"] != 2 {
		t.Error("missing attachment_count")
	}
}

func TestBuildSourceMeta_Minimal(t *testing.T) {
	ticket := Ticket{TicketID: "T-1"}
	meta := BuildSourceMeta(ticket)
	if len(meta) != 1 {
		t.Errorf("minimal ticket should have 1 meta field, got %d", len(meta))
	}
}
