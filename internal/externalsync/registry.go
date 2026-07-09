// SPDX-License-Identifier: Apache-2.0

// Package externalsync owns the provider adapter contract for Attune's
// external sync framework.
package externalsync

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
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
	Scopes         []string
	Credential     []byte
}

// CheckResult is returned by provider health probes.
type CheckResult struct {
	OK        bool
	Error     string
	Latency   time.Duration
	RequestID string
}

// ObjectSchema describes one provider object supported by an adapter.
type ObjectSchema struct {
	Type           string
	Fields         []string
	RequiredFields []string
	WritableFields []string
}

// PullRequest is the normalized input for provider pull sync.
type PullRequest struct {
	Connection Connection
	MappingID  string
	StreamKey  string
	Cursor     []byte
}

// PullResult is the normalized output for provider pull sync.
type PullResult struct {
	Records    []ExternalRecord
	StreamKey  string
	NextCursor []byte
}

// PushRequest is the normalized input for provider push sync.
type PushRequest struct {
	Connection Connection
	MappingID  string
	Records    []LocalRecord
}

// PushResult is the normalized output for provider push sync.
type PushResult struct {
	Results []WriteResult
}

// ExternalRecord is the provider-neutral representation returned by Pull.
type ExternalRecord struct {
	Key           string
	URL           string
	Version       string
	LocalObjectID string
	UpdatedAt     time.Time
	Deleted       bool
	Payload       []byte
}

// LocalRecord is the provider-neutral representation supplied to Push.
type LocalRecord struct {
	ID      string
	Version string
	Payload []byte
}

// WriteResult is the per-record result returned by Push.
type WriteResult struct {
	LocalID   string
	Key       string
	URL       string
	Version   string
	Retryable bool
	Error     *SyncError
}

// SyncError is a redacted, classified provider error.
type SyncError struct {
	Kind              string
	Message           string
	HTTPStatus        int
	ProviderRequestID string
	RetryAfter        *time.Time
	Retryable         bool
}

// Provider implements one external system.
type Provider interface {
	Provider() string
	Check(ctx context.Context, conn Connection) (CheckResult, error)
	Discover(ctx context.Context, conn Connection) ([]ObjectSchema, error)
	Pull(ctx context.Context, req PullRequest) (PullResult, error)
	Push(ctx context.Context, req PushRequest) (PushResult, error)
	ClassifyError(error) SyncError
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

// Register adds a provider adapter. Provider names are persisted and must stay
// lower-case ASCII tokens.
func Register(provider, display string, factory Factory) {
	if factory == nil {
		panic("externalsync: nil factory for provider " + provider)
	}
	if !providerShapeRE.MatchString(provider) {
		panic(fmt.Sprintf("externalsync: provider %q must match %s", provider, providerShapeRE.String()))
	}
	if strings.TrimSpace(display) == "" {
		panic("externalsync: provider " + provider + " registered with empty display")
	}
	if strings.ContainsAny(display, "\x00\n\r\t") {
		panic(fmt.Sprintf("externalsync: provider %q display contains control characters", provider))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := providers[provider]; exists {
		panic("externalsync: provider already registered: " + provider)
	}
	providers[provider] = registration{display: display, factory: factory}
}

// ResetForTest clears the registry in tests.
func ResetForTest() {
	if !testing.Testing() {
		panic("externalsync.ResetForTest must only be called from tests")
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
var ErrProviderUnavailable = errors.New("external sync provider unavailable")

// UnavailableError reports a missing adapter.
func UnavailableError(provider string) error {
	return fmt.Errorf("%w: %s", ErrProviderUnavailable, provider)
}

// NoopProvider is a provider that can be used in tests or dev wiring.
type NoopProvider struct {
	Name string
}

func (p NoopProvider) Provider() string {
	if p.Name == "" {
		return "noop"
	}
	return p.Name
}

func (p NoopProvider) Check(_ context.Context, _ Connection) (CheckResult, error) {
	return CheckResult{OK: true}, nil
}

func (p NoopProvider) Discover(_ context.Context, _ Connection) ([]ObjectSchema, error) {
	return []ObjectSchema{{
		Type:           "issue",
		Fields:         []string{"title", "status", "assignee"},
		RequiredFields: []string{"title"},
		WritableFields: []string{"title", "status", "assignee"},
	}}, nil
}

func (p NoopProvider) Pull(_ context.Context, _ PullRequest) (PullResult, error) {
	return PullResult{NextCursor: []byte("{}")}, nil
}

func (p NoopProvider) Push(_ context.Context, req PushRequest) (PushResult, error) {
	results := make([]WriteResult, 0, len(req.Records))
	for _, record := range req.Records {
		results = append(results, WriteResult{
			LocalID: record.ID,
			Key:     "noop:" + record.ID,
			Version: record.Version,
		})
	}
	return PushResult{Results: results}, nil
}

func (p NoopProvider) ClassifyError(err error) SyncError {
	if err == nil {
		return SyncError{}
	}
	return SyncError{Kind: "other", Message: err.Error(), Retryable: false}
}

func init() {
	Register("noop", "No-op", func() Provider { return ptrext.Of(NoopProvider{}) })
}
