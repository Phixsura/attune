// SPDX-License-Identifier: Apache-2.0

package jiraissue

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	core "github.com/Phixsura/attune/internal/externalsync"
)

const (
	defaultAPIPath        = "/rest/api/3"
	defaultLabelPrefix    = "attune-customer-request-"
	jiraMarkerCommentText = "attune:customer_request_id="
)

type providerConfig struct {
	SiteURL            string            `json:"site_url,omitempty"`
	APIBaseURL         string            `json:"api_base_url,omitempty"`
	ProjectKey         string            `json:"project_key,omitempty"`
	IssueType          string            `json:"issue_type,omitempty"`
	IssueTypeID        string            `json:"issue_type_id,omitempty"`
	Email              string            `json:"email,omitempty"`
	RequestLabelPrefix string            `json:"request_label_prefix,omitempty"`
	StatusTransitions  map[string]string `json:"status_transitions,omitempty"`
}

type settings struct {
	siteURL            string
	apiBase            string
	projectKey         string
	issueType          string
	issueTypeID        string
	email              string
	token              string
	requestLabelPrefix string
	statusTransitions  map[string]string
}

type cursorState struct {
	UpdatedSince string `json:"updated_since,omitempty"`
	StartAt      int    `json:"start_at,omitempty"`
}

func settingsFromConnection(conn core.Connection) (settings, error) {
	cfg, err := decodeProviderConfig(conn.ProviderConfig)
	if err != nil {
		return settings{}, err
	}
	projectKey := strings.TrimSpace(cfg.ProjectKey)
	if projectKey == "" || strings.Contains(projectKey, "/") {
		return settings{}, fmt.Errorf("jira provider_config requires project_key")
	}
	issueType := strings.TrimSpace(cfg.IssueType)
	issueTypeID := strings.TrimSpace(cfg.IssueTypeID)
	if issueType == "" && issueTypeID == "" {
		return settings{}, fmt.Errorf("jira provider_config requires issue_type or issue_type_id")
	}
	apiBase, siteURL, err := resolveBases(conn.BaseURL, cfg.SiteURL, cfg.APIBaseURL)
	if err != nil {
		return settings{}, err
	}
	email := strings.TrimSpace(cfg.Email)
	if email == "" {
		return settings{}, fmt.Errorf("jira provider_config requires email")
	}
	token := strings.TrimSpace(string(conn.Credential))
	if token == "" {
		return settings{}, fmt.Errorf("jira credential is required")
	}
	labelPrefix := strings.TrimSpace(cfg.RequestLabelPrefix)
	if labelPrefix == "" {
		labelPrefix = defaultLabelPrefix
	}
	if !strings.HasSuffix(labelPrefix, "-") {
		labelPrefix += "-"
	}
	transitions := map[string]string{}
	for k, v := range cfg.StatusTransitions {
		key := strings.TrimSpace(strings.ToLower(k))
		val := strings.TrimSpace(v)
		if key != "" && val != "" {
			transitions[key] = val
		}
	}
	return settings{
		siteURL:            siteURL,
		apiBase:            apiBase,
		projectKey:         projectKey,
		issueType:          issueType,
		issueTypeID:        issueTypeID,
		email:              email,
		token:              token,
		requestLabelPrefix: labelPrefix,
		statusTransitions:  transitions,
	}, nil
}

func decodeProviderConfig(raw []byte) (providerConfig, error) {
	if len(raw) == 0 {
		return providerConfig{}, nil
	}
	cfg := providerConfig{}
	if err := json.Unmarshal(raw, &cfg); err != nil { // ptrext:allow unmarshal-out-param
		return providerConfig{}, fmt.Errorf("decode jira provider_config: %w", err)
	}
	cfg.SiteURL = strings.TrimSpace(cfg.SiteURL)
	cfg.APIBaseURL = strings.TrimSpace(cfg.APIBaseURL)
	cfg.ProjectKey = strings.TrimSpace(cfg.ProjectKey)
	cfg.IssueType = strings.TrimSpace(cfg.IssueType)
	cfg.IssueTypeID = strings.TrimSpace(cfg.IssueTypeID)
	cfg.Email = strings.TrimSpace(cfg.Email)
	cfg.RequestLabelPrefix = strings.TrimSpace(cfg.RequestLabelPrefix)
	if len(cfg.StatusTransitions) == 0 {
		cfg.StatusTransitions = nil
	}
	return cfg, nil
}

func resolveBases(connBaseURL, siteURL, apiBaseURL string) (string, string, error) {
	rawAPI := strings.TrimSpace(apiBaseURL)
	if rawAPI == "" {
		rawSite := strings.TrimSpace(siteURL)
		if rawSite == "" {
			rawSite = strings.TrimSpace(connBaseURL)
		}
		if rawSite == "" {
			return "", "", fmt.Errorf("jira provider_config requires site_url or connection base_url")
		}
		site, api, err := normalizeJiraBases(rawSite, "")
		if err != nil {
			return "", "", err
		}
		return api, site, nil
	}
	site, api, err := normalizeJiraBases(siteURL, rawAPI)
	if err != nil {
		return "", "", err
	}
	return api, site, nil
}

