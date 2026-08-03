// SPDX-License-Identifier: Apache-2.0

// Package customerrequestview persists per-user Customer Request saved views.
package customerrequestview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	crrepo "github.com/Phixsura/attune/internal/repo/customerrequest"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
)

const savedViewsSettingPrefix = "customer_request.saved_views.user:"

var (
	ErrNotFound = errors.New("customerrequestview: not found")
	ErrConflict = errors.New("customerrequestview: conflict")
	ErrInvalid  = errors.New("customerrequestview: invalid")
)

type State struct {
	Query         string            `json:"q,omitempty"`
	Statuses      []crrepo.Status   `json:"status,omitempty"`
	Priorities    []crrepo.Priority `json:"priority,omitempty"`
	OwnerMemberID string            `json:"ownerMemberId,omitempty"`
	Visibility    crrepo.Visibility `json:"visibility,omitempty"`
	Sort          crrepo.Sort       `json:"sort,omitempty"`
	Direction     crrepo.Direction  `json:"direction,omitempty"`
	FeedbackID    int64             `json:"feedbackId,omitempty"`
	AccountKey    string            `json:"accountKey,omitempty"`
}

type View struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	State     State     `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type settingsStore interface {
	Get(ctx context.Context, tenantID, key string) (string, error)
	Set(ctx context.Context, tenantID, key, value, updatedBy string) error
}

type Repo struct {
	settings settingsStore
}

func New(settings settingsStore) *Repo {
	return ptrext.Of(Repo{settings: settings})
}

func (r *Repo) List(ctx context.Context, tenantID, userID string) ([]View, error) {
	envelope, err := r.load(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	return sortViews(envelope.Views), nil
}

func (r *Repo) Upsert(ctx context.Context, tenantID, userID string, view View, updatedBy string) (*View, error) {
	if strings.TrimSpace(view.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if strings.TrimSpace(updatedBy) == "" {
		return nil, fmt.Errorf("%w: updated_by is required", ErrInvalid)
	}
	view = normalizeView(view)

	envelope, err := r.load(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if view.ID == "" {
		view.ID = uuid.NewString()
		view.CreatedAt = now
		view.UpdatedAt = now
		if err := envelope.append(view); err != nil {
			return nil, err
		}
	} else {
		updated, ok, conflict := envelope.replace(view.ID, view)
		if conflict {
			return nil, ErrConflict
		}
		if !ok {
			return nil, ErrNotFound
		}
		updated.UpdatedAt = now
		view = updated
	}

	if err := r.save(ctx, tenantID, userID, envelope, updatedBy); err != nil {
		return nil, err
	}
	return ptrext.Of(view), nil
}

func (r *Repo) Delete(ctx context.Context, tenantID, userID, id, updatedBy string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalid)
	}
	if strings.TrimSpace(updatedBy) == "" {
		return fmt.Errorf("%w: updated_by is required", ErrInvalid)
	}

	envelope, err := r.load(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	if !envelope.delete(strings.TrimSpace(id)) {
		return ErrNotFound
	}
	return r.save(ctx, tenantID, userID, envelope, updatedBy)
}

func (r *Repo) load(ctx context.Context, tenantID, userID string) (storedEnvelope, error) {
	var envelope storedEnvelope
	if strings.TrimSpace(userID) == "" {
		return envelope, fmt.Errorf("%w: user id is required", ErrInvalid)
	}
	raw, err := r.settings.Get(ctx, tenantID, settingKey(userID))
	if errors.Is(err, systemsettings.ErrNotFound) {
		return storedEnvelope{Version: 1, Views: []View{}}, nil
	}
	if err != nil {
		return envelope, fmt.Errorf("customerrequestview.load: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		var legacy []View
		if legacyErr := json.Unmarshal([]byte(raw), &legacy); legacyErr == nil {
			return storedEnvelope{Version: 1, Views: legacy}, nil
		}
		return envelope, fmt.Errorf("customerrequestview.load: decode saved views: %w", err)
	}
	if envelope.Version == 0 {
		envelope.Version = 1
	}
	if envelope.Views == nil {
		envelope.Views = []View{}
	}
	return envelope, nil
}

func (r *Repo) save(ctx context.Context, tenantID, userID string, envelope storedEnvelope, updatedBy string) error {
	envelope.Version = 1
	if envelope.Views == nil {
		envelope.Views = []View{}
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("customerrequestview.save: marshal saved views: %w", err)
	}
	if err := r.settings.Set(ctx, tenantID, settingKey(userID), string(payload), updatedBy); err != nil {
		return fmt.Errorf("customerrequestview.save: %w", err)
	}
	return nil
}

func settingKey(userID string) string {
	return savedViewsSettingPrefix + strings.TrimSpace(userID)
}

type storedEnvelope struct {
	Version int    `json:"version"`
	Views   []View `json:"views"`
}

func (e *storedEnvelope) append(view View) error {
	if e == nil {
		return fmt.Errorf("%w: internal envelope is nil", ErrInvalid)
	}
	if existing, ok := e.findByName(view.Name, ""); ok {
		return fmt.Errorf("%w: view name %q already exists as %q", ErrConflict, view.Name, existing.ID)
	}
	e.Views = append(e.Views, view)
	return nil
}

func (e *storedEnvelope) replace(id string, view View) (View, bool, bool) {
	if e == nil {
		return View{}, false, false
	}
	if existing, ok := e.findByName(view.Name, id); ok {
		return View{}, false, existing.ID != id
	}
	for i := range e.Views {
		if e.Views[i].ID != id {
			continue
		}
		view.CreatedAt = e.Views[i].CreatedAt
		if view.CreatedAt.IsZero() {
			view.CreatedAt = time.Now().UTC()
		}
		e.Views[i] = view
		return view, true, false
	}
	return View{}, false, false
}

func (e *storedEnvelope) delete(id string) bool {
	if e == nil {
		return false
	}
	for i := range e.Views {
		if e.Views[i].ID != id {
			continue
		}
		e.Views = append(e.Views[:i], e.Views[i+1:]...)
		return true
	}
	return false
}

func (e *storedEnvelope) findByName(name, allowID string) (View, bool) {
	trimmed := strings.TrimSpace(name)
	for _, view := range e.Views {
		if strings.EqualFold(strings.TrimSpace(view.Name), trimmed) && (allowID == "" || view.ID != allowID) {
			return view, true
		}
	}
	return View{}, false
}

func normalizeView(view View) View {
	view.ID = strings.TrimSpace(view.ID)
	view.Name = strings.TrimSpace(view.Name)
	view.State = normalizeState(view.State)
	return view
}

func normalizeState(state State) State {
	state.Query = strings.TrimSpace(state.Query)
	state.Statuses = normalizeStatuses(state.Statuses)
	state.Priorities = normalizePriorities(state.Priorities)
	state.OwnerMemberID = strings.TrimSpace(state.OwnerMemberID)
	if state.Visibility == "" {
		state.Visibility = crrepo.VisibilityActive
	}
	if state.Sort == "" {
		state.Sort = crrepo.SortUpdatedAt
	}
	if state.Direction == "" {
		state.Direction = crrepo.DirectionDesc
	}
	return state
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

func sortViews(views []View) []View {
	if len(views) == 0 {
		return []View{}
	}
	out := append([]View(nil), views...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}
