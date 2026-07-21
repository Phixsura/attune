// SPDX-License-Identifier: Apache-2.0

// Package jiraissue syncs Attune external object mappings with Jira issues
// through the external sync provider contract.
package jiraissue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const providerID = "jira"

// Provider implements the Jira external sync adapter.
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

// NewProvider returns a Jira external sync provider.
func NewProvider(opts ...Option) *Provider {
	p := ptrext.Of(Provider{client: core.NewHTTPClient(15 * time.Second)})
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func init() {
	core.Register(providerID, "Jira", func() core.Provider { return NewProvider() })
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
	rawURL, err := repoAPIURL(cfg, "myself")
	if err != nil {
		return core.CheckResult{OK: false, Error: err.Error(), Latency: time.Since(start)}, nil
	}
	_, headers, err := p.request(ctx, cfg, http.MethodGet, rawURL, nil)
	result := core.CheckResult{
		OK:        err == nil,
		Latency:   time.Since(start),
		RequestID: firstHeader(headers, "X-Request-Id", "X-Atlassian-Request-Id", "Atlassian-Request-Id", "X-Arequestid"),
	}
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
				"key",
				"summary",
				"description",
				"status",
				"status_category",
				"labels",
				"assignee",
				"reporter",
				"project_key",
				"issue_type",
				"url",
				"created_at",
				"updated_at",
				"resolved_at",
				"comment_count",
				"comments",
				"issue_links",
				"request_marker",
			},
			RequiredFields: []string{"summary"},
			WritableFields: []string{
				"summary",
				"description",
				"labels",
				"status",
				"request_marker",
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
	rawURL, err := searchURL(cfg, cursor)
	if err != nil {
		return core.PullResult{}, err
	}
	body, _, err := p.request(ctx, cfg, http.MethodGet, rawURL, nil)
	if err != nil {
		return core.PullResult{}, err
	}
	result, err := decodeSearchResponse(body)
	if err != nil {
		return core.PullResult{}, validationError("%v", err)
	}
	records, maxUpdated, err := normalizeIssues(cfg, result.Issues)
	if err != nil {
		return core.PullResult{}, validationError("%v", err)
	}
	next, err := nextCursor(cursor, result, maxUpdated)
	if err != nil {
		return core.PullResult{}, err
	}
	return core.PullResult{Records: records, NextCursor: next}, nil
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

	issue, err := p.resolveWriteIssue(ctx, cfg, payload)
	if err != nil {
		return failedWrite(record.ID, payload.ExternalKey, err), err
	}

	if issue == nil {
		issue, err = p.createIssue(ctx, cfg, record, payload)
		if err != nil {
			return failedWrite(record.ID, payload.ExternalKey, err), err
		}
	}

	currentIssue := ptrext.Indirect(issue)
	if err := p.updateIssue(ctx, cfg, currentIssue, record, payload); err != nil {
		return failedWrite(record.ID, currentIssue.Key, err), err
	}
	if err := p.ensureRequestComment(ctx, cfg, currentIssue, payload); err != nil {
		return failedWrite(record.ID, currentIssue.Key, err), err
	}
	if err := p.ensureRequestedStatus(ctx, cfg, currentIssue, payload.Status); err != nil {
		return failedWrite(record.ID, currentIssue.Key, err), err
	}

	fresh, err := p.getIssue(ctx, cfg, issue.Key)
	if err != nil {
		return failedWrite(record.ID, issue.Key, err), err
	}
	return core.WriteResult{
		LocalID: record.ID,
		Key:     fresh.Key,
		URL:     issueURL(cfg, fresh.Key),
		Version: issueVersion(ptrext.Indirect(fresh)),
	}, nil
}

func (p *Provider) resolveWriteIssue(ctx context.Context, cfg settings, payload localIssuePayload) (*jiraIssue, error) {
	if payload.ExternalKey != "" {
		issue, err := p.getIssue(ctx, cfg, payload.ExternalKey)
		if err != nil {
			if isNotFound(err) && payload.CustomerRequestID != "" {
				return p.findIssueByRequestMarker(ctx, cfg, payload.CustomerRequestID)
			}
			return nil, err
		}
		return issue, nil
	}
	if payload.CustomerRequestID == "" {
		return nil, nil
	}
	return p.findIssueByRequestMarker(ctx, cfg, payload.CustomerRequestID)
}

func (p *Provider) createIssue(ctx context.Context, cfg settings, record core.LocalRecord, payload localIssuePayload) (*jiraIssue, error) {
	req, err := buildCreateRequest(cfg, record, payload)
	if err != nil {
		return nil, err
	}
	body, err := writeRequestPayload(req)
	if err != nil {
		return nil, err
	}
	rawURL, err := repoAPIURL(cfg, "issue")
	if err != nil {
		return nil, err
	}
	respBody, _, err := p.request(ctx, cfg, http.MethodPost, rawURL, body)
	if err != nil {
		return nil, err
	}
	issue, err := decodeIssueResponse(respBody)
	if err != nil {
		return nil, validationError("%v", err)
	}
	return ptrext.Of(issue), nil
}

func (p *Provider) getIssue(ctx context.Context, cfg settings, issueKey string) (*jiraIssue, error) {
	rawURL, err := repoAPIURL(cfg, "issue", strings.TrimSpace(issueKey))
	if err != nil {
		return nil, err
	}
	u, err := parseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse jira issue url: %w", err)
	}
	q := u.Query()
	q.Set("fields", strings.Join(searchFields(), ","))
	u.RawQuery = q.Encode()
	body, _, err := p.request(ctx, cfg, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	issue, err := decodeIssueResponse(body)
	if err != nil {
		return nil, validationError("%v", err)
	}
	return ptrext.Of(issue), nil
}

func (p *Provider) findIssueByRequestMarker(ctx context.Context, cfg settings, customerRequestID string) (*jiraIssue, error) {
	customerRequestID = strings.TrimSpace(customerRequestID)
	if customerRequestID == "" {
		return nil, nil
	}
	for _, jql := range []string{
		buildMarkerSearchJQL(cfg, customerRequestID, true),
		buildMarkerSearchJQL(cfg, customerRequestID, false),
	} {
		result, err := p.searchIssues(ctx, cfg, jql, 0)
		if err != nil {
			return nil, err
		}
		for i := range result.Issues {
			issue := result.Issues[i]
			if issueHasMarker(cfg, issue, customerRequestID) {
				return ptrext.Of(issue), nil
			}
		}
	}
	return nil, nil
}

func (p *Provider) searchIssues(ctx context.Context, cfg settings, jql string, startAt int) (jiraSearchResponse, error) {
	rawURL, err := buildSearchURL(cfg, jql, startAt)
	if err != nil {
		return jiraSearchResponse{}, err
	}
	body, _, err := p.request(ctx, cfg, http.MethodGet, rawURL, nil)
	if err != nil {
		return jiraSearchResponse{}, err
	}
	return decodeSearchResponse(body)
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

func (p *Provider) updateIssue(ctx context.Context, cfg settings, issue jiraIssue, record core.LocalRecord, payload localIssuePayload) error {
	req, err := buildUpdateRequest(cfg, record, payload)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	body, err := writeRequestPayload(req)
	if err != nil {
		return err
	}
	rawURL, err := repoAPIURL(cfg, "issue", issue.Key)
	if err != nil {
		return err
	}
	_, _, err = p.request(ctx, cfg, http.MethodPut, rawURL, body)
	return err
}

func (p *Provider) ensureRequestComment(ctx context.Context, cfg settings, issue jiraIssue, payload localIssuePayload) error {
	marker := requestMarker(payload.CustomerRequestID)
	if marker == "" {
		return nil
	}
	if issueHasMarker(cfg, issue, marker) {
		return nil
	}
	comment := buildRequestComment(payload)
	if comment == "" {
		return nil
	}
	req := map[string]any{"body": adfDocument(comment)}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal jira comment payload: %w", err)
	}
	rawURL, err := repoAPIURL(cfg, "issue", issue.Key, "comment")
	if err != nil {
		return err
	}
	_, _, err = p.request(ctx, cfg, http.MethodPost, rawURL, body)
	return err
}

func (p *Provider) ensureRequestedStatus(ctx context.Context, cfg settings, issue jiraIssue, localStatus string) error {
	target := strings.TrimSpace(strings.ToLower(localStatus))
	if target == "" {
		return nil
	}
	if statusCategoryMatches(issue.Fields.Status.StatusCategory.Key, target) {
		return nil
	}
	transition, err := p.chooseTransition(ctx, cfg, issue.Key, target)
	if err != nil {
		return err
	}
	if transition == nil {
		if canSkipTransition(target) {
			return nil
		}
		return validationError("jira workflow does not expose a transition for status %q", localStatus)
	}
	if transition.ID == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"transition": map[string]any{"id": transition.ID},
	})
	if err != nil {
		return fmt.Errorf("marshal jira transition payload: %w", err)
	}
	rawURL, err := repoAPIURL(cfg, "issue", issue.Key, "transitions")
	if err != nil {
		return err
	}
	_, _, err = p.request(ctx, cfg, http.MethodPost, rawURL, body)
	return err
}

