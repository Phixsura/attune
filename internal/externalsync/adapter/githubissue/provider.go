// SPDX-License-Identifier: Apache-2.0

// Package githubissue syncs Attune external object mappings with GitHub
// Issues through the external sync provider contract.
package githubissue

import (
	"context"
	"encoding/json"
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
	next, err := nextCursor(cfg, cursor, maxUpdated, headers.Get("Link"))
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
	rawURL, body, err := writeTarget(cfg, record, payload)
	if err != nil {
		return failedWrite(record.ID, payload.ExternalKey, err), err
	}
	response, _, err := p.request(ctx, cfg, issueMethod(payload), rawURL, body)
	if err != nil {
		return failedWrite(record.ID, payload.ExternalKey, err), err
	}
	issue, err := decodeIssueResponse(response)
	if err != nil {
		return failedWrite(record.ID, payload.ExternalKey, err), validationError("%v", err)
	}
	key := payload.ExternalKey
	if issue.Number > 0 {
		key = strconv.Itoa(issue.Number)
	}
	return core.WriteResult{LocalID: record.ID, Key: key, URL: issue.HTMLURL, Version: issueVersion(issue)}, nil
}

func writeTarget(cfg settings, record core.LocalRecord, payload localIssuePayload) (string, []byte, error) {
	var req issueWriteRequest
	var err error
	if payload.ExternalKey == "" {
		req, err = buildCreateRequest(record, payload)
	} else {
		req, err = buildUpdateRequest(record, payload)
	}
	if err != nil {
		return "", nil, err
	}
	parts := []string{"issues"}
	if payload.ExternalKey != "" {
		parts = append(parts, payload.ExternalKey)
	}
	rawURL, err := repoAPIURL(cfg, parts...)
	if err != nil {
		return "", nil, err
	}
	body, err := writeRequestPayload(req)
	if err != nil {
		return "", nil, err
	}
	return rawURL, body, nil
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
