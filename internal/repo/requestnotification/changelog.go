// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type changelogVisibility struct {
	searchIndexingEnabled bool
	changelogEnabled      bool
	hidePublicTimestamps  bool
}

func (r *Repo) ListChangelogPosts(ctx context.Context, tenantID string, limit int, cursor string) (ChangelogListResult, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ChangelogListResult{}, ErrInvalidInput
	}
	limit = boundedLimit(limit)
	offset, err := parseCursor(cursor)
	if err != nil {
		return ChangelogListResult{}, err
	}

	visibility, err := r.loadChangelogVisibility(ctx, tenantID)
	if err != nil {
		return ChangelogListResult{}, err
	}
	if !visibility.changelogEnabled {
		return ChangelogListResult{}, ErrNotFound
	}

	posts, err := r.listChangelogPosts(ctx, tenantID, limit, offset)
	if err != nil {
		return ChangelogListResult{}, err
	}
	nextCursor := ""
	if len(posts) > limit {
		posts = posts[:limit]
		nextCursor = strconv.Itoa(offset + limit)
	}
	if len(posts) == 0 {
		return ChangelogListResult{
			Items:                posts,
			NoIndex:              !visibility.searchIndexingEnabled,
			HidePublicTimestamps: visibility.hidePublicTimestamps,
		}, nil
	}

	links, err := r.listChangelogRequestLinks(ctx, tenantID, changelogPostIDs(posts))
	if err != nil {
		return ChangelogListResult{}, err
	}
	attachChangelogRequests(posts, links)

	return ChangelogListResult{
		Items:                posts,
		NextCursor:           nextCursor,
		NoIndex:              !visibility.searchIndexingEnabled,
		HidePublicTimestamps: visibility.hidePublicTimestamps,
	}, nil
}

func (r *Repo) loadChangelogVisibility(ctx context.Context, tenantID string) (changelogVisibility, error) {
	var visibility changelogVisibility
	err := r.pool.QueryRow(ctx, `
		SELECT search_indexing_enabled, changelog_enabled, hide_public_timestamps
		FROM public_visibility_policies
		WHERE tenant_id = $1`, tenantID).Scan(&visibility.searchIndexingEnabled, &visibility.changelogEnabled, &visibility.hidePublicTimestamps)
	if errors.Is(err, pgx.ErrNoRows) {
		return changelogVisibility{}, ErrNotFound
	}
	if err != nil {
		return changelogVisibility{}, err
	}
	return visibility, nil
}

