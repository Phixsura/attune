// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

type GitHubIssueLinkTargetInput struct {
	TenantID     string
	ConnectionID uuid.UUID
	MappingID    *uuid.UUID
	IssueNumber  string
}

type GitHubIssueLinkTarget struct {
	MappingID       uuid.UUID
	ExternalSyncKey string
	ExternalKey     string
	ExternalURL     string
	Title           string
	Status          string
}

type ManagedGitHubIssueLinkInput struct {
	TenantID    string
	RequestID   uuid.UUID
	Provider    string
	ExternalKey string
	ExternalURL string
	MappingID   *uuid.UUID
}

type ManagedGitHubIssueLinkBinding struct {
	ExternalObjectLinkID uuid.UUID
	ConnectionID         uuid.UUID
	MappingID            uuid.UUID
	ExternalKey          string
}

type ManagedIssueSyncTarget struct {
	ConnectionID uuid.UUID
	MappingID    uuid.UUID
	ExternalKey  string
}

type githubIssueRef struct {
	host        string
	owner       string
	repo        string
	issueNumber string
}

type githubExternalConnectionConfig struct {
	RepoURL    string `json:"repo_url,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Repo       string `json:"repo,omitempty"`
	APIBaseURL string `json:"api_base_url,omitempty"`
}

type githubRepoTarget struct {
	scheme string
	host   string
	owner  string
	repo   string
}

func (r *Repo) ResolveGitHubIssueLinkTarget(ctx context.Context, in GitHubIssueLinkTargetInput) (*GitHubIssueLinkTarget, error) {
	issueNumber, ok := normalizeGitHubIssueNumber(in.IssueNumber)
	if in.TenantID == "" || in.ConnectionID == uuid.Nil || !ok {
		return nil, ErrInvalidInput
	}
	var mappingFilter any
	if in.MappingID != nil {
		mappingFilter = ptrext.Indirect(in.MappingID)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, c.provider_config, c.base_url
		  FROM external_connections c
		  JOIN external_object_mappings m
		    ON m.tenant_id = c.tenant_id
		   AND m.connection_id = c.id
		 WHERE c.tenant_id = $1
		   AND c.id = $2
		   AND c.provider = 'github'
		   AND c.enabled
		   AND c.status = 'active'
		   AND c.deleted_at IS NULL
		   AND m.enabled
		   AND m.local_object_type = 'customer_request'
		   AND m.external_object_type = 'issue'
		   AND m.direction IN ('pull', 'bidirectional')
		   AND ($3::uuid IS NULL OR m.id = $3)
		 ORDER BY m.created_at ASC, m.id ASC
		 LIMIT 2`,
		in.TenantID, in.ConnectionID, mappingFilter)
	if err != nil {
		return nil, fmt.Errorf("resolve github issue link target: %w", err)
	}
	defer rows.Close()
	type match struct {
		mappingID      uuid.UUID
		providerConfig []byte
		baseURL        string
	}
	matches := []match{}
	for rows.Next() {
		var item match
		if err := rows.Scan(&item.mappingID, &item.providerConfig, &item.baseURL); err != nil {
			return nil, err
		}
		matches = append(matches, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve github issue link target rows: %w", err)
	}
	if len(matches) != 1 {
		return nil, ErrConflict
	}
	target, ok := githubRepoTargetFromRaw(matches[0].providerConfig, matches[0].baseURL)
	if !ok {
		return nil, ErrInvalidInput
	}
	externalURL, err := target.issueURL(issueNumber)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(GitHubIssueLinkTarget{
		MappingID:       matches[0].mappingID,
		ExternalSyncKey: issueNumber,
		ExternalKey:     target.owner + "/" + target.repo + "#" + issueNumber,
		ExternalURL:     externalURL,
		Title:           target.owner + "/" + target.repo + "#" + issueNumber,
		Status:          "open",
	}), nil
}

func (r *Repo) BindManagedGitHubIssueLinkTx(ctx context.Context, tx pgx.Tx, in ManagedGitHubIssueLinkInput) (*ManagedGitHubIssueLinkBinding, error) {
	ref, ok := parseGitHubIssueRef(in.Provider, in.ExternalURL, in.ExternalKey)
	if !ok {
		return nil, nil
	}
	mappingID, ok, err := findMatchingGitHubIssueMapping(ctx, tx, in.TenantID, ref, in.MappingID)
	if err != nil || !ok {
		return nil, err
	}
	externalObjectLinkID, ok, err := ensureGitHubExternalIssueLink(ctx, tx, in, mappingID, ref)
	if err != nil || !ok {
		return nil, err
	}
	target, err := managedIssueSyncTargetTx(ctx, tx, in.TenantID, in.RequestID, externalObjectLinkID)
	if err != nil || target == nil {
		return nil, err
	}
	return ptrext.Of(ManagedGitHubIssueLinkBinding{
		ExternalObjectLinkID: externalObjectLinkID,
		ConnectionID:         target.ConnectionID,
		MappingID:            target.MappingID,
		ExternalKey:          target.ExternalKey,
	}), nil
}

func managedIssueSyncTargetTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	externalObjectLinkID uuid.UUID,
) (*ManagedIssueSyncTarget, error) {
	var target ManagedIssueSyncTarget
	err := tx.QueryRow(ctx, `
		SELECT m.connection_id, eol.mapping_id, eol.external_key
		  FROM external_object_links eol
		  JOIN external_object_mappings m
		    ON m.tenant_id = eol.tenant_id
		   AND m.id = eol.mapping_id
		  JOIN external_connections c
		    ON c.tenant_id = m.tenant_id
		   AND c.id = m.connection_id
		 WHERE eol.tenant_id = $1
		   AND eol.id = $3
		   AND eol.local_object_type = 'customer_request'
		   AND eol.local_object_id = $2::text
		   AND eol.external_object_type = 'issue'
		   AND eol.external_deleted_at IS NULL
		   AND eol.local_deleted_at IS NULL
		   AND m.local_object_type = 'customer_request'
		   AND m.external_object_type = 'issue'
		   AND m.direction IN ('pull', 'bidirectional')
		   AND m.enabled
		   AND c.provider = 'github'
		   AND c.enabled
		   AND c.status = 'active'
		   AND c.deleted_at IS NULL`,
		tenantID, requestID, externalObjectLinkID).Scan(&target.ConnectionID, &target.MappingID, &target.ExternalKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load managed issue sync target: %w", err)
	}
	return ptrext.Of(target), nil
}

