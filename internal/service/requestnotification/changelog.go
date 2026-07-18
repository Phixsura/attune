// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
)

type changelogRepository interface {
	ListChangelogPosts(ctx context.Context, tenantID string, limit int, cursor string) (repo.ChangelogListResult, error)
	GetChangelogRequest(ctx context.Context, tenantID string, requestID uuid.UUID) (repo.ChangelogRequest, error)
}

func (s *Service) ListChangelog(ctx context.Context, tenantID string, limit int, cursor string) (repo.ChangelogListResult, error) {
	lister, ok := s.repo.(changelogRepository)
	if !ok {
		return repo.ChangelogListResult{}, errors.New("request notification changelog repository not configured")
	}
	result, err := lister.ListChangelogPosts(ctx, strings.TrimSpace(tenantID), limit, cursor)
	return result, mapRepoError(err)
}

func (s *Service) resolvePublishDraft(
	ctx context.Context,
	tenantID string,
	request repo.RequestSummary,
	kind string,
	title string,
	body string,
) (string, string, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if kind != "changelog_post" {
		if title == "" || body == "" {
			return "", "", ErrValidation
		}
		return title, body, nil
	}
	if strings.TrimSpace(request.Status) != "shipped" {
		return "", "", ErrValidation
	}
	source, err := s.getChangelogRequest(ctx, tenantID, request.ID)
	if err != nil {
		return "", "", err
	}
	if title == "" {
		title = changelogDraftTitle(source)
	}
	if body == "" {
		body = changelogDraftBody(source)
	}
	if title == "" || body == "" {
		return "", "", ErrValidation
	}
	return title, body, nil
}

func (s *Service) getChangelogRequest(ctx context.Context, tenantID string, requestID uuid.UUID) (repo.ChangelogRequest, error) {
	lookup, ok := s.repo.(interface {
		GetChangelogRequest(ctx context.Context, tenantID string, requestID uuid.UUID) (repo.ChangelogRequest, error)
	})
	if !ok {
		return repo.ChangelogRequest{}, errors.New("request notification changelog repository not configured")
	}
	item, err := lookup.GetChangelogRequest(ctx, strings.TrimSpace(tenantID), requestID)
	return item, mapRepoError(err)
}

func changelogDraftTitle(source repo.ChangelogRequest) string {
	title := strings.TrimSpace(source.PublicTitle)
	if title == "" {
		title = strings.TrimSpace(source.PublicSlug)
	}
	if title == "" {
		return "Release notes"
	}
	return fmt.Sprintf("Release notes: %s", title)
}

func changelogDraftBody(source repo.ChangelogRequest) string {
	title := strings.TrimSpace(source.PublicTitle)
	if title == "" {
		title = strings.TrimSpace(source.PublicSlug)
	}
	if title == "" {
		title = "this request"
	}
	summary := strings.TrimSpace(source.PublicSummary)
	if summary == "" {
		return fmt.Sprintf("We shipped %s.", title)
	}
	return fmt.Sprintf("We shipped %s.\n\n%s", title, summary)
}
