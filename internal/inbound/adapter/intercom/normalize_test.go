// SPDX-License-Identifier: Apache-2.0

package intercom

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/infra/intercomclient"
)

func convWithParts(parts []part) conversation {
	return conversation{
		ID: "200", Title: "Feature ask", State: "open",
		CreatedAt: 1700000000, UpdatedAt: 1700009999,
		Source: intercomclient.ConversationSource{
			Type: "conversation", Subject: "Feature",
			Body:   "Please add dark mode",
			Author: partAuthor{Type: "user", ID: "c9", Name: "Zoe", Email: "zoe@example.com"},
		},
		Contacts: intercomclient.ConversationContacts{
			Contacts: []contactRef{{ID: "c9", ExternalID: "u-9"}},
		},
		Parts: intercomclient.ConversationParts{Parts: parts},
	}
}

func customerPart(id, body string) part {
	return part{ID: id, PartType: "comment", Body: body, Author: partAuthor{Type: "user", ID: "c9", Name: "Zoe"}}
}

func agentPart(id, body string) part {
	return part{ID: id, PartType: "comment", Body: body, Author: partAuthor{Type: "admin", ID: "a1", Name: "Sam"}}
}

func TestBuildContent_TagsAndNotes(t *testing.T) {
	conv := convWithParts([]part{
		agentPart("p1", "On it!"),
		{ID: "p2", PartType: "note", Body: "escalate internally", Author: partAuthor{Type: "admin", ID: "a1"}},
		{ID: "p3", PartType: "comment", Body: "bot summary", Author: partAuthor{Type: "bot", ID: "b1"}},
		{ID: "p4", PartType: "comment", Body: "ai answer", Author: partAuthor{Type: "admin", ID: "a1", FromAIAgent: true}},
		customerPart("p5", "Any update?"),
		{ID: "p6", PartType: "comment", Body: "redacted body", Author: partAuthor{Type: "user", ID: "c9"}, Redacted: true},
	})

	cr := buildContent(conv)
	if !strings.Contains(cr.text, "Feature ask") {
		t.Errorf("missing title: %q", cr.text)
	}
	if !strings.Contains(cr.text, "[customer] Please add dark mode") {
		t.Errorf("missing tagged seed: %q", cr.text)
	}
	if !strings.Contains(cr.text, "[agent] On it!") {
		t.Errorf("missing agent: %q", cr.text)
	}
	if strings.Contains(cr.text, "escalate internally") {
		t.Errorf("internal note leaked: %q", cr.text)
	}
	if !strings.Contains(cr.text, "[bot] bot summary") {
		t.Errorf("missing bot part: %q", cr.text)
	}
	if !strings.Contains(cr.text, "[bot] ai answer") {
		t.Errorf("from_ai_agent should be tagged bot: %q", cr.text)
	}
	if strings.Contains(cr.text, "redacted body") {
		t.Errorf("redacted part leaked: %q", cr.text)
	}
	if cr.customerMsgs != 2 { // seed + p5
		t.Errorf("customerMsgs = %d, want 2", cr.customerMsgs)
	}
	if cr.agentMsgs != 1 {
		t.Errorf("agentMsgs = %d, want 1", cr.agentMsgs)
	}
	if cr.botMsgs != 2 {
		t.Errorf("botMsgs = %d, want 2", cr.botMsgs)
	}
}

func TestBuildContent_UsesSubjectWhenNoTitle(t *testing.T) {
	conv := convWithParts(nil)
	conv.Title = ""
	cr := buildContent(conv)
	if !strings.HasPrefix(cr.text, "Feature\n\n") {
		t.Errorf("subject fallback missing: %q", cr.text)
	}
}

