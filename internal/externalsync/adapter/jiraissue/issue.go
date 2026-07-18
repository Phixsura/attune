// SPDX-License-Identifier: Apache-2.0

package jiraissue

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	core "github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var requestMarkerRE = regexp.MustCompile(`attune:customer_request_id=([0-9a-fA-F-]{36})`)

type jiraSearchResponse struct {
	StartAt    int         `json:"startAt"`
	MaxResults int         `json:"maxResults"`
	Total      int         `json:"total"`
	Issues     []jiraIssue `json:"issues"`
}

func (r jiraSearchResponse) hasNextPage() bool {
	return r.StartAt+len(r.Issues) < r.Total
}

type jiraIssue struct {
	ID     string          `json:"id"`
	Key    string          `json:"key"`
	Self   string          `json:"self"`
	Fields jiraIssueFields `json:"fields"`
}

type jiraIssueFields struct {
	Summary        string          `json:"summary"`
	Description    json.RawMessage `json:"description"`
	Status         jiraStatus      `json:"status"`
	Labels         []string        `json:"labels"`
	Assignee       *jiraUser       `json:"assignee"`
	Reporter       *jiraUser       `json:"reporter"`
	Project        jiraProject     `json:"project"`
	IssueType      jiraIssueType   `json:"issuetype"`
	Comment        jiraCommentPage `json:"comment"`
	Resolution     *jiraResolution `json:"resolution"`
	ResolutionDate string          `json:"resolutiondate"`
	Updated        string          `json:"updated"`
	Created        string          `json:"created"`
	IssueLinks     []jiraIssueLink `json:"issuelinks"`
}

type jiraStatus struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	StatusCategory jiraStatusCategory `json:"statusCategory"`
}

type jiraStatusCategory struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type jiraUser struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
}

type jiraProject struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type jiraIssueType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type jiraResolution struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type jiraCommentPage struct {
	StartAt    int           `json:"startAt"`
	MaxResults int           `json:"maxResults"`
	Total      int           `json:"total"`
	Comments   []jiraComment `json:"comments"`
}

type jiraComment struct {
	ID      string          `json:"id"`
	Body    json.RawMessage `json:"body"`
	Author  *jiraUser       `json:"author"`
	Created string          `json:"created"`
	Updated string          `json:"updated"`
}

type jiraIssueLink struct {
	ID           string           `json:"id"`
	Type         jiraLinkType     `json:"type"`
	InwardIssue  *jiraLinkedIssue `json:"inwardIssue,omitempty"`
	OutwardIssue *jiraLinkedIssue `json:"outwardIssue,omitempty"`
}

type jiraLinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

type jiraLinkedIssue struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Self   string      `json:"self"`
	Fields interface{} `json:"fields"`
}

type jiraTransition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   struct {
		ID             string             `json:"id"`
		Name           string             `json:"name"`
		StatusCategory jiraStatusCategory `json:"statusCategory"`
	} `json:"to"`
}

type issueCreateRequest struct {
	Fields issueWriteFields `json:"fields"`
}

type issueUpdateRequest struct {
	Fields issueWriteFields `json:"fields"`
}

type issueWriteFields struct {
	Project     *issueRef     `json:"project,omitempty"`
	IssueType   *issueTypeRef `json:"issuetype,omitempty"`
	Summary     string        `json:"summary,omitempty"`
	Description any           `json:"description,omitempty"`
	Labels      []string      `json:"labels,omitempty"`
}

type issueRef struct {
	Key string `json:"key,omitempty"`
	ID  string `json:"id,omitempty"`
}

type issueTypeRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
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

func buildCreateRequest(cfg settings, record core.LocalRecord, payload localIssuePayload) (*issueCreateRequest, error) {
	if strings.TrimSpace(payload.Title) == "" {
		return nil, validationError("jira issue summary is required")
	}
	labels := buildLabels(cfg, payload)
	issueType := issueTypeRefFromSettings(cfg)
	fields := issueWriteFields{
		Project:   ptrext.Of(issueRef{Key: cfg.projectKey}),
		IssueType: ptrext.Of(issueType),
		Summary:   payload.Title,
		Labels:    labels,
	}
	description := buildIssueDescription(payload, true)
	if strings.TrimSpace(description) != "" {
		fields.Description = adfDocument(description)
	}
	return ptrext.Of(issueCreateRequest{Fields: fields}), nil
}