func (r *Repo) listChangelogPosts(ctx context.Context, tenantID string, limit int, offset int) ([]ChangelogPost, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.id, p.thread_id, p.title, p.body, p.kind, p.published_at
		FROM public_update_posts p
		JOIN public_update_threads t
		  ON t.tenant_id = p.tenant_id
		 AND t.id = p.thread_id
		WHERE p.tenant_id = $1
		  AND p.state = 'published'
		  AND p.kind = 'changelog_post'
		  AND t.surface = 'changelog_post'
		  AND t.state = 'published'
		  AND EXISTS (
			SELECT 1
			FROM public_update_request_links l
			JOIN public_request_profiles prp
			  ON prp.tenant_id = l.tenant_id
			 AND prp.request_id = l.request_id
			JOIN public_moderation_subjects pms
			  ON pms.tenant_id = prp.tenant_id
			 AND pms.surface = 'request'
			 AND pms.subject_id = prp.id::text
			JOIN customer_requests cr
			  ON cr.tenant_id = prp.tenant_id
			 AND cr.id = prp.request_id
			WHERE l.tenant_id = p.tenant_id
			  AND l.update_id = p.id
			  AND l.role = 'primary'
			  AND prp.included_in_portal = TRUE
			  AND pms.state = 'approved'
			  AND cr.status = 'shipped'
			  AND cr.archived_at IS NULL
			  AND cr.merged_into_request_id IS NULL
		  )
		ORDER BY p.published_at DESC, p.id DESC
		LIMIT $2 OFFSET $3`, tenantID, limit+1, offset)
	if err != nil {
		return nil, fmt.Errorf("list changelog posts: %w", err)
	}
	defer rows.Close()
	return scanChangelogPosts(rows)
}

func (r *Repo) listChangelogRequestLinks(ctx context.Context, tenantID string, postIDs []uuid.UUID) (map[uuid.UUID][]ChangelogRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT l.update_id, prp.id, prp.public_slug, prp.public_title,
		 prp.public_summary, prp.public_state, prp.roadmap_column
		FROM public_update_request_links l
		JOIN public_request_profiles prp
		  ON prp.tenant_id = l.tenant_id
		 AND prp.request_id = l.request_id
		JOIN public_moderation_subjects pms
		  ON pms.tenant_id = prp.tenant_id
		 AND pms.surface = 'request'
		 AND pms.subject_id = prp.id::text
		JOIN customer_requests cr
		  ON cr.tenant_id = prp.tenant_id
		 AND cr.id = prp.request_id
		WHERE l.tenant_id = $1
		  AND l.update_id = ANY($2::uuid[])
		  AND prp.included_in_portal = TRUE
		  AND pms.state = 'approved'
		  AND cr.status = 'shipped'
		  AND cr.archived_at IS NULL
		  AND cr.merged_into_request_id IS NULL
		ORDER BY l.update_id, CASE WHEN l.role = 'primary' THEN 0 ELSE 1 END,
		 l.created_at ASC, prp.public_title ASC`, tenantID, postIDs)
	if err != nil {
		return nil, fmt.Errorf("list changelog post links: %w", err)
	}
	defer rows.Close()

	links := make(map[uuid.UUID][]ChangelogRequest, len(postIDs))
	for rows.Next() {
		var updateID uuid.UUID
		var request ChangelogRequest
		if err := rows.Scan(
			&updateID,
			&request.ID,
			&request.PublicSlug,
			&request.PublicTitle,
			&request.PublicSummary,
			&request.PublicState,
			&request.RoadmapColumn,
		); err != nil {
			return nil, err
		}
		links[updateID] = append(links[updateID], request)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return links, nil
}

func changelogPostIDs(posts []ChangelogPost) []uuid.UUID {
	postIDs := make([]uuid.UUID, 0, len(posts))
	for _, post := range posts {
		postIDs = append(postIDs, post.ID)
	}
	return postIDs
}

func attachChangelogRequests(posts []ChangelogPost, links map[uuid.UUID][]ChangelogRequest) {
	for i := range posts {
		posts[i].Requests = links[posts[i].ID]
	}
}

func (r *Repo) GetChangelogRequest(ctx context.Context, tenantID string, requestID uuid.UUID) (ChangelogRequest, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || requestID == uuid.Nil {
		return ChangelogRequest{}, ErrInvalidInput
	}
	row := r.pool.QueryRow(ctx, `
		SELECT prp.id, prp.public_slug, prp.public_title, prp.public_summary,
		 prp.public_state, prp.roadmap_column
		FROM public_request_profiles prp
		JOIN public_visibility_policies pol
		  ON pol.tenant_id = prp.tenant_id
		JOIN public_moderation_subjects pms
		  ON pms.tenant_id = prp.tenant_id
		 AND pms.surface = 'request'
		 AND pms.subject_id = prp.id::text
		JOIN customer_requests cr
		  ON cr.tenant_id = prp.tenant_id
		 AND cr.id = prp.request_id
		WHERE prp.tenant_id = $1
		  AND prp.request_id = $2
		  AND pol.changelog_enabled = TRUE
		  AND prp.included_in_portal = TRUE
		  AND pms.state = 'approved'
		  AND cr.status = 'shipped'
		  AND cr.archived_at IS NULL
		  AND cr.merged_into_request_id IS NULL
		LIMIT 1`, tenantID, requestID)
	var out ChangelogRequest
	if err := row.Scan(
		&out.ID,
		&out.PublicSlug,
		&out.PublicTitle,
		&out.PublicSummary,
		&out.PublicState,
		&out.RoadmapColumn,
	); err != nil {
		return ChangelogRequest{}, mapNotFound(err)
	}
	return out, nil
}

func scanChangelogPosts(rows pgx.Rows) ([]ChangelogPost, error) {
	var out []ChangelogPost
	for rows.Next() {
		var post ChangelogPost
		if err := rows.Scan(&post.ID, &post.ThreadID, &post.Title, &post.Body, &post.Kind, &post.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, post)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseCursor(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("%w: invalid cursor", ErrInvalidInput)
	}
	return offset, nil
}