func TestBuildContent_BotDroppedFirstUnderPressure(t *testing.T) {
	// Enough bot noise to exceed the cap, but human thread fits.
	long := strings.Repeat("x", 400)
	var parts []part
	for i := 0; i < 12; i++ {
		parts = append(parts, part{
			ID: fmt.Sprintf("b%d", i), PartType: "comment",
			Body:   "bot " + long,
			Author: partAuthor{Type: "bot", ID: "b1"},
		})
	}
	parts = append(parts, customerPart("c1", "the actual question"))
	parts = append(parts, agentPart("a1", "the actual answer"))

	conv := convWithParts(parts)
	cr := buildContent(conv)
	if len(cr.text) > maxContentLen+len(" [truncated]") {
		t.Errorf("content too long: %d", len(cr.text))
	}
	if !strings.Contains(cr.text, "the actual question") {
		t.Errorf("human customer message dropped: %q", cr.text)
	}
	if !strings.Contains(cr.text, "the actual answer") {
		t.Errorf("human agent message dropped: %q", cr.text)
	}
	if strings.Contains(cr.text, "bot xxxx") {
		t.Errorf("bot messages should be dropped first under pressure")
	}
}

func TestBuildContent_StructuralTruncationKeepsHeadAndTail(t *testing.T) {
	long := strings.Repeat("y", 300)
	var parts []part
	for i := 1; i <= 20; i++ {
		parts = append(parts, customerPart(fmt.Sprintf("c%d", i), fmt.Sprintf("msg%02d %s", i, long)))
	}
	conv := convWithParts(parts)
	cr := buildContent(conv)

	if len(cr.text) > maxContentLen+len(" [truncated]") {
		t.Errorf("content length %d over cap", len(cr.text))
	}
	// Head: first 3 customer messages (seed + msg01, msg02 at minimum).
	if !strings.Contains(cr.text, "msg01") || !strings.Contains(cr.text, "msg02") {
		t.Errorf("head messages missing: %.200q", cr.text)
	}
	// Tail: the last customer messages.
	if !strings.Contains(cr.text, "msg20") {
		t.Errorf("tail message missing")
	}
	if !strings.Contains(cr.text, "messages omitted") {
		t.Errorf("omission marker missing")
	}
	// Middle omitted.
	if strings.Contains(cr.text, "msg10") {
		t.Errorf("middle message should be omitted")
	}
}

func TestInferType(t *testing.T) {
	base := convWithParts(nil)

	urgent := base
	urgent.Priority = "priority"
	if got := inferType(urgent); got != "bug_report" {
		t.Errorf("priority → %q, want bug_report", got)
	}

	rated := base
	rated.Rating = &intercomclient.ConversationRating{Rating: 1} // ptrext:allow test-fixture
	if got := inferType(rated); got != "complaint" {
		t.Errorf("low rating → %q, want complaint", got)
	}

	highRated := base
	highRated.Rating = &intercomclient.ConversationRating{Rating: 5} // ptrext:allow test-fixture
	if got := inferType(highRated); got != "" {
		t.Errorf("high rating → %q, want empty", got)
	}

	if got := inferType(base); got != "" {
		t.Errorf("plain → %q, want empty", got)
	}
}

func TestResolveSourceUser_FallbackChain(t *testing.T) {
	conv := convWithParts(nil)

	full := intercomContact{ID: "c9", Email: "resolved@example.com", Name: "Resolved"}
	if got := resolveSourceUser(full, conv); got != "resolved@example.com" {
		t.Errorf("email preferred, got %q", got)
	}
	nameOnly := intercomContact{ID: "c9", Name: "Resolved"}
	if got := resolveSourceUser(nameOnly, conv); got != "Resolved" {
		t.Errorf("name fallback, got %q", got)
	}
	// Empty contact → seed author email.
	if got := resolveSourceUser(intercomContact{}, conv); got != "zoe@example.com" {
		t.Errorf("seed author email fallback, got %q", got)
	}
	// No author identity → contact: prefix.
	bare := conv
	bare.Source.Author = partAuthor{}
	if got := resolveSourceUser(intercomContact{ID: "c9"}, bare); got != "contact:c9" {
		t.Errorf("id fallback, got %q", got)
	}
	if got := resolveSourceUser(intercomContact{}, bare); got != "" {
		t.Errorf("empty fallback, got %q", got)
	}
}

