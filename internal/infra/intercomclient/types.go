// SPDX-License-Identifier: Apache-2.0

// Package intercomclient provides a shared, SSRF-hardened Intercom API
// client for attune. It sits at the infra layer (parallel to
// infra/zendeskclient) and is consumed by the inbound adapter for
// conversation extraction (#230) and the future externalsync adapter
// for bidirectional sync (#32).
package intercomclient

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ConversationPage is one page of the conversations search response.
type ConversationPage struct {
	Conversations []Conversation `json:"conversations"`
	TotalCount    int64          `json:"total_count"`
	// StartingAfter is the cursor for the next page; empty when this is
	// the last page.
	StartingAfter string
}

// Conversation is the Intercom conversation shape. List/search responses
// omit Parts; GetConversation populates them (≤500, API hard cap).
type Conversation struct {
	ID                  string               `json:"id"`
	Title               string               `json:"title"`
	State               string               `json:"state"` // open | closed | snoozed
	Priority            string               `json:"priority"`
	CreatedAt           int64                `json:"created_at"` // unix seconds
	UpdatedAt           int64                `json:"updated_at"` // unix seconds
	AdminAssigneeID     int64                `json:"admin_assignee_id"`
	TeamAssigneeID      int64                `json:"team_assignee_id"`
	Source              ConversationSource   `json:"source"`
	Contacts            ConversationContacts `json:"contacts"`
	Company             *Company             `json:"company"`
	Tags                TagList              `json:"tags"`
	Rating              *ConversationRating  `json:"conversation_rating"`
	Parts               ConversationParts    `json:"conversation_parts"`
	CustomAttributes    map[string]any       `json:"custom_attributes"`
	AIAgentParticipated bool                 `json:"ai_agent_participated"`
	AIAgent             *AIAgent             `json:"ai_agent"`
}

// ConversationSource is the seed message that started the conversation.
type ConversationSource struct {
	Type    string     `json:"type"` // conversation | email | phone_call | ...
	Subject string     `json:"subject"`
	Body    string     `json:"body"`
	Author  PartAuthor `json:"author"`
	// URL is the page the customer started the conversation from
	// (Messenger conversations only; blank for email/Twitter/bots).
	URL string `json:"url"`
}

// AIAgent records how Intercom's AI agent (Fin) left the conversation.
// An escalated or negative-feedback resolution is a strong product-pain
// signal; a confirmed resolution is routine support noise.
type AIAgent struct {
	SourceType      string `json:"source_type"`
	ResolutionState string `json:"resolution_state"` // assumed_resolution | confirmed_resolution | escalated | negative_feedback | procedure_handoff
	LastAnswerType  string `json:"last_answer_type"` // ai_answer | custom_answer
	Rating          int    `json:"rating"`           // 1-5, 0 = unrated
	RatingRemark    string `json:"rating_remark"`
}

// ConversationContacts wraps the contact references on a conversation.
type ConversationContacts struct {
	Contacts []ContactRef `json:"contacts"`
}

// ContactRef is the minimal contact reference embedded in conversations.
type ContactRef struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
}

// TagList wraps the tags on a conversation.
type TagList struct {
	Tags []Tag `json:"tags"`
}

// Tag is an Intercom conversation tag.
type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ConversationRating is the CSAT rating left on a conversation.
type ConversationRating struct {
	Rating int    `json:"rating"`
	Remark string `json:"remark"`
}

// ConversationParts wraps the thread messages returned by GetConversation.
type ConversationParts struct {
	Parts []Part `json:"conversation_parts"`
}

// Part is one message in a conversation thread.
type Part struct {
	ID        string     `json:"id"`
	PartType  string     `json:"part_type"` // comment | note | assignment | ...
	Body      string     `json:"body"`
	CreatedAt int64      `json:"created_at"`
	Author    PartAuthor `json:"author"`
	Redacted  bool       `json:"redacted"`
}

// PartAuthor identifies who wrote a conversation part.
type PartAuthor struct {
	Type        string `json:"type"` // user | lead | contact | admin | team | bot
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	FromAIAgent bool   `json:"from_ai_agent"`
}

// Contact is the resolved contact shape from contacts/search.
type Contact struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
	Role       string `json:"role"` // user | lead
	Email      string `json:"email"`
	Name       string `json:"name"`
}

// Company is the company attached to a conversation. GetCompany fills
// the profile fields; conversation embeds carry only id/name.
type Company struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Profile fields (GET /companies/{id}).
	MonthlySpend int         `json:"monthly_spend"` // revenue this company generates
	Size         int         `json:"size"`          // employee count
	Industry     string      `json:"industry"`
	Plan         CompanyPlan `json:"plan"`
}

// CompanyPlan is the subscription plan recorded on a company.
type CompanyPlan struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Admin is a workspace teammate (from GET /admins).
type Admin struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// AccountInfo holds the resolved identity of an Intercom connection
// (from GET /me).
type AccountInfo struct {
	WorkspaceID   string
	WorkspaceName string
	Region        string
	AdminEmail    string
}

// APIError represents an Intercom API error response.
type APIError struct {
	Method string
	Status int
	Code   string
}

// Error implements the error interface.
func (e APIError) Error() string {
	if e.Code == "" {
		return "intercom " + e.Method + " failed"
	}
	if e.Status > 0 {
		return "intercom " + e.Method + ": " + e.Code + " status=" + strconv.Itoa(e.Status)
	}
	return "intercom " + e.Method + ": " + e.Code
}

// Permanent reports whether this error should disable the source.
// 401 unauthorized (token revoked/invalid) and 403 api_plan_restricted
// both require operator action.
func (e APIError) Permanent() bool {
	switch e.Code {
	case "unauthorized", "api_plan_restricted", "forbidden", "token_revoked", "token_expired":
		return true
	default:
		return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	}
}

// RateLimitError is returned on 429 responses. RetryAfter is derived
// from X-RateLimit-Reset (Intercom does not send Retry-After).
type RateLimitError struct {
	Method     string
	RetryAfter time.Duration
}

// Error implements the error interface.
func (e RateLimitError) Error() string {
	return fmt.Sprintf("intercom %s: rate limited (retry after %s)", e.Method, e.RetryAfter)
}

// nowFn is overrideable in tests for deterministic reset math.
var nowFn = time.Now

// parseRateLimitReset derives a retry-after duration from the
// X-RateLimit-Reset header (unix seconds). Intercom enforces limits in
// 10-second windows, so the fallback is 10s; the result is clamped to
// [1s, 15m].
func parseRateLimitReset(h http.Header) time.Duration {
	const fallback = 10 * time.Second
	val := h.Get("X-RateLimit-Reset")
	if val == "" {
		return fallback
	}
	resetAt, err := strconv.ParseInt(val, 10, 64)
	if err != nil || resetAt <= 0 {
		return fallback
	}
	d := time.Unix(resetAt, 0).Sub(nowFn())
	if d < time.Second {
		return time.Second
	}
	if d > 15*time.Minute {
		return 15 * time.Minute
	}
	return d
}
