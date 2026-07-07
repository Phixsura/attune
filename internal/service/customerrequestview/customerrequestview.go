// SPDX-License-Identifier: Apache-2.0

// Package customerrequestview manages saved Customer Request list views.
package customerrequestview

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	crrepo "github.com/Phixsura/attune/internal/repo/customerrequest"
	viewrepo "github.com/Phixsura/attune/internal/repo/customerrequestview"
)

var ErrValidation = errors.New("customerrequestview: validation failed")

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
	return s.repo.Upsert(ctx, tenantID, userID, View{
		ID:    strings.TrimSpace(input.ID),
		Name:  name,
		State: state,
	}, updatedBy)
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
	state.Query = strings.TrimSpace(state.Query)
	if len([]rune(state.Query)) > 200 {
		return State{}, fmt.Errorf("%w: query is too long", ErrValidation)
	}
	state.Statuses = normalizeStatuses(state.Statuses)
	for _, status := range state.Statuses {
		if !validStatus(status) {
			return State{}, fmt.Errorf("%w: invalid status", ErrValidation)
		}
	}
	state.Priorities = normalizePriorities(state.Priorities)
	for _, priority := range state.Priorities {
		if !validPriority(priority) {
			return State{}, fmt.Errorf("%w: invalid priority", ErrValidation)
		}
	}
	state.OwnerMemberID = strings.TrimSpace(state.OwnerMemberID)
	if state.OwnerMemberID != "" {
		if _, err := uuid.Parse(state.OwnerMemberID); err != nil {
			return State{}, fmt.Errorf("%w: invalid owner member id", ErrValidation)
		}
	}
	if state.Visibility == "" {
		state.Visibility = crrepo.VisibilityActive
	}
	if !validVisibility(state.Visibility) {
		return State{}, fmt.Errorf("%w: invalid visibility", ErrValidation)
	}
	if state.Sort == "" {
		state.Sort = crrepo.SortUpdatedAt
	}
	if !validSort(state.Sort) {
		return State{}, fmt.Errorf("%w: invalid sort", ErrValidation)
	}
	if state.Direction == "" {
		state.Direction = crrepo.DirectionDesc
	}
	if !validDirection(state.Direction) {
		return State{}, fmt.Errorf("%w: invalid direction", ErrValidation)
	}
	if state.FeedbackID < 0 {
		return State{}, fmt.Errorf("%w: invalid feedback id", ErrValidation)
	}
	return state, nil
}

func normalizeStatuses(values []crrepo.Status) []crrepo.Status {
	if len(values) == 0 {
		return nil
	}
	out := make([]crrepo.Status, 0, len(values))
	seen := make(map[crrepo.Status]struct{}, len(values))
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

func normalizePriorities(values []crrepo.Priority) []crrepo.Priority {
	if len(values) == 0 {
		return nil
	}
	out := make([]crrepo.Priority, 0, len(values))
	seen := make(map[crrepo.Priority]struct{}, len(values))
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

func validStatus(status crrepo.Status) bool {
	switch status {
	case crrepo.StatusOpen, crrepo.StatusPlanned, crrepo.StatusInProgress, crrepo.StatusShipped, crrepo.StatusCancelled:
		return true
	default:
		return false
	}
}

func validPriority(priority crrepo.Priority) bool {
	switch priority {
	case crrepo.PriorityNone, crrepo.PriorityLow, crrepo.PriorityMedium, crrepo.PriorityHigh, crrepo.PriorityUrgent:
		return true
	default:
		return false
	}
}

func validVisibility(visibility crrepo.Visibility) bool {
	switch visibility {
	case crrepo.VisibilityActive, crrepo.VisibilityMerged, crrepo.VisibilityArchived, crrepo.VisibilityAll:
		return true
	default:
		return false
	}
}

func validSort(sort crrepo.Sort) bool {
	switch sort {
	case crrepo.SortUpdatedAt, crrepo.SortCustomerCount, crrepo.SortSupportingFeedbackCount, crrepo.SortLatestFeedbackAt,
		crrepo.SortPriority, crrepo.SortRevenueImpact, crrepo.SortDecisionScore, crrepo.SortDeliveryHealth:
		return true
	default:
		return false
	}
}

func validDirection(direction crrepo.Direction) bool {
	switch direction {
	case crrepo.DirectionAsc, crrepo.DirectionDesc:
		return true
	default:
		return false
	}
}