func TestBuildIngestInput_MetaAndKey(t *testing.T) {
	conv := convWithParts([]part{agentPart("p1", "ok")})
	conv.Company = &intercomclient.Company{ID: "co1", Name: "例子公司"}              // ptrext:allow test-fixture
	conv.Rating = &intercomclient.ConversationRating{Rating: 4, Remark: "great"} // ptrext:allow test-fixture
	conv.Tags = intercomclient.TagList{Tags: []intercomclient.Tag{{ID: "t1", Name: "feature"}, {ID: "t2", Name: "ux"}}}
	conv.AIAgentParticipated = true

	contacts := map[string]intercomContact{
		"c9": {ID: "c9", ExternalID: "u-9", Email: "zoe@example.com", Name: "Zoe", Role: "user"},
	}
	in := buildIngestInput("src-1", "My Source", "ws42", conv, contacts)

	if in.Source != "intercom" {
		t.Errorf("Source = %q", in.Source)
	}
	if in.IdempotencyKey != "intercom_ws42_200_1700009999" {
		t.Errorf("IdempotencyKey = %q", in.IdempotencyKey)
	}
	if in.PageURL != "https://app.intercom.com/a/inbox/ws42/inbox/conversation/200" {
		t.Errorf("PageURL = %q", in.PageURL)
	}
	if in.SourceUser != "zoe@example.com" {
		t.Errorf("SourceUser = %q", in.SourceUser)
	}
	m := in.SourceMeta
	if m["inbound_source_id"] != "src-1" || m["inbound_source_name"] != "My Source" {
		t.Errorf("well-known keys wrong: %v %v", m["inbound_source_id"], m["inbound_source_name"])
	}
	if m["intercom_company_name"] != "例子公司" {
		t.Errorf("company name = %v", m["intercom_company_name"])
	}
	if m["intercom_rating"] != 4 || m["intercom_rating_remark"] != "great" {
		t.Errorf("rating meta = %v / %v", m["intercom_rating"], m["intercom_rating_remark"])
	}
	if m["intercom_ai_agent_participated"] != true {
		t.Errorf("ai_agent_participated = %v", m["intercom_ai_agent_participated"])
	}
	if !strings.Contains(m["intercom_tags"].(string), "feature") {
		t.Errorf("tags json = %v", m["intercom_tags"])
	}
	if m["intercom_contact_external_id"] != "u-9" {
		t.Errorf("contact_external_id = %v", m["intercom_contact_external_id"])
	}
}

func TestMatchesStateFilter(t *testing.T) {
	conv := convWithParts(nil) // state=open
	if !matchesStateFilter(conv, nil) {
		t.Error("empty filter must match")
	}
	if !matchesStateFilter(conv, []string{"open", "closed"}) {
		t.Error("open should match [open closed]")
	}
	if matchesStateFilter(conv, []string{"closed"}) {
		t.Error("open should not match [closed]")
	}
	if !matchesStateFilter(conv, []string{"OPEN"}) {
		t.Error("filter must be case-insensitive")
	}
}

func TestSanitizeKeyPart(t *testing.T) {
	cases := map[string]string{
		"ws42":        "ws42",
		"ws 42/x":     "ws_42_x",
		"":            "unknown",
		"___":         "unknown",
		"Abc-def_9":   "Abc-def_9",
		" trimmed ":   "trimmed",
		"中文workspace": "workspace",
	}
	for in, want := range cases {
		if got := sanitizeKeyPart(in); got != want {
			t.Errorf("sanitizeKeyPart(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrimaryContact(t *testing.T) {
	conv := convWithParts(nil)
	resolved := map[string]intercomContact{
		"c9": {ID: "c9", Email: "full@example.com"},
	}
	if got := primaryContact(conv, resolved); got.Email != "full@example.com" {
		t.Errorf("resolved contact not used: %+v", got)
	}
	// Unresolved → bare ref carries the external ID.
	if got := primaryContact(conv, nil); got.ExternalID != "u-9" {
		t.Errorf("bare ref fallback: %+v", got)
	}
	empty := conv
	empty.Contacts = intercomclient.ConversationContacts{}
	if got := primaryContact(empty, nil); got.ID != "" {
		t.Errorf("no contacts → zero value: %+v", got)
	}
}
