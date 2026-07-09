// SPDX-License-Identifier: Apache-2.0

package githubissue

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	core "github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

const defaultAPIBase = "https://api.github.com"

type providerConfig struct {
	RepoURL    string `json:"repo_url,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Repo       string `json:"repo,omitempty"`
	APIBaseURL string `json:"api_base_url,omitempty"`
}

type settings struct {
	owner   string
	repo    string
	apiBase string
	token   string
}

type cursorState struct {
	UpdatedSince string `json:"updated_since,omitempty"`
	NextURL      string `json:"next_url,omitempty"`
}

func settingsFromConnection(conn core.Connection) (settings, error) {
	cfg, err := decodeProviderConfig(conn.ProviderConfig)
	if err != nil {
		return settings{}, err
	}
	owner, repo, err := resolveRepo(cfg)
	if err != nil {
		return settings{}, err
	}
	apiBase, err := resolveAPIBase(conn.BaseURL, cfg.APIBaseURL)
	if err != nil {
		return settings{}, err
	}
	token := strings.TrimSpace(string(conn.Credential))
	if token == "" {
		return settings{}, fmt.Errorf("github credential is required")
	}
	return settings{owner: owner, repo: repo, apiBase: apiBase, token: token}, nil
}

func decodeProviderConfig(raw []byte) (providerConfig, error) {
	if len(raw) == 0 {
		return providerConfig{}, nil
	}
	cfg := providerConfig{}
	if err := json.Unmarshal(raw, &cfg); err != nil { // ptrext:allow unmarshal-out-param
		return providerConfig{}, fmt.Errorf("decode github provider_config: %w", err)
	}
	cfg.RepoURL = strings.TrimSpace(cfg.RepoURL)
	cfg.Owner = strings.TrimSpace(cfg.Owner)
	cfg.Repo = strings.TrimSpace(cfg.Repo)
	cfg.APIBaseURL = strings.TrimSpace(cfg.APIBaseURL)
	return cfg, nil
}

func resolveRepo(cfg providerConfig) (string, string, error) {
	if cfg.RepoURL != "" {
		return parseRepoURL(cfg.RepoURL)
	}
	if cfg.Owner == "" || cfg.Repo == "" {
		return "", "", fmt.Errorf("github provider_config requires repo_url or owner and repo")
	}
	if strings.Contains(cfg.Owner, "/") || strings.Contains(cfg.Repo, "/") {
		return "", "", fmt.Errorf("github owner and repo must not contain slashes")
	}
	return cfg.Owner, strings.TrimSuffix(cfg.Repo, ".git"), nil
}

func parseRepoURL(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse github repo_url: %w", err)
	}
	if u.Scheme != "https" && !isLoopbackHTTP(u) {
		return "", "", fmt.Errorf("github repo_url must use https")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("github repo_url must include owner and repo path")
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

func isLoopbackHTTP(u *url.URL) bool {
	return u.Scheme == "http" && nethardening.IsLoopbackHost(u.Hostname())
}

func resolveAPIBase(connBaseURL, configBaseURL string) (string, error) {
	raw := strings.TrimSpace(configBaseURL)
	if raw == "" {
		raw = strings.TrimSpace(connBaseURL)
	}
	if raw == "" {
		raw = defaultAPIBase
	}
	raw = strings.TrimRight(raw, "/")
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse github api_base_url: %w", err)
	}
	if u.Scheme != "https" && !isLoopbackHTTP(u) {
		return "", fmt.Errorf("github api_base_url must use https")
	}
	if err := core.ValidateProviderURL(raw); err != nil {
		return "", err
	}
	return raw, nil
}

func decodeCursor(raw []byte) (cursorState, error) {
	if len(raw) == 0 {
		return cursorState{}, nil
	}
	cursor := cursorState{}
	if err := json.Unmarshal(raw, &cursor); err != nil { // ptrext:allow unmarshal-out-param
		return cursorState{}, fmt.Errorf("decode github cursor: %w", err)
	}
	cursor.UpdatedSince = strings.TrimSpace(cursor.UpdatedSince)
	cursor.NextURL = strings.TrimSpace(cursor.NextURL)
	if cursor.UpdatedSince != "" {
		if _, err := time.Parse(time.RFC3339, cursor.UpdatedSince); err != nil {
			return cursorState{}, fmt.Errorf("github cursor updated_since must be RFC3339: %w", err)
		}
	}
	return cursor, nil
}

func encodeCursor(cursor cursorState) ([]byte, error) {
	out, err := json.Marshal(cursor)
	if err != nil {
		return nil, fmt.Errorf("encode github cursor: %w", err)
	}
	return out, nil
}
