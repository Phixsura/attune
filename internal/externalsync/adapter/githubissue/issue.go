// SPDX-License-Identifier: Apache-2.0

package githubissue

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	core "github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const customerRequestMarker = "attune:customer_request_id"

var customerRequestMarkerRE = regexp.MustCompile(`<!--\s*attune:customer_request_id=([0-9a-fA-F-]{36})\s*-->`)

type apiIssue struct {
	Number      int        `json:"number"`
	HTMLURL     string     `json:"html_url"`
	Title       string     `json:"title"`
	State       string     `json:"state"`
	StateReason *string    `json:"state_reason"`
	Locked      bool       `json:"locked"`
	Assignee    *apiUser   `json:"assignee"`
	Assignees   []apiUser  `json:"assignees"`
	Labels      []apiLabel `json:"labels"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	Body        string     `json:"body"`
	PullRequest any        `json:"pull_request,omitempty"`
}

type apiUser struct {
	Login string `json:"login"`
}

type apiLabel struct {
	Name string `json:"name"`
}

type normalizedIssue struct {
	Number      int      `json:"number"`
	Title       string   `json:"title"`
	State       string   `json:"state"`
	StateReason string   `json:"state_reason,omitempty"`
	Locked      bool     `json:"locked"`
	Assignee    string   `json:"assignee,omitempty"`
	Assignees   []string `json:"assignees,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	URL         string   `json:"url,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	ClosedAt    string   `json:"closed_at,omitempty"`
}

type localIssuePayload struct {
	ExternalKey       string   `json:"external_key,omitempty"`
	Title             string   `json:"title,omitempty"`
	Body              string   `json:"body,omitempty"`
	State             string   `json:"state,omitempty"`
	Status            string   `json:"status,omitempty"`
	Priority          string   `json:"priority,omitempty"`
	DisplayID         string   `json:"display_id,omitempty"`
	Labels            []string `json:"labels,omitempty"`
	CustomerRequestID string   `json:"customer_request_id,omitempty"`
	BodySet           bool     `json:"-"`
}