func buildUpdateRequest(cfg settings, record core.LocalRecord, payload localIssuePayload) (*issueUpdateRequest, error) {
	if strings.TrimSpace(payload.Title) == "" && !payload.BodySet && len(payload.Labels) == 0 && payload.CustomerRequestID == "" {
		return nil, nil
	}
	fields := issueWriteFields{
		Summary: payload.Title,
		Labels:  buildLabels(cfg, payload),
	}
	description := buildIssueDescription(payload, payload.BodySet)
	if payload.BodySet {
		fields.Description = adfDocument(description)
	}
	return ptrext.Of(issueUpdateRequest{Fields: fields}), nil
}

func issueTypeRefFromSettings(cfg settings) issueTypeRef {
	if strings.TrimSpace(cfg.issueTypeID) != "" {
		return issueTypeRef{ID: cfg.issueTypeID}
	}
	return issueTypeRef{Name: cfg.issueType}
}

func buildLabels(cfg settings, payload localIssuePayload) []string {
	labels := make([]string, 0, len(payload.Labels))
	for _, label := range payload.Labels {
		labels = append(labels, sanitizeLabel(label))
	}
	if marker := requestLabel(cfg, payload.CustomerRequestID); marker != "" {
		labels = append(labels, marker)
	}
	if status := sanitizeLabel("attune-status-" + strings.ToLower(strings.TrimSpace(payload.Status))); status != "" {
		labels = append(labels, status)
	}
	if priority := sanitizeLabel("attune-priority-" + strings.ToLower(strings.TrimSpace(payload.Priority))); priority != "" {
		labels = append(labels, priority)
	}
	return uniqueSortedLabels(labels)
}

