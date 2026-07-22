// SPDX-License-Identifier: Apache-2.0

// Package githubissue syncs Attune external object mappings with GitHub
// Issues through the external sync provider contract.
package githubissue

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	core "github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const providerID = "github"

// Provider implements the GitHub Issues external sync adapter.
type Provider struct {
	client *http.Client
}

// Option customizes a Provider.
type Option func(*Provider)

// WithHTTPClient injects a client for tests.
func WithHTTPClient(client *http.Client) Option {
	return func(p *Provider) {
		if client != nil {
			p.client = client
		}
	}
}

// NewProvider returns a GitHub Issues external sync provider.
func NewProvider(opts ...Option) *Provider {
	p := ptrext.Of(Provider{client: core.NewHTTPClient(15 * time.Second)})
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func init() {
	core.Register(providerID, "GitHub", func() core.Provider { return NewProvider() })
}

func (p *Provider) Provider() string {
	return providerID
}

func (p *Provider) Check(ctx context.Context, conn core.Connection) (core.CheckResult, error) {
	start := time.Now()
	cfg, err := settingsFromConnection(conn)
	if err != nil {
		return core.CheckResult{OK: false, Error: err.Error(), Latency: time.Since(start)}, nil
	}
	rawURL, err := repoAPIURL(cfg)
	if err != nil {
		return core.CheckResult{OK: false, Error: err.Error(), Latency: time.Since(start)}, nil
	}
	_, headers, err := p.request(ctx, cfg, http.MethodGet, rawURL, nil)
	result := core.CheckResult{OK: err == nil, Latency: time.Since(start), RequestID: headers.Get("X-GitHub-Request-Id")}
	if err == nil {
		return result, nil
	}
	classified := p.ClassifyError(err)
	result.Error = classified.Message
	if classified.HTTPStatus > 0 {
		return result, nil
	}
	return result, err
}

func (p *Provider) Discover(_ context.Context, _ core.Connection) ([]core.ObjectSchema, error) {
	return []core.ObjectSchema{
		{
			Type: "issue",
			Fields: []string{
				"number",
				"title",
				"state",
				"state_reason",
				"labels",
				"assignee",
				"assignees",
				"url",
				"updated_at",
				"closed_at",
				"comments",
				"comment.id",
				"comment.body",
				"comment.author_login",
				"comment.updated_at",
				"delivery_artifact.type",
				"delivery_artifact.relationship",
				"delivery_artifact.external_key",
				"delivery_artifact.title",
				"delivery_artifact.status",
				"delivery_artifact.updated_at",
			},
			RequiredFields: []string{"title"},
			WritableFields: []string{
				"title",
				"body",
				"state",
				"labels",
				"assignees",
				"customer_request_id",
			},
		},
	}, nil
}

func (p *Provider) Pull(ctx context.Context, req core.PullRequest) (core.PullResult, error) {
	cfg, err := settingsFromConnection(req.Connection)
	if err != nil {
		return core.PullResult{}, validationError("%v", err)
	}
	cursor, err := decodeCursor(req.Cursor)
	if err != nil {
		return core.PullResult{}, validationError("%v", err)
	}
	hint := decodePullHint(req.InputMetadata)
	if hint.IssueNumber > 0 {
		return p.pullSingleIssue(ctx, cfg, cursor, hint)
	}
	rawURL, err := issuesURL(cfg, cursor)
	if err != nil {
		return core.PullResult{}, err
	}
	body, headers, err := p.request(ctx, cfg, http.MethodGet, rawURL, nil)
	if err != nil {
		return core.PullResult{}, err
	}
	issues := []apiIssue{}
	if err := json.Unmarshal(body, &issues); err != nil { // ptrext:allow unmarshal-out-param
		return core.PullResult{}, validationError("decode github issues response: %v", err)
	}
	records, maxUpdated, err := normalizeIssues(issues)
	if err != nil {
		return core.PullResult{}, validationError("%v", err)
	}
	children, err := p.commentChildrenForIssues(ctx, cfg, issues, hint)
	if err != nil {
		return core.PullResult{}, err
	}
	deliveryChildren, err := p.deliveryArtifactChildrenForIssues(ctx, cfg, issues)
	if err != nil {
		return core.PullResult{}, err
	}
	children = append(children, deliveryChildren...)
	next, err := nextCursor(cfg, cursor, maxUpdated, headers.Get("Link"))
	if err != nil {
		return core.PullResult{}, err
	}
	return core.PullResult{Records: records, Children: children, NextCursor: next}, nil
}

func (p *Provider) pullSingleIssue(ctx context.Context, cfg settings, cursor cursorState, hint pullHint) (core.PullResult, error) {
	issue, err := p.fetchIssue(ctx, cfg, hint.IssueNumber)
	if err != nil {
		return core.PullResult{}, err
	}
	records, _, err := normalizeIssues([]apiIssue{issue})
	if err != nil {
		return core.PullResult{}, validationError("%v", err)
	}
	children, err := p.commentChildrenForIssues(ctx, cfg, []apiIssue{issue}, hint)
	if err != nil {
		return core.PullResult{}, err
	}
	deliveryChildren, err := p.deliveryArtifactChildrenForIssues(ctx, cfg, []apiIssue{issue})
	if err != nil {
		return core.PullResult{}, err
	}
	children = append(children, deliveryChildren...)
	next, err := encodeCursor(cursor)
	if err != nil {
		return core.PullResult{}, err
	}
	return core.PullResult{Records: records, Children: children, NextCursor: next}, nil
}

func (p *Provider) fetchIssue(ctx context.Context, cfg settings, issueNumber int) (apiIssue, error) {
	rawURL, err := issueURL(cfg, issueNumber)
	if err != nil {
		return apiIssue{}, err
	}
	body, _, err := p.request(ctx, cfg, http.MethodGet, rawURL, nil)
	if err != nil {
		return apiIssue{}, err
	}
	issue, err := decodeIssueResponse(body)
	if err != nil {
		return apiIssue{}, validationError("%v", err)
	}
	return issue, nil
}

func (p *Provider) commentChildrenForIssues(ctx context.Context, cfg settings, issues []apiIssue, hint pullHint) ([]core.ExternalChildRecord, error) {
	children := []core.ExternalChildRecord{}
	for _, issue := range issues {
		if issue.PullRequest != nil || !shouldFetchComments(issue, hint) {
			continue
		}
		comments, err := p.fetchIssueComments(ctx, cfg, issue.Number)
		if err != nil {
			return nil, err
		}
		normalized, err := normalizeCommentChildren(strconv.Itoa(issue.Number), comments)
		if err != nil {
			return nil, validationError("%v", err)
		}
		normalized, err = appendDeletedCommentHint(strconv.Itoa(issue.Number), normalized, issue, hint)
		if err != nil {
			return nil, validationError("%v", err)
		}
		children = append(children, normalized...)
	}
	return children, nil
}

func (p *Provider) deliveryArtifactChildrenForIssues(ctx context.Context, cfg settings, issues []apiIssue) ([]core.ExternalChildRecord, error) {
	if !cfg.syncDeliveryArtifacts {
		return nil, nil
	}
	children := []core.ExternalChildRecord{}
	for _, issue := range issues {
		if !shouldFetchDeliveryArtifacts(issue) {
			continue
		}
		timeline, err := p.fetchIssueTimeline(ctx, cfg, issue.Number)
		if isOptionalTimelineUnavailable(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		normalized, err := normalizeTimelineDeliveryChildren(cfg, strconv.Itoa(issue.Number), timeline)
		if err != nil {
			return nil, validationError("%v", err)
		}
		children = append(children, normalized...)
	}
	return children, nil
}

func shouldFetchDeliveryArtifacts(issue apiIssue) bool {
	return issue.Number > 0 && issue.PullRequest == nil && extractCustomerRequestID(issue.Body) != ""
}

func appendDeletedCommentHint(parentKey string, children []core.ExternalChildRecord, issue apiIssue, hint pullHint) ([]core.ExternalChildRecord, error) {
	if hint.EventType != "issue_comment" || hint.Action != "deleted" ||
		hint.IssueNumber != issue.Number || hint.CommentID <= 0 {
		return children, nil
	}
	commentKey := strconv.FormatInt(hint.CommentID, 10)
	for _, child := range children {
		if child.Type == "comment" && child.Key == commentKey {
			return children, nil
		}
	}
	child, err := deletedCommentChild(parentKey, hint.CommentID)
	if err != nil {
		return nil, err
	}
	return append(children, child), nil
}

func shouldFetchComments(issue apiIssue, hint pullHint) bool {
	return issue.Comments > 0 || (hint.IssueNumber == issue.Number && hint.CommentID > 0)
}

func (p *Provider) fetchIssueComments(ctx context.Context, cfg settings, issueNumber int) ([]apiComment, error) {
	rawURL, err := issueCommentsURL(cfg, issueNumber)
	if err != nil {
		return nil, err
	}
	var out []apiComment
	for rawURL != "" {
		body, headers, err := p.request(ctx, cfg, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		comments, err := decodeCommentsResponse(body)
		if err != nil {
			return nil, validationError("%v", err)
		}
		out = append(out, comments...)
		next := parseNextLink(headers.Get("Link"))
		if next == "" {
			break
		}
		rawURL, err = validateNextURL(cfg, next)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (p *Provider) fetchIssueTimeline(ctx context.Context, cfg settings, issueNumber int) ([]apiTimelineEvent, error) {
	rawURL, err := issueTimelineURL(cfg, issueNumber)
	if err != nil {
		return nil, err
	}
	var out []apiTimelineEvent
	for rawURL != "" {
		body, headers, err := p.request(ctx, cfg, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		events, err := decodeTimelineResponse(body)
		if err != nil {
			return nil, validationError("%v", err)
		}
		out = append(out, events...)
		next := parseNextLink(headers.Get("Link"))
		if next == "" {
			break
		}
		rawURL, err = validateNextURL(cfg, next)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func isOptionalTimelineUnavailable(err error) bool {
	if err == nil {
		return false
	}
	providerErr := (*providerError)(nil)
	if !errors.As(err, &providerErr) { // ptrext:allow errors.As out-param
		return false
	}
	return providerErr.kind == "not_found"
}

func (p *Provider) Push(ctx context.Context, req core.PushRequest) (core.PushResult, error) {
	if len(req.Records) == 0 {
		return core.PushResult{}, nil
	}
	cfg, err := settingsFromConnection(req.Connection)
	if err != nil {
		return core.PushResult{}, validationError("%v", err)
	}
	results := make([]core.WriteResult, 0, len(req.Records))
	for _, record := range req.Records {
		result, err := p.pushOne(ctx, cfg, record)
		results = append(results, result)
		if err != nil {
			continue
		}
	}
	return core.PushResult{Results: results}, nil
}

func (p *Provider) ClassifyError(err error) core.SyncError {
	return classifyError(err)
}

func (p *Provider) pushOne(ctx context.Context, cfg settings, record core.LocalRecord) (core.WriteResult, error) {
	payload, err := decodeLocalPayload(record)
	if err != nil {
		return failedWrite(record.ID, "", err), err
	}
	issue, err := p.writeIssue(ctx, cfg, record, payload)
	if err != nil {
		return failedWrite(record.ID, payload.ExternalKey, err), err
	}
	key := payload.ExternalKey
	if issue.Number > 0 {
		key = strconv.Itoa(issue.Number)
	}
	if err := p.ensureManagedComment(ctx, cfg, record, payload, issue); err != nil {
		return failedWriteWithIssue(record.ID, key, issue, err), err
	}
	return core.WriteResult{LocalID: record.ID, Key: key, URL: issue.HTMLURL, Version: issueVersion(issue)}, nil
}

func (p *Provider) writeIssue(ctx context.Context, cfg settings, record core.LocalRecord, payload localIssuePayload) (apiIssue, error) {
	if payload.ExternalKey == "" {
		req, err := buildCreateRequest(cfg, record, payload)
		if err != nil {
			return apiIssue{}, err
		}
		return p.sendIssueWrite(ctx, cfg, http.MethodPost, []string{"issues"}, req)
	}

	issueNumber, normalizedKey, err := normalizeIssueWriteKey(cfg, payload.ExternalKey)
	if err != nil {
		return apiIssue{}, err
	}
	payload.ExternalKey = normalizedKey
	current, err := p.fetchIssue(ctx, cfg, issueNumber)
	if err != nil {
		return apiIssue{}, err
	}
	req, err := buildUpdateRequest(cfg, record, payload, current)
	if err != nil {
		return apiIssue{}, err
	}
	if req.empty() {
		return current, nil
	}
	return p.sendIssueWrite(ctx, cfg, http.MethodPatch, []string{"issues", payload.ExternalKey}, req)
}

func (p *Provider) sendIssueWrite(ctx context.Context, cfg settings, method string, parts []string, req issueWriteRequest) (apiIssue, error) {
	rawURL, err := repoAPIURL(cfg, parts...)
	if err != nil {
		return apiIssue{}, err
	}
	body, err := writeRequestPayload(req)
	if err != nil {
		return apiIssue{}, err
	}
	response, _, err := p.request(ctx, cfg, method, rawURL, body)
	if err != nil {
		return apiIssue{}, err
	}
	issue, err := decodeIssueResponse(response)
	if err != nil {
		return apiIssue{}, err
	}
	return issue, nil
}

func (p *Provider) ensureManagedComment(ctx context.Context, cfg settings, record core.LocalRecord, payload localIssuePayload, issue apiIssue) error {
	if !cfg.syncComments || issue.Number <= 0 {
		return nil
	}
	body, marker := managedCommentBody(record, payload)
	if body == "" || marker == "" {
		return nil
	}
	comments, err := p.fetchIssueComments(ctx, cfg, issue.Number)
	if err != nil {
		return err
	}
	existing := findManagedComment(comments, marker)
	if existing != nil {
		if bodyDigest(existing.Body) == bodyDigest(body) {
			return nil
		}
		return p.updateManagedComment(ctx, cfg, existing.ID, body)
	}
	return p.createManagedComment(ctx, cfg, issue.Number, body)
}

func (p *Provider) createManagedComment(ctx context.Context, cfg settings, issueNumber int, body string) error {
	rawURL, err := repoAPIURL(cfg, "issues", strconv.Itoa(issueNumber), "comments")
	if err != nil {
		return err
	}
	payload, err := writeCommentPayload(body)
	if err != nil {
		return err
	}
	response, _, err := p.request(ctx, cfg, http.MethodPost, rawURL, payload)
	if err != nil {
		return err
	}
	if _, err := decodeCommentResponse(response); err != nil {
		return err
	}
	return nil
}

func (p *Provider) updateManagedComment(ctx context.Context, cfg settings, commentID int64, body string) error {
	rawURL, err := issueCommentURL(cfg, commentID)
	if err != nil {
		return err
	}
	payload, err := writeCommentPayload(body)
	if err != nil {
		return err
	}
	response, _, err := p.request(ctx, cfg, http.MethodPatch, rawURL, payload)
	if err != nil {
		return err
	}
	if _, err := decodeCommentResponse(response); err != nil {
		return err
	}
	return nil
}

func failedWrite(localID, key string, err error) core.WriteResult {
	syncErr := classifyError(err)
	return core.WriteResult{
		LocalID:   localID,
		Key:       key,
		Retryable: syncErr.Retryable,
		Error:     ptrext.Of(syncErr),
	}
}

func failedWriteWithIssue(localID, key string, issue apiIssue, err error) core.WriteResult {
	result := failedWrite(localID, key, err)
	result.URL = issue.HTMLURL
	result.Version = issueVersion(issue)
	return result
}
