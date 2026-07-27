// SPDX-License-Identifier: Apache-2.0

// Package cohortsync owns the provider adapter contract for Attune's
// cohort sync framework. Adapters self-register via init(); cmd/attune
// blank-imports them so the registry is populated before webhook handlers
// start.
package cohortsync

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

var providerShapeRE = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Connection is the decrypted provider-facing connection shape.
type Connection struct {
	ID             string
	TenantID       string
	Provider       string
	Name           string
	AuthType       string
	BaseURL        string
	ProviderConfig []byte
	Credential     []byte
}

// MemberDelta is one user entering or leaving a cohort.
type MemberDelta struct {
	ExternalUserID string
	Email          string
	DisplayName    string
	Properties     map[string]any
	Action         string // "add" or "remove"
}

// SyncPayload is the normalized input from any provider webhook.
type SyncPayload struct {
	Provider         string
	ExternalCohortID string
	CohortName       string
	IsFullSnapshot   bool // true = replace entire membership (Mixpanel "members" action)
	Deltas           []MemberDelta
}

// Provider is the adapter interface for a cohort analytics provider.
type Provider interface {
	// Provider returns the stable provider token (e.g. "amplitude").
	Provider() string

	// ParseWebhook normalizes a raw HTTP request body into a SyncPayload.
	// The handler reads the body and passes it as bytes (not *http.Request)
	// so the framework root avoids a net/http dependency.
	ParseWebhook(body []byte, headers map[string]string, secret []byte) (SyncPayload, error)

	// PullCohort fetches the current full membership for on-demand refresh.
	// Called when the operator clicks "Sync Now" in Console.
	PullCohort(ctx context.Context, conn Connection, externalCohortID string) (SyncPayload, error)
}

// Factory returns a fresh provider instance.
type Factory func() Provider

// Entry is one registered provider.
type Entry struct {
	Provider string
	Display  string
	Factory  Factory
}

type registration struct {
	display string
	factory Factory
}

var (
	mu        sync.RWMutex
	providers = map[string]registration{}
)

// Register adds a cohort sync provider adapter. Provider names are persisted
// and must stay lower-case ASCII tokens.
func Register(provider, display string, factory Factory) {
	if factory == nil {
		panic("cohortsync: nil factory for provider " + provider)
	}
	if !providerShapeRE.MatchString(provider) {
		panic(fmt.Sprintf("cohortsync: provider %q must match %s", provider, providerShapeRE.String()))
	}
	if strings.TrimSpace(display) == "" {
		panic("cohortsync: provider " + provider + " registered with empty display")
	}
	if strings.ContainsAny(display, "\x00\n\r\t") {
		panic(fmt.Sprintf("cohortsync: provider %q display contains control characters", provider))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := providers[provider]; exists {
		panic("cohortsync: provider already registered: " + provider)
	}
	providers[provider] = registration{display: display, factory: factory}
}

// ResetForTest clears the registry in tests.
func ResetForTest() {
	if !testing.Testing() {
		panic("cohortsync.ResetForTest must only be called from tests")
	}
	mu.Lock()
	defer mu.Unlock()
	providers = map[string]registration{}
}

// Providers returns deterministic provider entries.
func Providers() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Entry, 0, len(providers))
	for provider, reg := range providers {
		out = append(out, Entry{Provider: provider, Display: reg.display, Factory: reg.factory})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// Lookup returns a provider instance for provider.
func Lookup(provider string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	reg, ok := providers[provider]
	if !ok {
		return nil, false
	}
	return reg.factory(), true
}

// ValidateProviderToken verifies the persisted provider token shape.
func ValidateProviderToken(provider string) error {
	if !providerShapeRE.MatchString(provider) {
		return fmt.Errorf("provider must match %s", providerShapeRE.String())
	}
	return nil
}

// ErrProviderUnavailable is returned when no adapter is registered.
var ErrProviderUnavailable = errors.New("cohort sync provider unavailable")

// UnavailableError reports a missing adapter.
func UnavailableError(provider string) error {
	return fmt.Errorf("%w: %s", ErrProviderUnavailable, provider)
}
