// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

func TestRegisterLookupAndProvidersAreDeterministic(t *testing.T) {
	ResetForTest()
	t.Cleanup(restoreNoopProviderForTest)

	Register("zendesk", "Zendesk", func() Provider { return NoopProvider{Name: "zendesk"} })
	Register("github", "GitHub", func() Provider { return NoopProvider{Name: "github"} })

	entries := Providers()
	if len(entries) != 2 {
		t.Fatalf("Providers len = %d; want 2", len(entries))
	}
	if entries[0].Provider != "github" || entries[1].Provider != "zendesk" {
		t.Fatalf("Providers order = %q, %q; want github, zendesk", entries[0].Provider, entries[1].Provider)
	}

	provider, ok := Lookup("github")
	if !ok {
		t.Fatal("Lookup(github) returned ok=false")
	}
	if provider.Provider() != "github" {
		t.Fatalf("provider name = %q; want github", provider.Provider())
	}
	if result, err := provider.Check(context.Background(), Connection{}); err != nil || !result.OK {
		t.Fatalf("Check = (%+v, %v); want OK without error", result, err)
	}
}

func TestRegisterRejectsInvalidEntries(t *testing.T) {
	tests := []struct {
		name string
		run  func()
		want string
	}{
		{
			name: "nil factory",
			run:  func() { Register("github", "GitHub", nil) },
			want: "nil factory",
		},
		{
			name: "invalid provider token",
			run:  func() { Register("GitHub", "GitHub", func() Provider { return NoopProvider{} }) },
			want: "must match",
		},
		{
			name: "empty display",
			run:  func() { Register("github", " ", func() Provider { return NoopProvider{} }) },
			want: "empty display",
		},
		{
			name: "display control character",
			run:  func() { Register("github", "GitHub\nIssues", func() Provider { return NoopProvider{} }) },
			want: "control characters",
		},
		{
			name: "duplicate provider",
			run: func() {
				Register("github", "GitHub", func() Provider { return NoopProvider{} })
				Register("github", "GitHub", func() Provider { return NoopProvider{} })
			},
			want: "already registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetForTest()
			t.Cleanup(restoreNoopProviderForTest)
			defer func() {
				got := recover()
				if got == nil {
					t.Fatalf("Register did not panic")
				}
				if !strings.Contains(got.(string), tt.want) {
					t.Fatalf("panic = %q; want substring %q", got, tt.want)
				}
			}()
			tt.run()
		})
	}
}

func TestValidateProviderTokenAndUnavailableError(t *testing.T) {
	if err := ValidateProviderToken("github_issue"); err != nil {
		t.Fatalf("ValidateProviderToken valid token returned error: %v", err)
	}
	if err := ValidateProviderToken("GitHub"); err == nil || !strings.Contains(err.Error(), "must match") {
		t.Fatalf("ValidateProviderToken invalid error = %v; want shape error", err)
	}
	if err := UnavailableError("missing"); !errors.Is(err, ErrProviderUnavailable) ||
		!strings.Contains(err.Error(), "missing") {
		t.Fatalf("UnavailableError = %v; want wrapped provider unavailable", err)
	}
	if _, ok := Lookup("missing"); ok {
		t.Fatal("Lookup(missing) returned ok=true")
	}
}

func TestNoopProviderMetadataAndDiscovery(t *testing.T) {
	provider := NoopProvider{Name: "custom"}
	if provider.Provider() != "custom" {
		t.Fatalf("Provider = %q; want custom", provider.Provider())
	}
	if (NoopProvider{}).Provider() != "noop" {
		t.Fatalf("zero Provider = %q; want noop", (NoopProvider{}).Provider())
	}
	schemas, err := provider.Discover(context.Background(), Connection{})
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Type != "issue" || len(schemas[0].WritableFields) == 0 {
		t.Fatalf("schemas = %#v; want issue schema with writable fields", schemas)
	}
}

func TestNoopProviderSyncAndClassification(t *testing.T) {
	provider := NoopProvider{Name: "custom"}
	pull, err := provider.Pull(context.Background(), PullRequest{Cursor: []byte(`{"page":1}`)})
	if err != nil {
		t.Fatalf("Pull returned error: %v", err)
	}
	if string(pull.NextCursor) != "{}" {
		t.Fatalf("pull next cursor = %s; want empty JSON object", string(pull.NextCursor))
	}
	push, err := provider.Push(context.Background(), PushRequest{Records: []LocalRecord{
		{ID: "cr-1", Version: "v1"},
		{ID: "cr-2", Version: "v2"},
	}})
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	if len(push.Results) != 2 ||
		push.Results[0].Key != "noop:cr-1" ||
		push.Results[1].Version != "v2" {
		t.Fatalf("push results = %#v; want echo results", push.Results)
	}
	if classified := provider.ClassifyError(errors.New("boom")); classified.Kind != "other" ||
		classified.Message != "boom" ||
		classified.Retryable {
		t.Fatalf("classified error = %#v; want non-retryable other", classified)
	}
	if classified := provider.ClassifyError(nil); classified != (SyncError{}) {
		t.Fatalf("nil classified error = %#v; want zero", classified)
	}
}

func TestEgressPolicyClientAndURLValidation(t *testing.T) {
	previous := egressPolicy
	t.Cleanup(func() { SetEgressPolicy(previous) })
	SetEgressPolicy(nethardening.Policy{})

	client := NewHTTPClient(0)
	if client.Timeout != 10*time.Second {
		t.Fatalf("default timeout = %s; want 10s", client.Timeout)
	}
	if client.Transport == nil {
		t.Fatal("client transport is nil; want hardened OTel transport")
	}
	custom := NewHTTPClient(250 * time.Millisecond)
	if custom.Timeout != 250*time.Millisecond {
		t.Fatalf("custom timeout = %s; want 250ms", custom.Timeout)
	}
	if err := ValidateProviderURL("https://api.github.com"); err != nil {
		t.Fatalf("public provider URL returned error: %v", err)
	}
	if err := ValidateProviderURL("http://127.0.0.1:8080"); err == nil {
		t.Fatal("loopback URL validation returned nil; want blocked")
	}

	SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	if err := ValidateProviderURL("http://127.0.0.1:8080"); err != nil {
		t.Fatalf("loopback URL with allow policy returned error: %v", err)
	}
}

func restoreNoopProviderForTest() {
	ResetForTest()
	Register("noop", "No-op", func() Provider { return NoopProvider{} })
}
