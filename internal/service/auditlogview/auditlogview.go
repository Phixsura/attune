// SPDX-License-Identifier: Apache-2.0

// Package auditlogview manages saved audit-log investigation views.
package auditlogview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogviewrepo "github.com/Phixsura/attune/internal/repo/auditlogview"
)

// ErrValidation wraps request validation problems for HTTP mapping.
var ErrValidation = errors.New("auditlogview: validation failed")

// State mirrors the snapshot that can be restored into the audit-log page.
type State = auditlogviewrepo.State

// View is the persisted saved view.
type View = auditlogviewrepo.View

// SaveInput captures the payload for create/update operations.
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

// Service is the domain layer for audit-log views.
type Service struct {
	repo repo
}

func New(repo repo) *Service {
	return ptrext.Of(Service{repo: repo})
}

func (s *Service) List(ctx context.Context, tenantID, userID string) ([]View, error) {
	return s.repo.List(ctx, tenantID, userID)
}

func (s *Service) Save(ctx context.Context, tenantID, userID string, input SaveInput) (*View, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if strings.TrimSpace(input.UpdatedBy) == "" {
		return nil, fmt.Errorf("%w: updated_by is required", ErrValidation)
	}
	state, err := normalizeState(input.State)
	if err != nil {
		return nil, err
	}
	view, err := s.repo.Upsert(ctx, tenantID, userID, View{
		ID:    strings.TrimSpace(input.ID),
		Name:  strings.TrimSpace(input.Name),
		State: state,
	}, input.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (s *Service) Delete(ctx context.Context, tenantID, userID, id, updatedBy string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if strings.TrimSpace(updatedBy) == "" {
		return fmt.Errorf("%w: updated_by is required", ErrValidation)
	}
	return s.repo.Delete(ctx, tenantID, userID, id, updatedBy)
}

func normalizeState(state State) (State, error) {
	state.Actions = normalizeStrings(state.Actions)
	state.ActorType = strings.TrimSpace(state.ActorType)
	state.ActorID = strings.TrimSpace(state.ActorID)
	state.TargetType = strings.TrimSpace(state.TargetType)
	state.TargetID = strings.TrimSpace(state.TargetID)
	state.LocalQuery = strings.TrimSpace(state.LocalQuery)
	state.InspectedEntryID = strings.TrimSpace(state.InspectedEntryID)
	state.From = strings.TrimSpace(state.From)
	state.To = strings.TrimSpace(state.To)

	if state.From != "" {
		if _, err := time.Parse(time.RFC3339, state.From); err != nil {
			return State{}, fmt.Errorf("%w: from must be RFC3339", ErrValidation)
		}
	}
	if state.To != "" {
		if _, err := time.Parse(time.RFC3339, state.To); err != nil {
			return State{}, fmt.Errorf("%w: to must be RFC3339", ErrValidation)
		}
	}
	return state, nil
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