func uniqueSortedLabels(labels []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

func sanitizeLabel(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	prevDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-._")
	if len(out) > 255 {
		out = out[:255]
	}
	return out
}

func requestLabel(cfg settings, customerRequestID string) string {
	customerRequestID = strings.TrimSpace(customerRequestID)
	if customerRequestID == "" {
		return ""
	}
	if _, err := uuid.Parse(customerRequestID); err != nil {
		return ""
	}
	prefix := normalizedRequestLabelPrefix(cfg.requestLabelPrefix)
	if prefix == "" {
		return ""
	}
	return sanitizeLabel(prefix + customerRequestID)
}

func requestMarker(customerRequestID string) string {
	customerRequestID = strings.TrimSpace(customerRequestID)
	if customerRequestID == "" {
		return ""
	}
	if _, err := uuid.Parse(customerRequestID); err != nil {
		return ""
	}
	return jiraMarkerCommentText + customerRequestID
}

func buildIssueDescription(payload localIssuePayload, includeMarker bool) string {
	parts := []string{}
	if body := strings.TrimSpace(payload.Body); body != "" {
		parts = append(parts, body)
	}
	if includeMarker {
		if marker := requestMarker(payload.CustomerRequestID); marker != "" {
			parts = append(parts, "Attune request ID: "+strings.TrimSpace(payload.CustomerRequestID))
			parts = append(parts, marker)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func buildRequestComment(payload localIssuePayload) string {
	marker := requestMarker(payload.CustomerRequestID)
	if marker == "" {
		return ""
	}
	parts := []string{}
	if display := strings.TrimSpace(payload.DisplayID); display != "" {
		parts = append(parts, "Attune request "+display)
	}
	parts = append(parts, "Request ID: "+strings.TrimSpace(payload.CustomerRequestID))
	parts = append(parts, marker)
	return strings.Join(parts, "\n")
}

func decodeSearchResponse(raw []byte) (jiraSearchResponse, error) {
	resp := jiraSearchResponse{}
	if err := json.Unmarshal(raw, &resp); err != nil { // ptrext:allow unmarshal-out-param
		return jiraSearchResponse{}, fmt.Errorf("decode jira search response: %w", err)
	}
	return resp, nil
}

func decodeIssueResponse(raw []byte) (jiraIssue, error) {
	issue := jiraIssue{}
	if err := json.Unmarshal(raw, &issue); err != nil { // ptrext:allow unmarshal-out-param
		return jiraIssue{}, fmt.Errorf("decode jira issue response: %w", err)
	}
	return issue, nil
}

func normalizeIssues(cfg settings, issues []jiraIssue) ([]core.ExternalRecord, time.Time, error) {
	records := make([]core.ExternalRecord, 0, len(issues))
	var maxUpdated time.Time
	for _, issue := range issues {
		updatedAt, err := parseJiraTime(issue.Fields.Updated)
		if err != nil {
			return nil, time.Time{}, err
		}
		if updatedAt.After(maxUpdated) {
			maxUpdated = updatedAt
		}
		record, err := normalizeIssueRecord(cfg, issue, updatedAt)
		if err != nil {
			return nil, time.Time{}, err
		}
		records = append(records, record)
	}
	return records, maxUpdated, nil
}

func normalizeIssueRecord(cfg settings, issue jiraIssue, updatedAt time.Time) (core.ExternalRecord, error) {
	payload, err := json.Marshal(normalizedIssuePayload(cfg, issue, updatedAt))
	if err != nil {
		return core.ExternalRecord{}, fmt.Errorf("marshal jira issue payload: %w", err)
	}
	return core.ExternalRecord{
		Key:           issue.Key,
		URL:           issueURLFromIssue(cfg, issue),
		Version:       issueVersion(issue),
		LocalObjectID: extractCustomerRequestID(cfg, issue),
		UpdatedAt:     updatedAt,
		Payload:       payload,
	}, nil
}

type normalizedIssue struct {
	Key            string              `json:"key"`
	Summary        string              `json:"summary"`
	Description    string              `json:"description,omitempty"`
	Status         string              `json:"status,omitempty"`
	StatusCategory string              `json:"status_category,omitempty"`
	Labels         []string            `json:"labels,omitempty"`
	Assignee       string              `json:"assignee,omitempty"`
	Reporter       string              `json:"reporter,omitempty"`
	ProjectKey     string              `json:"project_key,omitempty"`
	IssueType      string              `json:"issue_type,omitempty"`
	URL            string              `json:"url,omitempty"`
	CreatedAt      string              `json:"created_at,omitempty"`
	UpdatedAt      string              `json:"updated_at,omitempty"`
	ResolvedAt     string              `json:"resolved_at,omitempty"`
	CommentCount   int                 `json:"comment_count,omitempty"`
	Comments       []normalizedComment `json:"comments,omitempty"`
	IssueLinks     []normalizedLink    `json:"issue_links,omitempty"`
	RequestMarker  string              `json:"request_marker,omitempty"`
}

type normalizedComment struct {
	ID      string `json:"id,omitempty"`
	Author  string `json:"author,omitempty"`
	Created string `json:"created,omitempty"`
	Updated string `json:"updated,omitempty"`
	Body    string `json:"body,omitempty"`
}

type normalizedLink struct {
	Type      string `json:"type,omitempty"`
	Direction string `json:"direction,omitempty"`
	Key       string `json:"key,omitempty"`
	URL       string `json:"url,omitempty"`
}

func normalizedIssuePayload(cfg settings, issue jiraIssue, updatedAt time.Time) normalizedIssue {
	out := normalizedIssue{
		Key:            issue.Key,
		Summary:        strings.TrimSpace(issue.Fields.Summary),
		Description:    issueText(issue.Fields.Description),
		Status:         strings.TrimSpace(issue.Fields.Status.Name),
		StatusCategory: strings.TrimSpace(issue.Fields.Status.StatusCategory.Key),
		Labels:         normalizeLabels(issue.Fields.Labels),
		ProjectKey:     strings.TrimSpace(issue.Fields.Project.Key),
		IssueType:      strings.TrimSpace(issue.Fields.IssueType.Name),
		URL:            issueURLFromIssue(cfg, issue),
		CreatedAt:      normalizedTime(issue.Fields.Created),
		UpdatedAt:      issueVersionTime(updatedAt),
		ResolvedAt:     "",
		CommentCount:   issueCommentCount(issue.Fields.Comment),
		Comments:       normalizeComments(issue.Fields.Comment.Comments),
		IssueLinks:     normalizeLinks(cfg, issue.Fields.IssueLinks),
	}
	if issue.Fields.Assignee != nil {
		out.Assignee = strings.TrimSpace(issue.Fields.Assignee.DisplayName)
	}
	if issue.Fields.Reporter != nil {
		out.Reporter = strings.TrimSpace(issue.Fields.Reporter.DisplayName)
	}
	if issue.Fields.Resolution != nil {
		out.ResolvedAt = normalizedTime(issue.Fields.ResolutionDate)
	}
	out.RequestMarker = extractCustomerRequestID(cfg, issue)
	return out
}

func issueCommentCount(page jiraCommentPage) int {
	if page.Total > 0 {
		return page.Total
	}
	return len(page.Comments)
}

func normalizeComments(comments []jiraComment) []normalizedComment {
	out := make([]normalizedComment, 0, len(comments))
	for _, comment := range comments {
		out = append(out, normalizedComment{
			ID:      strings.TrimSpace(comment.ID),
			Author:  commentAuthor(comment.Author),
			Created: normalizedTime(comment.Created),
			Updated: normalizedTime(comment.Updated),
			Body:    issueText(comment.Body),
		})
	}
	return out
}

func commentAuthor(user *jiraUser) string {
	if user == nil {
		return ""
	}
	if name := strings.TrimSpace(user.DisplayName); name != "" {
		return name
	}
	if email := strings.TrimSpace(user.Email); email != "" {
		return email
	}
	return strings.TrimSpace(user.AccountID)
}

func normalizeLinks(cfg settings, links []jiraIssueLink) []normalizedLink {
	out := make([]normalizedLink, 0, len(links))
	for _, link := range links {
		direction := ""
		key := ""
		url := ""
		if link.InwardIssue != nil {
			direction = "inward"
			key = strings.TrimSpace(link.InwardIssue.Key)
			url = issueURLFromKey(cfg, key)
		}
		if link.OutwardIssue != nil {
			direction = "outward"
			key = strings.TrimSpace(link.OutwardIssue.Key)
			url = issueURLFromKey(cfg, key)
		}
		out = append(out, normalizedLink{
			Type:      strings.TrimSpace(link.Type.Name),
			Direction: direction,
			Key:       key,
			URL:       url,
		})
	}
	return out
}

func issueURLFromIssue(cfg settings, issue jiraIssue) string {
	if url := issueURLFromKey(cfg, issue.Key); url != "" {
		return url
	}
	return strings.TrimSpace(issue.Self)
}

func issueURLFromKey(cfg settings, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	base := browseBaseURL(cfg)
	if base == "" {
		return key
	}
	return strings.TrimRight(base, "/") + "/browse/" + key
}

func issueVersion(issue jiraIssue) string {
	return normalizedTime(issue.Fields.Updated)
}

func issueVersionTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func normalizedTime(raw string) string {
	ts, err := parseJiraTime(raw)
	if err != nil || ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func parseJiraTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-0700",
		"2006-01-02 15:04",
		"2006/01/02 15:04",
	}
	var lastErr error
	for _, layout := range layouts {
		ts, err := time.Parse(layout, raw)
		if err == nil {
			return ts.UTC(), nil
		}
		lastErr = err
	}
	return time.Time{}, fmt.Errorf("parse jira time %q: %w", raw, lastErr)
}

func issueText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil { // ptrext:allow unmarshal-out-param
		return strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(adfText(v))
}

func adfText(v any) string {
	switch value := v.(type) {
	case map[string]any:
		if text, ok := value["text"].(string); ok {
			return text
		}
		var parts []string
		if content, ok := value["content"].([]any); ok {
			for _, child := range content {
				parts = append(parts, adfText(child))
			}
		}
		if value["type"] == "hardBreak" {
			return "\n"
		}
		if len(parts) == 0 {
			return ""
		}
		joined := strings.Join(parts, "")
		switch value["type"] {
		case "paragraph", "heading", "blockquote", "codeBlock", "listItem":
			return strings.TrimSpace(joined) + "\n"
		default:
			return joined
		}
	case []any:
		parts := make([]string, 0, len(value))
		for _, child := range value {
			parts = append(parts, adfText(child))
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func adfDocument(text string) map[string]any {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	content := make([]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		content = append(content, map[string]any{
			"type": "paragraph",
			"content": []any{
				map[string]any{"type": "text", "text": line},
			},
		})
	}
	if len(content) == 0 {
		content = append(content, map[string]any{
			"type": "paragraph",
			"content": []any{
				map[string]any{"type": "text", "text": ""},
			},
		})
	}
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": content,
	}
}

func writeRequestPayload(v any) ([]byte, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal jira write payload: %w", err)
	}
	return out, nil
}

func decodeLocalPayload(record core.LocalRecord) (localIssuePayload, error) {
	return normalizeLocalPayload(record)
}

func normalizeLocalPayload(record core.LocalRecord) (localIssuePayload, error) {
	if len(record.Payload) == 0 {
		return localIssuePayload{}, validationError("jira local record payload is required")
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(record.Payload, &fields); err != nil { // ptrext:allow unmarshal-out-param
		return localIssuePayload{}, validationError("decode jira local record payload: %v", err)
	}
	payload := localIssuePayload{}
	if err := json.Unmarshal(record.Payload, &payload); err != nil { // ptrext:allow unmarshal-out-param
		return localIssuePayload{}, validationError("decode jira local record payload: %v", err)
	}
	payload.ExternalKey = strings.TrimSpace(payload.ExternalKey)
	payload.Title = strings.TrimSpace(payload.Title)
	payload.Body = strings.TrimSpace(payload.Body)
	payload.Status = strings.TrimSpace(payload.Status)
	payload.Priority = strings.TrimSpace(payload.Priority)
	payload.DisplayID = strings.TrimSpace(payload.DisplayID)
	payload.CustomerRequestID = strings.TrimSpace(payload.CustomerRequestID)
	payload.Labels = normalizeLabels(payload.Labels)
	_, payload.BodySet = fields["body"]
	return payload, nil
}

func parseURL(raw string) (*url.URL, error) {
	return url.Parse(strings.TrimSpace(raw))
}

func browseBaseURL(cfg settings) string {
	base := strings.TrimRight(strings.TrimSpace(cfg.siteURL), "/")
	if base != "" {
		return base
	}
	base = strings.TrimRight(strings.TrimSpace(cfg.apiBase), "/")
	if base == "" {
		return ""
	}
	base = strings.TrimSuffix(base, defaultAPIPath)
	return strings.TrimRight(base, "/")
}

func buildSearchURL(cfg settings, jql string, startAt int) (string, error) {
	raw, err := repoAPIURL(cfg, "search")
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse jira search url: %w", err)
	}
	q := u.Query()
	q.Set("jql", jql)
	q.Set("fields", strings.Join(searchFields(), ","))
	q.Set("maxResults", "100")
	if startAt > 0 {
		q.Set("startAt", fmt.Sprintf("%d", startAt))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func buildMarkerSearchJQL(cfg settings, customerRequestID string, byLabel bool) string {
	parts := []string{`project = "` + escapeJQL(cfg.projectKey) + `"`}
	if byLabel {
		if label := requestLabel(cfg, customerRequestID); label != "" {
			parts = append(parts, `labels = "`+escapeJQL(label)+`"`)
		}
	} else if marker := strings.TrimSpace(customerRequestID); marker != "" {
		parts = append(parts, `text ~ "`+escapeJQL(marker)+`"`)
	}
	parts = append(parts, `ORDER BY updated DESC, id DESC`)
	return strings.Join(parts, " AND ")
}

func normalizeLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		label = sanitizeLabel(label)
		if label != "" {
			out = append(out, label)
		}
	}
	return uniqueSortedLabels(out)
}

func extractCustomerRequestID(cfg settings, issue jiraIssue) string {
	if marker := extractCustomerRequestIDFromLabels(cfg, issue.Fields.Labels); marker != "" {
		return marker
	}
	if marker := extractCustomerRequestIDFromText(issueText(issue.Fields.Description)); marker != "" {
		return marker
	}
	for _, comment := range issue.Fields.Comment.Comments {
		if marker := extractCustomerRequestIDFromText(issueText(comment.Body)); marker != "" {
			return marker
		}
	}
	return ""
}

func extractCustomerRequestIDFromLabels(cfg settings, labels []string) string {
	prefix := normalizedRequestLabelPrefix(cfg.requestLabelPrefix)
	if prefix == "" {
		prefix = defaultLabelPrefix
	}
	for _, label := range labels {
		label = sanitizeLabel(label)
		if strings.HasPrefix(label, prefix) {
			id := strings.TrimPrefix(label, prefix)
			if _, err := uuid.Parse(id); err == nil {
				return id
			}
		}
	}
	return ""
}

func extractCustomerRequestIDFromText(text string) string {
	matches := requestMarkerRE.FindStringSubmatch(text)
	if len(matches) != 2 {
		return ""
	}
	if _, err := uuid.Parse(matches[1]); err != nil {
		return ""
	}
	return matches[1]
}

func issueHasMarker(cfg settings, issue jiraIssue, marker string) bool {
	marker = markerCustomerRequestID(marker)
	if marker == "" {
		return false
	}
	if marker == extractCustomerRequestIDFromLabels(cfg, issue.Fields.Labels) {
		return true
	}
	if marker == extractCustomerRequestIDFromText(issueText(issue.Fields.Description)) {
		return true
	}
	for _, comment := range issue.Fields.Comment.Comments {
		if marker == extractCustomerRequestIDFromText(issueText(comment.Body)) {
			return true
		}
	}
	return false
}

func markerCustomerRequestID(marker string) string {
	marker = strings.TrimSpace(marker)
	if marker == "" {
		return ""
	}
	marker = strings.TrimPrefix(marker, jiraMarkerCommentText)
	if matches := requestMarkerRE.FindStringSubmatch(marker); len(matches) == 2 {
		marker = matches[1]
	}
	if _, err := uuid.Parse(marker); err != nil {
		return ""
	}
	return marker
}

func findTransition(transitions []jiraTransition, raw string) *jiraTransition {
	for i := range transitions {
		transition := ptrext.Of(transitions[i])
		if strings.EqualFold(transition.ID, raw) || strings.EqualFold(transition.Name, raw) {
			return transition
		}
		if strings.EqualFold(transition.To.Name, raw) || strings.EqualFold(transition.To.StatusCategory.Key, raw) {
			return transition
		}
	}
	return nil
}

func chooseHeuristicTransition(transitions []jiraTransition, localStatus string) *jiraTransition {
	localStatus = strings.ToLower(strings.TrimSpace(localStatus))
	if localStatus == "" {
		return nil
	}
	var categoryTargets []string
	var nameTargets []string
	switch localStatus {
	case "shipped", "cancelled":
		categoryTargets = []string{"done"}
		nameTargets = []string{"done", "closed", "resolved", "cancelled"}
	case "in_progress":
		categoryTargets = []string{"indeterminate"}
		nameTargets = []string{"in progress", "progress", "doing"}
	case "planned", "open":
		categoryTargets = []string{"new"}
		nameTargets = []string{"to do", "open", "planned", "backlog"}
	default:
		return nil
	}
	for i := range transitions {
		transition := ptrext.Of(transitions[i])
		if hasStringFold(categoryTargets, transition.To.StatusCategory.Key) ||
			hasStringFold(nameTargets, transition.Name) ||
			hasStringFold(nameTargets, transition.To.Name) {
			return transition
		}
	}
	return nil
}

func hasStringFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func statusCategoryMatches(category, localStatus string) bool {
	localStatus = strings.ToLower(strings.TrimSpace(localStatus))
	category = strings.ToLower(strings.TrimSpace(category))
	switch localStatus {
	case "shipped", "cancelled":
		return category == "done"
	case "in_progress":
		return category == "indeterminate"
	case "planned", "open":
		return category == "new"
	default:
		return false
	}
}

func canSkipTransition(localStatus string) bool {
	switch strings.ToLower(strings.TrimSpace(localStatus)) {
	case "open", "planned":
		return true
	default:
		return false
	}
}

func firstHeader(headers http.Header, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	classified := classifyError(err)
	return classified.HTTPStatus == http.StatusNotFound
}
