// SPDX-License-Identifier: Apache-2.0

// Package featureflag provides simple feature flag support.
package featureflag

import (
	"context"
	"sync"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Flag represents a feature flag.
type Flag struct {
	Name        string
	Description string
	Enabled     bool
}

// Store manages feature flags.
type Store struct {
	mu    sync.RWMutex
	flags map[string]*Flag
}

// NewStore creates a new feature flag store.
func NewStore() *Store {
	return ptrext.Of(Store{
		flags: make(map[string]*Flag),
	})
}

// Register registers a feature flag with default state.
func (s *Store) Register(name, description string, defaultEnabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flags[name] = ptrext.Of(Flag{
		Name:        name,
		Description: description,
		Enabled:     defaultEnabled,
	})
}

// IsEnabled checks if a feature flag is enabled.
func (s *Store) IsEnabled(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if f, ok := s.flags[name]; ok {
		return f.Enabled
	}
	return false
}

// SetEnabled sets the enabled state of a feature flag.
func (s *Store) SetEnabled(name string, enabled bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.flags[name]; ok {
		f.Enabled = enabled
		return true
	}
	return false
}

// All returns all registered flags.
func (s *Store) All() []Flag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Flag, 0, len(s.flags))
	for _, f := range s.flags {
		result = append(result, ptrext.Indirect(f))
	}
	return result
}

// contextKey is the context key for feature flags.
type contextKey struct{}

// WithStore adds a feature flag store to the context.
func WithStore(ctx context.Context, store *Store) context.Context {
	return context.WithValue(ctx, contextKey{}, store)
}

// FromContext retrieves the feature flag store from context.
func FromContext(ctx context.Context) *Store {
	if v := ctx.Value(contextKey{}); v != nil {
		if s, ok := v.(*Store); ok {
			return s
		}
	}
	return nil
}

// Enabled checks if a feature is enabled using the store from context.
// Returns false if no store is in context or flag doesn't exist.
func Enabled(ctx context.Context, name string) bool {
	if s := FromContext(ctx); s != nil {
		return s.IsEnabled(name)
	}
	return false
}

// Global is the default global feature flag store.
var Global = NewStore()