func (r *Repo) TombstoneLocalIssueExternalLinkTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, externalObjectLinkID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE external_object_links
		   SET local_deleted_at = COALESCE(local_deleted_at, NOW()),
		       sync_state = 'deleted',
		       sync_error = '',
		       tombstone_reason = 'local_unlinked',
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND local_object_id = $2::text
		   AND id = $3
		   AND local_object_type = 'customer_request'
		   AND external_object_type = 'issue'
		   AND local_deleted_at IS NULL`,
		tenantID, requestID, externalObjectLinkID)
	if err != nil {
		return fmt.Errorf("tombstone local external issue link: %w", err)
	}
	return nil
}

func parseGitHubIssueRef(provider, externalURL, externalKey string) (githubIssueRef, bool) {
	if strings.TrimSpace(provider) != "github" {
		return githubIssueRef{}, false
	}
	if ref, ok := parseGitHubIssueURL(externalURL); ok {
		return ref, true
	}
	return parseGitHubIssueKey(externalKey)
}

func parseGitHubIssueURL(raw string) (githubIssueRef, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Path == "" {
		return githubIssueRef{}, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "issues" {
		return githubIssueRef{}, false
	}
	ref, ok := normalizedGitHubIssueRef(parts[0], parts[1], parts[3])
	ref.host = strings.ToLower(u.Hostname())
	return ref, ok
}

func parseGitHubIssueKey(raw string) (githubIssueRef, bool) {
	key := strings.TrimSpace(raw)
	issueIndex := strings.LastIndex(key, "#")
	if issueIndex <= 0 || issueIndex == len(key)-1 {
		return githubIssueRef{}, false
	}
	repoParts := strings.Split(key[:issueIndex], "/")
	if len(repoParts) != 2 {
		return githubIssueRef{}, false
	}
	return normalizedGitHubIssueRef(repoParts[0], repoParts[1], key[issueIndex+1:])
}

func normalizedGitHubIssueRef(owner, repo, issueNumber string) (githubIssueRef, bool) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSuffix(strings.TrimSpace(repo), ".git")
	issueNumber = strings.TrimSpace(issueNumber)
	parsed, err := strconv.ParseInt(issueNumber, 10, 64)
	if owner == "" || repo == "" || err != nil || parsed <= 0 {
		return githubIssueRef{}, false
	}
	return githubIssueRef{owner: owner, repo: repo, issueNumber: strconv.FormatInt(parsed, 10)}, true
}

func normalizeGitHubIssueNumber(raw string) (string, bool) {
	issueNumber := strings.TrimSpace(raw)
	parsed, err := strconv.ParseInt(issueNumber, 10, 64)
	if err != nil || parsed <= 0 {
		return "", false
	}
	return strconv.FormatInt(parsed, 10), true
}

func findMatchingGitHubIssueMapping(ctx context.Context, tx pgx.Tx, tenantID string, ref githubIssueRef, mappingID *uuid.UUID) (uuid.UUID, bool, error) {
	var mappingFilter any
	if mappingID != nil {
		mappingFilter = ptrext.Indirect(mappingID)
	}
	rows, err := tx.Query(ctx, `
		SELECT m.id, c.provider_config, c.base_url
		  FROM external_connections c
		  JOIN external_object_mappings m
		    ON m.tenant_id = c.tenant_id
		   AND m.connection_id = c.id
		 WHERE c.tenant_id = $1
		   AND c.provider = 'github'
		   AND c.enabled
		   AND c.status = 'active'
		   AND c.deleted_at IS NULL
		   AND m.enabled
		   AND m.local_object_type = 'customer_request'
		   AND m.external_object_type = 'issue'
		   AND m.direction IN ('pull', 'bidirectional')
		   AND ($2::uuid IS NULL OR m.id = $2)
		 ORDER BY c.created_at ASC, m.created_at ASC`,
		tenantID, mappingFilter)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("find github issue sync mapping: %w", err)
	}
	defer rows.Close()
	var matchedID uuid.UUID
	matched := false
	for rows.Next() {
		var mappingID uuid.UUID
		var providerConfig []byte
		var baseURL string
		if err := rows.Scan(&mappingID, &providerConfig, &baseURL); err != nil {
			return uuid.Nil, false, err
		}
		if githubConnectionMatchesIssueRef(providerConfig, baseURL, ref) {
			if matched {
				return uuid.Nil, false, ErrConflict
			}
			matchedID = mappingID
			matched = true
		}
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, false, fmt.Errorf("find github issue sync mapping rows: %w", err)
	}
	return matchedID, matched, nil
}

func githubConnectionMatchesIssueRef(raw []byte, baseURL string, ref githubIssueRef) bool {
	target, ok := githubRepoTargetFromRaw(raw, baseURL)
	if !ok || !strings.EqualFold(target.owner, ref.owner) || !strings.EqualFold(target.repo, ref.repo) {
		return false
	}
	return ref.host == "" || githubHostsMatch(target.host, ref.host)
}

func githubRepoTargetFromRaw(raw []byte, connectionBaseURL string) (githubRepoTarget, bool) {
	cfg := githubExternalConnectionConfig{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil { // ptrext:allow unmarshal-out-param
			return githubRepoTarget{}, false
		}
	}
	return githubRepoTargetFromConfig(cfg, connectionBaseURL)
}

func githubRepoTargetFromConfig(cfg githubExternalConnectionConfig, connectionBaseURL string) (githubRepoTarget, bool) {
	if strings.TrimSpace(cfg.RepoURL) != "" {
		u, err := url.Parse(strings.TrimSpace(cfg.RepoURL))
		if err != nil {
			return githubRepoTarget{}, false
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return githubRepoTarget{}, false
		}
		scheme := strings.TrimSpace(u.Scheme)
		if scheme == "" {
			scheme = "https"
		}
		host := strings.ToLower(u.Host)
		owner := strings.TrimSpace(parts[0])
		repo := strings.TrimSuffix(strings.TrimSpace(parts[1]), ".git")
		target := githubRepoTarget{scheme: scheme, host: host, owner: owner, repo: repo}
		return target, target.valid()
	}
	scheme, host, ok := githubBrowserBaseFromAPIBase(connectionBaseURL, cfg.APIBaseURL)
	if !ok {
		return githubRepoTarget{}, false
	}
	owner := strings.TrimSpace(cfg.Owner)
	repo := strings.TrimSuffix(strings.TrimSpace(cfg.Repo), ".git")
	target := githubRepoTarget{scheme: scheme, host: host, owner: owner, repo: repo}
	return target, target.valid()
}

func githubBrowserBaseFromAPIBase(connectionBaseURL, configBaseURL string) (string, string, bool) {
	raw := strings.TrimSpace(configBaseURL)
	if raw == "" {
		raw = strings.TrimSpace(connectionBaseURL)
	}
	if raw == "" {
		return "https", "github.com", true
	}
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", false
	}
	if strings.EqualFold(u.Hostname(), "api.github.com") {
		return "https", "github.com", true
	}
	return u.Scheme, strings.ToLower(u.Host), true
}

func (target githubRepoTarget) valid() bool {
	return target.scheme != "" && target.host != "" && target.owner != "" && target.repo != ""
}

func githubHostsMatch(targetHost, refHost string) bool {
	if strings.EqualFold(targetHost, refHost) {
		return true
	}
	u, err := url.Parse("https://" + targetHost)
	return err == nil && strings.EqualFold(u.Hostname(), refHost)
}

func (target githubRepoTarget) issueURL(issueNumber string) (string, error) {
	raw, err := url.JoinPath(target.scheme+"://"+target.host, target.owner, target.repo, "issues", issueNumber)
	if err != nil {
		return "", fmt.Errorf("build github issue url: %w", err)
	}
	return raw, nil
}

func ensureGitHubExternalIssueLink(
	ctx context.Context,
	tx pgx.Tx,
	in ManagedGitHubIssueLinkInput,
	mappingID uuid.UUID,
	ref githubIssueRef,
) (uuid.UUID, bool, error) {
	var linkID uuid.UUID
	var localObjectID string
	var localDeleted bool
	err := tx.QueryRow(ctx, `
		SELECT id, local_object_id, local_deleted_at IS NOT NULL
		  FROM external_object_links
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND external_object_type = 'issue'
		   AND external_key = $3
		   AND external_deleted_at IS NULL`,
		in.TenantID, mappingID, ref.issueNumber).Scan(&linkID, &localObjectID, &localDeleted)
	if err == nil {
		if localDeleted {
			if err := rejectDifferentActiveGitHubExternalIssueLink(ctx, tx, in.TenantID, mappingID, in.RequestID, ref.issueNumber); err != nil {
				return uuid.Nil, false, err
			}
		} else if localObjectID != in.RequestID.String() {
			return uuid.Nil, false, ErrConflict
		}
		if err := refreshGitHubExternalIssueLink(ctx, tx, linkID, in.RequestID, in.ExternalURL); err != nil {
			return uuid.Nil, false, err
		}
		return linkID, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, fmt.Errorf("find existing github external issue link: %w", err)
	}
	var localExternalKey string
	err = tx.QueryRow(ctx, `
		SELECT id, external_key
		  FROM external_object_links
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND local_object_type = 'customer_request'
		   AND local_object_id = $3
		   AND local_deleted_at IS NULL`,
		in.TenantID, mappingID, in.RequestID.String()).Scan(&linkID, &localExternalKey)
	if err == nil {
		if localExternalKey != ref.issueNumber {
			return uuid.Nil, false, ErrConflict
		}
		if err := refreshGitHubExternalIssueLink(ctx, tx, linkID, in.RequestID, in.ExternalURL); err != nil {
			return uuid.Nil, false, err
		}
		return linkID, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, fmt.Errorf("find existing customer request external issue link: %w", err)
	}
	linkID = uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO external_object_links
		 (id, tenant_id, mapping_id, local_object_type, local_object_id,
		  external_object_type, external_key, external_url, sync_state, sync_error)
		VALUES ($1, $2, $3, 'customer_request', $4, 'issue', $5, $6, 'pending', '')
		RETURNING id`,
		linkID, in.TenantID, mappingID, in.RequestID.String(), ref.issueNumber,
		truncateGitHubIssueLinkText(in.ExternalURL, 2048)).Scan(&linkID)
	if err != nil {
		return uuid.Nil, false, mapGitHubIssueLinkWriteError(err, "insert github external issue link")
	}
	return linkID, true, nil
}

