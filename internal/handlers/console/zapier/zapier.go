// SPDX-License-Identifier: Apache-2.0

// Package zapier implements the Zapier REST Hook subscription protocol,
// allowing attune events to trigger Zapier Zaps.
package zapier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Subscription holds a Zapier REST Hook subscription.
type Subscription struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	TargetURL string `json:"target_url"`
	Event     string `json:"event"`
}

// Store abstracts subscription persistence.
type Store interface {
	Create(ctx context.Context, sub Subscription) error
	Delete(ctx context.Context, tenantID, id string) error
	ListByTenant(ctx context.Context, tenantID string) ([]Subscription, error)
}

// Handler serves the Zapier REST Hook subscription endpoints.
type Handler struct {
	store Store
}

// NewHandler creates a Zapier handler.
func NewHandler(store Store) *Handler {
	return ptrext.Of(Handler{store: store})
}

// Subscribe handles POST /zapier/subscribe — creates a new subscription.
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		http.Error(w, "missing tenant", http.StatusBadRequest)
		return
	}
	var req struct {
		TargetURL string `json:"target_url"`
		Event     string `json:"event"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { // ptrext:allow unmarshal-out-param
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sub := Subscription{
		ID:        fmt.Sprintf("zap_%s_%s", tenantID, req.Event),
		TenantID:  tenantID,
		TargetURL: req.TargetURL,
		Event:     req.Event,
	}
	if err := h.store.Create(r.Context(), sub); err != nil {
		logext.Errorf(r.Context(), "[zapier] create subscription: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sub)
}

// Unsubscribe handles DELETE /zapier/subscribe/{id} — removes a subscription.
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-ID")
	subID := r.PathValue("id")
	if tenantID == "" || subID == "" {
		http.Error(w, "missing tenant or id", http.StatusBadRequest)
		return
	}
	if err := h.store.Delete(r.Context(), tenantID, subID); err != nil {
		logext.Errorf(r.Context(), "[zapier] delete subscription: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// MemStore is an in-memory subscription store for testing.
type MemStore struct {
	mu   sync.RWMutex
	subs map[string][]Subscription
}

// NewMemStore creates an in-memory store.
func NewMemStore() *MemStore {
	return ptrext.Of(MemStore{subs: make(map[string][]Subscription)})
}

// Create adds a subscription.
func (m *MemStore) Create(_ context.Context, sub Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[sub.TenantID] = append(m.subs[sub.TenantID], sub)
	return nil
}

// Delete removes a subscription by ID.
func (m *MemStore) Delete(_ context.Context, tenantID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	subs := m.subs[tenantID]
	for i, s := range subs {
		if s.ID == id {
			m.subs[tenantID] = append(subs[:i], subs[i+1:]...)
			return nil
		}
	}
	return nil
}

// ListByTenant returns all subscriptions for a tenant.
func (m *MemStore) ListByTenant(_ context.Context, tenantID string) ([]Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subs[tenantID], nil
}