type issueWriteRequest struct {
	Title  string   `json:"title,omitempty"`
	Body   string   `json:"body,omitempty"`
	State  string   `json:"state,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

func issuesURL(cfg settings, cursor cursorState) (string, error) {
	if cursor.NextURL != "" {
		return validateNextURL(cfg, cursor.NextURL)
	}
	raw, err := repoAPIURL(cfg, "issues")
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("direction", "asc")
	q.Set("per_page", "100")
	q.Set("sort", "updated")
	q.Set("state", "all")
	if cursor.UpdatedSince != "" {
		q.Set("since", cursor.UpdatedSince)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func repoAPIURL(cfg settings, parts ...string) (string, error) {
	joined := append([]string{"repos", cfg.owner, cfg.repo}, parts...)
	raw, err := url.JoinPath(cfg.apiBase, joined...)
	if err != nil {
		return "", fmt.Errorf("build github api url: %w", err)
	}
	return raw, nil
}

func validateNextURL(cfg settings, raw string) (string, error) {
	page, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse github cursor next_url: %w", err)
	}
	issuesRaw, err := repoAPIURL(cfg, "issues")
	if err != nil {
		return "", err
	}
	issuesBase, err := url.Parse(issuesRaw)
	if err != nil {
		return "", fmt.Errorf("parse github issues url: %w", err)
	}
	if page.Scheme != issuesBase.Scheme || !strings.EqualFold(page.Host, issuesBase.Host) {
		return "", validationError("github cursor next_url must stay on configured api host")
	}
	if !pathHasBase(page.EscapedPath(), issuesBase.EscapedPath()) {
		return "", validationError("github cursor next_url must stay under configured repository issues path")
	}
	return raw, nil
}

func pathHasBase(pagePath, basePath string) bool {
	basePath = strings.TrimRight(basePath, "/")
	if basePath == "" {
		return true
	}
	return pagePath == basePath || strings.HasPrefix(pagePath, basePath+"/")
}

func normalizeIssues(issues []apiIssue) ([]core.ExternalRecord, time.Time, error) {
	records := make([]core.ExternalRecord, 0, len(issues))
	var maxUpdated time.Time
	for _, issue := range issues {
		if issue.UpdatedAt.After(maxUpdated) {
			maxUpdated = issue.UpdatedAt
		}
		if issue.PullRequest != nil {
			continue
		}
		record, err := normalizeIssueRecord(issue)
		if err != nil {
			return nil, time.Time{}, err
		}
		records = append(records, record)
	}
	return records, maxUpdated, nil
}

func normalizeIssueRecord(issue apiIssue) (core.ExternalRecord, error) {
	payload, err := json.Marshal(normalizedIssuePayload(issue))
	if err != nil {
		return core.ExternalRecord{}, fmt.Errorf("marshal github issue payload: %w", err)
	}
	return core.ExternalRecord{
		Key:           strconv.Itoa(issue.Number),
		URL:           issue.HTMLURL,
		Version:       issueVersion(issue),
		LocalObjectID: extractCustomerRequestID(issue.Body),
		UpdatedAt:     issue.UpdatedAt,
		Payload:       payload,
	}, nil
}

func normalizedIssuePayload(issue apiIssue) normalizedIssue {
	out := normalizedIssue{
		Number:      issue.Number,
		Title:       issue.Title,
		State:       issue.State,
		StateReason: ptrext.Indirect(issue.StateReason),
		Locked:      issue.Locked,
		URL:         issue.HTMLURL,
		UpdatedAt:   issueVersion(issue),
		ClosedAt:    optionalTimeString(issue.ClosedAt),
		Labels:      issueLabels(issue.Labels),
		Assignees:   issueAssignees(issue.Assignees),
	}
	if issue.Assignee != nil {
		out.Assignee = issue.Assignee.Login
	}
	return out
}

func issueVersion(issue apiIssue) string {
	if issue.UpdatedAt.IsZero() {
		return ""
	}
	return issue.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func optionalTimeString(ts *time.Time) string {
	if ts == nil {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func issueLabels(labels []apiLabel) []string {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		name := strings.TrimSpace(label.Name)
		if name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func issueAssignees(users []apiUser) []string {
	out := make([]string, 0, len(users))
	for _, user := range users {
		login := strings.TrimSpace(user.Login)
		if login != "" {
			out = append(out, login)
		}
	}
	sort.Strings(out)
	return out
}

func nextCursor(cfg settings, previous cursorState, maxUpdated time.Time, linkHeader string) ([]byte, error) {
	if nextURL := parseNextLink(linkHeader); nextURL != "" {
		nextURL, err := validateNextURL(cfg, nextURL)
		if err != nil {
			return nil, err
		}
		return encodeCursor(cursorState{UpdatedSince: previous.UpdatedSince, NextURL: nextURL})
	}
	if maxUpdated.IsZero() {
		return encodeCursor(cursorState{UpdatedSince: previous.UpdatedSince})
	}
	return encodeCursor(cursorState{UpdatedSince: maxUpdated.UTC().Format(time.RFC3339Nano)})
}

func parseNextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start >= 0 && end > start {
			return strings.TrimSpace(part[start+1 : end])
		}
	}
	return ""
}

func decodeLocalPayload(record core.LocalRecord) (localIssuePayload, error) {
	if len(record.Payload) == 0 {
		return localIssuePayload{}, validationError("github local record payload is required")
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(record.Payload, &fields); err != nil { // ptrext:allow unmarshal-out-param
		return localIssuePayload{}, validationError("decode github local record payload: %v", err)
	}
	payload := localIssuePayload{}
	if err := json.Unmarshal(record.Payload, &payload); err != nil { // ptrext:allow unmarshal-out-param
		return localIssuePayload{}, validationError("decode github local record payload: %v", err)
	}
	payload.ExternalKey = strings.TrimSpace(payload.ExternalKey)
	payload.Title = strings.TrimSpace(payload.Title)
	payload.State = strings.TrimSpace(payload.State)
	payload.Status = strings.TrimSpace(payload.Status)
	payload.Priority = strings.TrimSpace(payload.Priority)
	payload.DisplayID = strings.TrimSpace(payload.DisplayID)
	payload.CustomerRequestID = strings.TrimSpace(payload.CustomerRequestID)
	payload.Labels = cleanLabels(payload.Labels)
	_, payload.BodySet = fields["body"]
	return payload, nil
}

func buildCreateRequest(record core.LocalRecord, payload localIssuePayload) (issueWriteRequest, error) {
	if payload.Title == "" {
		return issueWriteRequest{}, validationError("github issue title is required")
	}
	return issueWriteRequest{
		Title:  payload.Title,
		Body:   withCustomerRequestMarker(payload.Body, record.ID, payload.CustomerRequestID),
		Labels: payload.Labels,
	}, nil
}

func buildUpdateRequest(record core.LocalRecord, payload localIssuePayload) (issueWriteRequest, error) {
	if _, err := strconv.Atoi(payload.ExternalKey); err != nil {
		return issueWriteRequest{}, validationError("github external_key must be an issue number")
	}
	req := issueWriteRequest{
		Title:  payload.Title,
		State:  githubState(payload),
		Labels: payload.Labels,
	}
	if payload.BodySet {
		req.Body = withCustomerRequestMarker(payload.Body, record.ID, payload.CustomerRequestID)
	}
	return req, nil
}

func cleanLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label != "" {
			out = append(out, label)
		}
	}
	return out
}

func withCustomerRequestMarker(body, recordID, payloadID string) string {
	id := markerID(recordID, payloadID)
	if id == "" || strings.Contains(body, customerRequestMarker) {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Sprintf("<!-- %s=%s -->", customerRequestMarker, id)
	}
	return fmt.Sprintf("%s\n\n<!-- %s=%s -->", body, customerRequestMarker, id)
}

func markerID(recordID, payloadID string) string {
	for _, candidate := range []string{payloadID, recordID} {
		if _, err := uuid.Parse(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func extractCustomerRequestID(body string) string {
	matches := customerRequestMarkerRE.FindStringSubmatch(body)
	if len(matches) != 2 {
		return ""
	}
	if _, err := uuid.Parse(matches[1]); err != nil {
		return ""
	}
	return matches[1]
}

func decodeIssueResponse(body []byte) (apiIssue, error) {
	issue := apiIssue{}
	if err := json.Unmarshal(body, &issue); err != nil { // ptrext:allow unmarshal-out-param
		return apiIssue{}, fmt.Errorf("decode github issue response: %w", err)
	}
	return issue, nil
}

func writeRequestPayload(req issueWriteRequest) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal github issue request: %w", err)
	}
	return body, nil
}

func issueMethod(payload localIssuePayload) string {
	if payload.ExternalKey == "" {
		return http.MethodPost
	}
	return http.MethodPatch
}

func githubState(payload localIssuePayload) string {
	if payload.State != "" {
		return payload.State
	}
	switch payload.Status {
	case "shipped", "cancelled":
		return "closed"
	default:
		return "open"
	}
}