func (p *Provider) chooseTransition(ctx context.Context, cfg settings, issueKey, localStatus string) (*jiraTransition, error) {
	if cfg.statusTransitions != nil {
		if raw, ok := cfg.statusTransitions[localStatus]; ok {
			raw = strings.TrimSpace(raw)
			if raw != "" {
				transitions, err := p.listTransitions(ctx, cfg, issueKey)
				if err != nil {
					return nil, err
				}
				if transition := findTransition(transitions, raw); transition != nil {
					return transition, nil
				}
			}
		}
	}
	transitions, err := p.listTransitions(ctx, cfg, issueKey)
	if err != nil {
		return nil, err
	}
	return chooseHeuristicTransition(transitions, localStatus), nil
}

func (p *Provider) listTransitions(ctx context.Context, cfg settings, issueKey string) ([]jiraTransition, error) {
	rawURL, err := repoAPIURL(cfg, "issue", issueKey, "transitions")
	if err != nil {
		return nil, err
	}
	u, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("expand", "transitions.fields")
	u.RawQuery = q.Encode()
	body, _, err := p.request(ctx, cfg, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp := struct {
		Transitions []jiraTransition `json:"transitions"`
	}{}
	if err := json.Unmarshal(body, &resp); err != nil { // ptrext:allow unmarshal-out-param
		return nil, fmt.Errorf("decode jira transitions response: %w", err)
	}
	return resp.Transitions, nil
}