func rejectDifferentActiveGitHubExternalIssueLink(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	mappingID uuid.UUID,
	requestID uuid.UUID,
	externalKey string,
) error {
	var activeExternalKey string
	err := tx.QueryRow(ctx, `
		SELECT external_key
		  FROM external_object_links
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND local_object_type = 'customer_request'
		   AND local_object_id = $3
		   AND local_deleted_at IS NULL
		 LIMIT 1`,
		tenantID, mappingID, requestID.String()).Scan(&activeExternalKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find active customer request external issue link: %w", err)
	}
	if activeExternalKey != externalKey {
		return ErrConflict
	}
	return nil
}

func refreshGitHubExternalIssueLink(ctx context.Context, tx pgx.Tx, linkID, requestID uuid.UUID, externalURL string) error {
	_, err := tx.Exec(ctx, `
		UPDATE external_object_links
		   SET local_object_id = $2,
		       external_url = $3,
		       external_deleted_at = NULL,
		       local_deleted_at = NULL,
		       sync_state = 'pending',
		       sync_error = '',
		       tombstone_reason = '',
		       updated_at = NOW()
		 WHERE id = $1`,
		linkID, requestID.String(), truncateGitHubIssueLinkText(externalURL, 2048))
	if err != nil {
		return fmt.Errorf("refresh github external issue link: %w", err)
	}
	return nil
}

func truncateGitHubIssueLinkText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func mapGitHubIssueLinkWriteError(err error, op string) error {
	if pgxutil.IsCheckViolation(err) {
		return ErrInvalidInput
	}
	if pgxutil.IsUniqueViolation(err) {
		return ErrConflict
	}
	if pgxutil.IsForeignKeyViolation(err) {
		return ErrLocalObjectNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}
