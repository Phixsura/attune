// SPDX-License-Identifier: Apache-2.0

// Package zendeskclient provides a shared, SSRF-hardened Zendesk API
// client for attune. It sits at the infra layer (parallel to
// infra/llmclient) and is consumed by the inbound adapter for ticket
// extraction (#229) and the future externalsync adapter for
// bidirectional sync (#31).
package zendeskclient

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// TicketPage is the response shape from the incremental ticket export.
type TicketPage struct {
	Tickets     []Ticket `json:"tickets"`
	AfterCursor string   `json:"after_cursor"`
	EndOfStream bool     `json:"end_of_stream"`
}

// Ticket is the Zendesk ticket shape returned by incremental exports.
type Ticket struct {
	ID                 int64              `json:"id"`
	URL                string             `json:"url"`
	Subject            string             `json:"subject"`
	Description        string             `json:"description"`
	Status             string             `json:"status"`
	Priority           string             `json:"priority"`
	Type               string             `json:"type"`
	Tags               []string           `json:"tags"`
	CustomFields       []CustomField      `json:"custom_fields"`
	RequesterID        int64              `json:"requester_id"`
	SubmitterID        int64              `json:"submitter_id"`
	AssigneeID         int64              `json:"assignee_id"`
	OrganizationID     int64              `json:"organization_id"`
	GroupID            int64              `json:"group_id"`
	CreatedAt          string             `json:"created_at"`
	UpdatedAt          string             `json:"updated_at"`
	GeneratedTimestamp int64              `json:"generated_timestamp"`
	Via                TicketVia          `json:"via"`
	SatisfactionRating SatisfactionRating `json:"satisfaction_rating"`
}

// TicketVia carries the channel through which the ticket was created.
type TicketVia struct {
	Channel string `json:"channel"`
}

// SatisfactionRating holds the CSAT survey result.
type SatisfactionRating struct {
	Score   string `json:"score"`
	Comment string `json:"comment"`
}

// CustomField is a Zendesk ticket custom field (id + value).
type CustomField struct {
	ID    int64 `json:"id"`
	Value any   `json:"value"`
}

// Comment is a Zendesk ticket comment.
type Comment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	Public    bool   `json:"public"`
	AuthorID  int64  `json:"author_id"`
	CreatedAt string `json:"created_at"`
}

// User is the minimal user shape needed for requester resolution.
type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Organization is the minimal organization shape.
type Organization struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// AccountInfo holds the resolved identity of a Zendesk connection.
type AccountInfo struct {
	Subdomain string
	AccountID int64
	URL       string
}

// OAuthToken holds OAuth 2.0 token data. All fields are stored in a
// single encrypted blob in Config.OAuthTokenEncrypted.
type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}

// APIError represents a Zendesk API error response.
type APIError struct {
	Method string
	Status int
	Code   string
}

// Error implements the error interface.
func (e APIError) Error() string {
	if e.Code == "" {
		return "zendesk " + e.Method + " failed"
	}
	if e.Status > 0 {
		return "zendesk " + e.Method + ": " + e.Code + " status=" + strconv.Itoa(e.Status)
	}
	return "zendesk " + e.Method + ": " + e.Code
}

// Permanent reports whether this error should disable the source.
func (e APIError) Permanent() bool {
	switch e.Code {
	case "unauthorized", "forbidden":
		return true
	default:
		return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	}
}

// RateLimitError is returned on 429 responses. It carries the
// Retry-After duration so the caller can sleep before retrying.
type RateLimitError struct {
	Method     string
	RetryAfter time.Duration
}

// Error implements the error interface.
func (e RateLimitError) Error() string {
	return fmt.Sprintf("zendesk %s: rate limited (retry after %s)", e.Method, e.RetryAfter)
}

// ParseRetryAfter extracts the Retry-After header value as a duration.
// Falls back to 60s if the header is missing or unparseable.
func ParseRetryAfter(h http.Header) time.Duration {
	val := h.Get("Retry-After")
	if val == "" {
		return 60 * time.Second
	}
	secs, err := strconv.Atoi(val)
	if err != nil || secs <= 0 {
		return 60 * time.Second
	}
	d := time.Duration(secs) * time.Second
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return d
}
