// SPDX-License-Identifier: Apache-2.0

package githubissue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

const attuneCommentMarker = "attune:comment_id"

var (
	customerRequestMarkerRE = regexp.MustCompile(`<!--\s*attune:customer_request_id=([0-9a-fA-F-]{36})\s*-->`)
	attuneCommentMarkerRE   = regexp.MustCompile(`<!--\s*` + regexp.QuoteMeta(attuneCommentMarker) + `=([A-Za-z0-9:_-]+)\s*-->`)
)

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
	Comments    int        `json:"comments"`
	PullRequest any        `json:"pull_request,omitempty"`
}

type apiUser struct {
	ID      int64  `json:"id"`
	Login   string `json:"login"`
	HTMLURL string `json:"html_url"`
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
	Comments    int      `json:"comments,omitempty"`
}

type apiComment struct {
	ID        int64     `json:"id"`
	HTMLURL   string    `json:"html_url"`
	Body      string    `json:"body"`
	User      *apiUser  `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type normalizedComment struct {
	ID               int64  `json:"id"`
	AuthorLogin      string `json:"author_login,omitempty"`
	AuthorExternalID string `json:"author_external_id,omitempty"`
	AuthorURL        string `json:"author_url,omitempty"`
	Body             string `json:"body"`
	BodyDigest       string `json:"body_digest"`
	Marker           string `json:"marker,omitempty"`
	URL              string `json:"url,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	Deleted          bool   `json:"deleted,omitempty"`
}