func normalizeJiraBases(siteURL, apiBaseURL string) (string, string, error) {
	rawSite := strings.TrimSpace(siteURL)
	rawAPI := strings.TrimSpace(apiBaseURL)
	if rawAPI == "" {
		if rawSite == "" {
			return "", "", fmt.Errorf("jira provider_config requires api_base_url or site_url")
		}
		trimmed := strings.TrimRight(rawSite, "/")
		if err := core.ValidateProviderURL(trimmed); err != nil {
			return "", "", err
		}
		apiBase := trimmed + defaultAPIPath
		if err := core.ValidateProviderURL(apiBase); err != nil {
			return "", "", err
		}
		return trimmed, apiBase, nil
	}
	apiBase := strings.TrimRight(rawAPI, "/")
	if err := core.ValidateProviderURL(apiBase); err != nil {
		return "", "", err
	}
	if rawSite == "" {
		site := strings.TrimSuffix(apiBase, defaultAPIPath)
		site = strings.TrimRight(site, "/")
		if site == "" {
			site = apiBase
		}
		return site, apiBase, nil
	}
	site := strings.TrimRight(rawSite, "/")
	if err := core.ValidateProviderURL(site); err != nil {
		return "", "", err
	}
	return site, apiBase, nil
}

func repoAPIURL(cfg settings, parts ...string) (string, error) {
	joined := append([]string{strings.TrimRight(cfg.apiBase, "/")}, parts...)
	raw, err := url.JoinPath(joined[0], joined[1:]...)
	if err != nil {
		return "", fmt.Errorf("build jira api url: %w", err)
	}
	return raw, nil
}

func issueURL(cfg settings, key string) string {
	base := strings.TrimRight(cfg.siteURL, "/")
	if base == "" {
		base = strings.TrimSuffix(strings.TrimRight(cfg.apiBase, "/"), defaultAPIPath)
		base = strings.TrimRight(base, "/")
	}
	if base == "" {
		return ""
	}
	return base + "/browse/" + strings.TrimSpace(key)
}

func searchURL(cfg settings, cursor cursorState) (string, error) {
	raw, err := repoAPIURL(cfg, "search")
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse jira search url: %w", err)
	}
	q := u.Query()
	q.Set("jql", buildSearchJQL(cfg, cursor))
	q.Set("fields", strings.Join(searchFields(), ","))
	q.Set("maxResults", "100")
	if cursor.StartAt > 0 {
		q.Set("startAt", fmt.Sprintf("%d", cursor.StartAt))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func buildSearchJQL(cfg settings, cursor cursorState) string {
	parts := []string{`project = "` + escapeJQL(cfg.projectKey) + `"`}
	if since := querySince(cursor); !since.IsZero() {
		parts = append(parts, `updated >= "`+since.Format("2006-01-02 15:04")+`"`)
	}
	parts = append(parts, `ORDER BY updated ASC, id ASC`)
	return strings.Join(parts, " AND ")
}

func querySince(cursor cursorState) time.Time {
	if strings.TrimSpace(cursor.UpdatedSince) == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, cursor.UpdatedSince)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC().Add(-1 * time.Minute)
}

func decodeCursor(raw []byte) (cursorState, error) {
	if len(raw) == 0 {
		return cursorState{}, nil
	}
	cursor := cursorState{}
	if err := json.Unmarshal(raw, &cursor); err != nil { // ptrext:allow unmarshal-out-param
		return cursorState{}, fmt.Errorf("decode jira cursor: %w", err)
	}
	cursor.UpdatedSince = strings.TrimSpace(cursor.UpdatedSince)
	if cursor.StartAt < 0 {
		return cursorState{}, fmt.Errorf("jira cursor start_at must be non-negative")
	}
	if cursor.UpdatedSince != "" {
		if _, err := time.Parse(time.RFC3339Nano, cursor.UpdatedSince); err != nil {
			return cursorState{}, fmt.Errorf("jira cursor updated_since must be RFC3339: %w", err)
		}
	}
	return cursor, nil
}

func encodeCursor(cursor cursorState) ([]byte, error) {
	out, err := json.Marshal(cursor)
	if err != nil {
		return nil, fmt.Errorf("encode jira cursor: %w", err)
	}
	return out, nil
}

func nextCursor(previous cursorState, result jiraSearchResponse, maxUpdated time.Time) ([]byte, error) {
	next := previous
	if result.hasNextPage() {
		next.StartAt = previous.StartAt + len(result.Issues)
		return encodeCursor(next)
	}
	next.StartAt = 0
	if !maxUpdated.IsZero() {
		next.UpdatedSince = maxUpdated.UTC().Format(time.RFC3339Nano)
	}
	return encodeCursor(next)
}

func searchFields() []string {
	return []string{
		"summary",
		"description",
		"status",
		"labels",
		"assignee",
		"reporter",
		"updated",
		"created",
		"resolution",
		"resolutiondate",
		"project",
		"issuetype",
		"comment",
		"issuelinks",
	}
}

func escapeJQL(raw string) string {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), `\`, `\\`)
	raw = strings.ReplaceAll(raw, `"`, `\"`)
	return raw
}
