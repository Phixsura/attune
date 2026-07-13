// SPDX-License-Identifier: Apache-2.0

// Package publicvisibilityview manages saved public-visibility moderation views.
package publicvisibilityview

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	viewrepo "github.com/Phixsura/attune/internal/repo/publicvisibilityview"
)

var ErrValidation = errors.New("publicvisibilityview: validation failed")

type (
	State = viewrepo.State
	View  = viewrepo.View
)

type SaveInput struct {
	ID        string
	Name      string
	State     State
	UpdatedBy string
}

type repo interface {
	List(ctx context.Context, tenantID, userID string) ([]View, error)
	Upsert(ctx context.Context, tenantID, userID string, view View, updatedBy string) (*View, error)
	Delete(ctx context.Context, tenantID, userID, id, updatedBy string) error
}

type Service struct {
	repo repo
}

func New(repo repo) *Service {
	return ptrext.Of(Service{repo: repo})
}

func (s *Service) List(ctx context.Context, tenantID, userID string) ([]View, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: tenant and user are required", ErrValidation)
	}
	return s.repo.List(ctx, strings.TrimSpace(tenantID), strings.TrimSpace(userID))
}

func (s *Service) Save(ctx context.Context, tenantID, userID string, input SaveInput) (*View, error) {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return nil, fmt.Errorf("%w: tenant and user are required", ErrValidation)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if len([]rune(name)) > 80 {
		return nil, fmt.Errorf("%w: name is too long", ErrValidation)
	}
	updatedBy := strings.TrimSpace(input.UpdatedBy)
	if updatedBy == "" {
		return nil, fmt.Errorf("%w: updated_by is required", ErrValidation)
	}
	state, err := normalizeState(input.State)
	if err != nil {
		return nil, err
	}
	view, err := s.repo.Upsert(ctx, tenantID, userID, View{
		ID:    strings.TrimSpace(input.ID),
		Name:  name,
		State: state,
	}, updatedBy)
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (s *Service) Delete(ctx context.Context, tenantID, userID, id, updatedBy string) error {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return fmt.Errorf("%w: tenant and user are required", ErrValidation)
	}
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if strings.TrimSpace(updatedBy) == "" {
		return fmt.Errorf("%w: updated_by is required", ErrValidation)
	}
	return s.repo.Delete(ctx, tenantID, userID, strings.TrimSpace(id), strings.TrimSpace(updatedBy))
}

func normalizeState(state State) (State, error) {
	state.QueueView = strings.ToLower(strings.TrimSpace(state.QueueView))
	if state.QueueView == "" {
		state.QueueView = "pending"
	}
	switch state.QueueView {
	case "pending", "approved", "blocked", "all":
	default:
		return State{}, fmt.Errorf("%w: invalid queue view", ErrValidation)
	}
	state.Surfaces = normalizeSurfaces(state.Surfaces)
	for _, surface := range state.Surfaces {
		if !validSurface(surface) {
			return State{}, fmt.Errorf("%w: invalid surface", ErrValidation)
		}
	}
	return state, nil
}

func normalizeSurfaces(values []pvrepo.Surface) []pvrepo.Surface {
	if len(values) == 0 {
		return nil
	}
	out := make([]pvrepo.Surface, 0, len(values))
	seen := make(map[pvrepo.Surface]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func validSurface(surface pvrepo.Surface) bool {
	switch surface {
	case pvrepo.SurfaceRequest, pvrepo.SurfaceRequestComment, pvrepo.SurfaceRoadmapItem, pvrepo.SurfaceChangelogPost, pvrepo.SurfacePortalSubmission:
		return true
	default:
		return false
	}
}