type pullHint struct {
	IssueNumber int
	CommentID   int64
	EventType   string
	Action      string
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

type commentWriteRequest struct {
	Body string `json:"body"`
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

func issueURL(cfg settings, number int) (string, error) {
	if number <= 0 {
		return "", validationError("github issue number must be positive")
	}
	return repoAPIURL(cfg, "issues", strconv.Itoa(number))
}

func issueCommentsURL(cfg settings, number int) (string, error) {
	raw, err := repoAPIURL(cfg, "issues", strconv.Itoa(number), "comments")
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("per_page", "100")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func issueCommentURL(cfg settings, commentID int64) (string, error) {
	if commentID <= 0 {
		return "", validationError("github issue comment id must be positive")
	}
	return repoAPIURL(cfg, "issues", "comments", strconv.FormatInt(commentID, 10))
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
		Comments:    issue.Comments,
	}
	if issue.Assignee != nil {
		out.Assignee = issue.Assignee.Login
	}
	return out
}

func normalizeCommentChildren(parentKey string, comments []apiComment) ([]core.ExternalChildRecord, error) {
	children := make([]core.ExternalChildRecord, 0, len(comments))
	for _, comment := range comments {
		child, err := normalizeCommentChild(parentKey, comment)
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return children, nil
}

func normalizeCommentChild(parentKey string, comment apiComment) (core.ExternalChildRecord, error) {
	payload, err := json.Marshal(normalizedCommentPayload(comment))
	if err != nil {
		return core.ExternalChildRecord{}, fmt.Errorf("marshal github issue comment payload: %w", err)
	}
	return core.ExternalChildRecord{
		ParentKey: parentKey,
		Type:      "comment",
		Key:       strconv.FormatInt(comment.ID, 10),
		URL:       comment.HTMLURL,
		Version:   commentVersion(comment),
		UpdatedAt: comment.UpdatedAt,
		Payload:   payload,
	}, nil
}

func deletedCommentChild(parentKey string, commentID int64) (core.ExternalChildRecord, error) {
	payload, err := json.Marshal(normalizedComment{
		ID:         commentID,
		BodyDigest: bodyDigest(""),
		Deleted:    true,
	})
	if err != nil {
		return core.ExternalChildRecord{}, fmt.Errorf("marshal github deleted issue comment payload: %w", err)
	}
	return core.ExternalChildRecord{
		ParentKey: parentKey,
		Type:      "comment",
		Key:       strconv.FormatInt(commentID, 10),
		Deleted:   true,
		Payload:   payload,
	}, nil
}

func normalizedCommentPayload(comment apiComment) normalizedComment {
	out := normalizedComment{
		ID:         comment.ID,
		Body:       comment.Body,
		BodyDigest: bodyDigest(comment.Body),
		Marker:     extractAttuneCommentID(comment.Body),
		URL:        comment.HTMLURL,
		CreatedAt:  optionalNonZeroTimeString(comment.CreatedAt),
		UpdatedAt:  commentVersion(comment),
	}
	if comment.User != nil {
		out.AuthorLogin = strings.TrimSpace(comment.User.Login)
		if comment.User.ID > 0 {
			out.AuthorExternalID = strconv.FormatInt(comment.User.ID, 10)
		}
		out.AuthorURL = strings.TrimSpace(comment.User.HTMLURL)
	}
	return out
}

func issueVersion(issue apiIssue) string {
	if issue.UpdatedAt.IsZero() {
		return ""
	}
	return issue.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func commentVersion(comment apiComment) string {
	if comment.UpdatedAt.IsZero() {
		return ""
	}
	return comment.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func optionalTimeString(ts *time.Time) string {
	if ts == nil {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func optionalNonZeroTimeString(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func bodyDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
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

func buildCreateRequest(cfg settings, record core.LocalRecord, payload localIssuePayload) (issueWriteRequest, error) {
	if payload.Title == "" {
		return issueWriteRequest{}, validationError("github issue title is required")
	}
	return issueWriteRequest{
		Title:  payload.Title,
		Body:   withCustomerRequestMarker(payload.Body, record.ID, payload.CustomerRequestID),
		Labels: mergeLabels(cfg.defaultLabels, payload.Labels),
	}, nil
}

func buildUpdateRequest(cfg settings, record core.LocalRecord, payload localIssuePayload, current apiIssue) (issueWriteRequest, error) {
	if _, _, err := normalizeIssueWriteKey(cfg, payload.ExternalKey); err != nil {
		return issueWriteRequest{}, err
	}
	req := issueWriteRequest{State: githubStateForUpdate(payload, cfg.allowReopen)}
	if cfg.linkedExistingWritePolicy != linkedExistingWriteManagedFields {
		return req, nil
	}
	req.Title = payload.Title
	req.Labels = mergeManagedLabels(cfg, issueLabels(current.Labels), payload.Labels)
	if payload.BodySet {
		req.Body = withCustomerRequestMarker(payload.Body, record.ID, payload.CustomerRequestID)
	}
	return req, nil
}

func normalizeIssueWriteKey(cfg settings, raw string) (int, string, error) {
	key := strings.TrimSpace(raw)
	if issueNumber := positiveInt(key); issueNumber > 0 {
		return issueNumber, strconv.Itoa(issueNumber), nil
	}
	repoKey, issueKey, ok := strings.Cut(key, "#")
	if !ok || issueKey == "" {
		return 0, "", validationError("github external_key must be an issue number")
	}
	parts := strings.Split(repoKey, "/")
	if len(parts) != 2 ||
		!strings.EqualFold(strings.TrimSpace(parts[0]), cfg.owner) ||
		!strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(parts[1]), ".git"), cfg.repo) {
		return 0, "", validationError("github external_key repository must match the configured github repo")
	}
	issueNumber := positiveInt(issueKey)
	if issueNumber <= 0 {
		return 0, "", validationError("github external_key must be an issue number")
	}
	return issueNumber, strconv.Itoa(issueNumber), nil
}

func (req issueWriteRequest) empty() bool {
	return req.Title == "" && req.Body == "" && req.State == "" && len(req.Labels) == 0
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

func mergeLabels(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, labels := range groups {
		for _, label := range cleanLabels(labels) {
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			out = append(out, label)
		}
	}
	sort.Strings(out)
	return out
}

func mergeManagedLabels(cfg settings, currentLabels, desiredLabels []string) []string {
	out := []string{}
	for _, label := range cleanLabels(currentLabels) {
		if strings.HasPrefix(label, cfg.managedLabelPrefix) {
			continue
		}
		out = append(out, label)
	}
	return mergeLabels(out, cfg.defaultLabels, desiredLabels)
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

func extractAttuneCommentID(body string) string {
	matches := attuneCommentMarkerRE.FindStringSubmatch(body)
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func managedCommentBody(record core.LocalRecord, payload localIssuePayload) (string, string) {
	requestID := markerID(record.ID, payload.CustomerRequestID)
	if requestID == "" {
		return "", ""
	}
	commentID := managedCommentID(requestID)
	body := strings.TrimSpace(payload.Body)
	if body == "" {
		body = "No request context provided."
	}
	body = truncateString(body, 4500)
	return fmt.Sprintf("Attune request context\n\n%s\n\n<!-- %s=%s -->\n<!-- %s=%s -->",
		body, attuneCommentMarker, commentID, customerRequestMarker, requestID), commentID
}

func managedCommentID(requestID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("attune:github:issue-comment:"+requestID)).String()
}

func findManagedComment(comments []apiComment, marker string) *apiComment {
	for i := range comments {
		if extractAttuneCommentID(comments[i].Body) == marker {
			return ptrext.Of(comments[i])
		}
	}
	return nil
}

func truncateString(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func decodePullHint(raw []byte) pullHint {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil { // ptrext:allow unmarshal-out-param
		return pullHint{}
	}
	issueNumber := positiveInt(payload["issue_number"])
	if issueNumber == 0 {
		issueNumber = positiveInt(payload["external_key"])
	}
	if issueNumber == 0 {
		issueNumber = issueNumberFromExternalKey(stringField(payload["external_key"]))
	}
	return pullHint{
		IssueNumber: issueNumber,
		CommentID:   positiveInt64(payload["comment_id"]),
		EventType:   stringField(payload["event_type"]),
		Action:      stringField(payload["action"]),
	}
}

func positiveInt(value any) int {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func issueNumberFromExternalKey(externalKey string) int {
	_, issueNumber, ok := strings.Cut(strings.TrimSpace(externalKey), "#")
	if !ok {
		return 0
	}
	return positiveInt(issueNumber)
}

func positiveInt64(value any) int64 {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int64(v)
		}
	case int64:
		if v > 0 {
			return v
		}
	case int:
		if v > 0 {
			return int64(v)
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func stringField(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func decodeIssueResponse(body []byte) (apiIssue, error) {
	issue := apiIssue{}
	if err := json.Unmarshal(body, &issue); err != nil { // ptrext:allow unmarshal-out-param
		return apiIssue{}, fmt.Errorf("decode github issue response: %w", err)
	}
	return issue, nil
}

func decodeCommentsResponse(body []byte) ([]apiComment, error) {
	comments := []apiComment{}
	if err := json.Unmarshal(body, &comments); err != nil { // ptrext:allow unmarshal-out-param
		return nil, fmt.Errorf("decode github issue comments response: %w", err)
	}
	return comments, nil
}

func decodeCommentResponse(body []byte) (apiComment, error) {
	comment := apiComment{}
	if err := json.Unmarshal(body, &comment); err != nil { // ptrext:allow unmarshal-out-param
		return apiComment{}, fmt.Errorf("decode github issue comment response: %w", err)
	}
	return comment, nil
}

func writeRequestPayload(req issueWriteRequest) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal github issue request: %w", err)
	}
	return body, nil
}

func writeCommentPayload(body string) ([]byte, error) {
	payload, err := json.Marshal(commentWriteRequest{Body: body})
	if err != nil {
		return nil, fmt.Errorf("marshal github issue comment request: %w", err)
	}
	return payload, nil
}

func githubStateForUpdate(payload localIssuePayload, allowReopen bool) string {
	state := strings.ToLower(strings.TrimSpace(payload.State))
	switch state {
	case "closed":
		return "closed"
	case "open":
		if allowReopen {
			return "open"
		}
		return ""
	}
	if payload.State != "" {
		return state
	}
	switch strings.ToLower(strings.TrimSpace(payload.Status)) {
	case "shipped", "cancelled":
		return "closed"
	case "open", "planned", "in_progress":
		if allowReopen {
			return "open"
		}
	}
	return ""
}

func githubState(payload localIssuePayload) string {
	if state := githubStateForUpdate(payload, true); state != "" {
		return state
	}
	if strings.TrimSpace(payload.State) == "" && strings.TrimSpace(payload.Status) == "" {
		return "open"
	}
	return ""
}
